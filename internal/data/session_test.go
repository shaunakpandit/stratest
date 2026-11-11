package data

import (
	"testing"
	"time"
)

// hourly returns n hourly bars from start; Close is the bar index + 1.
func hourly(start time.Time, n int) []Bar {
	var bars []Bar
	for i := range n {
		bars = append(bars, Bar{Time: start.Add(time.Duration(i) * time.Hour).UTC(), Open: 1, High: 2, Low: 0.5, Close: float64(i + 1), Volume: 1})
	}
	return bars
}

func TestGlobexSession(t *testing.T) {
	// Sunday 2024-03-10 is the day DST starts; the Monday session opens Sunday 18:00 EDT.
	sunday := time.Date(2024, 3, 10, 12, 0, 0, 0, NY)
	bars := Globex.Normalize(hourly(sunday, 36), "1h") // through Monday 23:00
	// Dropped: Sunday 12:00-17:00 (no session), Monday 17:00 (maintenance break).
	if len(bars) != 29 {
		t.Fatalf("kept %d bars, want 29", len(bars))
	}
	if got := bars[0].Time.In(NY).Format("Mon 15:04"); got != "Sun 18:00" {
		t.Errorf("first bar %s, want Sun 18:00", got)
	}
	daily := Globex.Resample(bars, "1d")
	if len(daily) != 2 || daily[0].Volume != 23 || daily[1].Volume != 6 {
		t.Fatalf("daily bars wrong: %+v", daily)
	}
	if end := Globex.End("1d", daily[0].Time).In(NY).Format("Mon 15:04"); end != "Mon 17:00" {
		t.Errorf("Monday session ends %s, want Mon 17:00", end)
	}
	four := Globex.Resample(bars[:23], "4h")
	if len(four) != 6 || four[1].Time.In(NY).Hour() != 22 || four[5].Volume != 3 {
		t.Errorf("4h buckets wrong: %+v", four)
	}
	// Yahoo stamps futures days at 00:00 ET of the trading day.
	day := Globex.Normalize([]Bar{{Time: time.Date(2024, 3, 11, 0, 0, 0, 0, NY), Close: 1}}, "1d")
	if len(day) != 1 || !day[0].Time.Equal(bars[0].Time) {
		t.Errorf("daily bar stamped %v, want the Sunday 18:00 open", day)
	}
	if friday := Globex.Normalize(hourly(time.Date(2024, 3, 15, 18, 0, 0, 0, NY), 1), "1h"); len(friday) != 0 {
		t.Errorf("Friday evening belongs to no session: %+v", friday)
	}
	week := Globex.Resample(Globex.Normalize(hourly(sunday, 24*8), "1h"), "1w")
	if len(week) != 2 || week[0].Volume != 5*23 {
		t.Errorf("weekly bars wrong: %+v", week)
	}
}

func TestSessionOf(t *testing.T) {
	if Symbol("/MNQ") != "MNQ=F" || Symbol("AAPL") != "AAPL" {
		t.Error("Symbol alias mapping")
	}
	if SessionOf("MNQ=F") != Globex || SessionOf("AAPL") != RegularHours {
		t.Error("SessionOf")
	}
}
