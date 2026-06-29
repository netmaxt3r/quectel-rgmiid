package commands

import (
	"strconv"
	"strings"
)

type TddPattern struct {
	Enable       bool   `json:"enable"`
	PatternIndex int    `json:"pattern_index"`
	Periodicity  string `json:"periodicity"`
	DlSlots      int    `json:"dl_slots"`
	DlSymbols    int    `json:"dl_symbols"`
	UlSlots      int    `json:"ul_slots"`
	UlSymbols    int    `json:"ul_symbols"`
}

type Nr5gTddInfo struct {
	Patterns []TddPattern `json:"tdd_patterns"`
}

func (t *Nr5gTddInfo) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "nr5g_tdd_info",
		Command:        `AT+QNWCFG="nr5g_tdd_info"`,
		ResponsePrefix: `+QNWCFG: "nr5g_tdd_info",`,
	}
}

func (t *Nr5gTddInfo) ParseResponse(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	t.Patterns = nil
	for _, line := range resp {
		parts := strings.Split(line, ",")
		if len(parts) < 7 {
			continue
		}
		enableVal, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		patIdx, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		periodVal, err3 := strconv.Atoi(strings.TrimSpace(parts[2]))
		dlSlots, err4 := strconv.Atoi(strings.TrimSpace(parts[3]))
		dlSyms, err5 := strconv.Atoi(strings.TrimSpace(parts[4]))
		ulSlots, err6 := strconv.Atoi(strings.TrimSpace(parts[5]))
		ulSyms, err7 := strconv.Atoi(strings.TrimSpace(parts[6]))

		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil || err7 != nil {
			continue
		}

		periodStr := "Unknown"
		switch periodVal {
		case 0:
			periodStr = "0.5 ms"
		case 1:
			periodStr = "0.625 ms"
		case 2:
			periodStr = "1 ms"
		case 3:
			periodStr = "1.25 ms"
		case 4:
			periodStr = "2 ms"
		case 5:
			periodStr = "2.5 ms"
		case 6:
			periodStr = "5 ms"
		case 7:
			periodStr = "10 ms"
		}

		t.Patterns = append(t.Patterns, TddPattern{
			Enable:       enableVal != 0,
			PatternIndex: patIdx,
			Periodicity:  periodStr,
			DlSlots:      dlSlots,
			DlSymbols:    dlSyms,
			UlSlots:      ulSlots,
			UlSymbols:    ulSyms,
		})
	}
}
