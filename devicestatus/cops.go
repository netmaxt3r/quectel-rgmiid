package devicestatus

import (
	"strings"
)

// COPS represents the response of the AT+COPS? command.
type COPS struct {
	Carrier string `json:"carrier"`
}

func (c *COPS) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "cops",
		Command:        "AT+COPS?",
		ResponsePrefix: "+COPS:",
	}
}

func (c *COPS) ParseRespone(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	c.Carrier = "Unknown"
	for _, line := range resp {
		parts := strings.Split(line, ",")
		if len(parts) >= 3 {
			carrier := parts[2]
			carrier = strings.Trim(carrier, "\"")
			c.Carrier = carrier
		}
	}

}
