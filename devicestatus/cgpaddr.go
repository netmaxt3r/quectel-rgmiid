package devicestatus

import (
	"fmt"
	"net"
	"strings"
)

// CGPADDR represents the response of the AT+CGPADDR command.
type CGPADDR struct {
	IPAddress   string `json:"ip_address"`
	IPv6Address string `json:"ipv6_address"`
}

func (c *CGPADDR) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "cgpaddr",
		Command:        "AT+CGPADDR",
		ResponsePrefix: "+CGPADDR:",
	}
}

func (c *CGPADDR) ParseRespone(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	for _, line := range resp {
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		c.parseIPAddress(parts[1])
		if len(parts) >= 3 {
			c.parseIPAddress(parts[2])
		}
	}
}

func (c *CGPADDR) parseIPAddress(ipStr string) {
	ipStr = strings.Trim(strings.TrimSpace(ipStr), "\"")
	if ipStr == "" {
		return
	}

	parts := strings.Split(ipStr, ".")
	if len(parts) == 4 {
		if ipStr != "0.0.0.0" {
			c.IPAddress = appendIP(c.IPAddress, ipStr)
		}
	} else if len(parts) == 16 {
		var octets [16]byte
		valid := true
		for i := 0; i < 16; i++ {
			var val int
			_, err := fmt.Sscanf(parts[i], "%d", &val)
			if err != nil || val < 0 || val > 255 {
				valid = false
				break
			}
			octets[i] = byte(val)
		}
		if valid {
			ipv6 := fmt.Sprintf("%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x",
				octets[0], octets[1], octets[2], octets[3],
				octets[4], octets[5], octets[6], octets[7],
				octets[8], octets[9], octets[10], octets[11],
				octets[12], octets[13], octets[14], octets[15])
			netIP := net.ParseIP(ipv6)
			if netIP != nil && !netIP.IsUnspecified() {
				c.IPv6Address = appendIP(c.IPv6Address, netIP.String())
			}
		}
	} else if strings.Contains(ipStr, ":") {
		netIP := net.ParseIP(ipStr)
		if netIP != nil && !netIP.IsUnspecified() {
			c.IPv6Address = appendIP(c.IPv6Address, netIP.String())
		}
	}
}
