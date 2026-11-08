package data

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"stratest/internal/config"
)

// Polygon uses the aggregates endpoint and needs POLYGON_API_KEY.
type Polygon struct{}

func (Polygon) Name() string { return "polygon" }

func (Polygon) MaxHistory(string) time.Duration { return 20 * 365 * day } // plan-dependent; the API reports the real limit

// Polygon buckets are clock-aligned, so 1h and 4h are built from 30m bars to anchor them at 09:30.
var polygonInterval = map[string]string{"1m": "1/minute", "5m": "5/minute", "15m": "15/minute", "30m": "30/minute", "1h": "30/minute", "4h": "30/minute", "1d": "1/day", "1w": "1/week"}

func (Polygon) Fetch(ticker string, from, to time.Time, interval string) ([]Bar, error) {
	key := config.PolygonKey()
	if key == "" {
		return nil, errors.New("POLYGON_API_KEY is not set")
	}
	url := fmt.Sprintf("https://api.polygon.io/v2/aggs/ticker/%s/range/%s/%d/%d?adjusted=true&sort=asc&limit=50000",
		ticker, polygonInterval[interval], from.UnixMilli(), to.UnixMilli())
	var bars []Bar
	for url != "" {
		var page struct {
			Results []struct {
				T             int64
				O, H, L, C, V float64
			}
			NextURL string `json:"next_url"`
		}
		if err := getJSON(url, http.Header{"Authorization": {"Bearer " + key}}, &page); err != nil {
			return nil, fmt.Errorf("polygon %s: %w", ticker, err)
		}
		for _, r := range page.Results {
			bars = append(bars, Bar{Time: time.UnixMilli(r.T).UTC(), Open: r.O, High: r.H, Low: r.L, Close: r.C, Volume: r.V})
		}
		url = page.NextURL
	}
	bars = Normalize(bars, interval)
	if interval == "1h" || interval == "4h" {
		bars = Resample(bars, interval)
	}
	return bars, nil
}
