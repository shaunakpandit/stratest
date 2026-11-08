package backtest

import (
	"math"
	"sort"
	"time"

	"stratest/internal/data"
	"stratest/internal/strategy"
)

// EngineVersion is part of the result cache key; bump it when results would change.
const EngineVersion = "2"

type Config struct {
	FeeBps, SlipBps, Capital float64
	Start                    time.Time // bars before Start are warm-up only
}

type Trade struct {
	Side                  float64
	EntryTime, ExitTime   time.Time
	EntryPrice, ExitPrice float64
	Return                float64 // net of entry and exit costs
	Bars                  int
}

type Result struct {
	Ticker string
	Times  []time.Time
	Equity []float64
	Trades []Trade
	Metrics
}

func position(signal float64) float64 {
	if math.IsNaN(signal) {
		return 0
	}
	return signal
}

// Run backtests one ticker. The signal at base bar i becomes the position held over bar i+1,
// i.e. it is filled at the close of bar i, after the signal is known.
func Run(d *strategy.Data, signal []float64, cfg Config) Result {
	base := d.Native(d.Base)
	start := sort.Search(len(base.Time), func(i int) bool { return !base.Time[i].Before(cfg.Start) })
	times, closes := base.Time[start:], base.Close[start:]
	if len(closes) < 2 {
		return Result{Metrics: Metrics{FinalEquity: cfg.Capital}}
	}
	cost := (cfg.FeeBps + cfg.SlipBps) / 1e4
	positions := make([]float64, len(closes)) // flat at the window start
	returns := make([]float64, len(closes))
	equity := make([]float64, len(closes))
	equity[0] = cfg.Capital
	for i := 1; i < len(closes); i++ {
		positions[i] = position(signal[start+i-1])
		returns[i] = positions[i]*(closes[i]/closes[i-1]-1) - math.Abs(positions[i]-positions[i-1])*cost
		equity[i] = equity[i-1] * (1 + returns[i])
	}
	fills := make([]time.Time, len(times)) // a bar's close is when its fill happens
	for i, t := range times {
		fills[i] = data.End(d.Base, t)
	}
	trades := tradeLog(positions, fills, closes, cost)
	return Result{Times: times, Equity: equity, Trades: trades,
		Metrics: compute(equity, returns[1:], positions, trades, closes, BarsPerYear(d.Base))}
}

// tradeLog splits positions into contiguous non-zero runs. A trade entered for bar i was filled
// at the close of bar i-1; open trades are closed at the final bar.
func tradeLog(positions []float64, times []time.Time, closes []float64, cost float64) []Trade {
	var trades []Trade
	entry := -1
	for i := 1; i <= len(positions); i++ {
		current := 0.0
		if i < len(positions) {
			current = positions[i]
		}
		if current == positions[i-1] {
			continue
		}
		if entry >= 0 {
			side := positions[i-1]
			trades = append(trades, Trade{Side: side, EntryTime: times[entry-1], EntryPrice: closes[entry-1],
				ExitTime: times[i-1], ExitPrice: closes[i-1], Bars: i - entry,
				Return: side*(closes[i-1]/closes[entry-1]-1) - 2*math.Abs(side)*cost})
			entry = -1
		}
		if current != 0 {
			entry = i
		}
	}
	return trades
}
