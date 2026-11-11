package strategy

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"maps"
	"math"
	"math/rand/v2"
	"strconv"
	"sync"
	"testing/fstest"
	"time"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"

	"stratest/internal/data"
)

type SignalFunc func(*data.Data) []float64

const header = `package strategy

import (
	"math"
	"sort"
	"time"
	. "stratest/sandboxapi"
)

`

var allowedImports = map[string]bool{"math": true, "sort": true, "time": true, "stratest/sandboxapi": true}

// exports is the whole world visible to strategy code: three stdlib packages (minus clocks and
// sleeping) and the ta + Data API merged under one dot-importable path.
var exports = sync.OnceValue(func() interp.Exports {
	api := maps.Clone(Symbols["stratest/internal/ta/ta"])
	maps.Copy(api, Symbols["stratest/internal/data/data"])
	timeSymbols := maps.Clone(stdlib.Symbols["time/time"])
	for _, name := range []string{"Now", "Sleep", "Since", "Until", "After", "AfterFunc", "Tick", "NewTimer", "NewTicker"} {
		delete(timeSymbols, name)
	}
	return interp.Exports{
		"math/math": stdlib.Symbols["math/math"], "sort/sort": stdlib.Symbols["sort/sort"], "time/time": timeSymbols,
		"stratest/sandboxapi/sandboxapi": api,
	}
})

func checkSource(source string) error {
	file, err := parser.ParseFile(token.NewFileSet(), "strategy.go", source, 0)
	if err != nil {
		return err
	}
	for _, spec := range file.Imports {
		if path, _ := strconv.Unquote(spec.Path.Value); !allowedImports[path] {
			return fmt.Errorf("import %q is not allowed; only math, sort and time are available", path)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if _, ok := n.(*ast.GoStmt); ok && err == nil {
			err = errors.New("goroutines are not allowed")
		}
		return err == nil
	})
	return err
}

// Compile builds Signals in a fresh interpreter. Interpreters are not shared between goroutines.
func Compile(code string) (SignalFunc, error) {
	source := header + code
	if err := checkSource(source); err != nil {
		return nil, err
	}
	i := interp.New(interp.Options{SourcecodeFilesystem: fstest.MapFS{}, Stdout: io.Discard, Stderr: io.Discard})
	if err := i.Use(exports()); err != nil {
		return nil, err
	}
	if _, err := i.Eval(source); err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	value, err := i.Eval("strategy.Signals")
	if err != nil {
		return nil, errors.New("code must define func Signals(d *Data) []float64")
	}
	fn, ok := value.Interface().(func(*data.Data) []float64)
	if !ok {
		return nil, fmt.Errorf("Signals has type %s, want func(d *Data) []float64", value.Type())
	}
	return fn, nil
}

// Call runs fn, turning panics (bad index, undeclared timeframe) and timeouts into errors.
func Call(fn SignalFunc, d *data.Data, timeout time.Duration) ([]float64, error) {
	type result struct {
		out []float64
		err error
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- result{err: fmt.Errorf("strategy panicked: %v", r)}
			}
		}()
		done <- result{out: fn(d)}
	}()
	select {
	case r := <-done:
		return r.out, r.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("strategy took longer than %s", timeout)
	}
}

// Synthetic returns n random-walk bars at the base timeframe over regular sessions.
func Synthetic(base string, n int) []data.Bar {
	rng := rand.New(rand.NewPCG(1, 2))
	step := time.Duration(data.Minutes(base)) * time.Minute
	if !data.Intraday(base) {
		step = 390 * time.Minute
	}
	bars, price := []data.Bar{}, 100.0
	for day := time.Date(2024, 1, 1, 9, 30, 0, 0, data.NY); len(bars) < n; day = day.AddDate(0, 0, 1) {
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday || (base == "1w" && day.Weekday() != time.Monday) {
			continue
		}
		for t := day; t.Before(day.Add(390*time.Minute)) && len(bars) < n; t = t.Add(step) {
			open := price
			price *= 1 + rng.NormFloat64()*0.005
			bars = append(bars, data.Bar{Time: t.UTC(), Open: open, High: max(open, price) * 1.001, Low: min(open, price) * 0.999, Close: price, Volume: 1000 + rng.Float64()*1000})
		}
	}
	return bars
}

func sameSignal(a, b float64) bool { return a == b || (math.IsNaN(a) && math.IsNaN(b)) }

// Validate compiles code and runs it on synthetic data, checking output shape, declared timeframes,
// and that truncating the future never changes past signals (the lookahead probe).
func Validate(code string, timeframes []string, base string) error {
	if err := data.CheckTimeframes(timeframes, base); err != nil {
		return err
	}
	fn, err := Compile(code)
	if err != nil {
		return err
	}
	bars := Synthetic(base, 2000)
	full, err := Call(fn, data.NewData(data.RegularHours, base, bars, timeframes), 2*time.Second)
	if err != nil {
		return err
	}
	if len(full) != len(bars) {
		return fmt.Errorf("Signals returned %d values, want d.Len() = %d", len(full), len(bars))
	}
	for i, v := range full {
		if !math.IsNaN(v) && v != -1 && v != 0 && v != 1 {
			return fmt.Errorf("signal[%d] = %v; values must be -1, 0, 1 or NaN", i, v)
		}
	}
	rng := rand.New(rand.NewPCG(3, 4))
	for range 3 {
		k := len(bars)/4 + rng.IntN(len(bars)*3/4-1)
		truncated, err := Call(fn, data.NewData(data.RegularHours, base, bars[:k+1], timeframes), 2*time.Second)
		if err != nil {
			return fmt.Errorf("on data truncated at bar %d: %w", k, err)
		}
		for i := 0; i <= k && i < len(truncated); i++ {
			if !sameSignal(full[i], truncated[i]) {
				return fmt.Errorf("lookahead detected: signal[%d] is %v with full data but %v with data truncated at bar %d; only use bars [0..i] for signal i", i, full[i], truncated[i], k)
			}
		}
	}
	return nil
}
