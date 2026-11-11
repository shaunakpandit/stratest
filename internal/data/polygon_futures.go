package data

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// Polygon has no continuous futures series, so fetchFuture stitches one: each session uses the
// contract with the most daily volume. Prices are not back-adjusted, like Yahoo's =F series.

var polygonFutureResolution = map[string]string{"1m": "1min", "5m": "5min", "15m": "15min", "30m": "30min", "1h": "1hour", "4h": "1hour", "1d": "1day", "1w": "1day"}

// Contracts expiring this long after the window can still be the most traded one (Treasuries and
// metals roll a month or more ahead); later ones never are, and skipping them saves requests.
const rollHorizon = 180 * day

type futureContract struct {
	Ticker        string
	Type          string
	LastTradeDate string `json:"last_trade_date"`
}

type futureAggregate struct {
	WindowStart                    int64  `json:"window_start"`
	SessionEndDate                 string `json:"session_end_date"`
	Open, High, Low, Close, Volume float64
}

func (a futureAggregate) bar(daily bool) Bar {
	t := time.Unix(0, a.WindowStart).UTC()
	if daily { // window_start is midnight UTC; the session end date names the trading day
		t, _ = time.ParseInLocation(time.DateOnly, a.SessionEndDate, NY)
	}
	return Bar{Time: t, Open: a.Open, High: a.High, Low: a.Low, Close: a.Close, Volume: a.Volume}
}

func (p Polygon) fetchFuture(ticker string, from, to time.Time, interval string) ([]Bar, error) {
	contracts, err := p.contracts(strings.TrimSuffix(ticker, "=F"), from, to)
	if err != nil {
		return nil, err
	}
	daily := make([][]Bar, len(contracts))
	for i, contract := range contracts {
		if daily[i], err = p.futureBars(contract, "1d", from, to); err != nil {
			return nil, err
		}
	}
	chosen := frontContracts(daily)
	if !Intraday(interval) {
		bars := slices.Concat(slices.Collect(maps.Values(chosen))...)
		bars = Globex.Normalize(bars, "1d")
		if interval == "1w" {
			bars = Globex.Resample(bars, "1w")
		}
		return bars, nil
	}
	var bars []Bar
	for _, segment := range segments(chosen) {
		start, end := later(segment.From, from), earlier(segment.To, to)
		fresh, err := p.futureBars(contracts[segment.contract], interval, start, end)
		if err != nil {
			return nil, err
		}
		bars = append(bars, slices.DeleteFunc(fresh, func(b Bar) bool { return b.Time.Before(start) || !b.Time.Before(end) })...)
	}
	bars = Globex.Normalize(bars, interval)
	if interval == "4h" {
		bars = Globex.Resample(bars, "4h")
	}
	return bars, nil
}

// contracts lists the single (non-spread) contracts that could be the front month in [from, to),
// sorted by expiry. Each request is a point-in-time snapshot of listed contracts; one every
// rollHorizon catches contracts listed during a long window.
func (p Polygon) contracts(product string, from, to time.Time) ([]string, error) {
	expiry := map[string]string{}
	first, last := from.Format(time.DateOnly), to.Add(rollHorizon).Format(time.DateOnly)
	for date := from; date.Before(to); date = date.Add(rollHorizon) {
		url := fmt.Sprintf("https://api.polygon.io/futures/v1/contracts?product_code=%s&type=single&date=%s&limit=1000", product, date.Format(time.DateOnly))
		rows, err := polygonPages[futureContract](url)
		if err != nil {
			return nil, fmt.Errorf("polygon %s contracts: %w", product, err)
		}
		for _, row := range rows {
			if row.Type == "single" && row.LastTradeDate >= first && row.LastTradeDate <= last {
				expiry[row.Ticker] = row.LastTradeDate
			}
		}
	}
	if len(expiry) == 0 {
		return nil, fmt.Errorf("polygon: no %s contracts trade between %s and %s", product, first, to.Format(time.DateOnly))
	}
	return slices.SortedFunc(maps.Keys(expiry), func(a, b string) int { return cmp.Or(cmp.Compare(expiry[a], expiry[b]), cmp.Compare(a, b)) }), nil
}

// futureBars fetches one contract's bars. The API takes whole dates, so the range is widened by a
// day on each side and the caller trims it.
func (Polygon) futureBars(contract, interval string, from, to time.Time) ([]Bar, error) {
	url := fmt.Sprintf("https://api.polygon.io/futures/v1/aggs/%s?resolution=%s&window_start.gte=%s&window_start.lt=%s&sort=window_start.asc&limit=50000",
		contract, polygonFutureResolution[interval], from.AddDate(0, 0, -1).Format(time.DateOnly), to.AddDate(0, 0, 1).Format(time.DateOnly))
	rows, err := polygonPages[futureAggregate](url)
	if err != nil {
		return nil, fmt.Errorf("polygon %s: %w", contract, err)
	}
	bars := make([]Bar, len(rows))
	for i, row := range rows {
		bars[i] = row.bar(!Intraday(interval))
	}
	return bars, nil
}

// frontContracts picks, per session, the daily bar of the contract with the most volume among those
// expiring no earlier than the one held the session before, so a roll never reverses.
// daily[k] holds contract k's daily bars, contracts sorted by expiry. The result is keyed by contract.
func frontContracts(daily [][]Bar) map[int][]Bar {
	sessions := map[time.Time][]Bar{} // per session, one bar per contract (zero Volume if absent)
	for k, bars := range daily {
		for _, b := range Globex.Normalize(bars, "1d") {
			if sessions[b.Time] == nil {
				sessions[b.Time] = make([]Bar, len(daily))
			}
			sessions[b.Time][k] = b
		}
	}
	chosen, held := map[int][]Bar{}, 0
	for _, session := range slices.SortedFunc(maps.Keys(sessions), time.Time.Compare) {
		bars := sessions[session]
		for k := held; k < len(bars); k++ {
			if bars[k].Volume > bars[held].Volume {
				held = k
			}
		}
		if bars[held].Volume > 0 {
			chosen[held] = append(chosen[held], bars[held])
		}
	}
	return chosen
}

type segment struct {
	TimeRange
	contract int
}

// segments turns each contract's chosen sessions into the time range it is held for. Rolls never
// reverse, so each contract is held over one contiguous run of sessions.
func segments(chosen map[int][]Bar) []segment {
	var out []segment
	for _, k := range slices.Sorted(maps.Keys(chosen)) {
		bars := chosen[k]
		out = append(out, segment{TimeRange{bars[0].Time, Globex.End("1d", bars[len(bars)-1].Time)}, k})
	}
	return out
}
