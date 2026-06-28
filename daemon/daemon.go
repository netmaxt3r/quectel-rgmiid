package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"rgmii/client"
	"rgmii/commands"
)

// Daemon coordinates polling the modem and dispatching custom commands.
type Daemon struct {
	client       *client.Client
	status       commands.ModemStatus
	statusMutex  sync.RWMutex
	cmdMutex     sync.Mutex // Ensures serial execution of AT commands
	pollInterval time.Duration
	callbacks    []func(commands.ModemStatus)
	callbacksMu  sync.RWMutex
}

// NewDaemon creates a new Daemon service.
func NewDaemon(addr string, pollInterval time.Duration) *Daemon {
	return &Daemon{
		client:       client.NewClient(addr),
		pollInterval: pollInterval,
		status: commands.ModemStatus{
			CSQ:              commands.CSQ{SignalCSQ: 99},
			ConnectionStatus: "Offline",
			RawResponses:     make(map[string]string),
		},
		callbacks: []func(commands.ModemStatus){},
	}
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
	if err := commands.ActivateData(d, ctxID); err != nil {
		return err
	}
	return nil
}

// DeactivateData disables data for a specific context ID.
func (d *Daemon) DeactivateData(ctxID int) error {
	if err := commands.DeactivateData(d, ctxID); err != nil {
		return err
	}
	return nil
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
	d.statusMutex.Lock()
	defer d.statusMutex.Unlock()

	ctx := &commands.ParsingContext{
		RawResponses: d.status.RawResponses,
		Connection:   d,
	}
	commands.RunParser(ctx, &d.status, &d.status.SMSList)
	commands.RunParser(ctx, &d.status, &d.status.SMSCapacity)
	d.status.LastUpdated = time.Now()
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
