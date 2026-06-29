package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"rgmii/client"
	"rgmii/commands"
)

// Daemon coordinates polling the modem and dispatching custom commands.
type Daemon struct {
	client          *client.Client
	status          commands.ModemStatus
	statusMutex     sync.RWMutex
	cmdMutex        sync.Mutex // Ensures serial execution of AT commands
	pollInterval    time.Duration
	callbacks       []func(commands.ModemStatus)
	callbacksMu     sync.RWMutex
	dynConfigsState map[string]*commands.DynamicConfigState
	dynStateMutex   sync.RWMutex
}

// NewDaemon creates a new Daemon service.
func NewDaemon(addr string, pollInterval time.Duration) *Daemon {
	d := &Daemon{
		client:       client.NewClient(addr),
		pollInterval: pollInterval,
		status: commands.ModemStatus{
			CSQ:              commands.CSQ{SignalCSQ: 99},
			ConnectionStatus: "Offline",
			RawResponses:     make(map[string]string),
		},
		callbacks:       []func(commands.ModemStatus){},
		dynConfigsState: make(map[string]*commands.DynamicConfigState),
	}
	d.client.OnConnect = d.handleClientConnect
	return d
}

// OnStatusUpdate registers a callback to be executed whenever a status poll completes.
func (d *Daemon) OnStatusUpdate(cb func(commands.ModemStatus)) {
	d.callbacksMu.Lock()
	defer d.callbacksMu.Unlock()
	d.callbacks = append(d.callbacks, cb)
}

// notifyCallbacks invokes all registered callbacks with a copy of the current status.
func (d *Daemon) notifyCallbacks() {
	status := d.GetStatus()
	d.callbacksMu.RLock()
	defer d.callbacksMu.RUnlock()
	for _, cb := range d.callbacks {
		go cb(status)
	}
}

// SendCommand locks the command mutex and sends a raw AT command to the client with a default 10s timeout.
func (d *Daemon) SendCommand(cmd string) (string, error) {
	return d.SendCommandWithTimeout(cmd, 10*time.Second)
}

// SendCommandWithTimeout locks the command mutex and sends a raw AT command with a custom timeout.
func (d *Daemon) SendCommandWithTimeout(cmd string, timeout time.Duration) (string, error) {
	d.cmdMutex.Lock()
	defer d.cmdMutex.Unlock()
	return d.client.SendCommand(cmd, timeout)
}

// SendRawCommandWithTimeout locks the command mutex and sends a completely raw AT command with a custom timeout.
func (d *Daemon) SendRawCommandWithTimeout(cmd string, timeout time.Duration) (string, error) {
	d.cmdMutex.Lock()
	defer d.cmdMutex.Unlock()
	return d.client.SendRawCommand(cmd, timeout)
}

type daemonInteractive struct {
	commands.InteractiveSession
	d *Daemon
}

func (di *daemonInteractive) Close() error {
	err := di.InteractiveSession.Close()
	di.d.cmdMutex.Unlock()
	return err
}

// StartInteractive locks the command mutex and begins an interactive streaming session.
func (d *Daemon) StartInteractive() (commands.InteractiveSession, error) {
	d.cmdMutex.Lock()
	is, err := d.client.StartInteractive()
	if err != nil {
		d.cmdMutex.Unlock()
		return nil, err
	}
	return &daemonInteractive{InteractiveSession: is, d: d}, nil
}

// GetStatus returns a copy of the current cached modem status.
func (d *Daemon) GetStatus() commands.ModemStatus {
	d.statusMutex.RLock()
	defer d.statusMutex.RUnlock()

	// Deep copy raw responses map
	rawCopy := maps.Clone(d.status.RawResponses)
	if rawCopy == nil {
		rawCopy = make(map[string]string)
	}

	// Deep copy APN configs map
	apnCopy := commands.CloneAPNConfigMap(d.status.APNConfigMap)
	if apnCopy == nil {
		apnCopy = make(map[int]commands.APNConfig)
	}

	statusCopy := d.status
	statusCopy.RawResponses = rawCopy
	statusCopy.APNConfigMap = apnCopy
	return statusCopy
}

// Start runs the periodic status poll loop. It blocks until context is cancelled.
func (d *Daemon) Start(ctx context.Context) {
	// Start the client reconnect loop
	d.client.Start(ctx)

	slog.Info("Starting background stats polling", "interval", d.pollInterval)

	// Initial poll
	d.PollAll()

	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping stats polling")
			return
		case <-ticker.C:
			ticker.Stop()
			d.PollAll()
			ticker.Reset(d.pollInterval)
		}
	}
}

func (d *Daemon) ExecuteATCommand(ctx *commands.ParsingContext, cmd commands.ATCommand) (string, error) {
	if ctx != nil && !cmd.NoCache {
		if rsp, found := ctx.RawResponses[cmd.Name]; found {
			return rsp, nil
		}
	}

	timeout := 10 * time.Second
	if cmd.Timeout > 0 {
		timeout = cmd.Timeout
	}

	var resp string
	var err error
	if cmd.IsRaw {
		resp, err = d.SendRawCommandWithTimeout(cmd.Command, timeout)
	} else {
		resp, err = d.SendCommandWithTimeout(cmd.Command, timeout)
	}

	if err != nil {
		slog.Error("Error running command", "command", cmd.Command, "error", err)
		if ctx != nil {
			ctx.RawResponses[cmd.Name] = fmt.Sprintf("Error: %v", err)
		}
	} else {
		if ctx != nil {
			ctx.RawResponses[cmd.Name] = resp
		}
	}
	return resp, err
}

// SetAPN configures an APN for a specific data context ID.
func (d *Daemon) SetAPN(ctxID int, cfg commands.APNConfig) error {
	if err := commands.SetAPN(d, ctxID, cfg); err != nil {
		return err
	}
	cfg.ContextID = ctxID
	d.statusMutex.Lock()
	if d.status.APNConfigMap == nil {
		d.status.APNConfigMap = make(map[int]commands.APNConfig)
	}
	d.status.APNConfigMap[ctxID] = cfg
	d.statusMutex.Unlock()
	return nil
}

// ActivateData enables data for a specific context ID.
func (d *Daemon) ActivateData(ctxID int) error {
	return commands.ActivateData(d, ctxID)
}

// DeactivateData disables data for a specific context ID.
func (d *Daemon) DeactivateData(ctxID int) error {
	return commands.DeactivateData(d, ctxID)
}

// PollAll queries the modem for all status parameters and updates the cache.
func (d *Daemon) PollAll() {
	if !d.client.IsConnected() {
		d.statusMutex.Lock()
		d.status.ConnectionStatus = "Offline"
		d.status.LastUpdated = time.Now()
		d.status.SessionUptime = "Offline"
		d.statusMutex.Unlock()
		d.notifyCallbacks()
		return
	}

	newStatus := commands.NewModemStatus()
	ctx := &commands.ParsingContext{
		RawResponses:  newStatus.RawResponses,
		Connection:    d,
		SessionUptime: FormatDuration(d.client.GetUptime()),
	}
	newStatus.Parse(ctx)

	// Save to cache
	d.statusMutex.Lock()
	d.status = *newStatus
	d.statusMutex.Unlock()

	d.notifyCallbacks()
}

// FormatDuration converts a duration to a human-readable string (e.g., 2h 15m 3s).
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	var parts []string
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%dh", h))
	}
	if m > 0 || h > 0 {
		parts = append(parts, fmt.Sprintf("%dm", m))
	}
	parts = append(parts, fmt.Sprintf("%ds", s))

	return strings.Join(parts, " ")
}

// PollSMSOnly polls only SMS messages and updates the cached status.
func (d *Daemon) PollSMSOnly() {
	if !d.client.IsConnected() {
		return
	}

	// Collect results into temporary structures without holding statusMutex.
	// This avoids holding statusMutex while acquiring cmdMutex (via AT commands).
	var smsList commands.SMSList
	var smsCapacity commands.SMSCapacity
	tmpStatus := commands.NewModemStatus()

	ctx := &commands.ParsingContext{
		RawResponses: make(map[string]string),
		Connection:   d,
	}
	commands.RunParser(ctx, tmpStatus, &smsList)
	commands.RunParser(ctx, tmpStatus, &smsCapacity)

	// Now lock statusMutex only for the cache update
	d.statusMutex.Lock()
	d.status.SMSList = smsList
	d.status.SMSCapacity = smsCapacity
	d.status.LastUpdated = time.Now()
	d.statusMutex.Unlock()
}

// DeleteSMS deletes an SMS message by its index.
func (d *Daemon) DeleteSMS(index int) error {
	_, err := d.SendCommand(fmt.Sprintf("AT+CMGD=%d", index))
	if err != nil {
		return fmt.Errorf("failed to delete SMS index %d: %w", index, err)
	}
	return nil
}

// SendSMS sends an SMS message via the modem.
func (d *Daemon) SendSMS(number, text string) error {
	return commands.SendSMS(d, number, text)
}

// SetATIDebug enables or disables real-time raw AT command logging.
func (d *Daemon) SetATIDebug(enabled bool) {
	d.client.Debug = enabled
}

func (d *Daemon) handleClientConnect() {
	slog.Info("Daemon connected/reconnected to modem. Querying dynamic configurations...")
	configs := commands.GetDynamicConfigs()
	states := make(map[string]*commands.DynamicConfigState)

	for _, cfg := range configs {
		cmdStr := fmt.Sprintf("AT+%s=?", cfg.Command)
		resp, err := d.SendCommand(cmdStr)
		if err != nil {
			slog.Error("Failed to query format for dynamic config", "name", cfg.Name, "command", cmdStr, "error", err)
			continue
		}

		subcommands := commands.ParseDynamicConfigResponse(cfg.Command, resp)
		states[strings.ToLower(cfg.Name)] = commands.NewDynamicConfigState(cfg, subcommands)
	}

	d.dynStateMutex.Lock()
	d.dynConfigsState = states
	d.dynStateMutex.Unlock()
	slog.Info("Dynamic configurations loaded", "count", len(states))
}
func (d *Daemon) GetDynamicConfigs() []string {
	d.dynStateMutex.RLock()
	defer d.dynStateMutex.RUnlock()
	return slices.Collect(maps.Keys(d.dynConfigsState))
}

func (d *Daemon) GetDynamicConfigState(name string) (*commands.DynamicConfigState, bool) {
	d.dynStateMutex.RLock()
	defer d.dynStateMutex.RUnlock()
	state, ok := d.dynConfigsState[strings.ToLower(name)]
	return state, ok
}
func parseDynamicConfigResponse(name, subname, resp string) ([]string, error) {
	lines := strings.Split(resp, "\n")
	if len(lines) > 0 {
		last := lines[len(lines)-1]
		last = strings.TrimSpace(last)
		if strings.HasPrefix(last, "ERROR") {
			return nil, errors.New(last)
		}
	}
	writeIdx := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			lines[writeIdx] = trimmed
			writeIdx++
		}
	}
	lines = lines[:writeIdx]
	rspPfx := fmt.Sprintf("+%s: \"%s\",", name, subname)
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
	return lines, nil
}
func (d *Daemon) QueryDynamicConfigValue(name, subname string) ([]string, string, error) {
	state, ok := d.GetDynamicConfigState(name)
	if !ok {
		return nil, "", fmt.Errorf("dynamic configuration %q not found", name)
	}

	cmdStr := fmt.Sprintf("AT+%s=%q", state.Config.Command, subname)
	resp, err := d.SendCommand(cmdStr)
	if err != nil {
		return nil, resp, err
	}

	trimmed := strings.TrimSpace(resp)
	lines, err := parseDynamicConfigResponse(name, subname, trimmed)
	if err != nil {
		return nil, resp, err
	}
	state.SetValue(subname, lines)
	return lines, resp, nil
}

func (d *Daemon) SetDynamicConfigValue(name, subname, args string) ([]string, string, error) {
	state, ok := d.GetDynamicConfigState(name)
	if !ok {
		return nil, "", fmt.Errorf("dynamic configuration %q not found", name)
	}

	cmdStr := fmt.Sprintf("AT+%s=%q,%s", state.Config.Command, subname, args)
	resp, err := d.SendCommand(cmdStr)
	if err != nil {
		return nil, resp, err
	}

	// Auto-query the new value to update cache
	return d.QueryDynamicConfigValue(name, subname)
}
