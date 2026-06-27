package devicestatus

import (
	"strconv"
	"strings"
)

type ServiceTech int

const (
	Unknown ServiceTech = -1
	NR5G_SA ServiceTech = iota
	NSA_5G
	LTE
	UMTS_3G
	EDGE
	GPRS
)

// +QENG: "servingcell",<state>,"NR5G-SA",<duplex_mode>,<mcc>,<mnc>,<cellid>,<pcid>,
// <nr_dl_arfcn>,<freq_band_ind>,<tac>,<rsrp>,<rsrq>,<sinr>,<tx_power>,<srxlev>
type ServingCell5GSA struct {
	State      string `json:"state"`
	Tech       string `json:"tech"`
	DuplexMode string `json:"duplex_mode"`
	MCC        int    `json:"mcc"`
	MNC        int    `json:"mnc"`
	CellId     string `json:"cellid"`
	PCID       int    `json:"pcid"`
	NrDlArfcn  string `json:"nr_dl_arfcn"`
	Band       int    `json:"band"`
	Tac        string `json:"tac"`
	RSRP       int    `json:"rsrp"`
	RSRQ       int    `json:"rsrq"`
	SINR       int    `json:"sinr"`
	TxPower    int    `json:"tx_power_v"`
	Srxlev     int    `json:"srxlev"`

	// Advanced modular fields
	Nr5gMimoLayers `json:",inline"`
	Nr5gCsi        `json:",inline"`
	Nr5gUlMcs      `json:",inline"`
	Nr5gDlMcs      `json:",inline"`
	Nr5gTxPwr      `json:",inline"`
}

func (s *ServingCell5GSA) ConnectionState() string {
	if s == nil {
		return "Unknown"
	}
	if s.State == "NOCONN" {
		return "Connected (Idle)"
	} else if s.State == "CONNECT" {
		return "Connected (Active)"
	}
	return s.State
}

// TODO NSA
// +QENG: "servingcell",<state>
// +QENG: "LTE",<mcc>,<mnc>,<cellid>,<pcid>,<earfcn>,<freq_band_ind>,<ul_bandwidth>,
// <dl_bandwidth>,<tac>,<rsrp>,<rsrq>,<rssi>,<sinr>,<cqi>,<tx_power>,<srxlev>
// +QENG: "NR5G-NSA",<mcc>,<mnc>,<pcid>,<nr_dl_arfcn>,<freq_band_ind>,<rsrp>,<rsrq>,<sinr>
type ServingCell5GNSA struct {
	State string `json:"state"`
	MCC   int    `json:"mcc"`
	MNC   int    `json:"mnc"`
	PCID  int    `json:"pcid"`
	RSRP  int    `json:"rsrp"`
	SINR  int    `json:"sinr"`
	RSRQ  int    `json:"rsrq"`
	ARFCN int    `json:"arfcn"`
	Band  int    `json:"band"`

	// Advanced modular fields
	Nr5gMimoLayers `json:",inline"`
	Nr5gCsi        `json:",inline"`
	Nr5gUlMcs      `json:",inline"`
	Nr5gDlMcs      `json:",inline"`
	Nr5gTxPwr      `json:",inline"`
}

func (s *ServingCell5GNSA) ConnectionState() string {
	if s == nil {
		return "Unknown"
	}
	if s.State == "NOCONN" {
		return "Connected (Idle)"
	} else if s.State == "CONNECT" {
		return "Connected (Active)"
	}
	return s.State
}

// +QENG: "servingcell",<state>,"LTE",<is_tdd>,<mcc>,<mnc>,<cellid>,<pcid>,<earfcn>,
// <freq_band_ind>,<ul_bandwidth>,<dl_bandwidth>,<tac>,<rsrp>,<rsrq>,<rssi>,<sinr>,
// <cqi>,<tx_power>,<srxlev>
type ServingCellLTE struct {
	State        string `json:"state"`
	Tech         string `json:"tech"`
	Is_tdd       bool   `json:"is_tdd"`
	MCC          int    `json:"mcc"`
	MNC          int    `json:"mnc"`
	CellId       string `json:"cellid"`
	PCID         int    `json:"pcid"`
	EARFCN       int    `json:"earfcn"`
	Band         int    `json:"band"`
	Ul_bandwidth string `json:"ul_bandwidth"`
	Dl_bandwidth string `json:"dl_bandwidth"`
	Tac          string `json:"tac"`
	RSRP         int    `json:"rsrp"`
	RSRQ         int    `json:"rsrq"`
	RSSI         int    `json:"rssi"`
	SINR         int    `json:"sinr"`
	CQI          int    `json:"cqi"`
	TxPower      int    `json:"tx_power_v"`
	Srxlev       int    `json:"srxlev"`

	// Advanced modular fields
	LteMimoLayers `json:",inline"`
	LteCsi        `json:",inline"`
	LteTxPwr      `json:",inline"`
}

func (s *ServingCellLTE) ConnectionState() string {
	if s == nil {
		return "Unknown"
	}
	if s.State == "NOCONN" {
		return "Connected (Idle)"
	} else if s.State == "CONNECT" {
		return "Connected (Active)"
	}
	return s.State
}

type ServingCell struct {
	AccessTechnology string            `json:"AccessTechnology"`
	ServiceTech      ServiceTech       `json:"ServiceTech"`
	NR5GSA           *ServingCell5GSA  `json:"nr5g_sa,omitempty"`
	NR5GNSA          *ServingCell5GNSA `json:"nr5g_nsa,omitempty"`
	LTE              *ServingCellLTE   `json:"lte,omitempty"`
	PrimaryCell      Cell              `json:"primary_cell"`
	Cells            []Cell            `json:"cells"`
}

type Cell interface {
	ConnectionState() string
}

func (sc *ServingCell) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "servingcell",
		Command:        "AT+QENG=\"servingcell\"",
		ResponsePrefix: "+QENG: \"servingcell\",",
	}
}

func (sc *ServingCell) ParseRespone(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	sc.AccessTechnology = "Unknown"
	sc.ServiceTech = Unknown
	defer func() {
		if ctx != nil {
			ctx.Tech = sc.ServiceTech
		}
		if status != nil {
			status.Tech = sc.AccessTechnology
		}
	}()
	if len(resp) > 0 {
		technology, tech := DetectTechnology(resp)
		sc.AccessTechnology = technology
		sc.ServiceTech = tech
	}
	if sc.ServiceTech == Unknown && raw != "" {
		rawLines := strings.Split(raw, "\n")
		technology, tech := DetectTechnology(rawLines)
		if tech != Unknown {
			sc.AccessTechnology = technology
			sc.ServiceTech = tech
		}
	}
	switch sc.ServiceTech {
	case NR5G_SA:
		for _, line := range resp {
			if strings.Contains(line, "NR5G-SA") {
				sc.parse5GSA(ctx, status, line)
				sc.PrimaryCell = sc.NR5GSA
				sc.Cells = []Cell{sc.NR5GSA}
				break
			}
		}
	case LTE:
		for _, line := range resp {
			if strings.Contains(line, "LTE") {
				sc.parseLTE(ctx, line)
				sc.PrimaryCell = sc.LTE
				sc.Cells = []Cell{sc.LTE}
				break
			}
		}
	case NSA_5G:
		//TODO FIX Me
		sc.parse5GNSA(ctx, status, raw)
		sc.PrimaryCell = sc.LTE
		sc.Cells = []Cell{sc.LTE, sc.NR5GNSA}
	}
	if sc.PrimaryCell != nil && status != nil {
		status.ConnectionState = sc.PrimaryCell.ConnectionState()
	}
}

func (sc *ServingCell) parse5GSA(ctx *ParsingContext, status *ModemStatus, resp string) {
	sc.NR5GSA = &ServingCell5GSA{}
	resp = strings.TrimPrefix(resp, `"servingcell",`)
	parts := splitCSV(resp)
	for i := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(parts[i]), "\"")
	}

	if len(parts) >= 10 {
		sc.NR5GSA.State = parts[0]
		sc.NR5GSA.Tech = parts[1]
		sc.NR5GSA.DuplexMode = parts[2]
		sc.NR5GSA.MCC, _ = strconv.Atoi(parts[3])
		sc.NR5GSA.MNC, _ = strconv.Atoi(parts[4])
		sc.NR5GSA.CellId = parts[5]
		sc.NR5GSA.PCID, _ = strconv.Atoi(parts[6])
		sc.NR5GSA.NrDlArfcn = parts[7]
		sc.NR5GSA.Band, _ = strconv.Atoi(parts[8])
		sc.NR5GSA.Tac = parts[9]

		rsrpIdx := 10
		if len(parts) > 11 && !strings.HasPrefix(parts[10], "-") {
			sc.NR5GSA.Srxlev, _ = strconv.Atoi(parts[10])
			rsrpIdx = 11
		}

		if len(parts) > rsrpIdx {
			sc.NR5GSA.RSRP, _ = strconv.Atoi(parts[rsrpIdx])
		}
		if len(parts) > rsrpIdx+1 {
			sc.NR5GSA.RSRQ, _ = strconv.Atoi(parts[rsrpIdx+1])
		}
		if len(parts) > rsrpIdx+2 {
			sc.NR5GSA.SINR, _ = strconv.Atoi(parts[rsrpIdx+2])
		}
		if len(parts) > rsrpIdx+3 {
			sc.NR5GSA.TxPower, _ = strconv.Atoi(parts[rsrpIdx+3])
		}
		if sc.NR5GSA.Srxlev == 0 && len(parts) > rsrpIdx+4 {
			sc.NR5GSA.Srxlev, _ = strconv.Atoi(parts[rsrpIdx+4])
		}
	}
}

func (sc *ServingCell) parseLTE(ctx *ParsingContext, resp string) {
	sc.LTE = &ServingCellLTE{}
	resp = strings.TrimPrefix(resp, `"servingcell",`)
	parts := splitCSV(resp)
	for i := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(parts[i]), "\"")
	}

	if len(parts) >= 12 {
		sc.LTE.State = parts[0]
		sc.LTE.Tech = parts[1]
		if parts[2] == "TDD" {
			sc.LTE.Is_tdd = true
		} else {
			sc.LTE.Is_tdd = false
		}
		sc.LTE.MCC, _ = strconv.Atoi(parts[3])
		sc.LTE.MNC, _ = strconv.Atoi(parts[4])
		sc.LTE.CellId = parts[5]
		sc.LTE.PCID, _ = strconv.Atoi(parts[6])
		sc.LTE.EARFCN, _ = strconv.Atoi(parts[7])
		sc.LTE.Band, _ = strconv.Atoi(parts[8])
		sc.LTE.Ul_bandwidth = parts[9]
		sc.LTE.Dl_bandwidth = parts[10]
		sc.LTE.Tac = parts[11]

		rsrpIdx := 12
		if len(parts) > 13 && !strings.HasPrefix(parts[12], "-") {
			rsrpIdx = 13
		}

		if len(parts) > rsrpIdx {
			sc.LTE.RSRP, _ = strconv.Atoi(parts[rsrpIdx])
		}
		if len(parts) > rsrpIdx+1 {
			sc.LTE.RSRQ, _ = strconv.Atoi(parts[rsrpIdx+1])
		}
		if len(parts) > rsrpIdx+2 {
			sc.LTE.RSSI, _ = strconv.Atoi(parts[rsrpIdx+2])
		}
		if len(parts) > rsrpIdx+3 {
			sc.LTE.SINR, _ = strconv.Atoi(parts[rsrpIdx+3])
		}
		if len(parts) > rsrpIdx+4 {
			sc.LTE.CQI, _ = strconv.Atoi(parts[rsrpIdx+4])
		}
		if len(parts) > rsrpIdx+5 {
			sc.LTE.TxPower, _ = strconv.Atoi(parts[rsrpIdx+5])
		}
		if len(parts) > rsrpIdx+6 {
			sc.LTE.Srxlev, _ = strconv.Atoi(parts[rsrpIdx+6])
		}
	}
}

func (sc *ServingCell) parse5GNSA(ctx *ParsingContext, status *ModemStatus, raw string) {
	sc.NR5GNSA = &ServingCell5GNSA{}
	sc.LTE = &ServingCellLTE{}

	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+QENG:") {
			continue
		}
		content := line[6:]
		parts := splitCSV(content)
		for i := range parts {
			parts[i] = strings.Trim(strings.TrimSpace(parts[i]), "\"")
		}
		if len(parts) == 0 {
			continue
		}
		if parts[0] == "servingcell" {
			if len(parts) >= 2 {
				sc.NR5GNSA.State = parts[1]
				sc.LTE.State = parts[1]
			}
		} else if parts[0] == "LTE" {
			if len(parts) >= 17 {
				sc.LTE.MCC, _ = strconv.Atoi(parts[1])
				sc.LTE.MNC, _ = strconv.Atoi(parts[2])
				sc.LTE.CellId = parts[3]
				sc.LTE.PCID, _ = strconv.Atoi(parts[4])
				sc.LTE.EARFCN, _ = strconv.Atoi(parts[5])
				sc.LTE.Band, _ = strconv.Atoi(parts[6])
				sc.LTE.Ul_bandwidth = parts[7]
				sc.LTE.Dl_bandwidth = parts[8]
				sc.LTE.Tac = parts[9]
				sc.LTE.RSRP, _ = strconv.Atoi(parts[10])
				sc.LTE.RSRQ, _ = strconv.Atoi(parts[11])
				sc.LTE.RSSI, _ = strconv.Atoi(parts[12])
				sc.LTE.SINR, _ = strconv.Atoi(parts[13])
				sc.LTE.CQI, _ = strconv.Atoi(parts[14])
				sc.LTE.TxPower, _ = strconv.Atoi(parts[15])
				sc.LTE.Srxlev, _ = strconv.Atoi(parts[16])
			}
		} else if parts[0] == "NR5G-NSA" {
			if len(parts) >= 9 {
				sc.NR5GNSA.MCC, _ = strconv.Atoi(parts[1])
				sc.NR5GNSA.MNC, _ = strconv.Atoi(parts[2])
				sc.NR5GNSA.PCID, _ = strconv.Atoi(parts[3])
				sc.NR5GNSA.RSRP, _ = strconv.Atoi(parts[4])
				sc.NR5GNSA.SINR, _ = strconv.Atoi(parts[5])
				sc.NR5GNSA.RSRQ, _ = strconv.Atoi(parts[6])
				sc.NR5GNSA.ARFCN, _ = strconv.Atoi(parts[7])
				sc.NR5GNSA.Band, _ = strconv.Atoi(parts[8])
			}
		}
	}
}

func DetectTechnology(lines []string) (string, ServiceTech) {
	hasNSA := false
	hasSA := false
	hasLTE := false
	for _, line := range lines {
		if strings.Contains(line, "NR5G-NSA") {
			hasNSA = true
		}
		if strings.Contains(line, "NR5G-SA") {
			hasSA = true
		}
		if strings.Contains(line, "LTE") {
			hasLTE = true
		}
	}
	if hasSA {
		return "NR5G-SA", NR5G_SA
	} else if hasNSA {
		return "5G NSA", NSA_5G
	} else if hasLTE {
		return "LTE", LTE
	}
	return "Unknown", Unknown
}
