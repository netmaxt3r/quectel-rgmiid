package commands

import (
	"testing"
)


func TestServingCell_ParseRespone(t *testing.T) {
	sc := &ServingCell{}
	lines := []string{`"NOCONN","NR5G-SA","TDD",405,86,"123456789",452,"4E","627264",78,-90,-11,12,15,10`}
	sc.ParseRespone(nil, nil, lines, "")

	if sc.AccessTechnology != "NR5G-SA" {
		t.Errorf("expected AccessTechnology NR5G-SA, got %q", sc.AccessTechnology)
	}
	if sc.ServiceTech != NR5G_SA {
		t.Errorf("expected ServiceTech NR5G_SA, got %v", sc.ServiceTech)
	}
	if sc.NR5GSA == nil {
		t.Fatal("expected NR5GSA to be non-nil")
	}
	if sc.NR5GSA.State != "NOCONN" {
		t.Errorf("expected NR5GSA.State NOCONN, got %q", sc.NR5GSA.State)
	}
}
