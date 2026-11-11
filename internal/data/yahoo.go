package data

import (
	"fmt"
	"net/http"
	"time"
)

// Yahoo uses the unofficial chart endpoint. It can break or rate-limit at any time; keep it in this file.
type Yahoo struct{}

func (Yahoo) Name() string { return "yahoo" }

func (Yahoo) MaxHistory(interval string) time.Duration {
	switch interval {
	case "1m":
		return 7 * day
	case "5m", "15m", "30m":
		return 60 * day
	case "1h", "4h":
		return 730 * day
	}
	return 100 * 365 * day
}

var yahooInterval = map[string]string{"1m": "1m", "5m": "5m", "15m": "15m", "30m": "30m", "1h": "60m", "1d": "1d", "1w": "1wk"}

func (y Yahoo) Fetch(ticker string, from, to time.Time, interval string) ([]Bar, error) {
	if interval == "4h" {
		bars, err := y.Fetch(ticker, from, to, "1h")
		return SessionOf(ticker).Resample(bars, "4h"), err
	}
	var response struct {
		Chart struct {
			Result []struct {
				Timestamp  []int64
				Indicators struct {
					Quote    []struct{ Open, High, Low, Close, Volume []float64 }
					Adjclose []struct{ Adjclose []float64 }
				}
			}
		}
	}
	url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?period1=%d&period2=%d&interval=%s&includePrePost=false&events=div%%2Csplits",
		ticker, from.Unix(), to.Unix(), yahooInterval[interval])
	if err := getJSON(url, http.Header{"User-Agent": {"Mozilla/5.0"}}, &response); err != nil {
		return nil, fmt.Errorf("yahoo %s: %w", ticker, err)
	}
	if len(response.Chart.Result) == 0 || len(response.Chart.Result[0].Indicators.Quote) == 0 {
		return nil, nil
	}
	result := response.Chart.Result[0]
	quote := result.Indicators.Quote[0]
	bars := make([]Bar, len(result.Timestamp))
	for i, timestamp := range result.Timestamp {
		// Intraday bars come split-adjusted; daily bars carry adjclose, which also folds in dividends.
		factor := 1.0
		if len(result.Indicators.Adjclose) > 0 && quote.Close[i] > 0 {
			factor = result.Indicators.Adjclose[0].Adjclose[i] / quote.Close[i]
		}
		bars[i] = Bar{Time: time.Unix(timestamp, 0).UTC(), Open: quote.Open[i] * factor, High: quote.High[i] * factor,
			Low: quote.Low[i] * factor, Close: quote.Close[i] * factor, Volume: quote.Volume[i]}
	}
	return SessionOf(ticker).Normalize(bars, interval), nil
}
