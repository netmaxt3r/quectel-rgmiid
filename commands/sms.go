package commands

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
)

type SMSMessage struct {
	Index         int    `json:"index"`
	Status        string `json:"status"`
	Sender        string `json:"sender"`
	Date          string `json:"date"`
	Content       string `json:"content"`
	MergedIndices []int  `json:"merged_indices,omitempty"`
}

type SMSList struct {
	SMS []SMSMessage `json:"sms"`
}

func (s *SMSList) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "sms",
		Command:        `AT+CMGF=1;+CMGL="ALL"`,
		ResponsePrefix: "",
	}
}

func (s *SMSList) ParseResponse(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	s.SMS = nil
	var current *SMSMessage

	for _, line := range resp {
		trimmed := strings.TrimSpace(line)
		if trimmed == "OK" || trimmed == "ERROR" {
			break
		}
		if strings.HasPrefix(line, "+CMGL:") {
			if current != nil {
				current.Sender = decodeSMSField(current.Sender)
				current.Content = decodeSMSField(current.Content)
				s.SMS = append(s.SMS, *current)
			}
			metaStr := strings.TrimPrefix(line, "+CMGL:")
			parts := SplitCSV(metaStr)
			if len(parts) >= 3 {
				var idx int
				_, _ = fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &idx)
				statusVal := strings.TrimSpace(parts[1])
				sender := strings.TrimSpace(parts[2])
				date := ""
				if len(parts) >= 5 {
					date = strings.TrimSpace(parts[4])
				}
				current = &SMSMessage{
					Index:  idx,
					Status: statusVal,
					Sender: sender,
					Date:   date,
				}
			}
		} else {
			if current != nil {
				if trimmed == "" && current.Content == "" {
					continue
				}
				if current.Content != "" {
					current.Content += "\n"
				}
				current.Content += line
			}
		}
	}
	if current != nil {
		current.Sender = decodeSMSField(current.Sender)
		current.Content = decodeSMSField(current.Content)
		s.SMS = append(s.SMS, *current)
	}

	// Group and concatenate multi-part SMS messages that have the same Sender, are close in time (within 5 seconds),
	// and where the preceding segment is at one of the standard maximum SMS capacities (indicating it was split).
	if len(s.SMS) > 0 {
		var merged []SMSMessage
		currentGroup := s.SMS[0]
		currentGroup.MergedIndices = []int{currentGroup.Index}

		for i := 1; i < len(s.SMS); i++ {
			msg := s.SMS[i]
			if msg.Sender == currentGroup.Sender && isCloseInTime(msg.Date, currentGroup.Date, 5*time.Second) && isMultipartSegment(s.SMS[i-1].Content) {
				currentGroup.Content += msg.Content
				if msg.Status == "REC UNREAD" {
					currentGroup.Status = "REC UNREAD"
				}
				currentGroup.MergedIndices = append(currentGroup.MergedIndices, msg.Index)
			} else {
				merged = append(merged, currentGroup)
				currentGroup = msg
				currentGroup.MergedIndices = []int{currentGroup.Index}
			}
		}
		merged = append(merged, currentGroup)
		s.SMS = merged
	}

	// Reverse the SMS slice to show them in descending order (newest/highest index first)
	for i, j := 0, len(s.SMS)-1; i < j; i, j = i+1, j-1 {
		s.SMS[i], s.SMS[j] = s.SMS[j], s.SMS[i]
	}
}

// DeleteSMS deletes an SMS message by its index (including all merged segments if it's a concatenated message).
func DeleteSMS(conn ATIConnection, index int, smsList []SMSMessage) error {
	var indicesToDelete []int
	for _, msg := range smsList {
		if msg.Index == index {
			if len(msg.MergedIndices) > 0 {
				indicesToDelete = msg.MergedIndices
			} else {
				indicesToDelete = []int{index}
			}
			break
		}
	}

	if len(indicesToDelete) == 0 {
		indicesToDelete = []int{index}
	}

	var errs []string
	for _, idx := range indicesToDelete {
		_, err := conn.ExecuteATCommand(nil, ATCommand{
			Name:    "delete_sms",
			Command: fmt.Sprintf("AT+CMGD=%d", idx),
			NoCache: true,
		})
		if err != nil {
			errs = append(errs, fmt.Sprintf("failed to delete SMS index %d: %v", idx, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("delete SMS errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// SendSMS sends an SMS message using the provided ATIConnection.
func SendSMS(conn ATIConnection, number, text string) error {
	// First ensure text mode is set
	_, err := conn.ExecuteATCommand(nil, ATCommand{
		Name:    "sms_text_mode",
		Command: "AT+CMGF=1",
		NoCache: true,
	})
	if err != nil {
		return fmt.Errorf("failed to set SMS text mode: %w", err)
	}

	// Start interactive session
	session, err := conn.StartInteractive()
	if err != nil {
		return fmt.Errorf("failed to start interactive session: %w", err)
	}
	defer session.Close()

	// 1. Send AT+CMGS="<number>"\r\n
	cmd1 := fmt.Sprintf("AT+CMGS=%q\r\n", number)
	if err := session.WriteCmd(cmd1); err != nil {
		return fmt.Errorf("failed to write SMS command: %w", err)
	}

	// Wait for ">" prompt
	var output strings.Builder
	timeout := 10 * time.Second
	promptReceived := false
	for !promptReceived {
		frame, err := session.ReadFrame(timeout)
		if err != nil {
			return fmt.Errorf("failed to read prompt: %w (output: %q)", err, output.String())
		}
		output.WriteString(frame)
		if strings.Contains(frame, ">") {
			promptReceived = true
		} else if isTerm, _ := IsTerminalResponse(frame); isTerm {
			return fmt.Errorf("modem returned error before prompt: %s", strings.TrimSpace(frame))
		}
	}

	// 2. Send "<text>\x1a"
	cmd2 := fmt.Sprintf("%s\x1a", text)
	if err := session.WriteCmd(cmd2); err != nil {
		return fmt.Errorf("failed to write SMS body: %w", err)
	}

	// Wait for final confirmation response (e.g. "+CMGS: ...\r\n\r\nOK")
	timeout = 30 * time.Second
	for {
		frame, err := session.ReadFrame(timeout)
		if err != nil {
			return fmt.Errorf("failed to read SMS sent confirmation: %w (output: %q)", err, output.String())
		}
		output.WriteString(frame)
		if isTerm, is_error := IsTerminalResponse(frame); isTerm {
			if is_error {
				return fmt.Errorf("modem returned error: %s", strings.TrimSpace(output.String()))
			}
			return nil
		}
	}
}

// decodeUCS2Hex attempts to decode a UCS-2 hex string (Big Endian) to a UTF-8 string.
// If it is not a valid UCS-2 hex string, it returns the original string unmodified.
func decodeUCS2Hex(s string) string {
	s = strings.TrimSpace(s)
	// Must be non-empty and length must be a multiple of 4
	if len(s) == 0 || len(s)%4 != 0 {
		return s
	}
	// Must contain only valid hex characters
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return s
		}
	}

	bytes, err := hex.DecodeString(s)
	if err != nil {
		return s
	}

	u16 := make([]uint16, len(bytes)/2)
	for i := 0; i < len(u16); i++ {
		u16[i] = uint16(bytes[2*i])<<8 | uint16(bytes[2*i+1])
	}

	runes := utf16.Decode(u16)
	if len(runes) == 0 {
		return s
	}

	// Validate printable runes and avoid ASCII collision false positives
	for i, r := range runes {
		if r == '\uFFFD' {
			return s // contains replacement character/invalid UTF-16
		}
		if !unicode.IsPrint(r) && r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return s // contains non-printable character
		}

		// If len is small, check for false positives on words like "cafe", "beef", "dead", "babe", etc.
		if len(s) <= 8 {
			// ASCII characters must have a high byte of 0x00
			if r < 128 && bytes[2*i] != 0 {
				return s
			}
			// Exclude Private Use Area, Hangul Syllables, and Presentation Forms for short words to avoid collisions
			if (r >= 0xAC00 && r <= 0xD7AF) || (r >= 0xE000 && r <= 0xF8FF) || (r >= 0xFB00 && r <= 0xFFFD) {
				return s
			}
		}
	}

	return string(runes)
}

// decodeSMSField decodes a string field that might be decimal ASCII encoded or UCS-2 hex encoded.
func decodeSMSField(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return s
	}
	// Try decimal ASCII decoding first if it contains only digits
	if isAllDigits(s) {
		if decoded := decodeDecimalASCII(s); decoded != s {
			return decoded
		}
		// If it is only digits, only allow UCS-2 decoding if it starts with "00"
		if strings.HasPrefix(s, "00") {
			return decodeUCS2Hex(s)
		}
		return s
	}
	// Try UCS-2 hex decoding
	return decodeUCS2Hex(s)
}

func isAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func decodeDecimalASCII(s string) string {
	if len(s) < 2 {
		return s
	}

	var sb strings.Builder
	idx := 0
	for idx < len(s) {
		if idx+1 >= len(s) {
			return s // remaining digits cannot form a valid ASCII code
		}
		
		var code int
		var consumed int
		
		firstDigit := s[idx]
		if firstDigit == '1' {
			if idx+2 >= len(s) {
				return s
			}
			val := int(s[idx]-'0')*100 + int(s[idx+1]-'0')*10 + int(s[idx+2]-'0')
			if val >= 100 && val <= 127 {
				code = val
				consumed = 3
			} else {
				return s
			}
		} else if firstDigit >= '3' && firstDigit <= '9' {
			val := int(s[idx]-'0')*10 + int(s[idx+1]-'0')
			if val >= 32 && val <= 99 {
				code = val
				consumed = 2
			} else {
				return s
			}
		} else {
			return s // invalid starting digit for printable ASCII
		}

		sb.WriteRune(rune(code))
		idx += consumed
	}

	return sb.String()
}

// parseSMSDate parses the SMS timestamp format (e.g. "26/07/03,12:55:35+22") ignoring timezone offset.
func parseSMSDate(dateStr string) (time.Time, error) {
	s := strings.TrimSpace(dateStr)
	// Remove timezone offset if present (e.g., "+22" or "-08" at the end of the string)
	if len(s) > 17 && (s[len(s)-3] == '+' || s[len(s)-3] == '-') {
		s = s[:len(s)-3]
	}
	return time.Parse("06/01/02,15:04:05", s)
}

// isCloseInTime returns true if d1 and d2 are within maxDiff duration of each other.
func isCloseInTime(d1, d2 string, maxDiff time.Duration) bool {
	t1, err1 := parseSMSDate(d1)
	t2, err2 := parseSMSDate(d2)
	if err1 != nil || err2 != nil {
		// Fallback to exact string match if parsing fails
		return d1 == d2
	}
	diff := t1.Sub(t2)
	if diff < 0 {
		diff = -diff
	}
	return diff <= maxDiff
}

// isMultipartSegment returns true if the content length matches standard segment limit capacities
// (67 or 70 characters for UCS-2, 153 or 160 characters for GSM 7-bit, 134 or 140 for 8-bit).
func isMultipartSegment(content string) bool {
	runes := []rune(content)
	length := len(runes)
	return length == 67 || length == 70 || length == 153 || length == 160 || length == 134 || length == 140
}
