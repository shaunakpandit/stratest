package data

import (
	"fmt"
	"math"
	"slices"
	"time"
	_ "time/tzdata" // NY session math must not depend on the host's zoneinfo
)

type Bar struct {
	Time                           time.Time // bar start, UTC
	Open, High, Low, Close, Volume float64
}

type Frame struct {
	Time                           []time.Time
	Open, High, Low, Close, Volume []float64
}

var NY, _ = time.LoadLocation("America/New_York")

// Nominal length in trading minutes; 1d is one 390-minute session, 1w five sessions.
var minutes = map[string]int{"1m": 1, "5m": 5, "15m": 15, "30m": 30, "1h": 60, "4h": 240, "1d": 390, "1w": 1950}

var Timeframes = []string{"1m", "5m", "15m", "30m", "1h", "4h", "1d", "1w"}

func Minutes(tf string) int { return minutes[tf] }

func Intraday(tf string) bool { return minutes[tf] > 0 && minutes[tf] < 390 }

// CheckTimeframes enforces that base is the finest timeframe and every other one is buildable from it.
func CheckTimeframes(timeframes []string, base string) error {
	if !slices.Contains(timeframes, base) {
		return fmt.Errorf("base %q must be one of the timeframes %v", base, timeframes)
	}
	for _, tf := range timeframes {
		switch {
		case minutes[tf] == 0:
			return fmt.Errorf("unsupported timeframe %q (supported: %v)", tf, Timeframes)
		case minutes[tf] < minutes[base]:
			return fmt.Errorf("timeframe %s is finer than base %s; base must be the finest", tf, base)
		case Intraday(tf) && minutes[tf]%minutes[base] != 0:
			return fmt.Errorf("timeframe %s is not a multiple of base %s", tf, base)
		}
	}
	return nil
}

func sessionOpen(t time.Time) time.Time {
	n := t.In(NY)
	return time.Date(n.Year(), n.Month(), n.Day(), 9, 30, 0, 0, NY)
}

// Bucket returns the session-anchored bucket of timeframe tf containing t.
// The end is the scheduled end, so a partial bucket at the edge of the data never looks complete.
func Bucket(tf string, t time.Time) (start, end time.Time) {
	open := sessionOpen(t)
	close := open.Add(390 * time.Minute)
	switch tf {
	case "1d":
		return open, close
	case "1w":
		monday := (int(open.Weekday()) + 6) % 7
		return open.AddDate(0, 0, -monday), close.AddDate(0, 0, 4-monday)
	}
	length := time.Duration(minutes[tf]) * time.Minute
	start = open.Add(t.Sub(open) / length * length)
	end = start.Add(length)
	if end.After(close) {
		end = close
	}
	return start, end
}

func End(tf string, t time.Time) time.Time {
	_, end := Bucket(tf, t)
	return end
}

// Resample aggregates bars into session-anchored tf buckets. Bars must be sorted and within regular hours.
func Resample(bars []Bar, tf string) []Bar {
	var out []Bar
	for _, b := range bars {
		start, _ := Bucket(tf, b.Time)
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
func Align(base []time.Time, baseTF string, htf []time.Time, tf string) []int {
	index := make([]int, len(base))
	j := -1
	for i, t := range base {
		end := End(baseTF, t)
		for j+1 < len(htf) && !End(tf, htf[j+1]).After(end) {
			j++
		}
		index[i] = j
	}
	return index
}

// Normalize sorts, dedupes and keeps regular-session bars; daily and weekly bars are stamped at the 09:30 open.
func Normalize(bars []Bar, interval string) []Bar {
	out := make([]Bar, 0, len(bars))
	for _, b := range bars {
		if !Intraday(interval) {
			b.Time = sessionOpen(b.Time.In(NY).Add(12 * time.Hour)) // providers stamp days at local midnight or the open
		}
		open := sessionOpen(b.Time)
		if b.Time.Before(open) || !b.Time.Before(open.Add(390*time.Minute)) || open.Weekday() == time.Saturday || open.Weekday() == time.Sunday || !(b.Close > 0) {
			continue
		}
		b.Time = b.Time.UTC()
		out = append(out, b)
	}
	slices.SortStableFunc(out, func(a, b Bar) int { return a.Time.Compare(b.Time) })
	return slices.CompactFunc(out, func(a, b Bar) bool { return a.Time.Equal(b.Time) })
}

func ToFrame(bars []Bar) Frame {
	f := Frame{}
	for _, b := range bars {
		f.Time = append(f.Time, b.Time)
		f.Open, f.High, f.Low = append(f.Open, b.Open), append(f.High, b.High), append(f.Low, b.Low)
		f.Close, f.Volume = append(f.Close, b.Close), append(f.Volume, b.Volume)
	}
	return f
}

// Data is what a strategy sees: base bars plus every declared timeframe, resampled from base.
type Data struct {
	Base   string
	native map[string]Frame
	index  map[string][]int
}

func NewData(base string, bars []Bar, timeframes []string) *Data {
	d := &Data{Base: base, native: map[string]Frame{}, index: map[string][]int{}}
	baseFrame := ToFrame(bars)
	for _, tf := range append([]string{base}, timeframes...) {
		frame := baseFrame
		if tf != base {
			frame = ToFrame(Resample(bars, tf))
		}
		d.native[tf] = frame
		d.index[tf] = Align(baseFrame.Time, base, frame.Time, tf)
	}
	return d
}

func (d *Data) Len() int { return len(d.native[d.Base].Time) }

// Native returns the unaligned bars of tf.
func (d *Data) Native(tf string) Frame {
	frame, ok := d.native[tf]
	if !ok {
		panic(fmt.Sprintf("timeframe %q was not declared", tf))
	}
	return frame
}

// Align maps a series computed on Native(tf) onto base bars, using only completed tf bars.
func (d *Data) Align(tf string, x []float64) []float64 {
	d.Native(tf)
	return gather(d.index[tf], x, math.NaN())
}

// Ind computes fn on native tf bars and aligns the result to base bars.
func (d *Data) Ind(tf string, fn func(Frame) []float64) []float64 {
	return d.Align(tf, fn(d.Native(tf)))
}

// TF returns tf bars aligned to base: entry i is the latest tf bar completed by base bar i.
func (d *Data) TF(tf string) Frame {
	f := d.Native(tf)
	return Frame{
		Time: gather(d.index[tf], f.Time, time.Time{}),
		Open: d.Align(tf, f.Open), High: d.Align(tf, f.High), Low: d.Align(tf, f.Low),
		Close: d.Align(tf, f.Close), Volume: d.Align(tf, f.Volume),
	}
}

func gather[T any](index []int, x []T, missing T) []T {
	out := make([]T, len(index))
	for i, j := range index {
		out[i] = missing
		if j >= 0 && j < len(x) {
			out[i] = x[j]
		}
	}
	return out
}
