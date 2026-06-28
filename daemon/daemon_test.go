package daemon

import (
	"strings"
	"testing"
	"time"

	"rgmii/commands"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		expected string
	}{
		{
			name:     "zero duration",
			input:    0,
			expected: "0s",
		},
		{
			name:     "negative duration",
			input:    -5 * time.Second,
			expected: "0s",
		},
		{
			name:     "seconds only",
			input:    45 * time.Second,
			expected: "45s",
		},
		{
			name:     "minutes and seconds",
			input:    12*time.Minute + 34*time.Second,
			expected: "12m 34s",
		},
		{
			name:     "hours, minutes and seconds",
			input:    2*time.Hour + 15*time.Minute + 3*time.Second,
			expected: "2h 15m 3s",
		},
		{
			name:     "hours and seconds only",
			input:    1*time.Hour + 5*time.Second,
			expected: "1h 0m 5s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDuration(tt.input)
			if got != tt.expected {
				t.Errorf("FormatDuration() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestParseSMSList(t *testing.T) {
	resp := `+CMGL: 1,"REC UNREAD","+1234567890",,"26/06/25,23:59:59+22"` + "\r\n" +
		`Hello World!` + "\r\n" +
		`+CMGL: 2,"REC READ","Google",,"26/06/26,00:05:00+22"` + "\r\n" +
		`Your verification code is 123456.` + "\r\n" +
		`It is valid for 5 minutes.` + "\r\n" +
		`OK` + "\r\n"

	lines := strings.Split(resp, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}

	smsList := &commands.SMSList{}
	smsList.ParseRespone(nil, nil, lines, resp)
	messages := smsList.SMS

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	if messages[0].Index != 1 || messages[0].Status != "REC UNREAD" || messages[0].Sender != "+1234567890" || messages[0].Date != "26/06/25,23:59:59+22" || messages[0].Content != "Hello World!" {
		t.Errorf("unexpected message 1: %+v", messages[0])
	}

	expectedContent2 := "Your verification code is 123456.\nIt is valid for 5 minutes."
	if messages[1].Index != 2 || messages[1].Status != "REC READ" || messages[1].Sender != "Google" || messages[1].Date != "26/06/26,00:05:00+22" || messages[1].Content != expectedContent2 {
		t.Errorf("unexpected message 2: %+v", messages[1])
	}
}

func TestDaemonCallbacks(t *testing.T) {
	d := NewDaemon("127.0.0.1:9999", 1*time.Second)

	ch := make(chan commands.ModemStatus, 2)
	d.OnStatusUpdate(func(status commands.ModemStatus) {
		ch <- status
	})

	// Manually trigger callback notification
	d.notifyCallbacks()

	select {
	case status := <-ch:
		if status.ConnectionStatus != "Offline" {
			t.Errorf("expected ConnectionStatus to be Offline, got %s", status.ConnectionStatus)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for status update callback")
	}
}

func TestSendSMS_Offline(t *testing.T) {
	d := NewDaemon("127.0.0.1:0", 1*time.Second)
	err := d.SendSMS("+1234567890", "Hello World")
	if err == nil {
		t.Fatal("expected error when sending SMS on offline daemon, got nil")
	}
}

func TestSetATIDebug(t *testing.T) {
	d := NewDaemon("127.0.0.1:0", 1*time.Second)
	
	// Default should be false
	if d.client.Debug {
		t.Errorf("expected default debug to be false, got true")
	}

	d.SetATIDebug(true)
	if !d.client.Debug {
		t.Errorf("expected debug to be true after SetATIDebug(true)")
	}

	d.SetATIDebug(false)
	if d.client.Debug {
		t.Errorf("expected debug to be false after SetATIDebug(false)")
	}
}
