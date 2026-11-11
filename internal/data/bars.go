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

// Nominal length in trading minutes, used to order timeframes; 1d is one 390-minute regular session, 1w five.
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
	Base    string
	session Session
	native  map[string]Frame
	index   map[string][]int
}

func NewData(session Session, base string, bars []Bar, timeframes []string) *Data {
	d := &Data{Base: base, session: session, native: map[string]Frame{}, index: map[string][]int{}}
	baseFrame := ToFrame(bars)
	for _, tf := range append([]string{base}, timeframes...) {
		frame := baseFrame
		if tf != base {
			frame = ToFrame(session.Resample(bars, tf))
		}
		d.native[tf] = frame
		d.index[tf] = session.Align(baseFrame.Time, base, frame.Time, tf)
	}
	return d
}

func (d *Data) Len() int { return len(d.native[d.Base].Time) }

func (d *Data) Session() Session { return d.session }

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
