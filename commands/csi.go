package commands

import "strings"

type LteCsi struct {
	Csi string `json:"csi"`
}

func (c *LteCsi) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "lte_csi",
		Command:        `AT+QNWCFG="lte_csi"`,
		ResponsePrefix: `+QNWCFG: "lte_csi",`,
	}
}

func (c *LteCsi) ParseResponse(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	c.Csi = "N/A"
	if len(resp) > 0 {
		args := strings.Split(resp[0], ",")
		for i := range args {
			args[i] = strings.TrimSpace(args[i])
		}
		if len(args) > 0 && args[0] != "" {
			c.Csi = formatCsi(args)
		}
	}
}

type Nr5gCsi struct {
	Csi string `json:"csi"`
}

func (c *Nr5gCsi) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "nr5g_csi",
		Command:        `AT+QNWCFG="nr5g_csi"`,
		ResponsePrefix: `+QNWCFG: "nr5g_csi",`,
	}
}

func (c *Nr5gCsi) ParseResponse(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	c.Csi = "N/A"
	if len(resp) > 0 {
		args := strings.Split(resp[0], ",")
		for i := range args {
			args[i] = strings.TrimSpace(args[i])
		}
		if len(args) > 0 && args[0] != "" {
			c.Csi = formatCsi(args)
		}
	}
}
