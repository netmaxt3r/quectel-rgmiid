package commands

import (
	"reflect"
	"testing"
)

func TestParseNr5gTddInfo(t *testing.T) {
	resp := []string{
		`1,0,6,3,2,6,4`,
		`1,1,4,4,0,0,0`,
	}

	tddInfo := &Nr5gTddInfo{}
	tddInfo.ParseResponse(nil, nil, resp, "")

	expectedPatterns := []TddPattern{
		{
			Enable:       true,
			PatternIndex: 0,
			Periodicity:  "5 ms",
			DlSlots:      3,
			DlSymbols:    2,
			UlSlots:      6,
			UlSymbols:    4,
		},
		{
			Enable:       true,
			PatternIndex: 1,
			Periodicity:  "2 ms",
			DlSlots:      4,
			DlSymbols:    0,
			UlSlots:      0,
			UlSymbols:    0,
		},
	}

	if !reflect.DeepEqual(tddInfo.Patterns, expectedPatterns) {
		t.Errorf("Expected patterns %+v, got %+v", expectedPatterns, tddInfo.Patterns)
	}
}
