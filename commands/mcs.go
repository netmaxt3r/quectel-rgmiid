package commands

import "strings"

type Nr5gUlMcs struct {
	UlMcs string `json:"nr5g_ul_mcs"`
}

func (m *Nr5gUlMcs) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "nr5g_ulMCS",
		Command:        `AT+QNWCFG="nr5g_ulMCS"`,
		ResponsePrefix: `+QNWCFG: "nr5g_ulMCS",`,
	}
}

func (m *Nr5gUlMcs) ParseResponse(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	m.UlMcs = "N/A"
	if len(resp) > 0 {
		args := strings.Split(resp[0], ",")
		for i := range args {
			args[i] = strings.TrimSpace(args[i])
		}
		if len(args) > 0 && args[0] != "" {
			m.UlMcs = formatMcs(args)
		}
	}
}

type Nr5gDlMcs struct {
	DlMcs string `json:"nr5g_dl_mcs"`
}

func (m *Nr5gDlMcs) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "nr5g_dlMCS",
		Command:        `AT+QNWCFG="nr5g_dlMCS"`,
		ResponsePrefix: `+QNWCFG: "nr5g_dlMCS",`,
	}
}

func (m *Nr5gDlMcs) ParseResponse(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	m.DlMcs = "N/A"
	if len(resp) > 0 {
		args := strings.Split(resp[0], ",")
		for i := range args {
			args[i] = strings.TrimSpace(args[i])
		}
		if len(args) > 0 && args[0] != "" {
			m.DlMcs = formatMcs(args)
		}
	}
}
