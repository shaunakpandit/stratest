package data

import (
	"slices"
	"strings"
	"time"
)

// Session is a trading day in New York time: it opens Open minutes after the trading day's
// midnight (negative for an evening open on the prior calendar day) and lasts Length minutes.
type Session struct{ Open, Length int }

var (
	RegularHours = Session{Open: 9*60 + 30, Length: 390}
	Globex       = Session{Open: -6 * 60, Length: 23 * 60} // CME: 18:00 the prior evening to 17:00
)

// LiquidFutures are the most traded CME contracts on the 18:00–17:00 ET Globex session, as Yahoo
// continuous front-month symbols. Grains are left out: their session differs.
var LiquidFutures = []string{
	"ES=F", "MES=F", "NQ=F", "MNQ=F", "RTY=F", "M2K=F", "YM=F", "MYM=F",
	"ZN=F", "ZF=F", "ZT=F", "ZB=F", "CL=F", "NG=F", "GC=F", "MGC=F", "6E=F",
}

func IsFuture(ticker string) bool { return strings.HasSuffix(ticker, "=F") }

// Symbol maps a /NQ-style futures alias to Yahoo's continuous contract NQ=F.
func Symbol(ticker string) string {
	if root, ok := strings.CutPrefix(ticker, "/"); ok {
		return root + "=F"
	}
	return ticker
}

func SessionOf(ticker string) Session {
	if IsFuture(ticker) {
		return Globex
	}
	return RegularHours
}

func (s Session) length() time.Duration { return time.Duration(s.Length) * time.Minute }

// openOn returns the open of the session traded on day's calendar date.
func (s Session) openOn(day time.Time) time.Time {
	n := day.In(NY)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, s.Open, 0, 0, NY)
}

// tradingDay returns the calendar date (NY midnight) of the session that t falls in or after.
func (s Session) tradingDay(t time.Time) time.Time {
	n := t.In(NY).Add(-time.Duration(s.Open) * time.Minute)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, NY)
}

func (s Session) open(t time.Time) time.Time { return s.openOn(s.tradingDay(t)) }

func weekend(day time.Time) bool {
	return day.Weekday() == time.Saturday || day.Weekday() == time.Sunday
}

// Bucket returns the session-anchored bucket of timeframe tf containing t.
// The end is the scheduled end, so a partial bucket at the edge of the data never looks complete.
func (s Session) Bucket(tf string, t time.Time) (start, end time.Time) {
	day := s.tradingDay(t)
	open := s.openOn(day)
	close := open.Add(s.length())
	switch tf {
	case "1d":
		return open, close
	case "1w":
		monday := (int(day.Weekday()) + 6) % 7
		return s.openOn(day.AddDate(0, 0, -monday)), s.openOn(day.AddDate(0, 0, 4-monday)).Add(s.length())
	}
	length := time.Duration(minutes[tf]) * time.Minute
	start = open.Add(t.Sub(open) / length * length)
	end = start.Add(length)
	if end.After(close) {
		end = close
	}
	return start, end
}

func (s Session) End(tf string, t time.Time) time.Time {
	_, end := s.Bucket(tf, t)
	return end
}

// Resample aggregates bars into session-anchored tf buckets. Bars must be sorted and within the session.
func (s Session) Resample(bars []Bar, tf string) []Bar {
	var out []Bar
	for _, b := range bars {
		start, _ := s.Bucket(tf, b.Time)
		start = start.UTC()
		if len(out) == 0 || !out[len(out)-1].Time.Equal(start) {
			b.Time = start
			out = append(out, b)
			continue
		}
		last := &out[len(out)-1]
		last.High, last.Low = max(last.High, b.High), min(last.Low, b.Low)
		last.Close, last.Volume = b.Close, last.Volume+b.Volume
	}
	return out
}

// Align maps every base bar to the latest htf bar whose close time is at or before the base bar's
// close time (-1 if none). This is the no-lookahead guarantee for cross-timeframe strategies.
func (s Session) Align(base []time.Time, baseTF string, htf []time.Time, tf string) []int {
	index := make([]int, len(base))
	j := -1
	for i, t := range base {
		end := s.End(baseTF, t)
		for j+1 < len(htf) && !s.End(tf, htf[j+1]).After(end) {
			j++
		}
		index[i] = j
	}
	return index
}

// Normalize sorts, dedupes and keeps in-session bars; daily and weekly bars are stamped at the session open.
func (s Session) Normalize(bars []Bar, interval string) []Bar {
	out := make([]Bar, 0, len(bars))
	for _, b := range bars {
		if !Intraday(interval) {
			b.Time = s.openOn(b.Time.In(NY).Add(12 * time.Hour)) // providers stamp days at local midnight or the open
		}
		day := s.tradingDay(b.Time)
		open := s.openOn(day)
		if b.Time.Before(open) || !b.Time.Before(open.Add(s.length())) || weekend(day) || !(b.Close > 0) {
			continue
		}
		b.Time = b.Time.UTC()
		out = append(out, b)
	}
	slices.SortStableFunc(out, func(a, b Bar) int { return a.Time.Compare(b.Time) })
	return slices.CompactFunc(out, func(a, b Bar) bool { return a.Time.Equal(b.Time) })
}
