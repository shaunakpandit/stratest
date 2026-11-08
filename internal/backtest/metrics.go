package backtest

import (
	"math"

	"stratest/internal/data"
)

// Metrics are fractions (0.05 = 5%) unless noted. Adding one means a field here, a line in
// compute, and a row in report.detailRows.
type Metrics struct {
	Profit, Return, MaxDrawdown, WinRate, BuyHold float64
	TradeCount                                    int
	CAGR, Sharpe, Sortino, ProfitFactor           float64
	AvgWin, AvgLoss, BestTrade, WorstTrade        float64
	AvgBars, Exposure, FinalEquity                float64
}

func BarsPerYear(base string) float64 {
	switch base {
	case "1d":
		return 252
	case "1w":
		return 52
	}
	return 252 * 390 / float64(data.Minutes(base))
}

func compute(equity, returns, positions []float64, trades []Trade, closes []float64, barsPerYear float64) Metrics {
	first, final := equity[0], equity[len(equity)-1]
	m := Metrics{
		FinalEquity: final, Profit: final - first, Return: final/first - 1,
		BuyHold: closes[len(closes)-1]/closes[0] - 1, TradeCount: len(trades),
		MaxDrawdown: maxDrawdown(equity),
		Exposure:    fraction(positions, func(p float64) bool { return p != 0 }),
	}
	if years := float64(len(returns)) / barsPerYear; years > 0 && final > 0 {
		m.CAGR = math.Pow(final/first, 1/years) - 1
	}
	mean, deviation, downside := stats(returns)
	if deviation > 0 {
		m.Sharpe = mean / deviation * math.Sqrt(barsPerYear)
	}
	if downside > 0 {
		m.Sortino = mean / downside * math.Sqrt(barsPerYear)
	}
	if len(trades) == 0 {
		return m
	}
	var wins, losses, gains, pains, bars float64
	m.BestTrade, m.WorstTrade = math.Inf(-1), math.Inf(1)
	for _, t := range trades {
		m.BestTrade, m.WorstTrade = max(m.BestTrade, t.Return), min(m.WorstTrade, t.Return)
		bars += float64(t.Bars)
		if t.Return > 0 {
			wins, gains = wins+1, gains+t.Return
		} else {
			losses, pains = losses+1, pains-t.Return
		}
	}
	m.WinRate, m.AvgBars = wins/float64(len(trades)), bars/float64(len(trades))
	m.AvgWin, m.AvgLoss = safeDivide(gains, wins), -safeDivide(pains, losses)
	m.ProfitFactor = safeDivide(gains, pains)
	if pains == 0 && gains > 0 {
		m.ProfitFactor = math.Inf(1)
	}
	return m
}

func maxDrawdown(equity []float64) float64 {
	peak, worst := equity[0], 0.0
	for _, e := range equity {
		peak = max(peak, e)
		worst = max(worst, 1-e/peak)
	}
	return worst
}

func stats(returns []float64) (mean, deviation, downside float64) {
	if len(returns) == 0 {
		return 0, 0, 0
	}
	n := float64(len(returns))
	for _, r := range returns {
		mean += r / n
	}
	for _, r := range returns {
		deviation += (r - mean) * (r - mean) / n
		downside += math.Pow(min(r, 0), 2) / n
	}
	return mean, math.Sqrt(deviation), math.Sqrt(downside)
}

func fraction(x []float64, keep func(float64) bool) float64 {
	count := 0.0
	for _, v := range x {
		if keep(v) {
			count++
		}
	}
	return safeDivide(count, float64(len(x)))
}

func safeDivide(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}
