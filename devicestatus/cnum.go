package devicestatus

import (
	"strings"
)

// CNUM represents the response of the AT+CNUM command.
type CNUM struct {
	SimNumber string `json:"sim_number"`
}

func (c *CNUM) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "cnum",
		Command:        "AT+CNUM",
		ResponsePrefix: "+CNUM:",
	}
}

func (c *CNUM) ParseRespone(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	c.SimNumber = "N/A"
	for _, line := range resp {
		parts := splitCSV(line)
		if len(parts) >= 2 {
			num := strings.Trim(strings.TrimSpace(parts[1]), "\"")
			if num != "" {
				c.SimNumber = num
				return
			}
		}
	}
}
