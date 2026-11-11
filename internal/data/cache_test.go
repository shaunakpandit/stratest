package data

import (
	"testing"
	"time"
)

type mockProvider struct{ calls []TimeRange }

func (m *mockProvider) Name() string                    { return "mock" }
func (m *mockProvider) MaxHistory(string) time.Duration { return 365 * day }
func (m *mockProvider) Fetch(_ string, from, to time.Time, _ string) ([]Bar, error) {
	m.calls = append(m.calls, TimeRange{from, to})
	var bars []Bar
	for t := from; t.Before(to); t = t.Add(day) {
		bars = append(bars, Bar{Time: t, Close: 1})
	}
	return RegularHours.Normalize(bars, "1d"), nil
}

func TestCacheFetchesOnlyMissing(t *testing.T) {
	Now = func() time.Time { return time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC) }
	defer func() { Now = time.Now }()
	dir, p := t.TempDir(), &mockProvider{}
	jan, feb, mar := time.Date(2024, 1, 1, 0, 0, 0, 0, NY), time.Date(2024, 2, 1, 0, 0, 0, 0, NY), time.Date(2024, 3, 1, 0, 0, 0, 0, NY)

	first, fetched, err := Load(dir, p, "AAPL", "1d", jan, feb, false)
	if err != nil || !fetched || len(p.calls) != 1 || len(first) != 23 {
		t.Fatalf("first load: %d bars, fetched %v, %d calls, %v", len(first), fetched, len(p.calls), err)
	}
	again, fetched, _ := Load(dir, p, "AAPL", "1d", jan, feb, false)
	if fetched || len(p.calls) != 1 || len(again) != len(first) {
		t.Fatalf("repeat load made %d calls, want 1 total", len(p.calls))
	}
	all, _, _ := Load(dir, p, "AAPL", "1d", jan, mar, false)
	if len(p.calls) != 2 || !p.calls[1].From.Equal(feb) || !p.calls[1].To.Equal(mar) {
		t.Fatalf("extended load fetched %v, want only [feb, mar)", p.calls[1:])
	}
	if len(all) != 23+21 {
		t.Errorf("got %d bars, want 44", len(all))
	}
	if listed := ListCached(dir); len(listed) != 1 || listed[0].Ticker != "AAPL" || listed[0].Bars != 44 || listed[0].Interval != "1d" {
		t.Errorf("ListCached = %+v", listed)
	}
}

func TestCacheLeavesOpenSessionUncovered(t *testing.T) {
	Now = func() time.Time { return time.Date(2024, 3, 5, 11, 0, 0, 0, NY) } // Tuesday, mid-session
	defer func() { Now = time.Now }()
	dir, p := t.TempDir(), &mockProvider{}
	from, to := time.Date(2024, 3, 1, 0, 0, 0, 0, NY), time.Date(2024, 3, 6, 0, 0, 0, 0, NY)
	Load(dir, p, "AAPL", "1d", from, to, false)
	Load(dir, p, "AAPL", "1d", from, to, false)
	if len(p.calls) != 2 || !p.calls[1].From.Equal(time.Date(2024, 3, 5, 0, 0, 0, 0, NY)) {
		t.Errorf("today should be refetched until the close, calls: %v", p.calls)
	}
}

func TestMissingRanges(t *testing.T) {
	at := func(day int) time.Time { return time.Date(2024, 1, day, 0, 0, 0, 0, time.UTC) }
	ranges := mergeRanges([]TimeRange{{at(10), at(15)}, {at(1), at(5)}, {at(4), at(8)}})
	if len(ranges) != 2 || !ranges[0].To.Equal(at(8)) {
		t.Fatalf("merge: %v", ranges)
	}
	gaps := missing(ranges, TimeRange{at(2), at(20)})
	if len(gaps) != 2 || !gaps[0].From.Equal(at(8)) || !gaps[0].To.Equal(at(10)) || !gaps[1].From.Equal(at(15)) {
		t.Errorf("gaps: %v", gaps)
	}
	if gaps := missing(ranges, TimeRange{at(2), at(7)}); len(gaps) != 0 {
		t.Errorf("covered range has gaps %v", gaps)
	}
}
