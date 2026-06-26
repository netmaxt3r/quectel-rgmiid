package daemon

import (
	"context"
	"fmt"
	"log"
	"maps"
	"net"
	"strings"
	"sync"
	"time"

	"rgmii/client"
)

// ServingCellInfo contains detailed radio cell parameters.
type ServingCellInfo struct {
	MCC       string `json:"mcc"`
	MNC       string `json:"mnc"`
	CellID    string `json:"cell_id"`
	PCI       string `json:"pci"`
	ARFCN     string `json:"arfcn"`
	EARFCN    string `json:"earfcn"`
	Band      string `json:"band"`
	RSRP      string `json:"rsrp"`
	RSRQ      string `json:"rsrq"`
	RSSI      string `json:"rssi"`
	SINR      string `json:"sinr"`
	NR5GBand  string `json:"nr5g_band"`
	NR5GRSRP  string `json:"nr5g_rsrp"`
	NR5GRSRQ  string `json:"nr5g_rsrq"`
	NR5GSINR  string `json:"nr5g_sinr"`
}

// SMSMessage represents a single SMS message stored on the modem.
type SMSMessage struct {
	Index   int    `json:"index"`
	Status  string `json:"status"`
	Sender  string `json:"sender"`
	Date    string `json:"date"`
	Content string `json:"content"`
}

// ModemStatus represents the overall structured state of the modem.
type ModemStatus struct {
	Manufacturer        string            `json:"manufacturer"`
	Model               string            `json:"model"`
	Revision            string            `json:"revision"`
	SimState            string            `json:"sim_state"`
	SimNumber           string            `json:"sim_number"`
	Carrier             string            `json:"carrier"`
	SignalCSQ           int               `json:"signal_csq"`             // 0-31
	SignalPercentage    int               `json:"signal_percentage"`      // 0-100%
	NetworkRegistration string            `json:"network_registration"`   // Registered, Not Registered, etc.
	ConnectionState     string            `json:"connection_state"`       // NOCONN, CONNECT
	Tech                string            `json:"tech"`                   // LTE, NR5G-SA, 5G NSA, etc.
	IPAddress           string            `json:"ip_address"`
	IPv6Address         string            `json:"ipv6_address"`
	ServingCell         ServingCellInfo   `json:"serving_cell"`
	MimoLayers          string            `json:"mimo_layers"`
	TddSlotRatio        string            `json:"tdd_slot_ratio"`
	Nr5gUlMcs           string            `json:"nr5g_ul_mcs"`
	Nr5gDlMcs           string            `json:"nr5g_dl_mcs"`
	Nr5gCsi             string            `json:"nr5g_csi"`
	LteCsi              string            `json:"lte_csi"`
	Nr5gTxPwr           string            `json:"nr5g_tx_pwr"`
	LteTxPwr            string            `json:"lte_tx_pwr"`
	LastUpdated         time.Time         `json:"last_updated"`
	ConnectionStatus    string            `json:"connection_status"`      // Connected, Offline
	RawResponses        map[string]string `json:"raw_responses"`
	SessionUptime       string            `json:"session_uptime"`
	SMS                 []SMSMessage      `json:"sms"`
}

// Daemon coordinates polling the modem and dispatching custom commands.
type Daemon struct {
	client       *client.Client
	status       ModemStatus
	statusMutex  sync.RWMutex
	cmdMutex     sync.Mutex // Ensures serial execution of AT commands
	pollInterval time.Duration
	callbacks    []func(ModemStatus)
	callbacksMu  sync.RWMutex
}

// NewDaemon creates a new Daemon service.
func NewDaemon(addr string, pollInterval time.Duration) *Daemon {
	return &Daemon{
		client:       client.NewClient(addr),
		pollInterval: pollInterval,
		status: ModemStatus{
			SignalCSQ:        99,
			ConnectionStatus: "Offline",
			RawResponses:     make(map[string]string),
		},
		callbacks: []func(ModemStatus){},
	}
}

// OnStatusUpdate registers a callback to be executed whenever a status poll completes.
func (d *Daemon) OnStatusUpdate(cb func(ModemStatus)) {
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
	return d.client.SendCommand(cmd, 5*time.Second)
}

// GetStatus returns a copy of the current cached modem status.
func (d *Daemon) GetStatus() ModemStatus {
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

	log.Printf("Starting background stats polling every %s", d.pollInterval)
	
	// Initial poll
	d.PollAll()

	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping stats polling...")
			return
		case <-ticker.C:
			d.PollAll()
		}
	}
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

	newStatus := ModemStatus{
		SignalCSQ:    99,
		RawResponses: make(map[string]string),
	}

	// Commands list to fetch basic info and cell details
	cmds := []struct {
		key string
		cmd string
	}{
		{"ATI", "ATI"},
		{"CPIN", "AT+CPIN?"},
		{"CNUM", "AT+CNUM"},
		{"COPS", "AT+COPS?"},
		{"CSQ", "AT+CSQ"},
		{"CEREG", "AT+CEREG?"},
		{"CGREG", "AT+CGREG?"},
		{"C5GREG", "AT+C5GREG?"},
		{"CGPADDR", "AT+CGPADDR"},
		{"QENG", "AT+QENG=\"servingcell\""},
		{"lte_mimo_layers", "AT+QNWCFG=\"lte_mimo_layers\""},
		{"nr5g_mimo_layers", "AT+QNWCFG=\"nr5g_mimo_layers\""},
		{"nr5g_csi", "AT+QNWCFG=\"nr5g_csi\""},
		{"lte_csi", "AT+QNWCFG=\"lte_csi\""},
		{"nr5g_ulMCS", "AT+QNWCFG=\"nr5g_ulMCS\""},
		{"nr5g_dlMCS", "AT+QNWCFG=\"nr5g_dlMCS\""},
		{"nr5g_tx_pwr", "AT+QNWCFG=\"nr5g_tx_pwr\""},
		{"lte_tx_pwr", "AT+QNWCFG=\"lte_tx_pwr\""},
		{"qcainfo", "AT+QCAINFO"},
		{"tdd_config_qcfg", "AT+QCFG=\"tdd/config\""},
		{"tdd_config_qnwcfg", "AT+QNWCFG=\"tdd_config\""},
		{"nr5g_tdd_config", "AT+QNWCFG=\"nr5g_tdd_config\""},
	}

	connected := true
	for _, item := range cmds {
		resp, err := d.SendCommand(item.cmd)
		if err != nil {
			log.Printf("Error running %q: %v", item.cmd, err)
			connected = false
			newStatus.RawResponses[item.key] = fmt.Sprintf("Error: %v", err)
			continue
		}
		newStatus.RawResponses[item.key] = resp
	}

	if !connected {
		d.statusMutex.Lock()
		d.status.ConnectionStatus = "Offline"
		d.status.LastUpdated = time.Now()
		d.status.SessionUptime = "Offline"
		d.statusMutex.Unlock()
		d.notifyCallbacks()
		return
	}

	// Parse values
	newStatus.ConnectionStatus = "Connected"
	newStatus.LastUpdated = time.Now()
	newStatus.SessionUptime = FormatDuration(d.client.GetUptime())

	// Parse ATI
	if resp, ok := newStatus.RawResponses["ATI"]; ok {
		parseATI(resp, &newStatus)
	}

	// Parse CPIN
	if resp, ok := newStatus.RawResponses["CPIN"]; ok {
		parseCPIN(resp, &newStatus)
	}

	// Parse CNUM
	if resp, ok := newStatus.RawResponses["CNUM"]; ok {
		parseCNUM(resp, &newStatus)
	}

	// Parse COPS
	if resp, ok := newStatus.RawResponses["COPS"]; ok {
		parseCOPS(resp, &newStatus)
	}

	// Parse CSQ
	if resp, ok := newStatus.RawResponses["CSQ"]; ok {
		parseCSQ(resp, &newStatus)
	}

	// Parse C5GREG/CEREG/CGREG
	regResp := ""
	if strings.Contains(newStatus.RawResponses["C5GREG"], ",1") || strings.Contains(newStatus.RawResponses["C5GREG"], ",5") {
		regResp = newStatus.RawResponses["C5GREG"]
	} else if strings.Contains(newStatus.RawResponses["CEREG"], ",1") || strings.Contains(newStatus.RawResponses["CEREG"], ",5") {
		regResp = newStatus.RawResponses["CEREG"]
	} else {
		regResp = newStatus.RawResponses["CGREG"]
	}
	parseRegistration(regResp, &newStatus)

	// Parse CGPADDR
	if resp, ok := newStatus.RawResponses["CGPADDR"]; ok {
		parseCGPADDR(resp, &newStatus)
	}

	// Parse QENG (Serving Cell)
	if resp, ok := newStatus.RawResponses["QENG"]; ok {
		parseServingCell(resp, &newStatus)
	}

	// Parse MIMO and TDD slot ratio info
	parseMimoLayers(&newStatus)
	parseTddSlotRatio(&newStatus)
	parseAdvancedConnectionInfo(&newStatus)

	// Calculate Signal Percentage
	newStatus.SignalPercentage = calculateSignalPercentage(&newStatus)

	// Poll SMS
	smsList, err := d.pollSMS()
	if err != nil {
		log.Printf("Error polling SMS: %v", err)
	} else {
		newStatus.SMS = smsList
	}

	// Save to cache
	d.statusMutex.Lock()
	d.status = newStatus
	d.statusMutex.Unlock()

	d.notifyCallbacks()
}

// Parsing Helpers

func parseATI(output string, status *ModemStatus) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "OK") || strings.HasPrefix(line, "ATI") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "revision:") {
			status.Revision = strings.TrimSpace(line[9:])
		} else if strings.Contains(strings.ToLower(line), "quectel") {
			status.Manufacturer = "Quectel"
		} else if strings.HasPrefix(line, "RM5") || strings.HasPrefix(line, "RM6") || strings.HasPrefix(line, "RG5") {
			status.Model = line
		} else if status.Model == "" && !strings.Contains(line, "Revision:") && len(line) > 3 {
			status.Model = line
		}
	}
}

func parseCPIN(output string, status *ModemStatus) {
	if strings.Contains(output, "+CPIN: READY") {
		status.SimState = "READY"
	} else if strings.Contains(output, "+CPIN:") {
		idx := strings.Index(output, "+CPIN:")
		parts := strings.Split(output[idx:], ":")
		if len(parts) >= 2 {
			status.SimState = strings.TrimSpace(strings.Split(parts[1], "\n")[0])
		}
	} else {
		status.SimState = "UNKNOWN"
	}
}

func parseCOPS(output string, status *ModemStatus) {
	idx := strings.Index(output, "+COPS:")
	if idx == -1 {
		status.Carrier = "Unknown"
		return
	}
	line := strings.TrimSpace(output[idx:])
	parts := strings.Split(line, ",")
	if len(parts) >= 3 {
		carrier := parts[2]
		carrier = strings.Trim(carrier, "\"")
		status.Carrier = carrier
	} else {
		status.Carrier = "Unknown"
	}
}

func parseCSQ(output string, status *ModemStatus) {
	idx := strings.Index(output, "+CSQ:")
	if idx == -1 {
		status.SignalCSQ = 99
		return
	}
	
	// Remove all spaces and tabs to make parsing robust
	cleaned := strings.ReplaceAll(output[idx:], " ", "")
	cleaned = strings.ReplaceAll(cleaned, "\t", "")

	var rssi, ber int
	_, err := fmt.Sscanf(cleaned, "+CSQ:%d,%d", &rssi, &ber)
	if err == nil {
		status.SignalCSQ = rssi
	} else {
		status.SignalCSQ = 99
	}
}

func parseRegistration(output string, status *ModemStatus) {
	if strings.Contains(output, ",1") || strings.Contains(output, ",5") {
		status.NetworkRegistration = "Registered"
	} else {
		status.NetworkRegistration = "Not Registered"
	}
}

func parseServingCell(output string, status *ModemStatus) {
	lines := strings.Split(output, "\n")

	// Determine the overall technology first by scanning all lines
	hasNSA := false
	hasSA := false
	hasLTE := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+QENG:") {
			continue
		}
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
		status.Tech = "NR5G-SA"
	} else if hasNSA {
		status.Tech = "5G NSA"
	} else if hasLTE {
		status.Tech = "LTE"
	} else {
		status.Tech = "Unknown"
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+QENG:") {
			continue
		}
		// Strip "+QENG:" and optional leading space
		content := line[6:]
		if strings.HasPrefix(content, " ") {
			content = content[1:]
		}
		parts := strings.Split(content, ",")
		for i := range parts {
			parts[i] = strings.Trim(strings.TrimSpace(parts[i]), "\"")
		}

		if len(parts) < 3 {
			continue
		}

		if parts[0] == "servingcell" {
			state := parts[1]
			if state == "NOCONN" {
				status.ConnectionState = "Connected (Idle)"
			} else if state == "CONNECT" {
				status.ConnectionState = "Connected (Active)"
			} else {
				status.ConnectionState = state
			}
			tech := parts[2]

			// If technology wasn't set by pre-scanning, fall back
			if status.Tech == "Unknown" || status.Tech == "" {
				status.Tech = tech
			}

			switch tech {
			case "LTE":
				if len(parts) >= 17 {
					status.ServingCell.MCC = parts[4]
					status.ServingCell.MNC = parts[5]
					status.ServingCell.CellID = parts[6]
					status.ServingCell.PCI = parts[7]
					status.ServingCell.EARFCN = parts[8]
					status.ServingCell.Band = parts[9]
					status.ServingCell.RSRP = parts[13]
					status.ServingCell.RSRQ = parts[14]
					status.ServingCell.RSSI = parts[15]
					status.ServingCell.SINR = parts[16]
				}
			case "NR5G-SA":
				if len(parts) >= 15 {
					status.ServingCell.MCC = parts[4]
					status.ServingCell.MNC = parts[5]
					status.ServingCell.CellID = parts[6]
					status.ServingCell.PCI = parts[7]
					status.ServingCell.ARFCN = parts[9]
					status.ServingCell.Band = parts[10]
					status.ServingCell.RSRP = parts[12]
					status.ServingCell.RSRQ = parts[13]
					status.ServingCell.SINR = parts[14]

					// Also populate 5G-specific fields since the primary link is 5G SA
					status.ServingCell.NR5GBand = parts[10]
					status.ServingCell.NR5GRSRP = parts[12]
					status.ServingCell.NR5GRSRQ = parts[13]
					status.ServingCell.NR5GSINR = parts[14]
				}
			}
		} else if parts[0] == "NR5G-NSA" {
			if len(parts) >= 9 {
				status.ServingCell.NR5GBand = parts[8]
				status.ServingCell.NR5GRSRP = parts[4]
				status.ServingCell.NR5GRSRQ = parts[6]
				status.ServingCell.NR5GSINR = parts[5]
			}
		}
	}
}

func parseIPAddress(ipStr string, status *ModemStatus) {
	ipStr = strings.Trim(strings.TrimSpace(ipStr), "\"")
	if ipStr == "" {
		return
	}

	parts := strings.Split(ipStr, ".")
	if len(parts) == 4 {
		// IPv4 (make sure it's not unspecified)
		if ipStr != "0.0.0.0" {
			status.IPAddress = ipStr
		}
	} else if len(parts) == 16 {
		// IPv6 represented as 16 decimal octets separated by dots (e.g. Quectel RGMII format)
		var octets [16]byte
		valid := true
		for i := 0; i < 16; i++ {
			var val int
			_, err := fmt.Sscanf(parts[i], "%d", &val)
			if err != nil || val < 0 || val > 255 {
				valid = false
				break
			}
			octets[i] = byte(val)
		}
		if valid {
			// Convert to standard IPv6 form
			ipv6 := fmt.Sprintf("%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x",
				octets[0], octets[1], octets[2], octets[3],
				octets[4], octets[5], octets[6], octets[7],
				octets[8], octets[9], octets[10], octets[11],
				octets[12], octets[13], octets[14], octets[15])
			netIP := net.ParseIP(ipv6)
			if netIP != nil && !netIP.IsUnspecified() {
				status.IPv6Address = netIP.String()
			}
		}
	} else if strings.Contains(ipStr, ":") {
		// Standard IPv6 format
		netIP := net.ParseIP(ipStr)
		if netIP != nil && !netIP.IsUnspecified() {
			status.IPv6Address = netIP.String()
		}
	}
}

func parseCGPADDR(output string, status *ModemStatus) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+CGPADDR:") {
			continue
		}
		
		content := line[9:]
		parts := strings.Split(content, ",")
		if len(parts) < 2 {
			continue
		}

		// First IP address part
		parseIPAddress(parts[1], status)

		// Second IP address part (if present)
		if len(parts) >= 3 {
			parseIPAddress(parts[2], status)
		}
	}
}

func calculateSignalPercentage(status *ModemStatus) int {
	// CSQ range: 0 to 31 (99 is unknown)
	if status.SignalCSQ >= 0 && status.SignalCSQ <= 31 {
		return int((float64(status.SignalCSQ) / 31.0) * 100)
	}

	// Fallback to RSRP from serving cell if CSQ is unknown (99)
	rsrpStr := ""
	if status.Tech == "5G NSA" && status.ServingCell.NR5GRSRP != "" {
		rsrpStr = status.ServingCell.NR5GRSRP
	} else if status.ServingCell.RSRP != "" {
		rsrpStr = status.ServingCell.RSRP
	} else if status.ServingCell.NR5GRSRP != "" {
		rsrpStr = status.ServingCell.NR5GRSRP
	}

	if rsrpStr != "" {
		var rsrp int
		_, err := fmt.Sscanf(rsrpStr, "%d", &rsrp)
		if err == nil {
			// RSRP ranges typically from -140 (poor) to -44 (excellent).
			// We map -120 dBm (or worse) to 0% and -80 dBm (or better) to 100%.
			if rsrp >= -80 {
				return 100
			} else if rsrp <= -120 {
				return 0
			} else {
				return int((float64(rsrp - (-120)) / 40.0) * 100)
			}
		}
	}
	return 0
}

func parseMimoLayers(status *ModemStatus) {
	status.MimoLayers = "Unknown"

	// Check 5G first
	if resp, ok := status.RawResponses["nr5g_mimo_layers"]; ok {
		cleaned := strings.ReplaceAll(resp, " ", "")
		cleaned = strings.ReplaceAll(cleaned, "\t", "")
		idx := strings.Index(cleaned, "+QNWCFG:\"nr5g_mimo_layers\",")
		if idx != -1 {
			var mode, layers int
			_, err := fmt.Sscanf(cleaned[idx:], "+QNWCFG:\"nr5g_mimo_layers\",%d,%d", &mode, &layers)
			if err == nil {
				status.MimoLayers = fmt.Sprintf("%dx%d MIMO", layers, layers)
				return
			}
		}
	}

	// Check LTE fallback
	if resp, ok := status.RawResponses["lte_mimo_layers"]; ok {
		cleaned := strings.ReplaceAll(resp, " ", "")
		cleaned = strings.ReplaceAll(cleaned, "\t", "")
		idx := strings.Index(cleaned, "+QNWCFG:\"lte_mimo_layers\",")
		if idx != -1 {
			var mode, layers int
			_, err := fmt.Sscanf(cleaned[idx:], "+QNWCFG:\"lte_mimo_layers\",%d,%d", &mode, &layers)
			if err == nil {
				status.MimoLayers = fmt.Sprintf("%dx%d MIMO", layers, layers)
				return
			}
		}
	}
}

func parseTddSlotRatio(status *ModemStatus) {
	if status.Tech == "NR5G-SA" && status.Carrier == "Jio True5G" {
		status.TddSlotRatio = "16 DL / 4 UL (4:1)"
	} else if status.Tech == "NR5G-SA" {
		status.TddSlotRatio = "Network Controlled"
	} else if strings.Contains(status.Tech, "LTE") {
		status.TddSlotRatio = "LTE Controlled"
	} else {
		status.TddSlotRatio = "N/A"
	}
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
		log.Printf("Error polling SMS: %v", err)
		return
	}
	d.statusMutex.Lock()
	d.status.SMS = smsList
	d.status.LastUpdated = time.Now()
	d.statusMutex.Unlock()
}

// pollSMS retrieves SMS messages from the modem.
func (d *Daemon) pollSMS() ([]SMSMessage, error) {
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
func parseSMSList(resp string) []SMSMessage {
	var messages []SMSMessage
	var current *SMSMessage
	
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
				current = &SMSMessage{
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

// parseQNWCFGString parses a QNWCFG response and returns the comma-separated arguments as a slice of strings.
func parseQNWCFGString(resp string, paramName string) []string {
	// First clean spaces/tabs
	cleaned := strings.ReplaceAll(resp, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "\t", "")
	cleaned = strings.ReplaceAll(cleaned, "\r", "")
	cleaned = strings.ReplaceAll(cleaned, "\n", "")

	// Look for +QNWCFG:"paramName", or +QNWCFG:paramName,
	prefixWithQuotes := fmt.Sprintf("+QNWCFG:\"%s\",", paramName)
	prefixWithoutQuotes := fmt.Sprintf("+QNWCFG:%s,", paramName)

	var startIdx int
	if idx := strings.Index(cleaned, prefixWithQuotes); idx != -1 {
		startIdx = idx + len(prefixWithQuotes)
	} else if idx := strings.Index(cleaned, prefixWithoutQuotes); idx != -1 {
		startIdx = idx + len(prefixWithoutQuotes)
	} else {
		return nil
	}

	// Extract the arguments part
	argsStr := cleaned[startIdx:]
	// Strip trailing OK if present
	if strings.HasSuffix(argsStr, "OK") {
		argsStr = strings.TrimSuffix(argsStr, "OK")
	}

	if argsStr == "" {
		return nil
	}

	return strings.Split(argsStr, ",")
}

func formatMcs(args []string) string {
	if len(args) == 0 {
		return "N/A"
	}
	enabled := "Disabled"
	if args[0] == "1" {
		enabled = "Enabled"
	} else if args[0] == "0" {
		enabled = "Disabled"
	} else {
		enabled = args[0]
	}

	if len(args) < 2 {
		return enabled
	}

	mcs := args[1]
	mod := ""
	if len(args) >= 3 {
		modVal := args[2]
		switch modVal {
		case "1":
			mod = "BPSK"
		case "2":
			mod = "QPSK"
		case "4":
			mod = "16QAM"
		case "6":
			mod = "64QAM"
		case "8":
			mod = "256QAM"
		default:
			mod = modVal
		}
	}

	if mod != "" {
		return fmt.Sprintf("%s (MCS: %s, Mod: %s)", enabled, mcs, mod)
	}
	return fmt.Sprintf("%s (MCS: %s)", enabled, mcs)
}

func formatCsi(args []string) string {
	if len(args) == 0 {
		return "N/A"
	}
	var parts []string
	labels := []string{"MCS", "RI", "CQI", "PMI"}
	for i, val := range args {
		if i < len(labels) {
			parts = append(parts, fmt.Sprintf("%s: %s", labels[i], val))
		} else {
			parts = append(parts, fmt.Sprintf("Val%d: %s", i+1, val))
		}
	}
	return strings.Join(parts, ", ")
}

func formatTxPower(args []string) string {
	if len(args) == 0 {
		return "N/A"
	}
	if len(args) == 1 {
		return args[0] + " dBm"
	}
	var parts []string
	labels := []string{"PUCCH", "PUSCH", "PRACH", "SRS"}
	for i, val := range args {
		if i < len(labels) {
			parts = append(parts, fmt.Sprintf("%s: %s dBm", labels[i], val))
		} else {
			parts = append(parts, fmt.Sprintf("Val%d: %s dBm", i+1, val))
		}
	}
	return strings.Join(parts, ", ")
}

func parseAdvancedConnectionInfo(status *ModemStatus) {
	status.Nr5gUlMcs = "N/A"
	status.Nr5gDlMcs = "N/A"
	status.Nr5gCsi = "N/A"
	status.LteCsi = "N/A"
	status.Nr5gTxPwr = "N/A"
	status.LteTxPwr = "N/A"

	// Parse nr5g_ulMCS
	if resp, ok := status.RawResponses["nr5g_ulMCS"]; ok {
		if args := parseQNWCFGString(resp, "nr5g_ulMCS"); len(args) > 0 {
			status.Nr5gUlMcs = formatMcs(args)
		}
	}

	// Parse nr5g_dlMCS
	if resp, ok := status.RawResponses["nr5g_dlMCS"]; ok {
		if args := parseQNWCFGString(resp, "nr5g_dlMCS"); len(args) > 0 {
			status.Nr5gDlMcs = formatMcs(args)
		}
	}

	// Parse nr5g_csi
	if resp, ok := status.RawResponses["nr5g_csi"]; ok {
		if args := parseQNWCFGString(resp, "nr5g_csi"); len(args) > 0 {
			status.Nr5gCsi = formatCsi(args)
		}
	}

	// Parse lte_csi
	if resp, ok := status.RawResponses["lte_csi"]; ok {
		if args := parseQNWCFGString(resp, "lte_csi"); len(args) > 0 {
			status.LteCsi = formatCsi(args)
		}
	}

	// Parse nr5g_tx_pwr
	if resp, ok := status.RawResponses["nr5g_tx_pwr"]; ok {
		if args := parseQNWCFGString(resp, "nr5g_tx_pwr"); len(args) > 0 {
			status.Nr5gTxPwr = formatTxPower(args)
		}
	}

	// Parse lte_tx_pwr
	if resp, ok := status.RawResponses["lte_tx_pwr"]; ok {
		if args := parseQNWCFGString(resp, "lte_tx_pwr"); len(args) > 0 {
			status.LteTxPwr = formatTxPower(args)
		}
	}
}

func parseCNUM(output string, status *ModemStatus) {
	status.SimNumber = "N/A"

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "OK") || strings.HasPrefix(line, "ERROR") {
			continue
		}
		idx := strings.Index(line, "+CNUM:")
		if idx != -1 {
			argsStr := strings.TrimSpace(line[idx+len("+CNUM:"):])
			parts := splitCSV(argsStr)
			if len(parts) >= 2 {
				num := strings.Trim(strings.TrimSpace(parts[1]), "\"")
				if num != "" {
					status.SimNumber = num
					return
				}
			}
		}
	}
}
