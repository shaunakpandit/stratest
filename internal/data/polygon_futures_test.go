package data

import (
	"testing"
	"time"
)

func TestFrontContractsRollForwardOnly(t *testing.T) {
	days := func(volumes ...float64) []Bar {
		var bars []Bar
		for i, volume := range volumes { // Monday 2024-03-11 onwards, stamped like Polygon's session_end_date
			bars = append(bars, Bar{Time: time.Date(2024, 3, 11+i, 0, 0, 0, 0, NY), Close: float64(i + 1), Volume: volume})
		}
		return bars
	}
	// Day 4: the expiring contract briefly out-trades the next one again; that must not roll back.
	chosen := frontContracts([][]Bar{days(100, 90, 40, 30, 0), days(10, 50, 60, 20, 80)})
	if len(chosen[0]) != 2 || len(chosen[1]) != 3 {
		t.Fatalf("held first contract %d sessions and second %d, want 2 and 3", len(chosen[0]), len(chosen[1]))
	}
	got := segments(chosen)
	monday := time.Date(2024, 3, 10, 18, 0, 0, 0, NY)
	if len(got) != 2 || !got[0].From.Equal(monday) || !got[0].To.Equal(got[1].From.Add(-time.Hour)) || got[1].contract != 1 {
		t.Errorf("segments %+v", got)
	}
	if end := got[1].To.In(NY).Format("Mon 15:04"); end != "Fri 17:00" {
		t.Errorf("last segment ends %s, want Fri 17:00", end)
	}
}
