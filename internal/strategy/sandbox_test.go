package strategy

import (
	"strings"
	"sync"
	"testing"

	"stratest/internal/data"
)

const crossCode = `func Signals(d *Data) []float64 {
	fast := d.Ind("15m", func(f Frame) []float64 { return SMA(f.Close, 20) })
	slow := d.Ind("4h", func(f Frame) []float64 { return SMA(f.Close, 50) })
	out := make([]float64, d.Len())
	for i := range out {
		if fast[i] > slow[i] { out[i] = 1 }
	}
	return out
}`

var crossTimeframes = []string{"15m", "4h"}

func TestValidateAccepts(t *testing.T) {
	if err := Validate(crossCode, crossTimeframes, "15m"); err != nil {
		t.Fatal(err)
	}
	helper := `
type state struct{ position float64 }
func clamp(x float64) float64 { return math.Max(-1, math.Min(1, x)) }
func Signals(d *Data) []float64 {
	rsi := d.Ind("1d", func(f Frame) []float64 { return RSI(f.Close, 14) })
	_, _, hist := MACD(d.TF("1d").Close, 12, 26, 9)
	s := state{}
	out := make([]float64, d.Len())
	for i := range out {
		if rsi[i] < 30 || hist[i] > 0 { s.position = 1 } else if rsi[i] > 70 { s.position = -1 }
		out[i] = clamp(s.position)
	}
	return out
}`
	if err := Validate(helper, []string{"1d"}, "1d"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]struct{ code, want string }{
		"bad length": {`func Signals(d *Data) []float64 { return make([]float64, 10) }`, "returned 10 values"},
		"bad values": {`func Signals(d *Data) []float64 { out := make([]float64, d.Len()); out[5] = 2; return out }`, "values must be"},
		"forbidden import": {`import "os"
func Signals(d *Data) []float64 { os.Exit(1); return nil }`, `import "os" is not allowed`},
		"undeclared timeframe": {`func Signals(d *Data) []float64 { _ = d.TF("1h"); return make([]float64, d.Len()) }`, `timeframe "1h" was not declared`},
		"goroutine":            {`func Signals(d *Data) []float64 { go func() {}(); return make([]float64, d.Len()) }`, "goroutines"},
		"no Signals":           {`func Other() {}`, "must define func Signals"},
		"syntax":               {`func Signals(d *Data) []float64 {`, "expected"},
		"lookahead": {`func Signals(d *Data) []float64 {
	c := d.TF("15m").Close
	out := make([]float64, d.Len())
	for i := range out {
		if i+1 < len(c) && c[i+1] > c[i] { out[i] = 1 }
	}
	return out
}`, "lookahead detected"},
		"whole-series lookahead": {`func Signals(d *Data) []float64 {
	c := d.TF("15m").Close
	average := SMA(c, len(c))[len(c)-1]
	out := make([]float64, d.Len())
	for i := range out {
		if c[i] > average { out[i] = 1 }
	}
	return out
}`, "lookahead detected"},
	}
	for name, c := range cases {
		err := Validate(c.code, crossTimeframes, "15m")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want error containing %q", name, err, c.want)
		}
	}
	if err := Validate(crossCode, []string{"4h", "15m"}, "4h"); err == nil {
		t.Error("a base coarser than another timeframe should be rejected")
	}
}

func TestCompileTimeAllowlist(t *testing.T) {
	_, err := Compile(`func Signals(d *Data) []float64 { time.Sleep(1); return nil }`)
	if err == nil {
		t.Error("time.Sleep should not be available")
	}
}

// Separate interpreters run safely in parallel even when the strategy keeps package state.
func TestInterpreterPerGoroutine(t *testing.T) {
	code := "var calls int\n" + strings.Replace(crossCode, "{\n", "{\n\tcalls++\n", 1)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			fn, err := Compile(code)
			if err != nil {
				t.Error(err)
				return
			}
			d := data.NewData("15m", Synthetic("15m", 500), crossTimeframes)
			if out, err := Call(fn, d, 5e9); err != nil || len(out) != 500 {
				t.Error(len(out), err)
			}
		}()
	}
	group.Wait()
}

func TestExampleStrategyIsValid(t *testing.T) {
	s, err := Load("../../strategies", "example_sma_cross")
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(s.Code, s.Timeframes, s.Base); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &Strategy{Name: "x", Description: "d", Tickers: []string{"AAPL"}, Timeframes: crossTimeframes, Base: "15m", Code: crossCode}
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir, "x")
	if err != nil || loaded.Base != "15m" || Validate(loaded.Code, loaded.Timeframes, loaded.Base) != nil {
		t.Fatalf("round trip failed: %+v %v", loaded, err)
	}
	if CheckName("Bad Name") == nil || CheckName("good_name-2") != nil {
		t.Error("slug check")
	}
}
