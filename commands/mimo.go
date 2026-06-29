package commands

import (
	"fmt"
	"strings"
)

type LteMimoLayers struct {
	MimoLayers string `json:"mimo_layers"`
}

func (m *LteMimoLayers) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "lte_mimo_layers",
		Command:        `AT+QNWCFG="lte_mimo_layers"`,
		ResponsePrefix: `+QNWCFG: "lte_mimo_layers",`,
	}
}

func (m *LteMimoLayers) ParseResponse(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	m.MimoLayers = "Unknown"
	if len(resp) > 0 {
		cleaned := strings.ReplaceAll(resp[0], " ", "")
		cleaned = strings.ReplaceAll(cleaned, "\t", "")
		var mode, layers int
		_, err := fmt.Sscanf(cleaned, "%d,%d", &mode, &layers)
		if err == nil {
			m.MimoLayers = fmt.Sprintf("%dx%d MIMO", layers, layers)
		}
	}
}

type Nr5gMimoLayers struct {
	MimoLayers string `json:"mimo_layers"`
}

func (m *Nr5gMimoLayers) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "nr5g_mimo_layers",
		Command:        `AT+QNWCFG="nr5g_mimo_layers"`,
		ResponsePrefix: `+QNWCFG: "nr5g_mimo_layers",`,
	}
}

func (m *Nr5gMimoLayers) ParseResponse(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	m.MimoLayers = "Unknown"
	if len(resp) > 0 {
		cleaned := strings.ReplaceAll(resp[0], " ", "")
		cleaned = strings.ReplaceAll(cleaned, "\t", "")
		var mode, layers int
		_, err := fmt.Sscanf(cleaned, "%d,%d", &mode, &layers)
		if err == nil {
			m.MimoLayers = fmt.Sprintf("%dx%d MIMO", layers, layers)
		}
	}
}
