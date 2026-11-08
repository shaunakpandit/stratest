package report

import (
	"cmp"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"stratest/internal/backtest"
	"stratest/internal/data"
)

var (
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	bold   = lipgloss.NewStyle().Bold(true)
	faint  = lipgloss.NewStyle().Faint(true)
	panel  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	cell   = lipgloss.NewStyle().Padding(0, 1)
	header = cell.Bold(true)
)

type Header struct {
	Name, Description, Provider string
	Timeframes                  []string
	Start, End                  time.Time
	Tickers                     int
	PriceCache, ResultCache     string
}

func Panel(rows [][2]string) string {
	lines := make([]string, len(rows))
	for i, row := range rows {
		lines[i] = bold.Render(fmt.Sprintf("%-13s", row[0]+":")) + " " + row[1]
	}
	return panel.Render(strings.Join(lines, "\n"))
}

func percent(x float64) string { return colored(x, fmt.Sprintf("%+.2f%%", x*100)) }
func plain(x float64) string   { return fmt.Sprintf("%.2f", x) }
func money(x float64) string   { return colored(x, fmt.Sprintf("%+.2f", x)) }
func date(t time.Time) string  { return t.Format("2006-01-02") }

func colored(x float64, text string) string {
	switch {
	case x > 0:
		return green.Render(text)
	case x < 0:
		return red.Render(text)
	}
	return text
}

func ratio(x float64) string {
	if math.IsInf(x, 1) {
		return "∞"
	}
	return plain(x)
}

func newTable(headers ...string) *table.Table {
	return table.New().Border(lipgloss.RoundedBorder()).Headers(headers...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return header
			}
			return cell
		})
}

// detailRows defines the top-N table; adding a metric is one line here.
var detailRows = []struct {
	label  string
	format func(r backtest.Result, spy float64) string
}{
	{"Profit $", func(r backtest.Result, _ float64) string { return money(r.Profit) }},
	{"Return", func(r backtest.Result, _ float64) string { return percent(r.Return) }},
	{"Max drawdown", func(r backtest.Result, _ float64) string { return percent(-r.MaxDrawdown) }},
	{"Trades", func(r backtest.Result, _ float64) string { return fmt.Sprint(r.TradeCount) }},
	{"Win rate", func(r backtest.Result, _ float64) string { return fmt.Sprintf("%.1f%%", r.WinRate*100) }},
	{"B&H ticker", func(r backtest.Result, _ float64) string { return percent(r.BuyHold) }},
	{"B&H SPY", func(_ backtest.Result, spy float64) string { return percent(spy) }},
	{"Alpha vs B&H", func(r backtest.Result, _ float64) string { return percent(r.Return - r.BuyHold) }},
	{"Alpha vs SPY", func(r backtest.Result, spy float64) string { return percent(r.Return - spy) }},
	{"CAGR", func(r backtest.Result, _ float64) string { return percent(r.CAGR) }},
	{"Sharpe", func(r backtest.Result, _ float64) string { return plain(r.Sharpe) }},
	{"Sortino", func(r backtest.Result, _ float64) string { return plain(r.Sortino) }},
	{"Profit factor", func(r backtest.Result, _ float64) string { return ratio(r.ProfitFactor) }},
	{"Avg win", func(r backtest.Result, _ float64) string { return percent(r.AvgWin) }},
	{"Avg loss", func(r backtest.Result, _ float64) string { return percent(r.AvgLoss) }},
	{"Best trade", func(r backtest.Result, _ float64) string { return percent(r.BestTrade) }},
	{"Worst trade", func(r backtest.Result, _ float64) string { return percent(r.WorstTrade) }},
	{"Avg bars held", func(r backtest.Result, _ float64) string { return fmt.Sprintf("%.1f", r.AvgBars) }},
	{"Exposure", func(r backtest.Result, _ float64) string { return fmt.Sprintf("%.1f%%", r.Exposure*100) }},
	{"Final equity", func(r backtest.Result, _ float64) string { return fmt.Sprintf("%.2f", r.FinalEquity) }},
}

func Print(w io.Writer, h Header, results []backtest.Result, spy float64, top int, allTrades bool) {
	fmt.Fprintln(w, Panel([][2]string{
		{"Strategy", bold.Render(h.Name)}, {"Description", h.Description},
		{"Timeframes", strings.Join(h.Timeframes, " + ")},
		{"Window", date(h.Start) + " → " + date(h.End)},
		{"Provider", h.Provider}, {"Tickers", fmt.Sprint(h.Tickers)},
		{"Cache", "prices " + h.PriceCache + " · results " + h.ResultCache},
	}))
	ranked := slices.SortedFunc(slices.Values(results), func(a, b backtest.Result) int { return cmp.Compare(b.Return, a.Return) })

	leaderboard := newTable("#", "Ticker", "Return %", "Max DD %", "Trades", "Win %", "Sharpe", "B&H %", "vs SPY %")
	for i, r := range ranked {
		leaderboard.Row(fmt.Sprint(i+1), bold.Render(r.Ticker), percent(r.Return), percent(-r.MaxDrawdown), fmt.Sprint(r.TradeCount),
			fmt.Sprintf("%.1f", r.WinRate*100), plain(r.Sharpe), percent(r.BuyHold), percent(r.Return-spy))
	}
	fmt.Fprintln(w, "\n"+bold.Render("Leaderboard"))
	fmt.Fprintln(w, leaderboard)

	best := ranked[:min(top, len(ranked))]
	detail := newTable(append([]string{"Metric"}, tickers(best)...)...)
	for _, row := range detailRows {
		values := []string{row.label}
		for _, r := range best {
			values = append(values, row.format(r, spy))
		}
		detail.Row(values...)
	}
	fmt.Fprintln(w, "\n"+bold.Render(fmt.Sprintf("Top %d", len(best))))
	fmt.Fprintln(w, detail)

	if len(best) == 0 {
		return
	}
	trades := best[0].Trades
	if !allTrades {
		trades = trades[max(0, len(trades)-10):]
	}
	log := newTable("Side", "Entry", "Entry $", "Exit", "Exit $", "Return %", "Bars")
	for _, t := range trades {
		side := "long"
		if t.Side < 0 {
			side = "short"
		}
		log.Row(side, t.EntryTime.In(data.NY).Format("2006-01-02 15:04"), plain(t.EntryPrice),
			t.ExitTime.In(data.NY).Format("2006-01-02 15:04"), plain(t.ExitPrice), percent(t.Return), fmt.Sprint(t.Bars))
	}
	fmt.Fprintln(w, "\n"+bold.Render(fmt.Sprintf("Trades for %s (%d of %d)", best[0].Ticker, len(trades), len(best[0].Trades))))
	fmt.Fprintln(w, log)
	// Extension point: an equity curve chart from best[0].Equity would go here.
	fmt.Fprintln(w, faint.Render("Fill times are New York time."))
}

func tickers(results []backtest.Result) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Ticker
	}
	return out
}
