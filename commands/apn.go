package commands

import (
	"fmt"
	"strconv"
	"strings"
)

type APNConfig struct {
	ContextID int    `json:"context_id"`
	APN       string `json:"apn"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	PDPType   string `json:"pdp_type"`
	Active    bool   `json:"active"`
}

// APNConfigs implements ATField to poll all PDP contexts from the modem.
type APNConfigs struct{}

func (a *APNConfigs) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "apn",
		Command:        "AT+CGDCONT?",
		ResponsePrefix: "+CGDCONT:",
	}
}

func (a *APNConfigs) ParseResponse(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	// Expected response lines (prefix already stripped):
	//   <cid>,"<PDP_type>","<APN>","<addr>",<d_comp>,<h_comp>
	if status.APNConfigMap == nil {
		status.APNConfigMap = make(map[int]APNConfig)
	}
	for _, line := range resp {
		parts := SplitCSV(line)
		if len(parts) >= 3 {
			cidStr := strings.TrimSpace(parts[0])
			cid, err := strconv.Atoi(cidStr)
			if err != nil {
				continue
			}
			cfg := APNConfig{
				ContextID: cid,
				PDPType:   strings.Trim(parts[1], "\""),
				APN:       strings.Trim(parts[2], "\""),
			}
			status.APNConfigMap[cid] = cfg
		}
	}
}

// SetAPN configures the APN on the modem for a specific PDP context.
func SetAPN(conn ATIConnection, ctxID int, cfg APNConfig) error {
	cmd := fmt.Sprintf("AT+CGDCONT=%d,%q,%q,\"\",0,0", ctxID, cfg.PDPType, cfg.APN)
	_, err := conn.ExecuteATCommand(
		&ParsingContext{RawResponses: make(map[string]string)},
		ATCommand{Command: cmd, Name: "setapn", NoCache: true},
	)
	return err
}

// ActivateData enables the PDP context (data connection) for a specific context ID.
func ActivateData(conn ATIConnection, ctxID int) error {
	cmd := fmt.Sprintf("AT+CGACT=1,%d", ctxID)
	_, err := conn.ExecuteATCommand(
		&ParsingContext{RawResponses: make(map[string]string)},
		ATCommand{Command: cmd, Name: "activate", NoCache: true},
	)
	return err
}

// DeactivateData disables the PDP context (data connection) for a specific context ID.
func DeactivateData(conn ATIConnection, ctxID int) error {
	cmd := fmt.Sprintf("AT+CGACT=0,%d", ctxID)
	_, err := conn.ExecuteATCommand(
		&ParsingContext{RawResponses: make(map[string]string)},
		ATCommand{Command: cmd, Name: "deactivate", NoCache: true},
	)
	return err
}

// CGACTStatus implements ATField to poll PDP context activation states.
type CGACTStatus struct{}

func (c *CGACTStatus) Command(ctx *ParsingContext) ATCommand {
	return ATCommand{
		Name:           "cgact",
		Command:        "AT+CGACT?",
		ResponsePrefix: "+CGACT:",
	}
}

func (c *CGACTStatus) ParseResponse(ctx *ParsingContext, status *ModemStatus, resp []string, raw string) {
	// Expected response lines (prefix already stripped):
	//   <cid>,<state>   where state is 0 or 1
	if status.APNConfigMap == nil {
		status.APNConfigMap = make(map[int]APNConfig)
	}
	for _, line := range resp {
		parts := SplitCSV(line)
		if len(parts) >= 2 {
			cid, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				continue
			}
			active := strings.TrimSpace(parts[1]) == "1"
			if cfg, ok := status.APNConfigMap[cid]; ok {
				cfg.Active = active
				status.APNConfigMap[cid] = cfg
			}
		}
	}
}
