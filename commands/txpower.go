package commands

import "strings"

type LteTxPwr struct {
	TxPwr string `json:"tx_pwr"`
}

func (t *LteTxPwr) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "lte_tx_pwr",
		Command:        `AT+QNWCFG="lte_tx_pwr"`,
		ResponsePrefix: `+QNWCFG: "lte_tx_pwr",`,
	}
}

func (t *LteTxPwr) ParseResponse(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	t.TxPwr = "N/A"
	if len(resp) > 0 {
		args := strings.Split(resp[0], ",")
		for i := range args {
			args[i] = strings.TrimSpace(args[i])
		}
		if len(args) > 0 && args[0] != "" {
			t.TxPwr = formatTxPower(args)
		}
	}
}

type Nr5gTxPwr struct {
	TxPwr string `json:"tx_pwr"`
}

func (t *Nr5gTxPwr) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "nr5g_tx_pwr",
		Command:        `AT+QNWCFG="nr5g_tx_pwr"`,
		ResponsePrefix: `+QNWCFG: "nr5g_tx_pwr",`,
	}
}

func (t *Nr5gTxPwr) ParseResponse(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	t.TxPwr = "N/A"
	if len(resp) > 0 {
		args := strings.Split(resp[0], ",")
		for i := range args {
			args[i] = strings.TrimSpace(args[i])
		}
		if len(args) > 0 && args[0] != "" {
			t.TxPwr = formatTxPower(args)
		}
	}
}
