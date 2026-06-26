package mqtt

import (
	"testing"
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
