package commands

import (
	"strings"
)

// CPIN represents the response of the AT+CPIN? command.
type CPIN struct {
	SimState string `json:"sim_state"`
}

func (c *CPIN) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "cpin",
		Command:        "AT+CPIN?",
		ResponsePrefix: "+CPIN:",
	}
}

func (c *CPIN) ParseResponse(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {

	c.SimState = "UNKNOWN"
	for _, line := range resp {
		if strings.Contains(line, "READY") {
			c.SimState = "READY"
		} else {
			c.SimState = line
		}
	}

}
