package commands

import (
	"fmt"
	"strings"
	"time"
)

type SMSMessage struct {
	Index   int    `json:"index"`
	Status  string `json:"status"`
	Sender  string `json:"sender"`
	Date    string `json:"date"`
	Content string `json:"content"`
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

func (s *SMSList) ParseRespone(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	s.SMS = nil
	var current *SMSMessage

	for _, line := range resp {
		trimmed := strings.TrimSpace(line)
		if trimmed == "OK" || trimmed == "ERROR" {
			break
		}
		if strings.HasPrefix(line, "+CMGL:") {
			if current != nil {
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
		s.SMS = append(s.SMS, *current)
	}
}

// SendSMS sends an SMS message using the provided ATIConnection.
func SendSMS(conn ATIConnection, number, text string) error {
	// First ensure text mode is set
	_, err := conn.ExecuteATCommand(nil, ATCommand{
		Name:    "sms_text_mode",
		Command: "AT+CMGF=1",
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
		} else if IsTerminalResponse(frame) {
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
		if IsTerminalResponse(frame) {
			if strings.Contains(output.String(), "ERROR") {
				return fmt.Errorf("modem returned error: %s", strings.TrimSpace(output.String()))
			}
			return nil
		}
	}
}
