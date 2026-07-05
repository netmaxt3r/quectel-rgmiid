package daemon

import (
	"testing"
	"time"

	"rgmii/commands"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		expected string
	}{
		{
			name:     "zero duration",
			input:    0,
			expected: "0s",
		},
		{
			name:     "negative duration",
			input:    -5 * time.Second,
			expected: "0s",
		},
		{
			name:     "seconds only",
			input:    45 * time.Second,
			expected: "45s",
		},
		{
			name:     "minutes and seconds",
			input:    12*time.Minute + 34*time.Second,
			expected: "12m 34s",
		},
		{
			name:     "hours, minutes and seconds",
			input:    2*time.Hour + 15*time.Minute + 3*time.Second,
			expected: "2h 15m 3s",
		},
		{
			name:     "hours and seconds only",
			input:    1*time.Hour + 5*time.Second,
			expected: "1h 0m 5s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDuration(tt.input)
			if got != tt.expected {
				t.Errorf("FormatDuration() = %q, want %q", got, tt.expected)
			}
		})
	}
}



func TestDaemonCallbacks(t *testing.T) {
	d := NewDaemon("127.0.0.1:9999", 1*time.Second)

	ch := make(chan commands.ModemStatus, 2)
	d.OnStatusUpdate(func(status commands.ModemStatus) {
		ch <- status
	})

	// Manually trigger callback notification
	d.notifyCallbacks()

	select {
	case status := <-ch:
		if status.ConnectionStatus != "Offline" {
			t.Errorf("expected ConnectionStatus to be Offline, got %s", status.ConnectionStatus)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for status update callback")
	}
}

func TestSendSMS_Offline(t *testing.T) {
	d := NewDaemon("127.0.0.1:0", 1*time.Second)
	err := d.SendSMS("+1234567890", "Hello World")
	if err == nil {
		t.Fatal("expected error when sending SMS on offline daemon, got nil")
	}
}

func TestSetATIDebug(t *testing.T) {
	d := NewDaemon("127.0.0.1:0", 1*time.Second)

	// Default should be false
	if d.client.Debug {
		t.Errorf("expected default debug to be false, got true")
	}

	d.SetATIDebug(true)
	if !d.client.Debug {
		t.Errorf("expected debug to be true after SetATIDebug(true)")
	}

	d.SetATIDebug(false)
	if d.client.Debug {
		t.Errorf("expected debug to be false after SetATIDebug(false)")
	}
}

func TestDaemonDynamicConfig(t *testing.T) {
	d := NewDaemon("127.0.0.1:0", 1*time.Second)

	cfg := commands.DynamicConfig{Name: "QMap", Command: "QMAP"}
	subcommands := []commands.DynamicSubcommand{
		{
			Name:      "VLAN",
			RawFormat: `"VLAN",(2-255),("enable","disable")`,
			Arguments: []string{"(2-255)", `("enable","disable")`},
		},
	}
	state := commands.NewDynamicConfigState(cfg, subcommands)

	d.dynConfigsState["qmap"] = state

	gotState, ok := d.GetDynamicConfigState("QMap")
	if !ok {
		t.Fatalf("expected to get dynamic config state for QMap")
	}
	if gotState.Config.Command != "QMAP" {
		t.Errorf("expected command to be QMAP, got %s", gotState.Config.Command)
	}

	gotState.SetValue("VLAN", []string{"OK"})
	val := gotState.GetValue("VLAN")
	if len(val) != 1 || val[0] != "OK" {
		t.Errorf("expected value to be OK, got %q", val)
	}

	val2 := gotState.GetValue("vlan")
	if len(val2) != 1 || val2[0] != "OK" {
		t.Errorf("expected case-insensitive value to be OK, got %q", val2)
	}
}
