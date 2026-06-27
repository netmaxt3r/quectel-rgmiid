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
	"rgmii/devicestatus"
)

// Daemon coordinates polling the modem and dispatching custom commands.
type Daemon struct {
	client       *client.Client
	status       devicestatus.ModemStatus
	statusMutex  sync.RWMutex
	cmdMutex     sync.Mutex // Ensures serial execution of AT commands
	pollInterval time.Duration
	callbacks    []func(devicestatus.ModemStatus)
	callbacksMu  sync.RWMutex
}

// NewDaemon creates a new Daemon service.
func NewDaemon(addr string, pollInterval time.Duration) *Daemon {
	return &Daemon{
		client:       client.NewClient(addr),
		pollInterval: pollInterval,
		status: devicestatus.ModemStatus{
			CSQ:              devicestatus.CSQ{SignalCSQ: 99},
			ConnectionStatus: "Offline",
			RawResponses:     make(map[string]string),
		},
		callbacks: []func(devicestatus.ModemStatus){},
	}
}

// OnStatusUpdate registers a callback to be executed whenever a status poll completes.
func (d *Daemon) OnStatusUpdate(cb func(devicestatus.ModemStatus)) {
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

// SendCommand locks the command mutex and sends a raw AT command to the client.
func (d *Daemon) SendCommand(cmd string) (string, error) {
	d.cmdMutex.Lock()
	defer d.cmdMutex.Unlock()
	return d.client.SendCommand(cmd, 10*time.Second)
}

// GetStatus returns a copy of the current cached modem status.
func (d *Daemon) GetStatus() devicestatus.ModemStatus {
	d.statusMutex.RLock()
	defer d.statusMutex.RUnlock()

	// Deep copy raw responses map
	rawCopy := maps.Clone(d.status.RawResponses)
	if rawCopy == nil {
		rawCopy = make(map[string]string)
	}

	statusCopy := d.status
	statusCopy.RawResponses = rawCopy
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
			d.PollAll()
		}
	}
}
func (d *Daemon) ExecuteATCommand(ctx *devicestatus.ParsingContext, cmd devicestatus.ATCommand) (string, error) {
	if rsp, found := ctx.RawResponses[cmd.Name]; found {
		return rsp, nil
	}
	resp, err := d.SendCommand(cmd.Command)
	if err != nil {
		slog.Error("Error running command", "command", cmd.Command, "error", err)
		ctx.RawResponses[cmd.Name] = fmt.Sprintf("Error: %v", err)
	} else {
		ctx.RawResponses[cmd.Name] = resp
	}
	return resp, err
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

	newStatus := devicestatus.NewModemStatus()
	ctx := &devicestatus.ParsingContext{
		RawResponses:  newStatus.RawResponses,
		Connection:    d,
		SessionUptime: FormatDuration(d.client.GetUptime()),
	}
	newStatus.Parse(ctx)

	// Poll SMS
	smsList, err := d.pollSMS()
	if err != nil {
		slog.Error("Error polling SMS", "error", err)
	} else {
		newStatus.SMS = smsList
	}

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
	smsList, err := d.pollSMS()
	if err != nil {
		slog.Error("Error polling SMS", "error", err)
		return
	}
	d.statusMutex.Lock()
	d.status.SMS = smsList
	d.status.LastUpdated = time.Now()
	d.statusMutex.Unlock()
}

// pollSMS retrieves SMS messages from the modem.
func (d *Daemon) pollSMS() ([]devicestatus.SMSMessage, error) {
	// Ensure text mode
	_, err := d.SendCommand("AT+CMGF=1")
	if err != nil {
		return nil, fmt.Errorf("failed to set SMS text mode: %w", err)
	}

	resp, err := d.SendCommand(`AT+CMGL="ALL"`)
	if err != nil {
		return nil, fmt.Errorf("failed to list SMS: %w", err)
	}

	return parseSMSList(resp), nil
}

// DeleteSMS deletes an SMS message by its index.
func (d *Daemon) DeleteSMS(index int) error {
	_, err := d.SendCommand(fmt.Sprintf("AT+CMGD=%d", index))
	if err != nil {
		return fmt.Errorf("failed to delete SMS index %d: %w", index, err)
	}
	return nil
}

// parseSMSList parses the output of AT+CMGL="ALL".
func parseSMSList(resp string) []devicestatus.SMSMessage {
	var messages []devicestatus.SMSMessage
	var current *devicestatus.SMSMessage

	lines := strings.Split(resp, "\n")
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "OK" || trimmed == "ERROR" {
			break
		}
		if strings.HasPrefix(line, "+CMGL:") {
			if current != nil {
				messages = append(messages, *current)
			}
			// Parse metadata
			metaStr := strings.TrimPrefix(line, "+CMGL:")
			parts := splitCSV(metaStr)
			if len(parts) >= 3 {
				var idx int
				_, _ = fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &idx)
				status := strings.TrimSpace(parts[1])
				sender := strings.TrimSpace(parts[2])
				date := ""
				if len(parts) >= 5 {
					date = strings.TrimSpace(parts[4])
				}
				current = &devicestatus.SMSMessage{
					Index:  idx,
					Status: status,
					Sender: sender,
					Date:   date,
				}
			}
		} else {
			if current != nil {
				if trimmed == "" && current.Content == "" {
					continue
				}
				if current.Content != "" {
					current.Content += "\n"
				}
				current.Content += line
			}
		}
	}
	if current != nil {
		messages = append(messages, *current)
	}
	return messages
}

// splitCSV splits a string by comma, ignoring commas inside double quotes.
func splitCSV(line string) []string {
	var parts []string
	var current strings.Builder
	inQuotes := false
	for i := 0; i < len(line); i++ {
		b := line[i]
		if b == '"' {
			inQuotes = !inQuotes
		} else if b == ',' && !inQuotes {
			parts = append(parts, current.String())
			current.Reset()
		} else {
			current.WriteByte(b)
		}
	}
	parts = append(parts, current.String())
	return parts
}
