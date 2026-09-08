package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"rgmii/commands"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Config holds the configuration parameters for the MQTT client.
type Config struct {
	Server          string
	Username        string
	Password        string
	Topic           string
	Discovery       bool
	DiscoveryPrefix string
	ModemAddr       string
}

// Client manages the MQTT connection and handles status publishing.
type Client struct {
	cfg                Config
	client             mqtt.Client
	discoveryPublished bool
	mu                 sync.Mutex
	lastStatus         *commands.ModemStatus
}

// NewClient instantiates a new MQTT client helper.
func NewClient(cfg Config) (*Client, error) {
	if cfg.Topic == "" {
		cfg.Topic = "rgmii"
	}
	if cfg.DiscoveryPrefix == "" {
		cfg.DiscoveryPrefix = "homeassistant"
	}

	c := &Client{
		cfg: cfg,
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(cfg.Server)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}

	// Create a stable client ID based on the modem address
	clientID := "rgmii-daemon"
	if cfg.ModemAddr != "" {
		clientID = fmt.Sprintf("rgmii-daemon-%s", sanitize(cfg.ModemAddr))
	}
	opts.SetClientID(clientID)

	opts.SetAutoReconnect(true)
	opts.SetCleanSession(true)
	opts.SetConnectTimeout(5 * time.Second)

	// Set connection callbacks
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		slog.Info("MQTT connection established", "server", cfg.Server)
		c.mu.Lock()
		last := c.lastStatus
		c.mu.Unlock()
		if last != nil && cfg.Discovery {
			go c.publishDiscovery(*last)
		}
	})

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		slog.Warn("MQTT connection lost", "error", err)
	})

	c.client = mqtt.NewClient(opts)
	return c, nil
}

// Connect starts the connection to the MQTT broker.
// If the initial connection attempt fails, it spawns a background goroutine
// to retry connecting until successful.
func (c *Client) Connect(ctx context.Context) error {
	token := c.client.Connect()
	if token.Wait() && token.Error() != nil {
		err := token.Error()
		slog.Error("MQTT initial connection attempt failed, starting background retry loop", "error", err)
		go c.retryConnectLoop(ctx)
		return err
	}
	return nil
}

func (c *Client) retryConnectLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("MQTT background retry loop stopped due to context cancellation")
			return
		case <-ticker.C:
			slog.Info("Retrying initial MQTT connection", "server", c.cfg.Server)
			token := c.client.Connect()
			if token.Wait() && token.Error() == nil {
				slog.Info("MQTT connection successfully established via background retry", "server", c.cfg.Server)
				return
			} else if token.Error() != nil {
				slog.Warn("MQTT background retry attempt failed", "error", token.Error())
			}
		}
	}
}

// PublishStatus is the callback function that will be registered with the Daemon.
func (c *Client) PublishStatus(status commands.ModemStatus) {
	c.mu.Lock()
	c.lastStatus = &status
	shouldPublishDiscovery := !c.discoveryPublished
	c.mu.Unlock()

	if shouldPublishDiscovery && c.cfg.Discovery {
		// Wait until we have some basic parsed modem info to register the device properly
		if status.Model != "" || status.SimState != "" {
			c.publishDiscovery(status)
			c.mu.Lock()
			c.discoveryPublished = true
			c.mu.Unlock()
		}
	}

	c.publishState(status)
}

type mqttStatusPayload struct {
	commands.ModemStatus
	SMS any `json:"sms,omitempty"`
}

func (c *Client) publishState(status commands.ModemStatus) {
	topic := fmt.Sprintf("%s/status", c.cfg.Topic)
	bytes, err := json.Marshal(mqttStatusPayload{ModemStatus: status})
	if err != nil {
		slog.Error("Failed to marshal status for MQTT", "error", err)
		return
	}

	token := c.client.Publish(topic, 0, false, bytes)
	// We run WaitTimeout to avoid blocking the caller indefinitely on network issues
	token.WaitTimeout(2 * time.Second)
}

func (c *Client) publishDiscovery(status commands.ModemStatus) {
	// deviceId := "rgmii_modem"
	//if status.SimNumber != "" && status.SimNumber != "N/A" {
	//	deviceId = "rgmii_" + sanitize(status.SimNumber)
	//} else {
	deviceId := "rgmii_" + sanitize(c.cfg.ModemAddr)
	//}

	stateTopic := fmt.Sprintf("%s/status", c.cfg.Topic)

	manufacturer := status.Manufacturer
	if manufacturer == "" {
		manufacturer = "Quectel"
	}
	model := status.Model
	if model == "" {
		model = "RGMII Modem"
	}
	revision := status.Revision
	if revision == "" {
		revision = "Unknown"
	}

	device := map[string]interface{}{
		"identifiers":  []string{deviceId},
		"name":         "Quectel RGMII Modem",
		"model":        model,
		"manufacturer": manufacturer,
		"sw_version":   revision,
	}

	// Helper function to publish config payload for a single sensor.
	pubSensor := func(component, sensorID, name string, extraConfig map[string]interface{}) {
		topic := fmt.Sprintf("%s/%s/%s/%s/config", c.cfg.DiscoveryPrefix, component, deviceId, sensorID)

		payload := map[string]interface{}{
			"name":        name,
			"state_topic": stateTopic,
			"unique_id":   fmt.Sprintf("%s_%s", deviceId, sensorID),
			"device":      device,
		}
		for k, v := range extraConfig {
			payload[k] = v
		}

		bytes, err := json.Marshal(payload)
		if err != nil {
			slog.Error("Failed to marshal HA discovery payload", "sensor_id", sensorID, "error", err)
			return
		}

		// Retain is true for Auto Discovery so HA receives it even if it starts after the daemon
		token := c.client.Publish(topic, 0, true, bytes)
		token.WaitTimeout(2 * time.Second)
	}

	// 1. Connection Status (binary_sensor)
	pubSensor("binary_sensor", "connection_status", "Connection Status", map[string]interface{}{
		"device_class":   "connectivity",
		"value_template": "{{ value_json.connection_status }}",
		"payload_on":     "Connected",
		"payload_off":    "Offline",
	})

	// 2. Signal Percentage (sensor)
	pubSensor("sensor", "signal_percentage", "Signal Percentage", map[string]interface{}{
		"unit_of_measurement": "%",
		"icon":                "mdi:signal",
		"value_template":      "{{ value_json.signal_percentage }}",
	})

	// 3. Signal CSQ (sensor)
	pubSensor("sensor", "signal_csq", "Signal CSQ", map[string]interface{}{
		"icon":           "mdi:signal-cellular-outline",
		"value_template": "{{ value_json.signal_csq }}",
	})

	// 4. Network Registration (sensor)
	pubSensor("sensor", "network_registration", "Network Registration", map[string]interface{}{
		"icon":           "mdi:cellphone-arrow-down",
		"value_template": "{{ value_json.network_registration }}",
	})

	// 5. Connection State (sensor)
	pubSensor("sensor", "connection_state", "Connection State", map[string]interface{}{
		"icon":           "mdi:transit-connection-variant",
		"value_template": "{{ value_json.connection_state }}",
	})

	// 6. Network Technology (sensor)
	pubSensor("sensor", "network_technology", "Network Technology", map[string]interface{}{
		"icon":           "mdi:cellular-5g",
		"value_template": "{{ value_json.tech }}",
	})

	// 7. Carrier (sensor)
	pubSensor("sensor", "carrier", "Carrier", map[string]interface{}{
		"icon":           "mdi:office-building",
		"value_template": "{{ value_json.carrier }}",
	})

	// 8. IP Address (sensor)
	pubSensor("sensor", "ip_address", "IP Address", map[string]interface{}{
		"icon":           "mdi:ip",
		"value_template": "{{ value_json.ip_address | join(', ') if value_json.ip_address else 'N/A' }}",
	})

	// 9. IPv6 Address (sensor)
	pubSensor("sensor", "ipv6_address", "IPv6 Address", map[string]interface{}{
		"icon":           "mdi:ip",
		"value_template": "{{ value_json.ipv6_address | join(', ') if value_json.ipv6_address else 'N/A' }}",
	})

	// 10. LTE RSRP (sensor)
	pubSensor("sensor", "lte_rsrp", "LTE RSRP", map[string]interface{}{
		"device_class":        "signal_strength",
		"unit_of_measurement": "dBm",
		"value_template":      "{{ value_json.get('service', {}).get('lte', {}).get('rsrp') or 'N/A' }}",
	})

	// 11. LTE RSRQ (sensor)
	pubSensor("sensor", "lte_rsrq", "LTE RSRQ", map[string]interface{}{
		"unit_of_measurement": "dB",
		"value_template":      "{{ value_json.get('service', {}).get('lte', {}).get('rsrq') or 'N/A' }}",
	})

	// 12. LTE SINR (sensor)
	pubSensor("sensor", "lte_sinr", "LTE SINR", map[string]interface{}{
		"unit_of_measurement": "dB",
		"value_template":      "{{ value_json.get('service', {}).get('lte', {}).get('sinr') or 'N/A' }}",
	})

	// 13. NR5G RSRP (sensor)
	pubSensor("sensor", "nr5g_rsrp", "NR5G RSRP", map[string]interface{}{
		"device_class":        "signal_strength",
		"unit_of_measurement": "dBm",
		"value_template":      "{{ value_json.get('service', {}).get('nr5g_sa', {}).get('rsrp') or value_json.get('service', {}).get('nr5g_nsa', {}).get('rsrp') or 'N/A' }}",
	})

	// 14. NR5G RSRQ (sensor)
	pubSensor("sensor", "nr5g_rsrq", "NR5G RSRQ", map[string]interface{}{
		"unit_of_measurement": "dB",
		"value_template":      "{{ value_json.get('service', {}).get('nr5g_sa', {}).get('rsrq') or value_json.get('service', {}).get('nr5g_nsa', {}).get('rsrq') or 'N/A' }}",
	})

	// 15. NR5G SINR (sensor)
	pubSensor("sensor", "nr5g_sinr", "NR5G SINR", map[string]interface{}{
		"unit_of_measurement": "dB",
		"value_template":      "{{ value_json.get('service', {}).get('nr5g_sa', {}).get('sinr') or value_json.get('service', {}).get('nr5g_nsa', {}).get('sinr') or 'N/A' }}",
	})

	// 16. LTE Band (sensor)
	pubSensor("sensor", "lte_band", "LTE Band", map[string]interface{}{
		"icon":           "mdi:radio-tower",
		"value_template": "{{ value_json.get('service', {}).get('lte', {}).get('band') or 'N/A' }}",
	})

	// 17. NR5G Band (sensor)
	pubSensor("sensor", "nr5g_band", "NR5G Band", map[string]interface{}{
		"icon":           "mdi:radio-tower",
		"value_template": "{{ value_json.get('service', {}).get('nr5g_sa', {}).get('band') or value_json.get('service', {}).get('nr5g_nsa', {}).get('band') or 'N/A' }}",
	})

	// 18. SIM Number (sensor)
	pubSensor("sensor", "sim", "SIM Number", map[string]interface{}{
		"icon":           "mdi:sim",
		"value_template": "{{ value_json.sim_number }}",
	})

	// 19. SIM State (sensor)
	pubSensor("sensor", "sim_state", "SIM State", map[string]interface{}{
		"icon":           "mdi:sim-card",
		"value_template": "{{ value_json.sim_state }}",
	})

	// 20. SMS Messages Count (sensor)
	pubSensor("sensor", "sms_used", "SMS Messages Count", map[string]interface{}{
		"icon":           "mdi:message-text",
		"value_template": "{{ value_json.used }}",
	})

	// 21. SMS Capacity (sensor)
	pubSensor("sensor", "sms_total", "SMS Capacity", map[string]interface{}{
		"icon":           "mdi:message-text-outline",
		"value_template": "{{ value_json.total }}",
	})

	// 22. Session Uptime (sensor)
	pubSensor("sensor", "session_uptime", "Session Uptime", map[string]interface{}{
		"icon":           "mdi:clock-outline",
		"value_template": "{{ value_json.session_uptime }}",
	})

	// 23. MIMO Configuration (sensor)
	pubSensor("sensor", "mimo_configuration", "MIMO Configuration", map[string]interface{}{
		"icon":           "mdi:antenna",
		"value_template": "{% if value_json.tech == 'LTE' and value_json.service.lte is defined %}{{ value_json.service.lte.mimo_layers }}{% elif value_json.tech == 'NR5G-SA' and value_json.service.nr5g_sa is defined %}{{ value_json.service.nr5g_sa.mimo_layers }}{% elif value_json.tech == '5G NSA' and value_json.service.nr5g_nsa is defined %}LTE: {{ value_json.service.lte.mimo_layers if value_json.service.lte is defined else 'Unknown' }} | 5G: {{ value_json.service.nr5g_nsa.mimo_layers }}{% else %}N/A{% endif %}",
	})

	// 24. LTE RSSI (sensor)
	pubSensor("sensor", "lte_rssi", "LTE RSSI", map[string]interface{}{
		"device_class":        "signal_strength",
		"unit_of_measurement": "dBm",
		"value_template":      "{{ value_json.get('service', {}).get('lte', {}).get('rssi') or 'N/A' }}",
	})

	// 25. LTE CQI (sensor)
	pubSensor("sensor", "lte_cqi", "LTE CQI", map[string]interface{}{
		"icon":           "mdi:quality-high",
		"value_template": "{{ value_json.get('service', {}).get('lte', {}).get('cqi') or 'N/A' }}",
	})

	// 26. LTE PCI (sensor)
	pubSensor("sensor", "lte_pcid", "LTE PCI", map[string]interface{}{
		"icon":           "mdi:identifier",
		"value_template": "{{ value_json.get('service', {}).get('lte', {}).get('pcid') or 'N/A' }}",
	})

	// 27. LTE EARFCN (sensor)
	pubSensor("sensor", "lte_earfcn", "LTE EARFCN", map[string]interface{}{
		"icon":           "mdi:radio-tower",
		"value_template": "{{ value_json.get('service', {}).get('lte', {}).get('earfcn') or 'N/A' }}",
	})

	// 28. LTE CSI (sensor)
	pubSensor("sensor", "lte_csi", "LTE CSI", map[string]interface{}{
		"icon":           "mdi:chart-bar",
		"value_template": "{{ value_json.get('service', {}).get('lte', {}).get('csi') or 'N/A' }}",
	})

	// 29. LTE Tx Power (sensor)
	pubSensor("sensor", "lte_tx_power", "LTE Tx Power", map[string]interface{}{
		"icon":           "mdi:transmission-tower",
		"value_template": "{{ value_json.get('service', {}).get('lte', {}).get('tx_pwr') or 'N/A' }}",
	})

	// 30. NR5G PCI (sensor)
	pubSensor("sensor", "nr5g_pcid", "NR5G PCI", map[string]interface{}{
		"icon":           "mdi:identifier",
		"value_template": "{{ value_json.get('service', {}).get('nr5g_sa', {}).get('pcid') or value_json.get('service', {}).get('nr5g_nsa', {}).get('pcid') or 'N/A' }}",
	})

	// 31. NR5G ARFCN (sensor)
	pubSensor("sensor", "nr5g_arfcn", "NR5G ARFCN", map[string]interface{}{
		"icon":           "mdi:radio-tower",
		"value_template": "{{ value_json.get('service', {}).get('nr5g_sa', {}).get('nr_dl_arfcn') or value_json.get('service', {}).get('nr5g_nsa', {}).get('arfcn') or 'N/A' }}",
	})

	// 32. NR5G CSI (sensor)
	pubSensor("sensor", "nr5g_csi", "NR5G CSI", map[string]interface{}{
		"icon":           "mdi:chart-bar",
		"value_template": "{{ value_json.get('service', {}).get('nr5g_sa', {}).get('csi') or value_json.get('service', {}).get('nr5g_nsa', {}).get('csi') or 'N/A' }}",
	})

	// 33. NR5G Tx Power (sensor)
	pubSensor("sensor", "nr5g_tx_power", "NR5G Tx Power", map[string]interface{}{
		"icon":           "mdi:transmission-tower",
		"value_template": "{{ value_json.get('service', {}).get('nr5g_sa', {}).get('tx_pwr') or value_json.get('service', {}).get('nr5g_nsa', {}).get('tx_pwr') or 'N/A' }}",
	})

	// 34. NR5G UL MCS (sensor)
	pubSensor("sensor", "nr5g_ul_mcs", "NR5G UL MCS", map[string]interface{}{
		"icon":           "mdi:speedometer",
		"value_template": "{{ value_json.get('service', {}).get('nr5g_sa', {}).get('nr5g_ul_mcs') or value_json.get('service', {}).get('nr5g_nsa', {}).get('nr5g_ul_mcs') or 'N/A' }}",
	})

	// 35. NR5G DL MCS (sensor)
	pubSensor("sensor", "nr5g_dl_mcs", "NR5G DL MCS", map[string]interface{}{
		"icon":           "mdi:speedometer",
		"value_template": "{{ value_json.get('service', {}).get('nr5g_sa', {}).get('nr5g_dl_mcs') or value_json.get('service', {}).get('nr5g_nsa', {}).get('nr5g_dl_mcs') or 'N/A' }}",
	})

	// 36. Cell ID (sensor)
	pubSensor("sensor", "cell_id", "Cell ID", map[string]interface{}{
		"icon":           "mdi:cellphone-tower",
		"value_template": "{% if value_json.tech == 'LTE' and value_json.service.lte is defined %}{{ value_json.service.lte.cellid }}{% elif value_json.tech == 'NR5G-SA' and value_json.service.nr5g_sa is defined %}{{ value_json.service.nr5g_sa.cellid }}{% elif value_json.tech == '5G NSA' and value_json.service.lte is defined %}{{ value_json.service.lte.cellid }}{% else %}N/A{% endif %}",
	})

	// 37. Raw Tx Power (sensor)
	pubSensor("sensor", "raw_tx_power", "Raw Tx Power", map[string]interface{}{
		"device_class":        "signal_strength",
		"unit_of_measurement": "dBm",
		"value_template":      "{% if value_json.tech == 'LTE' and value_json.service.lte is defined %}{{ value_json.service.lte.tx_power_v }}{% elif value_json.tech == 'NR5G-SA' and value_json.service.nr5g_sa is defined %}{{ value_json.service.nr5g_sa.tx_power_v }}{% elif value_json.tech == '5G NSA' and value_json.service.lte is defined %}{{ value_json.service.lte.tx_power_v }}{% else %}N/A{% endif %}",
	})

	// 38. Srxlev (sensor)
	pubSensor("sensor", "srxlev", "Srxlev", map[string]interface{}{
		"icon":           "mdi:signal-variant",
		"value_template": "{% if value_json.tech == 'LTE' and value_json.service.lte is defined %}{{ value_json.service.lte.srxlev }}{% elif value_json.tech == 'NR5G-SA' and value_json.service.nr5g_sa is defined %}{{ value_json.service.nr5g_sa.srxlev }}{% elif value_json.tech == '5G NSA' and value_json.service.lte is defined %}{{ value_json.service.lte.srxlev }}{% else %}N/A{% endif %}",
	})

	// 39. TDD Pattern (sensor)
	pubSensor("sensor", "tdd_pattern", "TDD Pattern", map[string]interface{}{
		"icon":           "mdi:clock-digital",
		"value_template": "{% set sa_patterns = value_json.service.nr5g_sa.tdd_patterns if (value_json.service.nr5g_sa is defined and value_json.service.nr5g_sa.tdd_patterns is defined) else [] %}{% set nsa_patterns = value_json.service.nr5g_nsa.tdd_patterns if (value_json.service.nr5g_nsa is defined and value_json.service.nr5g_nsa.tdd_patterns is defined) else [] %}{% set patterns = sa_patterns if value_json.tech == 'NR5G-SA' else nsa_patterns %}{% if patterns | length > 0 %}{% set ns = namespace(parts=[]) %}{% for p in patterns %}{% set ns.parts = ns.parts + ['P' ~ (p.pattern_index + 1) ~ ': ' ~ p.periodicity ~ ' (DL: ' ~ p.dl_slots ~ '/' ~ p.dl_symbols ~ ', UL: ' ~ p.ul_slots ~ '/' ~ p.ul_symbols ~ ')'] %}{% endfor %}{{ ns.parts | join(' | ') }}{% else %}N/A{% endif %}",
	})

	slog.Info("Published Home Assistant auto discovery configurations", "device_id", deviceId)
}

// Disconnect gracefully disconnects the MQTT client.
func (c *Client) Disconnect() {
	if c.client != nil && c.client.IsConnected() {
		c.client.Disconnect(250)
	}
}

func sanitize(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	res := sb.String()
	for strings.Contains(res, "__") {
		res = strings.ReplaceAll(res, "__", "_")
	}
	return strings.Trim(res, "_")
}
