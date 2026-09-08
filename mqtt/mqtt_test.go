package mqtt

import (
	"encoding/json"
	"strings"
	"testing"

	"rgmii/commands"
)

func TestSanitize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1:1883", "192_168_1_1_1883"},
		{"tcp://localhost", "tcp_localhost"},
		{"rgmii-modem", "rgmii_modem"},
		{"some__value__here", "some_value_here"},
	}

	for _, tt := range tests {
		actual := sanitize(tt.input)
		if actual != tt.expected {
			t.Errorf("sanitize(%q) = %q, expected %q", tt.input, actual, tt.expected)
		}
	}
}

func TestNewClient(t *testing.T) {
	cfg := Config{
		Server:    "tcp://localhost:1883",
		Topic:     "test",
		ModemAddr: "192.168.1.1",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if client.cfg.Topic != "test" {
		t.Errorf("expected topic to be test, got %s", client.cfg.Topic)
	}
}

func TestMQTTPayloadExcludesSMSData(t *testing.T) {
	status := commands.ModemStatus{
		Tech: "LTE",
		SMSList: commands.SMSList{
			SMS: []commands.SMSMessage{
				{
					Index:   1,
					Sender:  "+1234567890",
					Content: "Super secret verification code 123456",
					Date:    "26/07/03,12:55:35+22",
				},
			},
		},
		SMSCapacity: commands.SMSCapacity{
			Used:  1,
			Total: 255,
		},
	}

	// 1. In standard JSON context (e.g. REST API), SMS data should still be present
	standardJSON, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal standard status: %v", err)
	}
	if !strings.Contains(string(standardJSON), "Super secret") || !strings.Contains(string(standardJSON), `"sms":`) {
		t.Errorf("standard JSON context should retain SMS data, got: %s", string(standardJSON))
	}

	// 2. In MQTT status payload, SMS data should be omitted
	mqttPayloadBytes, err := json.Marshal(mqttStatusPayload{ModemStatus: status})
	if err != nil {
		t.Fatalf("failed to marshal mqtt payload: %v", err)
	}
	mqttPayloadStr := string(mqttPayloadBytes)

	if strings.Contains(mqttPayloadStr, "Super secret") || strings.Contains(mqttPayloadStr, `"sms":`) {
		t.Errorf("MQTT status payload must not contain SMS message data, got: %s", mqttPayloadStr)
	}

	// Verify other fields (e.g. capacity, tech) are still present in MQTT payload
	if !strings.Contains(mqttPayloadStr, `"used":1`) || !strings.Contains(mqttPayloadStr, `"tech":"LTE"`) {
		t.Errorf("MQTT payload missing expected modem fields, got: %s", mqttPayloadStr)
	}
}
