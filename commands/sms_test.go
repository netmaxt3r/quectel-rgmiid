package commands

import (
	"strings"
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
		`+CMGL: 2,1,,34`,
		`00040bd1ca662b390d5291c9d611000862703021555322080068007400740070`,
		`+CMGL: 13,1,,24`,
		`00040c9119214365870900086270508191342204d83edd70`,
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
		`+CMGL: 2,1,,151`,
		`00440bd1ca662b390d5291c9d6110008627030215553228c0500032a03010061003100320033003400350036003700380039003000310032003300340035003600370038003900300031003200330034003500360037003800390030003100320033003400350036003700380039003000310032003300340035003600370038003900300031003200330034003500360037003800390030003100320033003400350036`,
		`+CMGL: 3,1,,151`,
		`00440bd1ca662b390d5291c9d6110008627030215563228c0500032a03020062003100320033003400350036003700380039003000310032003300340035003600370038003900300031003200330034003500360037003800390030003100320033003400350036003700380039003000310032003300340035003600370038003900300031003200330034003500360037003800390030003100320033003400350036`,
		`+CMGL: 4,1,,48`,
		`00440bd1ca662b390d5291c9d611000862703021558322100500032a030300700061007200740033`,
		`+CMGL: 5,1,,34`,
		`00040bd1ca662b390d5291c9d61100006270302165002208f3323c2c0fd3cb`,
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
		`+CMGL: 6,1,,37`,
		`00040bd1ca662b390d5291c9d6110000627030215553220b53f45b4e07b5e767500c`,
		`+CMGL: 7,1,,37`,
		`00040bd1ca662b390d5291c9d6110000627030215573220b53f45b4e07b5e767900c`,
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

func TestUserPDUs(t *testing.T) {
	parser := &SMSList{}
	resp := []string{
		`+CMGL: 0,1,,23`,
		`0791199999999989040C9119999999999900006260722131152204F4F29C0E`,
		`+CMGL: 1,1,,22`,
		`0791199999999989040C9119999999999900006260823293842203E8F71A`,
		`+CMGL: 2,1,,163`,
		`0891191907127005534014D0CA662B390D5291C9D6110008627030215553228C050003A504010D280D3F0D190D4D0D190D330D410D1F0D4600200D2A0D470D300D3F0D7D00200D0E0D240D4D0D3000200D380D3F0D0200200D150D3E0D7C0D210D410D150D7E00200D090D230D4D0D1F0D460D280D4D0D280D4D00200D050D310D3F0D2F0D3E0D7B00200D060D170D4D0D300D390D3F0D150D4D0D150D410D280D4D0D280D410D230D4D0D1F`,
	}

	parser.ParseResponse(nil, nil, resp, "")

	if len(parser.SMS) != 3 {
		t.Fatalf("expected 3 SMS messages, got %d", len(parser.SMS))
	}

	m0 := parser.SMS[0] // Index 2
	if m0.Index != 2 || m0.Sender != "JM-ISATHI-G" || m0.Date != "26/07/03,12:55:35+22" {
		t.Errorf("unexpected message at index 0: %+v", m0)
	}
	if !strings.HasPrefix(m0.Content, "നിങ്ങളുടെ പേരിൽ എത്ര സിം കാർഡുകൾ ഉണ്ടെന്ന് ") {
		t.Errorf("unexpected Malayalam decoded text: %q", m0.Content)
	}

	m1 := parser.SMS[1] // Index 1
	if m1.Index != 1 || m1.Sender != "+919999999999" || m1.Date != "26/06/28,23:39:48+22" {
		t.Errorf("unexpected message at index 1: %+v", m1)
	}

	m2 := parser.SMS[2] // Index 0
	if m2.Index != 0 || m2.Sender != "+919999999999" || m2.Date != "26/06/27,12:13:51+22" || m2.Content != "test" {
		t.Errorf("unexpected message at index 2: %+v", m2)
	}
}

func TestParseSMSList(t *testing.T) {
	resp := []string{
		`+CMGL: 1,0,,24`,
		`00040a9121436587090000626052329595220cc8329bfd065ddf72363904`,
		`+CMGL: 2,1,,70`,
		`000406d1c7f7fbcc2e030000626062005000223cd9775d0eb297e569737a1ca6a7df6ed0f84d2e83d273504c36a3d56c2e45920e4acf41f6303b4d0699df72500dd44ebbebf4f2dc05`,
		`OK`,
	}

	smsList := &SMSList{}
	smsList.ParseResponse(nil, nil, resp, "")
	messages := smsList.SMS

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	expectedContent2 := "Your verification code is 123456.\nIt is valid for 5 minutes."
	if messages[0].Index != 2 || messages[0].Status != "REC READ" || messages[0].Sender != "Google" || messages[0].Date != "26/06/26,00:05:00+22" || messages[0].Content != expectedContent2 {
		t.Errorf("unexpected message 1: %+v", messages[0])
	}

	if messages[1].Index != 1 || messages[1].Status != "REC UNREAD" || messages[1].Sender != "+1234567890" || messages[1].Date != "26/06/25,23:59:59+22" || messages[1].Content != "Hello World!" {
		t.Errorf("unexpected message 2: %+v", messages[1])
	}
}

func TestSMSList_ParseResponse_SortingOrder(t *testing.T) {
	parser := &SMSList{}
	// Feed them in a completely non-chronological order of date and index:
	// 1. Index 1: middle date (26/06/28,23:39:48+22)
	// 2. Index 2: newest date (26/07/03,12:55:35+22)
	// 3. Index 0: oldest date (26/06/27,12:13:51+22)
	resp := []string{
		`+CMGL: 1,1,,22`,
		`0791199999999989040C9119999999999900006260823293842203E8F71A`,
		`+CMGL: 2,1,,163`,
		`0891191907127005534014D0CA662B390D5291C9D6110008627030215553228C050003A504010D280D3F0D190D4D0D190D330D410D1F0D4600200D2A0D470D300D3F0D7D00200D0E0D240D4D0D3000200D380D3F0D0200200D150D3E0D7C0D210D410D150D7E00200D090D230D4D0D1F0D460D280D4D0D280D4D00200D050D310D3F0D2F0D3E0D7B00200D060D170D4D0D300D390D3F0D150D4D0D150D410D280D4D0D280D410D230D4D0D1F`,
		`+CMGL: 0,1,,23`,
		`0791199999999989040C9119999999999900006260722131152204F4F29C0E`,
	}

	parser.ParseResponse(nil, nil, resp, "")

	if len(parser.SMS) != 3 {
		t.Fatalf("expected 3 SMS messages, got %d", len(parser.SMS))
	}

	// Should be sorted by date descending (newest first):
	// 1st: Index 2 (2026-07-03)
	// 2nd: Index 1 (2026-06-28)
	// 3rd: Index 0 (2026-06-27)
	if parser.SMS[0].Index != 2 {
		t.Errorf("expected 1st message to be Index 2, got Index %d", parser.SMS[0].Index)
	}
	if parser.SMS[1].Index != 1 {
		t.Errorf("expected 2nd message to be Index 1, got Index %d", parser.SMS[1].Index)
	}
	if parser.SMS[2].Index != 0 {
		t.Errorf("expected 3rd message to be Index 0, got Index %d", parser.SMS[2].Index)
	}
}

