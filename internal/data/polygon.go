package data

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"stratest/internal/config"
)

// Polygon uses the stock aggregates and futures endpoints (polygon_futures.go) and needs POLYGON_API_KEY.
type Polygon struct{}

func (Polygon) Name() string { return "polygon" }

func (Polygon) MaxHistory(string) time.Duration { return 20 * 365 * day } // plan-dependent; the API reports the real limit

// Polygon buckets are clock-aligned, so 1h and 4h are built from 30m bars to anchor them at 09:30.
var polygonInterval = map[string]string{"1m": "1/minute", "5m": "5/minute", "15m": "15/minute", "30m": "30/minute", "1h": "30/minute", "4h": "30/minute", "1d": "1/day", "1w": "1/week"}

func (p Polygon) Fetch(ticker string, from, to time.Time, interval string) ([]Bar, error) {
	if IsFuture(ticker) {
		return p.fetchFuture(ticker, from, to, interval)
	}
	url := fmt.Sprintf("https://api.polygon.io/v2/aggs/ticker/%s/range/%s/%d/%d?adjusted=true&sort=asc&limit=50000",
		ticker, polygonInterval[interval], from.UnixMilli(), to.UnixMilli())
	rows, err := polygonPages[struct {
		T             int64
		O, H, L, C, V float64
	}](url)
	if err != nil {
		return nil, fmt.Errorf("polygon %s: %w", ticker, err)
	}
	bars := make([]Bar, len(rows))
	for i, r := range rows {
		bars[i] = Bar{Time: time.UnixMilli(r.T).UTC(), Open: r.O, High: r.H, Low: r.L, Close: r.C, Volume: r.V}
	}
	bars = RegularHours.Normalize(bars, interval)
	if interval == "1h" || interval == "4h" {
		bars = RegularHours.Resample(bars, interval)
	}
	return bars, nil
}

// polygonPages follows next_url and collects every page's results.
func polygonPages[T any](url string) ([]T, error) {
	key := config.PolygonKey()
	if key == "" {
		return nil, errors.New("POLYGON_API_KEY is not set")
	}
	var out []T
	for url != "" {
		var page struct {
			Results []T
			NextURL string `json:"next_url"`
		}
		if err := getJSON(url, http.Header{"Authorization": {"Bearer " + key}}, &page); err != nil {
			return nil, err
		}
		out, url = append(out, page.Results...), page.NextURL
	}
	return out, nil
}
