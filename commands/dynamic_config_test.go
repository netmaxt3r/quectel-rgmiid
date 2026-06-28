package commands

import (
	"reflect"
	"testing"
)

func TestParseSubcommandFormat(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantArgs []string
		wantErr  bool
	}{
		{
			input:    `"WWAN",(0,1),(1-42),<IP_family>,<IP_address>`,
			wantName: "WWAN",
			wantArgs: []string{"(0,1)", "(1-42)", "<IP_family>", "<IP_address>"},
			wantErr:  false,
		},
		{
			input:    `"VLAN",(2-255),("enable","disable"),(1-3,11-13)`,
			wantName: "VLAN",
			wantArgs: []string{"(2-255)", `("enable","disable")`, "(1-3,11-13)"},
			wantErr:  false,
		},
		{
			input:    `"LAN",<IP_address>`,
			wantName: "LAN",
			wantArgs: []string{"<IP_address>"},
			wantErr:  false,
		},
		{
			input:    `"IPPT_NAT",(0,1)`,
			wantName: "IPPT_NAT",
			wantArgs: []string{"(0,1)"},
			wantErr:  false,
		},
		{
			input:    `"connect",(0-3),(0,1)`,
			wantName: "connect",
			wantArgs: []string{"(0-3)", "(0,1)"},
			wantErr:  false,
		},
		{
			input:    `"simple"`,
			wantName: "simple",
			wantArgs: nil,
			wantErr:  false,
		},
		{
			input:    `invalid`,
			wantName: "",
			wantArgs: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		gotName, gotArgs, err := ParseSubcommandFormat(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseSubcommandFormat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if gotName != tt.wantName {
			t.Errorf("ParseSubcommandFormat(%q) gotName = %q, want %q", tt.input, gotName, tt.wantName)
		}
		if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
			t.Errorf("ParseSubcommandFormat(%q) gotArgs = %v, want %v", tt.input, gotArgs, tt.wantArgs)
		}
	}
}

func TestParseDynamicConfigResponse(t *testing.T) {
	resp := `+QMAP: "WWAN",(0,1),(1-42),<IP_family>,<IP_address>
+QMAP: "DMZ",(0,1),(4,6),<IP_address>
+QMAP: "PING",<Server>,(1-10),(1-255)
+QMAP: "DNS",(1-3),<DNS_first_address>,<DNS_second_address>
+QMAP: "VLAN",(2-255),("enable","disable"),(1-3,11-13)
+QMAP: "IPPT_NAT",(0,1)
OK`

	subs := ParseDynamicConfigResponse("QMAP", resp)
	if len(subs) != 6 {
		t.Fatalf("Expected 6 subcommands, got %d", len(subs))
	}

	expected := []struct {
		name string
		args []string
	}{
		{"WWAN", []string{"(0,1)", "(1-42)", "<IP_family>", "<IP_address>"}},
		{"DMZ", []string{"(0,1)", "(4,6)", "<IP_address>"}},
		{"PING", []string{"<Server>", "(1-10)", "(1-255)"}},
		{"DNS", []string{"(1-3)", "<DNS_first_address>", "<DNS_second_address>"}},
		{"VLAN", []string{"(2-255)", `("enable","disable")`, "(1-3,11-13)"}},
		{"IPPT_NAT", []string{"(0,1)"}},
	}

	for i, ext := range expected {
		if subs[i].Name != ext.name {
			t.Errorf("subcommand %d name = %q, want %q", i, subs[i].Name, ext.name)
		}
		if !reflect.DeepEqual(subs[i].Arguments, ext.args) {
			t.Errorf("subcommand %d args = %v, want %v", i, subs[i].Arguments, ext.args)
		}
	}
}
