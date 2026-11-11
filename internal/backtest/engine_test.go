package backtest

import (
	"math"
	"testing"
	"time"

	"stratest/internal/data"
)

func daily(closes ...float64) *data.Data {
	var bars []data.Bar
	day := time.Date(2024, 3, 4, 9, 30, 0, 0, data.NY)
	for _, c := range closes {
		for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			day = day.AddDate(0, 0, 1)
		}
		bars = append(bars, data.Bar{Time: day.UTC(), Open: c, High: c, Low: c, Close: c})
		day = day.AddDate(0, 0, 1)
	}
	return data.NewData(data.RegularHours, "1d", bars, []string{"1d"})
}

func constant(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

var cfg = Config{FeeBps: 1, SlipBps: 1, Capital: 10_000}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestAlwaysLongIsBuyAndHoldMinusCosts(t *testing.T) {
	closes := []float64{100, 102, 99, 105, 110, 108}
	r := Run(daily(closes...), constant(len(closes), 1), cfg)
	cost := 2 / 1e4
	want := 10_000 * (closes[1]/closes[0] - cost) * closes[5] / closes[1]
	if !near(r.FinalEquity, want) {
		t.Errorf("final equity %v, want %v", r.FinalEquity, want)
	}
	if !near(r.BuyHold, 0.08) || r.Return >= r.BuyHold {
		t.Errorf("buy & hold %v, return %v", r.BuyHold, r.Return)
	}
	if r.TradeCount != 1 || r.Trades[0].Bars != 5 || r.Exposure != 5.0/6 {
		t.Errorf("trades %+v exposure %v", r.Trades, r.Exposure)
	}
}

func TestSignalTakesEffectNextBar(t *testing.T) {
	closes := []float64{100, 100, 100, 110, 121, 121}
	signal := []float64{0, 0, 1, 0, 0, math.NaN()} // signal at bar 2 holds bar 3 only
	r := Run(daily(closes...), signal, cfg)
	cost := 2 / 1e4
	want := 10_000 * (1 + 0.1 - cost) * (1 - cost)
	if !near(r.FinalEquity, want) {
		t.Errorf("final equity %v, want %v: the bar 4 move must not be captured", r.FinalEquity, want)
	}
	trade := r.Trades[0]
	if len(r.Trades) != 1 || trade.EntryPrice != 100 || trade.ExitPrice != 110 || trade.Bars != 1 || !near(trade.Return, 0.1-2*cost) {
		t.Errorf("trade %+v", trade)
	}
}

func TestWarmupIsTrimmed(t *testing.T) {
	d := daily(50, 60, 100, 110)
	start := d.Native("1d").Time[2]
	r := Run(d, constant(4, 1), Config{Capital: 100, Start: start})
	if len(r.Equity) != 2 || !near(r.FinalEquity, 110) || !near(r.BuyHold, 0.1) {
		t.Errorf("window not trimmed: equity %v buy & hold %v", r.Equity, r.BuyHold)
	}
}

func TestMetrics(t *testing.T) {
	equity := []float64{100, 120, 90, 110, 80, 100}
	trades := []Trade{{Return: 0.1, Bars: 2}, {Return: -0.05, Bars: 4}, {Return: 0.2, Bars: 1}, {Return: -0.1, Bars: 1}}
	m := compute(equity, []float64{0.2, -0.25, 0.22, -0.27, 0.25}, []float64{0, 1, 1, 0, 1, 1}, trades, []float64{10, 11}, 252)
	if !near(m.MaxDrawdown, 1-80.0/120) {
		t.Errorf("max drawdown %v", m.MaxDrawdown)
	}
	if m.WinRate != 0.5 || !near(m.ProfitFactor, 2) || !near(m.AvgWin, 0.15) || !near(m.AvgLoss, -0.075) {
		t.Errorf("win rate %v profit factor %v avg win %v avg loss %v", m.WinRate, m.ProfitFactor, m.AvgWin, m.AvgLoss)
	}
	if m.BestTrade != 0.2 || m.WorstTrade != -0.1 || m.AvgBars != 2 || m.Return != 0 || !near(m.Exposure, 4.0/6) {
		t.Errorf("%+v", m)
	}
}
