package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"stratest/internal/config"
	"stratest/internal/llm"
	"stratest/internal/strategy"
)

const goodCode = `func Signals(d *Data) []float64 {
	fast := d.Ind("15m", func(f Frame) []float64 { return SMA(f.Close, 20) })
	slow := d.Ind("4h", func(f Frame) []float64 { return SMA(f.Close, 50) })
	out := make([]float64, d.Len())
	for i := range out {
		if fast[i] > slow[i] { out[i] = 1 }
	}
	return out
}`

// fakeLLM serves the given codes in order, one per request.
func fakeLLM(t *testing.T, codes ...string) *int {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer, _ := json.Marshal(llm.Response{Timeframes: []string{"15m", "4h"}, Base: "15m", WarmupDays: 45, Code: codes[min(calls, len(codes)-1)]})
		calls++
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "```json\n" + string(answer) + "\n```"}}}})
	}))
	t.Cleanup(server.Close)
	llm.Endpoint = server.URL
	t.Setenv("OPENROUTER_API_KEY", "test")
	t.Chdir(t.TempDir())
	return &calls
}

func runNew(args ...string) error {
	newFlags.name, newFlags.description, newFlags.tickers, newFlags.yes = "", "", "", false
	root.SetArgs(append([]string{"new"}, args...))
	return root.Execute()
}

func TestNewNonInteractive(t *testing.T) {
	calls := fakeLLM(t, goodCode)
	desc := "Buy when the 15m SMA(20) crosses above the 4h SMA(50), sell when it crosses below"
	if err := runNew("--name", "sma_15m_x_4h", "--desc", desc, "--tickers", "aapl, msft,AAPL,,nvda", "--yes"); err != nil {
		t.Fatal(err)
	}
	s, err := strategy.Load(config.StrategiesDir, "sma_15m_x_4h")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Tickers, []string{"AAPL", "MSFT", "NVDA"}) || s.Base != "15m" || !slices.Equal(s.Timeframes, []string{"15m", "4h"}) {
		t.Errorf("saved %+v", s)
	}
	if s.PromptHash != llm.PromptHash(desc, config.Model()) || !strings.Contains(s.Code, "func Signals") {
		t.Errorf("hash %q code %q", s.PromptHash, s.Code)
	}
	if err := runNew("--name", "again", "--desc", desc, "--tickers", "SPY", "--yes"); err != nil || *calls != 1 {
		t.Errorf("same description should hit the LLM cache: %d calls, %v", *calls, err)
	}
}

func TestNewRetriesWithValidationError(t *testing.T) {
	calls := fakeLLM(t, `func Signals(d *Data) []float64 { return nil }`, goodCode)
	if err := runNew("--name", "retry", "--desc", "x", "--tickers", "A", "--yes"); err != nil || *calls != 2 {
		t.Fatalf("%d calls, %v", *calls, err)
	}
	calls = fakeLLM(t, `func Signals(d *Data) []float64 { return nil }`)
	if err := runNew("--name", "fails", "--desc", "x", "--tickers", "A", "--yes"); err == nil || *calls != 2 || strategy.Exists(config.StrategiesDir, "fails") {
		t.Fatalf("should give up after one retry: %d calls, %v", *calls, err)
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	calls := fakeLLM(t, goodCode)
	for _, args := range [][]string{
		{"--name", "Bad Name", "--desc", "x", "--tickers", "A", "--yes"},
		{"--name", "ok", "--desc", " ", "--tickers", "A", "--yes"},
		{"--name", "ok", "--desc", "x", "--tickers", " , ", "--yes"},
	} {
		if runNew(args...) == nil {
			t.Errorf("%v should fail", args)
		}
	}
	if *calls != 0 {
		t.Errorf("invalid input should not reach the LLM")
	}
}

func TestParseTickersAndWindow(t *testing.T) {
	if got := parseTickers("spy, Qqq", "qqq,iwm"); !slices.Equal(got, []string{"SPY", "QQQ", "IWM"}) {
		t.Errorf("parseTickers = %v", got)
	}
	start, end, err := window("6mo", "", "2025-06-01")
	if err != nil || start.Format("2006-01-02") != "2024-12-01" || end.Format("2006-01-02") != "2025-06-01" {
		t.Errorf("window = %v %v %v", start, end, err)
	}
	if _, _, err := window("1x", "", ""); err == nil {
		t.Error("bad period accepted")
	}
}
