package commands

import (
	"strings"
	"testing"
)

func TestParseRegistration(t *testing.T) {
	tests := []struct {
		name    string
		inputs  map[string]string
		wantReg string
	}{
		{
			name: "Registered Home Network",
			inputs: map[string]string{
				"C5GREG": "+C5GREG: 0,1\r\n\r\nOK",
			},
			wantReg: "Registered",
		},
		{
			name: "Registered Roaming",
			inputs: map[string]string{
				"C5GREG": "+C5GREG: 0,5\r\n\r\nOK",
			},
			wantReg: "Roaming",
		},
		{
			name: "Not Registered Searching",
			inputs: map[string]string{
				"C5GREG": "+C5GREG: 0,2\r\n\r\nOK",
			},
			wantReg: "Search",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &Registration{}
			ctx := &ParsingContext{
				Tech: NR5G_SA,
			}
			var resp []string
			var raw string
			for k, v := range tt.inputs {
				if strings.Contains(k, "5GREG") {
					lines := strings.Split(v, "\n")
					for _, l := range lines {
						l = strings.TrimSpace(l)
						if strings.HasPrefix(l, "+C5GREG:") {
							resp = append(resp, strings.TrimSpace(l[len("+C5GREG:"):]))
							raw = v
						}
					}
				}
			}
			reg.ParseResponse(ctx, nil, resp, raw)
			if reg.NetworkRegistration != tt.wantReg {
				t.Errorf("Registration.ParseResponse() = %q, want %q", reg.NetworkRegistration, tt.wantReg)
			}
		})
	}
}
