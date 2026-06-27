package devicestatus

import (
	"strings"
	"testing"
)

func TestParseCNUM(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantNumber string
	}{
		{
			name:       "standard number",
			input:      "+CNUM: \"My Number\",\"+12345678901\",145\n\nOK\n",
			wantNumber: "+12345678901",
		},
		{
			name:       "empty name",
			input:      "+CNUM: \"\",\"1234567890\",129\r\n\r\nOK\r\n",
			wantNumber: "1234567890",
		},
		{
			name:       "missing profile/no number on SIM",
			input:      "+CNUM: ,\"\",255\r\n\r\nOK\r\n",
			wantNumber: "N/A",
		},
		{
			name:       "error response",
			input:      "ERROR",
			wantNumber: "N/A",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := strings.Split(tt.input, "\n")
			var resp []string
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					resp = append(resp, trimmed)
				}
			}
			cn := &CNUM{}
			cn.ParseRespone(nil, nil, resp, tt.input)
			if cn.SimNumber != tt.wantNumber {
				t.Errorf("CNUM.ParseRespone() SimNumber = %q, want %q", cn.SimNumber, tt.wantNumber)
			}
		})
	}
}
