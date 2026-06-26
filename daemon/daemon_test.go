package daemon

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParseCSQ(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantCSQ  int
		wantPerc int
	}{
		{
			name:     "standard csq",
			input:    "\r\n+CSQ: 24,99\r\n\r\nOK\r\n",
			wantCSQ:  24,
		},
		{
			name:     "no space csq",
			input:    "\r\n+CSQ:14,99\r\n\r\nOK\r\n",
			wantCSQ:  14,
		},
		{
			name:     "error or empty csq",
			input:    "\r\nERROR\r\n",
			wantCSQ:  99,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := &ModemStatus{
				SignalCSQ: 99,
			}
			idx := strings.Index(tt.input, "+CSQ:")
			if idx != -1 {
				var rssi, ber int
				_, err := fmt.Sscanf(tt.input[idx:], "+CSQ: %d,%d", &rssi, &ber)
				if err != nil {
					t.Logf("Sscanf error: %v", err)
				}
			}
			parseCSQ(tt.input, status)
			if status.SignalCSQ != tt.wantCSQ {
				t.Errorf("parseCSQ() CSQ = %v, want %v", status.SignalCSQ, tt.wantCSQ)
			}
		})
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
			parseServingCell(tt.input, status)
			if status.Tech != tt.wantTech {
				t.Errorf("parseServingCell() Tech = %v, want %v", status.Tech, tt.wantTech)
			}
			if tt.name == "NR5G-NSA secondary cell" {
				if status.ServingCell.NR5GBand != "78" {
					t.Errorf("parseServingCell() NR5GBand = %v, want 78", status.ServingCell.NR5GBand)
				}
				if status.ServingCell.NR5GRSRP != "-90" {
					t.Errorf("parseServingCell() NR5GRSRP = %v, want -90", status.ServingCell.NR5GRSRP)
				}
				if status.ServingCell.NR5GRSRQ != "-11" {
					t.Errorf("parseServingCell() NR5GRSRQ = %v, want -11", status.ServingCell.NR5GRSRQ)
				}
				if status.ServingCell.NR5GSINR != "12" {
					t.Errorf("parseServingCell() NR5GSINR = %v, want 12", status.ServingCell.NR5GSINR)
				}
				return
			}
			if status.ServingCell.MCC != tt.wantMCC {
				t.Errorf("parseServingCell() MCC = %v, want %v", status.ServingCell.MCC, tt.wantMCC)
			}
			if status.ServingCell.MNC != tt.wantMNC {
				t.Errorf("parseServingCell() MNC = %v, want %v", status.ServingCell.MNC, tt.wantMNC)
			}
			if status.ServingCell.PCI != tt.wantPCI {
				t.Errorf("parseServingCell() PCI = %v, want %v", status.ServingCell.PCI, tt.wantPCI)
			}
			if status.ServingCell.RSRP != tt.wantRSRP {
				t.Errorf("parseServingCell() RSRP = %v, want %v", status.ServingCell.RSRP, tt.wantRSRP)
			}
			if status.ServingCell.RSRQ != tt.wantRSRQ {
				t.Errorf("parseServingCell() RSRQ = %v, want %v", status.ServingCell.RSRQ, tt.wantRSRQ)
			}
			if status.ServingCell.SINR != tt.wantSINR {
				t.Errorf("parseServingCell() SINR = %v, want %v", status.ServingCell.SINR, tt.wantSINR)
			}
		})
	}
}

func TestSignalPercentage(t *testing.T) {
	tests := []struct {
		name     string
		status   ModemStatus
		wantPerc int
	}{
		{
			name: "Valid CSQ (15)",
			status: ModemStatus{
				SignalCSQ: 15,
			},
			wantPerc: 48,
		},
		{
			name: "Invalid CSQ, Fallback to RSRP (-75)",
			status: ModemStatus{
				SignalCSQ: 99,
				ServingCell: ServingCellInfo{
					RSRP: "-75",
				},
			},
			wantPerc: 100,
		},
		{
			name: "Invalid CSQ, Fallback to RSRP (-100)",
			status: ModemStatus{
				SignalCSQ: 99,
				ServingCell: ServingCellInfo{
					RSRP: "-100",
				},
			},
			wantPerc: 50,
		},
		{
			name: "Invalid CSQ, Fallback to NR5GRSRP (-90)",
			status: ModemStatus{
				SignalCSQ: 99,
				ServingCell: ServingCellInfo{
					NR5GRSRP: "-90",
				},
			},
			wantPerc: 75,
		},
		{
			name: "Invalid CSQ, Fallback to RSRP (-130)",
			status: ModemStatus{
				SignalCSQ: 99,
				ServingCell: ServingCellInfo{
					RSRP: "-130",
				},
			},
			wantPerc: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateSignalPercentage(&tt.status)
			if got != tt.wantPerc {
				t.Errorf("calculateSignalPercentage() = %v, want %v", got, tt.wantPerc)
			}
		})
	}
}

func TestParseCGPADDR(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantIPv4    string
		wantIPv6    string
	}{
		{
			name: "Quectel decimal dot IPv6 and empty profiles",
			input: "+CGPADDR: 1,\"36.9.64.243.20.42.61.114.128.0.0.0.0.0.0.0\"\r\n" +
				"+CGPADDR: 2,\"0.0.0.0\",\"0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0\"\r\n" +
				"+CGPADDR: 3,\"0.0.0.0\",\"0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0\"\r\n",
			wantIPv4: "",
			wantIPv6: "2409:40f3:142a:3d72:8000::",
		},
		{
			name: "Standard IPv4 and IPv6",
			input: "+CGPADDR: 1,\"10.20.30.40\"\r\n" +
				"+CGPADDR: 2,\"2409:40f3:142a:3d72:8000::\"\r\n",
			wantIPv4: "10.20.30.40",
			wantIPv6: "2409:40f3:142a:3d72:8000::",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := &ModemStatus{}
			parseCGPADDR(tt.input, status)
			if status.IPAddress != tt.wantIPv4 {
				t.Errorf("parseCGPADDR() IPAddress = %q, want %q", status.IPAddress, tt.wantIPv4)
			}
			if status.IPv6Address != tt.wantIPv6 {
				t.Errorf("parseCGPADDR() IPv6Address = %q, want %q", status.IPv6Address, tt.wantIPv6)
			}
		})
	}
}

func TestParseRegistration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantReg  string
	}{
		{
			name:    "Registered Home Network",
			input:   "+C5GREG: 0,1\r\n\r\nOK",
			wantReg: "Registered",
		},
		{
			name:    "Registered Roaming",
			input:   "+C5GREG: 0,5\r\n\r\nOK",
			wantReg: "Registered",
		},
		{
			name:    "Not Registered Searching",
			input:   "+C5GREG: 0,2\r\n\r\nOK",
			wantReg: "Not Registered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := &ModemStatus{}
			parseRegistration(tt.input, status)
			if status.NetworkRegistration != tt.wantReg {
				t.Errorf("parseRegistration() = %q, want %q", status.NetworkRegistration, tt.wantReg)
			}
		})
	}
}

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

	messages := parseSMSList(resp)
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

func TestParseAdvancedConnectionInfo(t *testing.T) {
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

	t.Run("parseAdvancedConnectionInfo", func(t *testing.T) {
		status := &ModemStatus{
			RawResponses: map[string]string{
				"nr5g_ulMCS":  "+QNWCFG: \"nr5g_ulMCS\",1,28,8\nOK",
				"nr5g_dlMCS":  "+QNWCFG: \"nr5g_dlMCS\",1,24,6\nOK",
				"nr5g_csi":    "+QNWCFG: \"nr5g_csi\",24,4,15,0\nOK",
				"lte_csi":     "+QNWCFG: \"lte_csi\",20,2,12,1\nOK",
				"nr5g_tx_pwr": "+QNWCFG: \"nr5g_tx_pwr\",-10,-8,0,5\nOK",
				"lte_tx_pwr":  "+QNWCFG: \"lte_tx_pwr\",-5\nOK",
			},
		}

		parseAdvancedConnectionInfo(status)

		if status.Nr5gUlMcs != "Enabled (MCS: 28, Mod: 256QAM)" {
			t.Errorf("expected Nr5gUlMcs to be Enabled (MCS: 28, Mod: 256QAM), got %q", status.Nr5gUlMcs)
		}
		if status.Nr5gDlMcs != "Enabled (MCS: 24, Mod: 64QAM)" {
			t.Errorf("expected Nr5gDlMcs to be Enabled (MCS: 24, Mod: 64QAM), got %q", status.Nr5gDlMcs)
		}
		if status.Nr5gCsi != "MCS: 24, RI: 4, CQI: 15, PMI: 0" {
			t.Errorf("expected Nr5gCsi to be MCS: 24, RI: 4, CQI: 15, PMI: 0, got %q", status.Nr5gCsi)
		}
		if status.LteCsi != "MCS: 20, RI: 2, CQI: 12, PMI: 1" {
			t.Errorf("expected LteCsi to be MCS: 20, RI: 2, CQI: 12, PMI: 1, got %q", status.LteCsi)
		}
		if status.Nr5gTxPwr != "PUCCH: -10 dBm, PUSCH: -8 dBm, PRACH: 0 dBm, SRS: 5 dBm" {
			t.Errorf("expected Nr5gTxPwr to be PUCCH: -10 dBm, PUSCH: -8 dBm, PRACH: 0 dBm, SRS: 5 dBm, got %q", status.Nr5gTxPwr)
		}
		if status.LteTxPwr != "-5 dBm" {
			t.Errorf("expected LteTxPwr to be -5 dBm, got %q", status.LteTxPwr)
		}
	})
}

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
			status := &ModemStatus{}
			parseCNUM(tt.input, status)
			if status.SimNumber != tt.wantNumber {
				t.Errorf("parseCNUM() SimNumber = %q, want %q", status.SimNumber, tt.wantNumber)
			}
		})
	}
}

func TestDaemonCallbacks(t *testing.T) {
	d := NewDaemon("127.0.0.1:9999", 1*time.Second)

	ch := make(chan ModemStatus, 2)
	d.OnStatusUpdate(func(status ModemStatus) {
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




