package commands

import (
	"strings"
	"testing"
)

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseCGPADDR(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantIPv4 []string
		wantIPv6 []string
	}{
		{
			name: "Quectel decimal dot IPv6 and empty profiles",
			input: "+CGPADDR: 1,\"36.9.64.243.20.42.61.114.128.0.0.0.0.0.0.0\"\r\n" +
				"+CGPADDR: 2,\"0.0.0.0\",\"0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0\"\r\n" +
				"+CGPADDR: 3,\"0.0.0.0\",\"0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0\"\r\n",
			wantIPv4: nil,
			wantIPv6: []string{"2409:40f3:142a:3d72:8000::"},
		},
		{
			name: "Standard IPv4 and IPv6",
			input: "+CGPADDR: 1,\"10.20.30.40\"\r\n" +
				"+CGPADDR: 2,\"2409:40f3:142a:3d72:8000::\"\r\n",
			wantIPv4: []string{"10.20.30.40"},
			wantIPv6: []string{"2409:40f3:142a:3d72:8000::"},
		},
		{
			name: "Multiple IPv6 and IPv4 addresses (user case)",
			input: "+CGPADDR: 1,\"2409:40F3:001E:6843:8000:0000:0000:0000\"\r\n" +
				"+CGPADDR: 2,\"2409:4133:1453:4AF7:8000:0000:0000:0000\"\r\n" +
				"+CGPADDR: 3,\"0.0.0.0\",\"0000:0000:0000:0000:0000:0000:0000:0000\"\r\n",
			wantIPv4: nil,
			wantIPv6: []string{"2409:40f3:1e:6843:8000::", "2409:4133:1453:4af7:8000::"},
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
			cg := &CGPADDR{}
			cg.ParseRespone(nil, nil, resp, tt.input)
			if !slicesEqual(cg.IPAddress, tt.wantIPv4) {
				t.Errorf("CGPADDR.ParseRespone() IPAddress = %v, want %v", cg.IPAddress, tt.wantIPv4)
			}
			if !slicesEqual(cg.IPv6Address, tt.wantIPv6) {
				t.Errorf("CGPADDR.ParseRespone() IPv6Address = %v, want %v", cg.IPv6Address, tt.wantIPv6)
			}
		})
	}
}
