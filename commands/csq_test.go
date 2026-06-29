package commands

import (
	"strings"
	"testing"
)

func TestParseCSQ(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantCSQ int
	}{
		{
			name:    "standard csq",
			input:   "\r\n+CSQ: 24,99\r\n\r\nOK\r\n",
			wantCSQ: 24,
		},
		{
			name:    "no space csq",
			input:   "\r\n+CSQ:14,99\r\n\r\nOK\r\n",
			wantCSQ: 14,
		},
		{
			name:    "error or empty csq",
			input:   "\r\nERROR\r\n",
			wantCSQ: 99,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := strings.Split(tt.input, "\n")
			var resp []string
			csq := &CSQ{SignalCSQ: 99}
			pfx := csq.Command(nil).ResponsePrefix
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					if pfx != "" && strings.HasPrefix(trimmed, pfx) {
						resp = append(resp, strings.Trim(trimmed[len(pfx):], "\r "))
					} else if pfx == "" {
						resp = append(resp, trimmed)
					}
				}
			}
			csq.ParseResponse(nil, nil, resp, tt.input)
			if csq.SignalCSQ != tt.wantCSQ {
				t.Errorf("CSQ.ParseResponse() CSQ = %v, want %v", csq.SignalCSQ, tt.wantCSQ)
			}
		})
	}
}
