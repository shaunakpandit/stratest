package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"stratest/internal/backtest"
	"stratest/internal/config"
	"stratest/internal/data"
	"stratest/internal/report"
	"stratest/internal/strategy"
)

var runFlags struct {
	period, start, end, tickers, provider string
	noCache, trades                       bool
	top                                   int
	fee, slip, capital                    float64
}

func init() {
	command := &cobra.Command{Use: "run <name>", Short: "Backtest a saved strategy", Args: cobra.ExactArgs(1), RunE: run}
	flags := command.Flags()
	flags.StringVar(&runFlags.period, "period", config.Period, "window length: 30d, 6mo, 1y, 2y")
	flags.StringVar(&runFlags.start, "start", "", "window start date (YYYY-MM-DD), overrides --period")
	flags.StringVar(&runFlags.end, "end", "", "window end date (YYYY-MM-DD, exclusive), default today")
	flags.StringVar(&runFlags.tickers, "tickers", "", "comma-separated tickers, overrides the strategy file")
	flags.StringVar(&runFlags.provider, "provider", config.Provider(), "data provider: yahoo or polygon")
	flags.BoolVar(&runFlags.noCache, "no-cache", false, "refetch prices and recompute results")
	flags.IntVar(&runFlags.top, "top", config.Top, "tickers in the detail table")
	flags.BoolVar(&runFlags.trades, "trades", false, "show every trade of the best ticker, not just the last 10")
	flags.Float64Var(&runFlags.fee, "fee-bps", config.FeeBps, "fee per side in basis points")
	flags.Float64Var(&runFlags.slip, "slip-bps", config.SlipBps, "slippage per side in basis points")
	flags.Float64Var(&runFlags.capital, "capital", config.Capital, "starting capital")
	root.AddCommand(command)
}

var periodPattern = regexp.MustCompile(`^(\d+)(d|w|mo|y)$`)

func today() time.Time {
	now := data.Now().In(data.NY)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, data.NY)
}

// window defaults to [today - period, today): whole past days, so the cache keys stay stable all day.
func window(period, start, end string) (time.Time, time.Time, error) {
	to, from := today(), time.Time{}
	var err error
	if end != "" {
		if to, err = time.ParseInLocation("2006-01-02", end, data.NY); err != nil {
			return from, to, err
		}
	}
	if start != "" {
		from, err = time.ParseInLocation("2006-01-02", start, data.NY)
		return from, to, err
	}
	match := periodPattern.FindStringSubmatch(period)
	if match == nil {
		return from, to, fmt.Errorf("bad --period %q; use e.g. 30d, 6mo, 1y", period)
	}
	n, _ := strconv.Atoi(match[1])
	switch match[2] {
	case "d":
		from = to.AddDate(0, 0, -n)
	case "w":
		from = to.AddDate(0, 0, -7*n)
	case "mo":
		from = to.AddDate(0, -n, 0)
	case "y":
		from = to.AddDate(-n, 0, 0)
	}
	return from, to, nil
}

func run(_ *cobra.Command, args []string) error {
	s, err := strategy.Load(config.StrategiesDir, args[0])
	if err != nil {
		return err
	}
	if err := data.CheckTimeframes(s.Timeframes, s.Base); err != nil {
		return err
	}
	tickers := s.Tickers
	if runFlags.tickers != "" {
		tickers = parseTickers(runFlags.tickers)
	}
	if len(tickers) == 0 {
		return fmt.Errorf("no tickers: add some to the strategy file or pass --tickers")
	}
	provider, err := data.NewProvider(runFlags.provider)
	if err != nil {
		return err
	}
	start, end, err := window(runFlags.period, runFlags.start, runFlags.end)
	if err != nil {
		return err
	}
	limit := provider.MaxHistory(s.Base)
	if today().Sub(start) > limit {
		return fmt.Errorf("%s only provides %s data for ~%d days; use --provider polygon, a shorter --period, or a coarser base timeframe",
			provider.Name(), s.Base, int(limit.Hours()/24))
	}
	// Warm-up is clipped to what the provider can serve; early signals may then start as NaN.
	from := start.AddDate(0, 0, -s.WarmupDays)
	if earliest := today().Add(-limit).AddDate(0, 0, 1); from.Before(earliest) {
		from = earliest
		fmt.Fprintf(os.Stderr, "note: warm-up clipped to %s by the %s history limit\n", from.Format("2006-01-02"), provider.Name())
	}

	bars, fetched, err := fetchAll(provider, parseTickers(strings.Join(tickers, ","), config.Benchmark), s.Base, from, end)
	if err != nil {
		return err
	}
	results, hits, err := backtestAll(s, tickers, bars, start, end, provider.Name())
	if err != nil {
		return err
	}
	priceCache := "all cached"
	if fetched > 0 {
		priceCache = fmt.Sprintf("fetched %d/%d", fetched, len(bars))
	}
	report.Print(os.Stdout, report.Header{
		Name: s.Name, Description: s.Description, Provider: provider.Name(), Timeframes: s.Timeframes,
		Start: start, End: end, Tickers: len(tickers), PriceCache: priceCache,
		ResultCache: fmt.Sprintf("%d/%d cached", hits, len(tickers)),
	}, results, buyHold(bars[config.Benchmark], start), runFlags.top, runFlags.trades)
	return nil
}

func buyHold(bars []data.Bar, start time.Time) float64 {
	index := slices.IndexFunc(bars, func(b data.Bar) bool { return !b.Time.Before(start) })
	if index < 0 {
		return 0
	}
	return bars[len(bars)-1].Close/bars[index].Close - 1
}

// fetchAll loads every ticker through the cache, a few at a time.
func fetchAll(p data.Provider, tickers []string, interval string, from, to time.Time) (map[string][]data.Bar, int, error) {
	var mutex sync.Mutex
	var group sync.WaitGroup
	limit := make(chan struct{}, config.FetchParallel)
	out, fetched, errs := map[string][]data.Bar{}, 0, []error{}
	for _, ticker := range tickers {
		group.Add(1)
		go func() {
			defer group.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			bars, network, err := data.Load(config.PricesDir, p, ticker, interval, from, to, runFlags.noCache)
			if err == nil && len(bars) == 0 {
				err = fmt.Errorf("no %s data for %s between %s and %s", interval, ticker, from.Format("2006-01-02"), to.Format("2006-01-02"))
			}
			mutex.Lock()
			defer mutex.Unlock()
			out[ticker] = bars
			if network {
				fetched++
			}
			if err != nil {
				errs = append(errs, err)
			}
		}()
	}
	group.Wait()
	return out, fetched, errors.Join(errs...)
}

// backtestAll runs tickers in parallel. Each worker owns its interpreter: one compiled Signals is
// not safe to share once the strategy has package-level state. Cached results skip compilation.
func backtestAll(s *strategy.Strategy, tickers []string, bars map[string][]data.Bar, start, end time.Time, provider string) ([]backtest.Result, int, error) {
	cfg := backtest.Config{FeeBps: runFlags.fee, SlipBps: runFlags.slip, Capital: runFlags.capital, Start: start}
	results := make([]backtest.Result, len(tickers))
	errs := make([]error, len(tickers))
	cached := make([]bool, len(tickers))
	jobs := make(chan int)
	var group sync.WaitGroup
	for range min(runtime.NumCPU(), len(tickers)) {
		group.Add(1)
		go func() {
			defer group.Done()
			var signals strategy.SignalFunc
			for index := range jobs {
				ticker := tickers[index]
				key := strategy.Hash(s.Code, ticker, start.String(), end.String(), s.Base, strings.Join(s.Timeframes, ","), provider,
					fmt.Sprint(cfg.FeeBps, cfg.SlipBps, cfg.Capital, s.WarmupDays), backtest.EngineVersion)
				path := filepath.Join(config.ResultsDir, key+".gob")
				if !runFlags.noCache && data.ReadGob(path, &results[index]) == nil {
					cached[index] = true
					continue
				}
				if signals == nil {
					if signals, errs[index] = strategy.Compile(s.Code); errs[index] != nil {
						continue
					}
				}
				d := data.NewData(s.Base, bars[ticker], s.Timeframes)
				signal, err := strategy.Call(signals, d, time.Minute)
				if err == nil && len(signal) != d.Len() {
					err = fmt.Errorf("Signals returned %d values for %d bars", len(signal), d.Len())
				}
				if err != nil {
					errs[index] = fmt.Errorf("%s: %w", ticker, err)
					continue
				}
				results[index] = backtest.Run(d, signal, cfg)
				results[index].Ticker = ticker
				errs[index] = data.WriteGob(path, results[index])
			}
		}()
	}
	for index := range tickers {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, 0, err
	}
	return results, len(slices.DeleteFunc(cached, func(hit bool) bool { return !hit })), nil
}
