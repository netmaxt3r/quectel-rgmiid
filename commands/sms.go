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
		Command:        `AT+CMGF=0;+CMGL=4`,
		ResponsePrefix: "",
	}
}

func mapPDUStatus(statusVal string) string {
	switch strings.TrimSpace(statusVal) {
	case "0":
		return "REC UNREAD"
	case "1":
		return "REC READ"
	case "2":
		return "STO UNSENT"
	case "3":
		return "STO SENT"
	default:
		return statusVal
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
			metaStr := strings.TrimPrefix(line, "+CMGL:")
			parts := SplitCSV(metaStr)
			if len(parts) >= 2 {
				var idx int
				_, _ = fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &idx)
				statusVal := mapPDUStatus(strings.TrimSpace(parts[1]))
				current = &SMSMessage{
					Index:  idx,
					Status: statusVal,
				}
			}
		} else {
			if current != nil {
				sender, date, content, err := parsePDU(trimmed)
				if err != nil {
					current.Content = trimmed
				} else {
					current.Sender = sender
					current.Date = date
					current.Content = content
				}
				s.SMS = append(s.SMS, *current)
				current = nil
			}
		}
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

// SendSMS sends an SMS message using the provided ATIConnection in PDU mode.
func SendSMS(conn ATIConnection, number, text string) error {
	// First ensure PDU mode is set
	_, err := conn.ExecuteATCommand(nil, ATCommand{
		Name:    "sms_pdu_mode",
		Command: "AT+CMGF=0",
		NoCache: true,
	})
	if err != nil {
		return fmt.Errorf("failed to set SMS PDU mode: %w", err)
	}

	pdus, lengths, err := encodeSMSPDUs(number, text)
	if err != nil {
		return fmt.Errorf("failed to encode SMS to PDU: %w", err)
	}

	for i, pdu := range pdus {
		length := lengths[i]

		// Start interactive session
		session, err := conn.StartInteractive()
		if err != nil {
			return fmt.Errorf("failed to start interactive session: %w", err)
		}

		// 1. Send AT+CMGS=<length>\r\n
		cmd1 := fmt.Sprintf("AT+CMGS=%d\r\n", length)
		if err := session.WriteCmd(cmd1); err != nil {
			session.Close()
			return fmt.Errorf("failed to write SMS command: %w", err)
		}

		// Wait for ">" prompt
		var output strings.Builder
		timeout := 10 * time.Second
		promptReceived := false
		for !promptReceived {
			frame, err := session.ReadFrame(timeout)
			if err != nil {
				session.Close()
				return fmt.Errorf("failed to read prompt: %w (output: %q)", err, output.String())
			}
			output.WriteString(frame)
			if strings.Contains(frame, ">") {
				promptReceived = true
			} else if isTerm, _ := IsTerminalResponse(frame); isTerm {
				session.Close()
				return fmt.Errorf("modem returned error before prompt: %s", strings.TrimSpace(frame))
			}
		}

		// 2. Send "<pdu>\x1a"
		cmd2 := fmt.Sprintf("%s\x1a", pdu)
		if err := session.WriteCmd(cmd2); err != nil {
			session.Close()
			return fmt.Errorf("failed to write SMS PDU: %w", err)
		}

		// Wait for final confirmation response (e.g. "+CMGS: ...\r\n\r\nOK")
		timeout = 30 * time.Second
		sentConfirmed := false
		for !sentConfirmed {
			frame, err := session.ReadFrame(timeout)
			if err != nil {
				session.Close()
				return fmt.Errorf("failed to read SMS sent confirmation: %w (output: %q)", err, output.String())
			}
			output.WriteString(frame)
			if isTerm, is_error := IsTerminalResponse(frame); isTerm {
				if is_error {
					session.Close()
					return fmt.Errorf("modem returned error: %s", strings.TrimSpace(output.String()))
				}
				sentConfirmed = true
			}
		}
		session.Close()
	}

	return nil
}

var gsmBasic = [128]rune{
	'@', '£', '$', '¥', 'è', 'é', 'ù', 'ì', 'ò', 'Ç', '\n', 'Ø', 'ø', '\r', 'Å', 'å',
	'Δ', '_', 'Φ', 'Γ', 'Λ', 'Ω', 'Π', 'Ψ', 'Σ', 'Θ', 'Ξ', '\x1b', 'Æ', 'æ', 'ß', 'É',
	' ', '!', '"', '#', '¤', '%', '&', '\'', '(', ')', '*', '+', ',', '-', '.', '/',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', ':', ';', '<', '=', '>', '?',
	'¡', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O',
	'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z', 'Ä', 'Ö', 'Ñ', 'Ü', '§',
	'¿', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o',
	'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', 'ä', 'ö', 'ñ', 'ü', 'à',
}

func encodeGSM7Bit(s string) ([]byte, error) {
	var gsmBasicMap = make(map[rune]byte, 128)
	for idx, r := range gsmBasic {
		if r != '\x1b' {
			gsmBasicMap[r] = byte(idx)
		}
	}
	var gsmExtMap = map[rune]byte{
		'\x0c': 0x0A,
		'^':    0x14,
		'{':    0x28,
		'}':    0x29,
		'\\':   0x2F,
		'[':    0x3C,
		'~':    0x3D,
		']':    0x3E,
		'|':    0x40,
		'€':    0x65,
	}

	var septets []byte
	for _, r := range s {
		if extVal, isExt := gsmExtMap[r]; isExt {
			septets = append(septets, 0x1B, extVal)
		} else if basicVal, isBasic := gsmBasicMap[r]; isBasic {
			septets = append(septets, basicVal)
		} else {
			return nil, fmt.Errorf("character %q not representable in GSM 7-bit", r)
		}
	}
	return septets, nil
}

func decodeGSM7Bit(septets []byte) string {
	var sb strings.Builder
	i := 0
	for i < len(septets) {
		s := septets[i]
		if s == 0x1B {
			if i+1 < len(septets) {
				i++
				next := septets[i]
				switch next {
				case 0x0A:
					sb.WriteRune('\x0c')
				case 0x14:
					sb.WriteRune('^')
				case 0x28:
					sb.WriteRune('{')
				case 0x29:
					sb.WriteRune('}')
				case 0x2F:
					sb.WriteRune('\\')
				case 0x3C:
					sb.WriteRune('[')
				case 0x3D:
					sb.WriteRune('~')
				case 0x3E:
					sb.WriteRune(']')
				case 0x40:
					sb.WriteRune('|')
				case 0x65:
					sb.WriteRune('€')
				default:
					sb.WriteRune(gsmBasic[next])
				}
			} else {
				sb.WriteRune(' ')
			}
		} else {
			if int(s) < len(gsmBasic) {
				sb.WriteRune(gsmBasic[s])
			} else {
				sb.WriteRune('?')
			}
		}
		i++
	}
	return sb.String()
}

func unpack7Bit(src []byte, septetCount int) []byte {
	if len(src) == 0 {
		return nil
	}
	dst := make([]byte, 0, septetCount)
	var shift uint
	var accum uint32
	for _, b := range src {
		accum |= uint32(b) << shift
		shift += 8
		for shift >= 7 {
			septet := byte(accum & 0x7F)
			dst = append(dst, septet)
			accum >>= 7
			shift -= 7
			if len(dst) == septetCount {
				return dst
			}
		}
	}
	if shift > 0 && len(dst) < septetCount {
		septet := byte(accum & 0x7F)
		dst = append(dst, septet)
	}
	return dst
}

func pack7Bit(septets []byte, paddingBits int) []byte {
	var dst []byte
	var accum uint32
	var shift uint
	if paddingBits > 0 {
		shift = uint(paddingBits)
	}
	for _, s := range septets {
		accum |= uint32(s&0x7F) << shift
		shift += 7
		for shift >= 8 {
			dst = append(dst, byte(accum&0xFF))
			accum >>= 8
			shift -= 8
		}
	}
	if shift > 0 {
		dst = append(dst, byte(accum&0xFF))
	}
	return dst
}

func swapNibbles(val byte) byte {
	return (val >> 4) | (val << 4)
}

func decodeSemiOctets(b []byte, digitCount int) string {
	var sb strings.Builder
	for i := 0; i < len(b); i++ {
		low := b[i] & 0x0F
		high := (b[i] >> 4) & 0x0F
		if sb.Len() < digitCount {
			sb.WriteRune(rune('0' + low))
		}
		if sb.Len() < digitCount && high != 0x0F {
			sb.WriteRune(rune('0' + high))
		}
	}
	return sb.String()
}

func encodeAddressBytes(number string) (byte, []byte) {
	toa := byte(0x81)
	digitsStr := number
	if strings.HasPrefix(number, "+") {
		toa = 0x91
		digitsStr = number[1:]
	}

	var cleaned []rune
	for _, r := range digitsStr {
		if r >= '0' && r <= '9' {
			cleaned = append(cleaned, r)
		}
	}

	var res []byte
	for i := 0; i < len(cleaned); i += 2 {
		low := byte(cleaned[i] - '0')
		high := byte(0x0F)
		if i+1 < len(cleaned) {
			high = byte(cleaned[i+1] - '0')
		}
		res = append(res, low|(high<<4))
	}
	return toa, res
}

func encodeAlphanumericAddressBytes(text string) (byte, []byte) {
	toa := byte(0xD1)
	septets, _ := encodeGSM7Bit(text)
	packed := pack7Bit(septets, 0)
	return toa, packed
}

func decodeAddress(b []byte, length int, toa byte) (string, int, error) {
	if length == 0 {
		return "", 0, nil
	}

	isAlphanumeric := (toa & 0x70) == 0x50

	var byteLen int
	if isAlphanumeric {
		if length <= 11 {
			byteLen = (length * 7 + 7) / 8
		} else {
			byteLen = (length + 1) / 2
		}
	} else {
		byteLen = (length + 1) / 2
	}

	if len(b) < byteLen {
		return "", 0, fmt.Errorf("insufficient address bytes: expected %d, got %d", byteLen, len(b))
	}

	addrBytes := b[:byteLen]
	var addr string
	if isAlphanumeric {
		var septetCount int
		if length <= 11 {
			septetCount = length
		} else {
			septetCount = (byteLen * 8) / 7
		}
		septets := unpack7Bit(addrBytes, septetCount)
		addr = decodeGSM7Bit(septets)
		addr = strings.TrimRight(addr, "@\x00")
	} else {
		addr = decodeSemiOctets(addrBytes, length)
		if toa == 0x91 {
			addr = "+" + addr
		}
	}

	return addr, byteLen, nil
}

func decodeSCTS(b []byte) string {
	if len(b) < 7 {
		return ""
	}

	swap := func(val byte) int {
		low := val & 0x0F
		high := (val >> 4) & 0x0F
		return int(low)*10 + int(high)
	}

	yr := swap(b[0])
	mon := swap(b[1])
	day := swap(b[2])
	hr := swap(b[3])
	min := swap(b[4])
	sec := swap(b[5])

	tzByte := b[6]
	isNegative := (tzByte & 0x08) != 0
	units := tzByte & 0x07
	tens := tzByte >> 4
	tzVal := int(tens)*10 + int(units)

	sign := "+"
	if isNegative {
		sign = "-"
	}

	return fmt.Sprintf("%02d/%02d/%02d,%02d:%02d:%02d%s%02d", yr, mon, day, hr, min, sec, sign, tzVal)
}

func decodeUserData(udBytes []byte, udl int, dcs byte, udhi bool) (string, error) {
	if udl == 0 {
		return "", nil
	}

	coding := (dcs >> 2) & 0x03
	if (dcs & 0xF0) == 0xF0 {
		if (dcs & 0x04) == 0 {
			coding = 0
		} else {
			coding = 1
		}
	}

	var udhBytes int
	if udhi {
		if len(udBytes) < 1 {
			return "", fmt.Errorf("UDHI set but UD empty")
		}
		udhl := int(udBytes[0])
		udhBytes = udhl + 1
		if len(udBytes) < udhBytes {
			return "", fmt.Errorf("UDHI set but UD too short for UDH")
		}
	}

	if coding == 0 {
		byteLen := (udl * 7 + 7) / 8
		if len(udBytes) < byteLen {
			byteLen = len(udBytes)
		}
		septets := unpack7Bit(udBytes[:byteLen], udl)

		var startSeptet int
		if udhi {
			startSeptet = (udhBytes * 8 + 6) / 7
		}

		if startSeptet >= len(septets) {
			return "", nil
		}
		return decodeGSM7Bit(septets[startSeptet:]), nil

	} else if coding == 2 {
		if len(udBytes) < udl {
			udl = len(udBytes)
		}
		data := udBytes[:udl]
		if udhi {
			if udhBytes >= len(data) {
				return "", nil
			}
			data = data[udhBytes:]
		}

		u16 := make([]uint16, len(data)/2)
		for i := 0; i < len(u16); i++ {
			u16[i] = uint16(data[2*i])<<8 | uint16(data[2*i+1])
		}
		runes := utf16.Decode(u16)
		return string(runes), nil

	} else {
		if len(udBytes) < udl {
			udl = len(udBytes)
		}
		data := udBytes[:udl]
		if udhi {
			if udhBytes >= len(data) {
				return "", nil
			}
			data = data[udhBytes:]
		}
		return string(data), nil
	}
}

func parsePDU(pduHex string) (sender string, date string, content string, err error) {
	pduBytes, err := hex.DecodeString(strings.TrimSpace(pduHex))
	if err != nil {
		return "", "", "", fmt.Errorf("failed to decode hex: %w", err)
	}
	if len(pduBytes) < 1 {
		return "", "", "", fmt.Errorf("PDU too short")
	}

	smscLen := int(pduBytes[0])
	if len(pduBytes) < 1+smscLen {
		return "", "", "", fmt.Errorf("PDU too short for SMSC")
	}
	idx := 1 + smscLen

	if len(pduBytes) < idx+1 {
		return "", "", "", fmt.Errorf("PDU too short after SMSC")
	}

	firstOctet := pduBytes[idx]
	idx++

	udhi := (firstOctet & 0x40) != 0

	mti := firstOctet & 0x03
	if mti != 0x00 {
		if mti == 0x01 {
			if len(pduBytes) < idx+2 {
				return "", "", "", fmt.Errorf("PDU too short for SMS-SUBMIT header")
			}
			idx++ // skip MR

			daLen := int(pduBytes[idx])
			idx++
			if daLen > 0 {
				if len(pduBytes) < idx+1 {
					return "", "", "", fmt.Errorf("PDU too short for DA TOA")
				}
				daToa := pduBytes[idx]
				idx++

				addr, readBytes, err := decodeAddress(pduBytes[idx:], daLen, daToa)
				if err != nil {
					return "", "", "", fmt.Errorf("failed to decode DA: %w", err)
				}
				sender = addr
				idx += readBytes
			}

			if len(pduBytes) < idx+2 {
				return "", "", "", fmt.Errorf("PDU too short for SMS-SUBMIT PID/DCS")
			}
			idx++ // skip PID
			dcs := pduBytes[idx]
			idx++

			vpf := (firstOctet >> 3) & 0x03
			var vpLen int
			switch vpf {
			case 0x00:
				vpLen = 0
			case 0x01:
				vpLen = 7
			case 0x02:
				vpLen = 1
			case 0x03:
				vpLen = 7
			}
			if len(pduBytes) < idx+vpLen {
				return "", "", "", fmt.Errorf("PDU too short for VP")
			}
			idx += vpLen

			if len(pduBytes) < idx+1 {
				return "", "", "", fmt.Errorf("PDU too short for UDL")
			}
			udl := int(pduBytes[idx])
			idx++

			content, err = decodeUserData(pduBytes[idx:], udl, dcs, udhi)
			if err != nil {
				return "", "", "", fmt.Errorf("failed to decode user data: %w", err)
			}

			date = ""
			return sender, date, content, nil
		}
		return "", "", "", fmt.Errorf("unsupported PDU MTI: 0x%02x", mti)
	}

	if len(pduBytes) < idx+1 {
		return "", "", "", fmt.Errorf("PDU too short for OA Length")
	}
	oaLen := int(pduBytes[idx])
	idx++

	if oaLen > 0 {
		if len(pduBytes) < idx+1 {
			return "", "", "", fmt.Errorf("PDU too short for OA TOA")
		}
		oaToa := pduBytes[idx]
		idx++

		addr, readBytes, err := decodeAddress(pduBytes[idx:], oaLen, oaToa)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to decode OA: %w", err)
		}
		sender = addr
		idx += readBytes
	}

	if len(pduBytes) < idx+2 {
		return "", "", "", fmt.Errorf("PDU too short for PID/DCS")
	}
	idx++ // skip PID
	dcs := pduBytes[idx]
	idx++

	if len(pduBytes) < idx+7 {
		return "", "", "", fmt.Errorf("PDU too short for SCTS")
	}
	date = decodeSCTS(pduBytes[idx : idx+7])
	idx += 7

	if len(pduBytes) < idx+1 {
		return "", "", "", fmt.Errorf("PDU too short for UDL")
	}
	udl := int(pduBytes[idx])
	idx++

	content, err = decodeUserData(pduBytes[idx:], udl, dcs, udhi)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to decode user data: %w", err)
	}

	return sender, date, content, nil
}

func encodeSMSPDUs(number string, text string) ([]string, []int, error) {
	daToa, daDigits := encodeAddressBytes(number)

	var cleanedNum []rune
	for _, r := range number {
		if r >= '0' && r <= '9' {
			cleanedNum = append(cleanedNum, r)
		}
	}
	daLen := len(cleanedNum)

	var useUCS2 bool
	septets, err := encodeGSM7Bit(text)
	if err != nil {
		useUCS2 = true
	}

	var pdus []string
	var lengths []int

	if !useUCS2 {
		if len(septets) <= 160 {
			ud := pack7Bit(septets, 0)

			pduLen := 1 + 1 + 1 + 1 + 1 + len(daDigits) + 1 + 1 + 1 + len(ud)
			pduBytes := make([]byte, 0, pduLen)
			pduBytes = append(pduBytes, 0x00)
			pduBytes = append(pduBytes, 0x01)
			pduBytes = append(pduBytes, 0x00)
			pduBytes = append(pduBytes, byte(daLen), daToa)
			pduBytes = append(pduBytes, daDigits...)
			pduBytes = append(pduBytes, 0x00)
			pduBytes = append(pduBytes, 0x00)
			pduBytes = append(pduBytes, byte(len(septets)))
			pduBytes = append(pduBytes, ud...)

			pdus = append(pdus, hex.EncodeToString(pduBytes))
			lengths = append(lengths, len(pduBytes)-1)
		} else {
			const maxPartLen = 153
			totalParts := (len(septets) + maxPartLen - 1) / maxPartLen
			refNum := byte(time.Now().Unix() & 0xFF)

			for partIdx := 0; partIdx < totalParts; partIdx++ {
				start := partIdx * maxPartLen
				end := (partIdx + 1) * maxPartLen
				if end > len(septets) {
					end = len(septets)
				}
				partSeptets := septets[start:end]

				udh := []byte{0x05, 0x00, 0x03, refNum, byte(totalParts), byte(partIdx + 1)}

				paddingBits := (7 - (len(udh)*8)%7) % 7
				udhSeptets := (len(udh)*8 + 6) / 7

				udBodyPacked := pack7Bit(partSeptets, paddingBits)
				udHeaderAndBody := make([]byte, 0, len(udh)+len(udBodyPacked))
				udHeaderAndBody = append(udHeaderAndBody, udh...)
				udHeaderAndBody = append(udHeaderAndBody, udBodyPacked...)

				pduLen := 1 + 1 + 1 + 1 + 1 + len(daDigits) + 1 + 1 + 1 + len(udHeaderAndBody)
				pduBytes := make([]byte, 0, pduLen)
				pduBytes = append(pduBytes, 0x00)
				pduBytes = append(pduBytes, 0x41)
				pduBytes = append(pduBytes, 0x00)
				pduBytes = append(pduBytes, byte(daLen), daToa)
				pduBytes = append(pduBytes, daDigits...)
				pduBytes = append(pduBytes, 0x00)
				pduBytes = append(pduBytes, 0x00)
				pduBytes = append(pduBytes, byte(udhSeptets+len(partSeptets)))
				pduBytes = append(pduBytes, udHeaderAndBody...)

				pdus = append(pdus, hex.EncodeToString(pduBytes))
				lengths = append(lengths, len(pduBytes)-1)
			}
		}
	} else {
		runes := []rune(text)
		u16 := utf16.Encode(runes)

		if len(u16) <= 70 {
			ud := make([]byte, len(u16)*2)
			for i, val := range u16 {
				ud[2*i] = byte(val >> 8)
				ud[2*i+1] = byte(val & 0xFF)
			}

			pduLen := 1 + 1 + 1 + 1 + 1 + len(daDigits) + 1 + 1 + 1 + len(ud)
			pduBytes := make([]byte, 0, pduLen)
			pduBytes = append(pduBytes, 0x00)
			pduBytes = append(pduBytes, 0x01)
			pduBytes = append(pduBytes, 0x00)
			pduBytes = append(pduBytes, byte(daLen), daToa)
			pduBytes = append(pduBytes, daDigits...)
			pduBytes = append(pduBytes, 0x00)
			pduBytes = append(pduBytes, 0x08)
			pduBytes = append(pduBytes, byte(len(ud)))
			pduBytes = append(pduBytes, ud...)

			pdus = append(pdus, hex.EncodeToString(pduBytes))
			lengths = append(lengths, len(pduBytes)-1)
		} else {
			const maxPartLen = 67
			totalParts := (len(u16) + maxPartLen - 1) / maxPartLen
			refNum := byte(time.Now().Unix() & 0xFF)

			for partIdx := 0; partIdx < totalParts; partIdx++ {
				start := partIdx * maxPartLen
				end := (partIdx + 1) * maxPartLen
				if end > len(u16) {
					end = len(u16)
				}
				partU16 := u16[start:end]

				udh := []byte{0x05, 0x00, 0x03, refNum, byte(totalParts), byte(partIdx + 1)}

				ud := make([]byte, len(partU16)*2)
				for i, val := range partU16 {
					ud[2*i] = byte(val >> 8)
					ud[2*i+1] = byte(val & 0xFF)
				}

				udHeaderAndBody := make([]byte, 0, len(udh)+len(ud))
				udHeaderAndBody = append(udHeaderAndBody, udh...)
				udHeaderAndBody = append(udHeaderAndBody, ud...)

				pduLen := 1 + 1 + 1 + 1 + 1 + len(daDigits) + 1 + 1 + 1 + len(udHeaderAndBody)
				pduBytes := make([]byte, 0, pduLen)
				pduBytes = append(pduBytes, 0x00)
				pduBytes = append(pduBytes, 0x41)
				pduBytes = append(pduBytes, 0x00)
				pduBytes = append(pduBytes, byte(daLen), daToa)
				pduBytes = append(pduBytes, daDigits...)
				pduBytes = append(pduBytes, 0x00)
				pduBytes = append(pduBytes, 0x08)
				pduBytes = append(pduBytes, byte(len(udHeaderAndBody)))
				pduBytes = append(pduBytes, udHeaderAndBody...)

				pdus = append(pdus, hex.EncodeToString(pduBytes))
				lengths = append(lengths, len(pduBytes)-1)
			}
		}
	}

	return pdus, lengths, nil
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
