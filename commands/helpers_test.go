package commands

import (
	"testing"
)

func TestSplitCSV(t *testing.T) {
	parts := SplitCSV(`1,"REC UNREAD","+1234567890",,"26/06/25,23:59:59+22"`)
	if len(parts) < 5 {
		t.Fatalf("expected 5 parts, got %d", len(parts))
	}
	if parts[1] != "REC UNREAD" || parts[2] != "+1234567890" {
		t.Errorf("unexpected parse: %v", parts)
	}
}

func TestParseCSVToStruct_ServingCell5GSA(t *testing.T) {
	input := `"NOCONN","NR5G-SA","TDD",405,86,"123456789",452,"4E","627264",78,-90,-11,12,15,10`
	sc5g := &ServingCell5GSA{}
	err := ParseCSVToStruct(sc5g, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sc5g.State != "NOCONN" {
		t.Errorf("expected State NOCONN, got %q", sc5g.State)
	}
	if sc5g.Tech != "NR5G-SA" {
		t.Errorf("expected Tech NR5G-SA, got %q", sc5g.Tech)
	}
	if sc5g.DuplexMode != "TDD" {
		t.Errorf("expected DuplexMode TDD, got %q", sc5g.DuplexMode)
	}
	if sc5g.MCC != 405 {
		t.Errorf("expected MCC 405, got %d", sc5g.MCC)
	}
	if sc5g.MNC != 86 {
		t.Errorf("expected MNC 86, got %d", sc5g.MNC)
	}
	if sc5g.CellId != "123456789" {
		t.Errorf("expected CellId 123456789, got %q", sc5g.CellId)
	}
	if sc5g.PCID != 452 {
		t.Errorf("expected PCID 452, got %d", sc5g.PCID)
	}
	if sc5g.NrDlArfcn != "4E" {
		t.Errorf("expected NrDlArfcn 4E, got %q", sc5g.NrDlArfcn)
	}
	if sc5g.Band != 627264 {
		t.Errorf("expected Band 627264, got %d", sc5g.Band)
	}
	if sc5g.Tac != "78" {
		t.Errorf("expected Tac 78, got %q", sc5g.Tac)
	}
	if sc5g.RSRP != -90 {
		t.Errorf("expected RSRP -90, got %d", sc5g.RSRP)
	}
	if sc5g.RSRQ != -11 {
		t.Errorf("expected RSRQ -11, got %d", sc5g.RSRQ)
	}
	if sc5g.SINR != 12 {
		t.Errorf("expected SINR 12, got %d", sc5g.SINR)
	}
	if sc5g.TxPower != 15 {
		t.Errorf("expected TxPower 15, got %d", sc5g.TxPower)
	}
	if sc5g.Srxlev != 10 {
		t.Errorf("expected Srxlev 10, got %d", sc5g.Srxlev)
	}
}

func TestParseCSVToStruct_ServingCellLTE(t *testing.T) {
	input := `"CONNECT","LTE","TDD",405,86,"1A2B3C",231,1275,3,"5","5","A1B2",-85,-12,-55,15,18,22,30`
	sclte := &ServingCellLTE{}
	err := ParseCSVToStruct(sclte, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sclte.State != "CONNECT" {
		t.Errorf("expected State CONNECT, got %q", sclte.State)
	}
	if sclte.Tech != "LTE" {
		t.Errorf("expected Tech LTE, got %q", sclte.Tech)
	}
	if sclte.Is_tdd != true {
		t.Errorf("expected Is_tdd true, got %t", sclte.Is_tdd)
	}
	if sclte.MCC != 405 {
		t.Errorf("expected MCC 405, got %d", sclte.MCC)
	}
	if sclte.MNC != 86 {
		t.Errorf("expected MNC 86, got %d", sclte.MNC)
	}
	if sclte.CellId != "1A2B3C" {
		t.Errorf("expected CellId 1A2B3C, got %q", sclte.CellId)
	}
	if sclte.PCID != 231 {
		t.Errorf("expected PCID 231, got %d", sclte.PCID)
	}
	if sclte.EARFCN != 1275 {
		t.Errorf("expected EARFCN 1275, got %d", sclte.EARFCN)
	}
	if sclte.Band != 3 {
		t.Errorf("expected Band 3, got %d", sclte.Band)
	}
	if sclte.Ul_bandwidth != "5" {
		t.Errorf("expected Ul_bandwidth 5, got %q", sclte.Ul_bandwidth)
	}
	if sclte.Dl_bandwidth != "5" {
		t.Errorf("expected Dl_bandwidth 5, got %q", sclte.Dl_bandwidth)
	}
	if sclte.Tac != "A1B2" {
		t.Errorf("expected Tac A1B2, got %q", sclte.Tac)
	}
	if sclte.RSRP != -85 {
		t.Errorf("expected RSRP -85, got %d", sclte.RSRP)
	}
	if sclte.RSRQ != -12 {
		t.Errorf("expected RSRQ -12, got %d", sclte.RSRQ)
	}
	if sclte.RSSI != -55 {
		t.Errorf("expected RSSI -55, got %d", sclte.RSSI)
	}
	if sclte.SINR != 15 {
		t.Errorf("expected SINR 15, got %d", sclte.SINR)
	}
	if sclte.CQI != 18 {
		t.Errorf("expected CQI 18, got %d", sclte.CQI)
	}
	if sclte.TxPower != 22 {
		t.Errorf("expected TxPower 22, got %d", sclte.TxPower)
	}
	if sclte.Srxlev != 30 {
		t.Errorf("expected Srxlev 30, got %d", sclte.Srxlev)
	}
}
