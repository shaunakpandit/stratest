package data

import (
	"math"
	"testing"
	"time"
)

// session returns 15m bars for one NY session; Close and Volume are the bar index + offset.
func session(year int, month time.Month, day int, offset float64) []Bar {
	var bars []Bar
	open := time.Date(year, month, day, 9, 30, 0, 0, NY)
	for i := range 26 {
		v := offset + float64(i)
		bars = append(bars, Bar{Time: open.Add(time.Duration(i) * 15 * time.Minute).UTC(), Open: v, High: v + 0.5, Low: v - 0.5, Close: v, Volume: 1})
	}
	return bars
}

func TestAlignNoLookahead(t *testing.T) {
	bars := append(session(2024, 3, 4, 0), session(2024, 3, 5, 100)...)
	d := NewData(RegularHours, "15m", bars, []string{"15m", "4h"})
	h4 := d.TF("4h").Close
	if len(h4) != d.Len() {
		t.Fatalf("aligned length %d, want %d", len(h4), d.Len())
	}
	// 4h buckets: bars 0-15 (09:30-13:30) and 16-25 (13:30-16:00).
	for i := 0; i < 15; i++ {
		if !math.IsNaN(h4[i]) {
			t.Fatalf("bar %d sees %v before the first 4h bar completed", i, h4[i])
		}
	}
	// Bar 15 closes at 13:30, the same instant as the first 4h bar, so it may see it.
	expect := map[int]float64{15: 15, 16: 15, 20: 15, 24: 15, 25: 25, 26: 25, 40: 25, 41: 115, 51: 125}
	for i, want := range expect {
		if h4[i] != want {
			t.Errorf("bar %d sees 4h close %v, want %v", i, h4[i], want)
		}
	}
	slow := d.Ind("4h", func(f Frame) []float64 { return f.Close })
	if slow[16] != 15 || !math.IsNaN(slow[14]) {
		t.Errorf("Ind should align like TF: got %v, %v", slow[14], slow[16])
	}
}

func TestResampleSessionAnchored(t *testing.T) {
	hourly := RegularHours.Resample(session(2024, 3, 4, 0), "1h")
	if len(hourly) != 7 {
		t.Fatalf("got %d hourly bars, want 7", len(hourly))
	}
	first, last := hourly[0], hourly[6]
	if got := first.Time.In(NY).Format("15:04"); got != "09:30" {
		t.Errorf("first bucket starts %s, want 09:30", got)
	}
	if first.Open != 0 || first.Close != 3 || first.High != 3.5 || first.Low != -0.5 || first.Volume != 4 {
		t.Errorf("bad OHLCV aggregation: %+v", first)
	}
	if got := last.Time.In(NY).Format("15:04"); got != "15:30" || last.Volume != 2 {
		t.Errorf("partial last bucket: start %s volume %v, want 15:30 and 2", got, last.Volume)
	}
	if end := RegularHours.End("1h", last.Time).In(NY).Format("15:04"); end != "16:00" {
		t.Errorf("partial bucket ends %s, want 16:00", end)
	}

	four := RegularHours.Resample(session(2024, 3, 4, 0), "4h")
	if len(four) != 2 || four[0].Volume != 16 || four[1].Volume != 10 || four[1].Time.In(NY).Format("15:04") != "13:30" {
		t.Errorf("4h buckets wrong: %+v", four)
	}
}

func TestResampleAcrossDST(t *testing.T) {
	// DST started on Sunday 2024-03-10: the open moves from 14:30 UTC to 13:30 UTC.
	bars := append(session(2024, 3, 8, 0), session(2024, 3, 11, 100)...)
	hourly := RegularHours.Resample(bars, "1h")
	if len(hourly) != 14 {
		t.Fatalf("got %d hourly bars, want 14 (no bucket spans sessions)", len(hourly))
	}
	if hourly[0].Time.Hour() != 14 || hourly[7].Time.Hour() != 13 {
		t.Errorf("UTC opens %v and %v, want 14:30 and 13:30", hourly[0].Time, hourly[7].Time)
	}
	for _, b := range hourly {
		if b.Time.In(NY).Minute() != 30 {
			t.Errorf("bucket %v is not anchored at :30 NY", b.Time.In(NY))
		}
	}
	if daily := RegularHours.Resample(bars, "1d"); len(daily) != 2 || daily[1].Open != 100 || daily[1].Close != 125 {
		t.Errorf("daily bars wrong: %+v", daily)
	}
	if weekly := RegularHours.Resample(bars, "1w"); len(weekly) != 2 {
		t.Errorf("got %d weekly bars, want 2", len(weekly))
	}
}

func TestCheckTimeframes(t *testing.T) {
	good := [][]string{{"15m", "4h"}, {"1h", "4h", "1d"}, {"15m", "1d", "1w"}}
	for _, tfs := range good {
		if err := CheckTimeframes(tfs, tfs[0]); err != nil {
			t.Errorf("%v: %v", tfs, err)
		}
	}
	bad := [][]string{{"1h", "15m"}, {"4h", "1h"}, {"15m", "2h"}, {"30m", "1h", "4h", "45m"}}
	for _, tfs := range bad {
		if CheckTimeframes(tfs, tfs[0]) == nil {
			t.Errorf("%v with base %s should be rejected", tfs, tfs[0])
		}
	}
}

func TestNormalizeFiltersSession(t *testing.T) {
	open := time.Date(2024, 3, 4, 9, 30, 0, 0, NY)
	bars := []Bar{
		{Time: open.Add(15 * time.Minute), Close: 2}, {Time: open, Close: 1}, {Time: open, Close: 1},
		{Time: open.Add(-30 * time.Minute), Close: 9}, {Time: open.Add(390 * time.Minute), Close: 9},
	}
	if got := RegularHours.Normalize(bars, "15m"); len(got) != 2 || got[0].Close != 1 || got[1].Close != 2 {
		t.Errorf("got %+v", got)
	}
	daily := RegularHours.Normalize([]Bar{{Time: time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC), Close: 1}}, "1d")
	if !daily[0].Time.Equal(open) {
		t.Errorf("daily bar stamped %v, want the 09:30 open", daily[0].Time)
	}
}
