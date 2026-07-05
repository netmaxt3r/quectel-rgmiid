package commands

import (
	"testing"
)

func TestDecodeUCS2Hex(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Valid Malayalam UCS-2 hex
		{
			input:    "0D280D3F0D190D4D0D19",
			expected: "നിങ്ങ",
		},
		// Valid emoji UCS-2 hex (surrogate pairs)
		{
			input:    "D83EDD70",
			expected: "🥰",
		},
		// Valid ASCII UCS-2 hex
		{
			input:    "0068007400740070",
			expected: "http",
		},
		// Standard ASCII text should NOT be modified
		{
			input:    "eeeeeeeee",
			expected: "eeeeeeeee",
		},
		// Short English words that look like hex should NOT be modified
		{
			input:    "cafe",
			expected: "cafe",
		},
		{
			input:    "beef",
			expected: "beef",
		},
		{
			input:    "deadbeef",
			expected: "deadbeef",
		},
		// Non-hex strings should NOT be modified
		{
			input:    "+911234567890",
			expected: "+911234567890",
		},
	}

	for _, tt := range tests {
		actual := decodeUCS2Hex(tt.input)
		if actual != tt.expected {
			t.Errorf("decodeUCS2Hex(%q) = %q, expected %q", tt.input, actual, tt.expected)
		}
	}
}

func TestDecodeSMSField(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Valid Decimal ASCII representation of "JM-ISATHI-G"
		{
			input:    "7477457383658472734571",
			expected: "JM-ISATHI-G",
		},
		// Valid Decimal ASCII representation of "JM-TRAIND-G"
		{
			input:    "7477458482657378684571",
			expected: "JM-TRAIND-G",
		},
		// Normal phone number should NOT be modified
		{
			input:    "911234567890",
			expected: "911234567890",
		},
		// UCS-2 Hex should decode correctly
		{
			input:    "D83EDD70",
			expected: "🥰",
		},
	}

	for _, tt := range tests {
		actual := decodeSMSField(tt.input)
		if actual != tt.expected {
			t.Errorf("decodeSMSField(%q) = %q, expected %q", tt.input, actual, tt.expected)
		}
	}
}

func TestSMSList_ParseResponse_UCS2(t *testing.T) {
	parser := &SMSList{}
	resp := []string{
		`+CMGL: 2,"REC READ","7477457383658472734571",,"26/07/03,12:55:35+22"`,
		`0068007400740070`,
		`+CMGL: 13,"REC READ","+911234567890",,"26/07/05,18:19:43+22"`,
		`D83EDD70`,
	}

	parser.ParseResponse(nil, nil, resp, "")

	if len(parser.SMS) != 2 {
		t.Fatalf("expected 2 SMS messages, got %d", len(parser.SMS))
	}

	// Reverser is applied, so newest/highest index comes first
	m1 := parser.SMS[0]
	if m1.Index != 13 || m1.Content != "🥰" || m1.Sender != "+911234567890" {
		t.Errorf("unexpected first message: %+v", m1)
	}

	m2 := parser.SMS[1]
	if m2.Index != 2 || m2.Content != "http" || m2.Sender != "JM-ISATHI-G" {
		t.Errorf("unexpected second message: %+v", m2)
	}
}

func TestSMSList_ParseResponse_Concatenation(t *testing.T) {
	parser := &SMSList{}
	part1 := "a123456789012345678901234567890123456789012345678901234567890123456" // exactly 67 characters
	part2 := "b123456789012345678901234567890123456789012345678901234567890123456" // exactly 67 characters
	part3 := "part3"

	resp := []string{
		// Different timestamps but within 5 seconds -> should merge because previous segments are full (67 chars)
		`+CMGL: 2,"REC READ","JM-ISATHI-G",,"26/07/03,12:55:35+22"`,
		part1,
		`+CMGL: 3,"REC READ","JM-ISATHI-G",,"26/07/03,12:55:36+22"`,
		part2,
		`+CMGL: 4,"REC READ","JM-ISATHI-G",,"26/07/03,12:55:38+22"`,
		part3,
		// Different timestamp far apart (> 5 seconds) -> should NOT merge
		`+CMGL: 5,"REC READ","JM-ISATHI-G",,"26/07/03,12:56:00+22"`,
		`separate`,
	}

	parser.ParseResponse(nil, nil, resp, "")

	if len(parser.SMS) != 2 {
		t.Fatalf("expected 2 SMS messages, got %d", len(parser.SMS))
	}

	// Reverser is applied, so newest/highest index comes first (index 5 comes first)
	m1 := parser.SMS[0]
	if m1.Index != 5 || m1.Content != "separate" {
		t.Errorf("unexpected first message: %+v", m1)
	}

	m2 := parser.SMS[1]
	if m2.Index != 2 {
		t.Errorf("expected merged message index to be 2, got %d", m2.Index)
	}
	expectedContent := part1 + part2 + part3
	if m2.Content != expectedContent {
		t.Errorf("expected merged content to be %q, got %q", expectedContent, m2.Content)
	}
	if len(m2.MergedIndices) != 3 || m2.MergedIndices[0] != 2 || m2.MergedIndices[1] != 3 || m2.MergedIndices[2] != 4 {
		t.Errorf("unexpected MergedIndices: %v", m2.MergedIndices)
	}
}

func TestSMSList_ParseResponse_Separate(t *testing.T) {
	parser := &SMSList{}
	resp := []string{
		// Two separate short messages (not full segment capacity) sent within 2 seconds -> should NOT merge
		`+CMGL: 6,"REC READ","JM-ISATHI-G",,"26/07/03,12:55:35+22"`,
		`Short msg 1`,
		`+CMGL: 7,"REC READ","JM-ISATHI-G",,"26/07/03,12:55:37+22"`,
		`Short msg 2`,
	}

	parser.ParseResponse(nil, nil, resp, "")

	if len(parser.SMS) != 2 {
		t.Fatalf("expected 2 SMS messages, got %d", len(parser.SMS))
	}

	m1 := parser.SMS[0]
	if m1.Index != 7 || m1.Content != "Short msg 2" {
		t.Errorf("unexpected first message: %+v", m1)
	}

	m2 := parser.SMS[1]
	if m2.Index != 6 || m2.Content != "Short msg 1" {
		t.Errorf("unexpected second message: %+v", m2)
	}
}

type mockDeleteConnection struct {
	executedCmds []string
}

func (m *mockDeleteConnection) ExecuteATCommand(ctx *ParsingContext, cmd ATCommand) (string, error) {
	m.executedCmds = append(m.executedCmds, cmd.Command)
	return "OK", nil
}

func (m *mockDeleteConnection) StartInteractive() (InteractiveSession, error) {
	return nil, nil
}

func TestDeleteSMS(t *testing.T) {
	conn := &mockDeleteConnection{}
	smsList := []SMSMessage{
		{
			Index:         2,
			Sender:        "JM-ISATHI-G",
			Content:       "part1part2",
			MergedIndices: []int{2, 3},
		},
		{
			Index:   5,
			Sender:  "+911234567890",
			Content: "hello",
		},
	}

	// 1. Delete a merged SMS (should delete index 2 and index 3)
	err := DeleteSMS(conn, 2, smsList)
	if err != nil {
		t.Fatalf("DeleteSMS failed: %v", err)
	}
	expectedCmds := []string{"AT+CMGD=2", "AT+CMGD=3"}
	if len(conn.executedCmds) != 2 || conn.executedCmds[0] != expectedCmds[0] || conn.executedCmds[1] != expectedCmds[1] {
		t.Errorf("unexpected executed commands: %v", conn.executedCmds)
	}

	// 2. Delete a single SMS (should delete index 5)
	conn.executedCmds = nil
	err = DeleteSMS(conn, 5, smsList)
	if err != nil {
		t.Fatalf("DeleteSMS failed: %v", err)
	}
	expectedCmds2 := []string{"AT+CMGD=5"}
	if len(conn.executedCmds) != 1 || conn.executedCmds[0] != expectedCmds2[0] {
		t.Errorf("unexpected executed commands: %v", conn.executedCmds)
	}
}
