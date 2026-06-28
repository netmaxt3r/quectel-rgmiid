package commands

import (
	"fmt"
	"strings"
)

type SMSCapacity struct {
	Used  int `json:"used"`
	Total int `json:"total"`
}

func (c *SMSCapacity) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "sms_capacity",
		Command:        `AT+CPMS?`,
		ResponsePrefix: "+CPMS:",
	}
}

func (c *SMSCapacity) ParseRespone(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	// Expected response: +CPMS: "SM",<used>,<total>,"SM",<used>,<total>,"SM",<used>,<total>
	for _, line := range resp {
		parts := SplitCSV(line)
		// Find first storage spec (index 0 = storage name, 1 = used, 2 = total)
		if len(parts) >= 3 {
			var used, total int
			fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &used)
			fmt.Sscanf(strings.TrimSpace(parts[2]), "%d", &total)
			status.SMSCapacity = SMSCapacity{Used: used, Total: total}
			return
		}
	}
}
