package commands

import (
	"strings"
)

// ATI represents the response of the ATI command.
type ATI struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Revision     string `json:"revision"`
}

func (bc *ATI) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:    "ati",
		Command: "ATI",
	}
}

func (a *ATI) ParseRespone(ctx *ParsingContext, status *ModemStatus, resp []string, _ string) {
	for _, line := range resp {
		if line == "" || strings.HasPrefix(line, "OK") || strings.HasPrefix(line, "ATI") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "revision:") {
			a.Revision = strings.TrimSpace(line[9:])
		} else if strings.Contains(strings.ToLower(line), "quectel") {
			a.Manufacturer = "Quectel"
		} else if strings.HasPrefix(line, "RM5") || strings.HasPrefix(line, "RM6") || strings.HasPrefix(line, "RG5") {
			a.Model = line
		} else if a.Model == "" && !strings.Contains(line, "Revision:") && len(line) > 3 {
			a.Model = line
		}
	}
}
