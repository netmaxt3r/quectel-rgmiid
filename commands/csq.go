package commands

import (
	"fmt"
	"strings"
)

// Signal quality structure
type CSQ struct {
	SignalCSQ        int `json:"signal_csq"`        // 0-31, 99
	SignalPercentage int `json:"signal_percentage"` // 0-100
}

func (c *CSQ) Command(ctx *ParsingContext) ATCommand {
	if ctx != nil {
		switch ctx.Tech {
		case NR5G_SA:
			return ATCommand{
				Name:           "qcsq",
				Command:        "AT+QCSQ",
				ResponsePrefix: "+QCSQ:",
			}
		}
	}
	return ATCommand{
		Name:           "csq",
		Command:        "AT+CSQ",
		ResponsePrefix: "+CSQ:",
	}
}

func (c *CSQ) ParseRespone(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	c.SignalCSQ = 99
	c.SignalPercentage = 0

	tech := Unknown
	if ctx != nil {
		tech = ctx.Tech
	}

	if tech == NR5G_SA {
		if len(resp) > 0 {
			saq := &SACSQ{}
			ParseCSVToStruct(saq, resp[0])
			c.SignalPercentage = saq.ToSignalQuality()

		}
		return
		//TODO rest of tech
	}

	// Default standard CSQ parsing
	var rssi, ber int
	for _, line := range resp {
		line = strings.ReplaceAll(line, "\t", "")
		_, err := fmt.Sscanf(line, "%d,%d", &rssi, &ber)
		if err == nil {
			c.SignalCSQ = rssi
		} else {
			c.SignalCSQ = 99
		}
	}

	if c.SignalCSQ != 99 && c.SignalCSQ >= 0 {
		c.SignalPercentage = int((float64(c.SignalCSQ) / 31.0) * 100)
	}
}

type SACSQ struct {
	Tech string
	RSRP int
	SINR int
	RSRQ int
}

func (s *SACSQ) ToSignalQuality() int {
	minRSRP := -140
	maxRSRP := -70

	var pct int
	if s.RSRP <= minRSRP {
		pct = 0
	} else if s.RSRP >= maxRSRP {
		pct = 100
	} else {
		// Linear interpolation formula
		pct = ((s.RSRP - minRSRP) * 100) / (maxRSRP - minRSRP)
	}
	return pct
}
