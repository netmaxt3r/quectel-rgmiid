package devicestatus

import (
	"strconv"
	"strings"
	"testing"
)

type fakeConnection struct{}

func (f *fakeConnection) ExecuteATCommand(ctx *ParsingContext, cmd ATCommand) (string, error) {
	if cmd.Name == "ati" {
		return "Quectel\nRM520N-GL\nOK", nil
	}
	if cmd.Name == "servingcell" {
		if val, ok := ctx.RawResponses["QENG"]; ok {
			return val, nil
		}
	}
	// Try uppercase and lowercase lookup in RawResponses
	if val, ok := ctx.RawResponses[strings.ToUpper(cmd.Name)]; ok {
		return val, nil
	}
	if val, ok := ctx.RawResponses[cmd.Name]; ok {
		return val, nil
	}
	return "", nil
}

func stringToServiceTech(tech string) ServiceTech {
	switch tech {
	case "LTE":
		return LTE
	case "NR5G-SA":
		return NR5G_SA
	case "5G NSA":
		return NSA_5G
	default:
		return Unknown
	}
}

func TestParseServingCell(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantTech string
		wantMCC  string
		wantMNC  string
		wantPCI  string
		wantRSRP string
		wantRSRQ string
		wantSINR string
	}{
		{
			name:     "LTE serving cell",
			input:    `+QENG: "servingcell","CONNECT","LTE","FDD",405,86,1A2B3C,231,1275,3,5,5,A1B2,-85,-12,-55,15,18`,
			wantTech: "LTE",
			wantMCC:  "405",
			wantMNC:  "86",
			wantPCI:  "231",
			wantRSRP: "-85",
			wantRSRQ: "-12",
			wantSINR: "15",
		},
		{
			name:     "NR5G-SA serving cell",
			input:    `+QENG: "servingcell","CONNECT","NR5G-SA","TDD",405,86,123456789,452,4E,627264,78,12,-90,-11,12`,
			wantTech: "NR5G-SA",
			wantMCC:  "405",
			wantMNC:  "86",
			wantPCI:  "452",
			wantRSRP: "-90",
			wantRSRQ: "-11",
			wantSINR: "12",
		},
		{
			name:     "NR5G-NSA secondary cell",
			input:    `+QENG: "NR5G-NSA",405,86,452,-90,12,-11,627264,78`,
			wantTech: "5G NSA",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := &ModemStatus{}
			ctx := &ParsingContext{
				RawResponses: map[string]string{
					"QENG": tt.input,
				},
				Tech:       stringToServiceTech(tt.wantTech),
				Connection: &fakeConnection{},
			}
			status.Parse(ctx)
			if status.Tech != tt.wantTech {
				t.Errorf("ModemStatus.Parse() Tech = %v, want %v", status.Tech, tt.wantTech)
			}
			if tt.name == "NR5G-NSA secondary cell" {
				if status.ServingCell.NR5GNSA == nil {
					t.Fatal("expected NR5GNSA to be non-nil")
				}
				if status.ServingCell.NR5GNSA.Band != 78 {
					t.Errorf("NR5GBand = %v, want 78", status.ServingCell.NR5GNSA.Band)
				}
				if status.ServingCell.NR5GNSA.RSRP != -90 {
					t.Errorf("NR5GRSRP = %v, want -90", status.ServingCell.NR5GNSA.RSRP)
				}
				if status.ServingCell.NR5GNSA.RSRQ != -11 {
					t.Errorf("NR5GRSRQ = %v, want -11", status.ServingCell.NR5GNSA.RSRQ)
				}
				if status.ServingCell.NR5GNSA.SINR != 12 {
					t.Errorf("NR5GSINR = %v, want 12", status.ServingCell.NR5GNSA.SINR)
				}
				return
			}
			if tt.wantTech == "LTE" {
				if status.ServingCell.LTE == nil {
					t.Fatal("expected LTE to be non-nil")
				}
				wantMCCVal, _ := strconv.Atoi(tt.wantMCC)
				wantMNCVal, _ := strconv.Atoi(tt.wantMNC)
				wantPCIVal, _ := strconv.Atoi(tt.wantPCI)
				wantRSRPVal, _ := strconv.Atoi(tt.wantRSRP)
				wantRSRQVal, _ := strconv.Atoi(tt.wantRSRQ)
				wantSINRVal, _ := strconv.Atoi(tt.wantSINR)

				if status.ServingCell.LTE.MCC != wantMCCVal {
					t.Errorf("LTE.MCC = %v, want %v", status.ServingCell.LTE.MCC, wantMCCVal)
				}
				if status.ServingCell.LTE.MNC != wantMNCVal {
					t.Errorf("LTE.MNC = %v, want %v", status.ServingCell.LTE.MNC, wantMNCVal)
				}
				if status.ServingCell.LTE.PCID != wantPCIVal {
					t.Errorf("LTE.PCI = %v, want %v", status.ServingCell.LTE.PCID, wantPCIVal)
				}
				if status.ServingCell.LTE.RSRP != wantRSRPVal {
					t.Errorf("LTE.RSRP = %v, want %v", status.ServingCell.LTE.RSRP, wantRSRPVal)
				}
				if status.ServingCell.LTE.RSRQ != wantRSRQVal {
					t.Errorf("LTE.RSRQ = %v, want %v", status.ServingCell.LTE.RSRQ, wantRSRQVal)
				}
				if status.ServingCell.LTE.SINR != wantSINRVal {
					t.Errorf("LTE.SINR = %v, want %v", status.ServingCell.LTE.SINR, wantSINRVal)
				}
			}
			if tt.wantTech == "NR5G-SA" {
				if status.ServingCell.NR5GSA == nil {
					t.Fatal("expected NR5GSA to be non-nil")
				}
				wantMCCVal, _ := strconv.Atoi(tt.wantMCC)
				wantMNCVal, _ := strconv.Atoi(tt.wantMNC)
				wantPCIVal, _ := strconv.Atoi(tt.wantPCI)
				wantRSRPVal, _ := strconv.Atoi(tt.wantRSRP)
				wantRSRQVal, _ := strconv.Atoi(tt.wantRSRQ)
				wantSINRVal, _ := strconv.Atoi(tt.wantSINR)

				if status.ServingCell.NR5GSA.MCC != wantMCCVal {
					t.Errorf("NR5GSA.MCC = %v, want %v", status.ServingCell.NR5GSA.MCC, wantMCCVal)
				}
				if status.ServingCell.NR5GSA.MNC != wantMNCVal {
					t.Errorf("NR5GSA.MNC = %v, want %v", status.ServingCell.NR5GSA.MNC, wantMNCVal)
				}
				if status.ServingCell.NR5GSA.PCID != wantPCIVal {
					t.Errorf("NR5GSA.PCI = %v, want %v", status.ServingCell.NR5GSA.PCID, wantPCIVal)
				}
				if status.ServingCell.NR5GSA.RSRP != wantRSRPVal {
					t.Errorf("NR5GSA.RSRP = %v, want %v", status.ServingCell.NR5GSA.RSRP, wantRSRPVal)
				}
				if status.ServingCell.NR5GSA.RSRQ != wantRSRQVal {
					t.Errorf("NR5GSA.RSRQ = %v, want %v", status.ServingCell.NR5GSA.RSRQ, wantRSRQVal)
				}
				if status.ServingCell.NR5GSA.SINR != wantSINRVal {
					t.Errorf("NR5GSA.SINR = %v, want %v", status.ServingCell.NR5GSA.SINR, wantSINRVal)
				}
			}
		})
	}
}

func TestSignalPercentage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantPerc int
	}{
		{
			name:     "Valid CSQ (15)",
			input:    "15,99",
			wantPerc: 48,
		},
		{
			name:     "Valid CSQ (0)",
			input:    "0,99",
			wantPerc: 0,
		},
		{
			name:     "Valid CSQ (31)",
			input:    "31,99",
			wantPerc: 100,
		},
		{
			name:     "Invalid CSQ (99)",
			input:    "99,99",
			wantPerc: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			csq := &CSQ{}
			csq.ParseRespone(nil, nil, []string{tt.input}, tt.input)
			if csq.SignalPercentage != tt.wantPerc {
				t.Errorf("SignalPercentage = %v, want %v", csq.SignalPercentage, tt.wantPerc)
			}
		})
	}
}

func TestParseSMSList(t *testing.T) {
	parts := splitCSV(`1,"REC UNREAD","+1234567890",,"26/06/25,23:59:59+22"`)
	if len(parts) < 5 {
		t.Fatalf("expected 5 parts, got %d", len(parts))
	}
	if parts[1] != "REC UNREAD" || parts[2] != "+1234567890" {
		t.Errorf("unexpected parse: %v", parts)
	}
}

func TestParseAdvancedConnectionInfoHelpers(t *testing.T) {
	t.Run("parseQNWCFGString", func(t *testing.T) {
		tests := []struct {
			input     string
			paramName string
			expected  []string
		}{
			{
				input:     "+QNWCFG: \"nr5g_ulMCS\",1,28,8\r\n\r\nOK\r\n",
				paramName: "nr5g_ulMCS",
				expected:  []string{"1", "28", "8"},
			},
			{
				input:     "+QNWCFG:nr5g_ulMCS,0\nOK",
				paramName: "nr5g_ulMCS",
				expected:  []string{"0"},
			},
			{
				input:     "+QNWCFG: \"nr5g_csi\",24,2,15,3\n\nOK",
				paramName: "nr5g_csi",
				expected:  []string{"24", "2", "15", "3"},
			},
			{
				input:     "ERROR",
				paramName: "nr5g_ulMCS",
				expected:  nil,
			},
		}

		for _, tt := range tests {
			got := parseQNWCFGString(tt.input, tt.paramName)
			if len(got) != len(tt.expected) {
				t.Errorf("parseQNWCFGString(%q, %q) = %v, want %v", tt.input, tt.paramName, got, tt.expected)
				continue
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("parseQNWCFGString(%q, %q) = %v, want %v", tt.input, tt.paramName, got, tt.expected)
					break
				}
			}
		}
	})

	t.Run("formatMcs", func(t *testing.T) {
		tests := []struct {
			input    []string
			expected string
		}{
			{nil, "N/A"},
			{[]string{"0"}, "Disabled"},
			{[]string{"1"}, "Enabled"},
			{[]string{"1", "28"}, "Enabled (MCS: 28)"},
			{[]string{"1", "28", "8"}, "Enabled (MCS: 28, Mod: 256QAM)"},
			{[]string{"1", "15", "4"}, "Enabled (MCS: 15, Mod: 16QAM)"},
			{[]string{"1", "20", "unknown"}, "Enabled (MCS: 20, Mod: unknown)"},
		}
		for _, tt := range tests {
			got := formatMcs(tt.input)
			if got != tt.expected {
				t.Errorf("formatMcs(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		}
	})

	t.Run("formatCsi", func(t *testing.T) {
		tests := []struct {
			input    []string
			expected string
		}{
			{nil, "N/A"},
			{[]string{"25", "4", "12", "1"}, "MCS: 25, RI: 4, CQI: 12, PMI: 1"},
			{[]string{"25", "4"}, "MCS: 25, RI: 4"},
			{[]string{"1", "2", "3", "4", "5"}, "MCS: 1, RI: 2, CQI: 3, PMI: 4, Val5: 5"},
		}
		for _, tt := range tests {
			got := formatCsi(tt.input)
			if got != tt.expected {
				t.Errorf("formatCsi(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		}
	})

	t.Run("formatTxPower", func(t *testing.T) {
		tests := []struct {
			input    []string
			expected string
		}{
			{nil, "N/A"},
			{[]string{"-15"}, "-15 dBm"},
			{[]string{"-15", "-12"}, "PUCCH: -15 dBm, PUSCH: -12 dBm"},
			{[]string{"-15", "-12", "3", "0"}, "PUCCH: -15 dBm, PUSCH: -12 dBm, PRACH: 3 dBm, SRS: 0 dBm"},
			{[]string{"-15", "-12", "3", "0", "-5"}, "PUCCH: -15 dBm, PUSCH: -12 dBm, PRACH: 3 dBm, SRS: 0 dBm, Val5: -5 dBm"},
		}
		for _, tt := range tests {
			got := formatTxPower(tt.input)
			if got != tt.expected {
				t.Errorf("formatTxPower(%v) = %q, want %q", tt.input, got, tt.expected)
			}
		}
	})
}
