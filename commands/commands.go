package commands

import (
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"strings"
	"time"
)

type ATCommand struct {
	Name           string
	Command        string
	ResponsePrefix string
	Timeout        time.Duration
	IsRaw          bool
	NoCache        bool
}
type ATField interface {
	Command(ctx *ParsingContext) ATCommand
	ParseRespone(ctx *ParsingContext, status *ModemStatus, resp []string, raw string)
}

type InteractiveSession interface {
	Write(data string) error
	WriteCmd(cmd string) error
	ReadFrame(timeout time.Duration) (string, error)
	Close() error
}

type ATIConnection interface {
	ExecuteATCommand(ctx *ParsingContext, cmd ATCommand) (string, error)
	StartInteractive() (InteractiveSession, error)
}

// ParsingContext holds data needed during response parsing.
type ParsingContext struct {
	RawResponses  map[string]string
	Tech          ServiceTech
	Connection    ATIConnection
	SessionUptime string
}

// ATResponseParser is the interface implemented by status sub-structures.
type ATResponseParser interface {
	Parse(ctx *ParsingContext)
}

// ModemStatus represents the overall structured state of the modem.
// order of fields is important as it decides setting some parse context values like tech,
// carrier etc.
type ModemStatus struct {
	ATI         `json:",inline"`
	CPIN        `json:",inline"`
	CNUM        `json:",inline"`
	COPS        `json:",inline"`
	ServingCell `json:"service"`
	CSQ
	//TODO fix search state while tech is not yet discovered, for now reg/roaming status
	Registration
	CGPADDR
	SMSList      `json:",inline"`
	SMSCapacity  `json:",inline"`
	APNConfigs   `json:"-"`
	CGACTStatus  `json:"-"`
	APNConfigMap map[int]APNConfig `json:"apn_configs"`

	ConnectionState  string            `json:"connection_state"` // NOCONN, CONNECT
	Tech             string            `json:"tech"`             // LTE, NR5G-SA, 5G NSA, etc.
	LastUpdated      time.Time         `json:"last_updated"`
	ConnectionStatus string            `json:"connection_status"` // Connected, Offline
	RawResponses     map[string]string `json:"raw_responses"`
	SessionUptime    string            `json:"session_uptime"`
}

func NewModemStatus() *ModemStatus {
	return &ModemStatus{
		CSQ:          CSQ{SignalCSQ: 99},
		RawResponses: make(map[string]string),
		APNConfigMap: make(map[int]APNConfig),
	}
}

// CloneAPNConfigMap returns a deep copy of the APN config map.
func CloneAPNConfigMap(m map[int]APNConfig) map[int]APNConfig {
	return maps.Clone(m)
}
func RunParser(ctx *ParsingContext, status *ModemStatus, field ATField) {
	resp, err := ctx.Connection.ExecuteATCommand(ctx, field.Command(ctx))
	if err == nil {
		lines := strings.Split(resp, "\n")
		writeIdx := 0
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				lines[writeIdx] = trimmed
				writeIdx++
			}
		}
		lines = lines[:writeIdx]
		rspPfx := field.Command(ctx).ResponsePrefix
		if rspPfx != "" {
			writeIdx = 0
			for _, line := range lines {
				if strings.HasPrefix(line, rspPfx) {
					lines[writeIdx] = strings.Trim(line[len(rspPfx):], "\r ")
					writeIdx++
				}
			}
			lines = lines[:writeIdx]
		}
		field.ParseRespone(ctx, status, lines, resp)
	}
}
func (s *ModemStatus) Parse(ctx *ParsingContext) {
	if ctx.RawResponses == nil {
		ctx.RawResponses = make(map[string]string)
	}
	defer func() {
		s.RawResponses = ctx.RawResponses
	}()

	if ctx.Connection != nil {
		connected := true
		// check connection with a basic command
		cmd := ATCommand{
			Name:    "ati",
			Command: "ATI",
		}
		resp, err := ctx.Connection.ExecuteATCommand(ctx, cmd)
		if err != nil {
			slog.Error("Error running basic command", "command", cmd.Command, "error", err)
			connected = false
			ctx.RawResponses[cmd.Name] = fmt.Sprintf("Error: %v", err)
		} else {
			ctx.RawResponses[cmd.Name] = resp
		}

		if !connected {
			s.ConnectionStatus = "Offline"
			s.LastUpdated = time.Now()
			s.SessionUptime = "Offline"
			return
		}

		// Set daemon metadata
		s.ConnectionStatus = "Connected"
		s.LastUpdated = time.Now()
		s.SessionUptime = ctx.SessionUptime

		parseRecursive(ctx, s, reflect.ValueOf(s))
	} else {
		s.ConnectionStatus = "Offline"
		s.LastUpdated = time.Now()
		s.SessionUptime = "Offline"
	}

	// Set connection state
	if qeng, ok := ctx.RawResponses["QENG"]; ok {
		lines := strings.Split(qeng, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "+QENG:") && strings.Contains(line, "servingcell") {
				parts := strings.Split(line[6:], ",")
				for i := range parts {
					parts[i] = strings.Trim(strings.TrimSpace(parts[i]), "\"")
				}
				if len(parts) >= 2 {
					state := parts[1]
					if state == "NOCONN" {
						s.ConnectionState = "Connected (Idle)"
					} else if state == "CONNECT" {
						s.ConnectionState = "Connected (Active)"
					} else {
						s.ConnectionState = state
					}
				}
			}
		}
	}

}

func parseRecursive(ctx *ParsingContext, status *ModemStatus, val reflect.Value) {
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return
		}
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		if !field.CanInterface() {
			continue
		}
		if field.CanAddr() {
			addr := field.Addr()
			if atField, ok := addr.Interface().(ATField); ok {
				RunParser(ctx, status, atField)
			}
			// Recurse into fields
			parseRecursive(ctx, status, field)
		}
	}
}
