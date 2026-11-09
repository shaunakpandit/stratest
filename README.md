# stratest

Describe a trading strategy in plain English. An LLM (via OpenRouter) turns it into a small Go function, which is validated, saved as one YAML file, and backtested on all your tickers in parallel. Results print as terminal tables. Strategies can mix timeframes, for example a 15-minute signal filtered by a 4-hour trend.

```
$ stratest run example_sma_cross --period 45d
╭───────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ Strategy:     example_sma_cross                                                                       │
│ Description:  Buy when the 15m SMA(20) crosses above the 4h SMA(50), sell when it crosses back below. │
│ Timeframes:   15m + 4h                                                                                │
│ Window:       2026-09-23 → 2026-11-07                                                                 │
│ Provider:     yahoo                                                                                   │
│ Tickers:      10                                                                                      │
│ Cache:        prices all cached · results 10/10 cached                                                │
╰───────────────────────────────────────────────────────────────────────────────────────────────────────╯

Leaderboard
╭────┬────────┬──────────┬──────────┬────────┬───────┬────────┬─────────┬──────────╮
│ #  │ Ticker │ Return % │ Max DD % │ Trades │ Win % │ Sharpe │ B&H %   │ vs SPY % │
├────┼────────┼──────────┼──────────┼────────┼───────┼────────┼─────────┼──────────┤
│ 1  │ JPM    │ -0.88%   │ -5.28%   │ 2      │ 50.0  │ -0.41  │ -1.11%  │ +3.86%   │
│ 2  │ NVDA   │ -2.66%   │ -2.66%   │ 1      │ 0.0   │ -4.69  │ -18.39% │ +2.08%   │
│ 3  │ GOOGL  │ -3.18%   │ -4.31%   │ 1      │ 0.0   │ -3.56  │ -17.93% │ +1.56%   │
│ …  │        │          │          │        │       │        │         │          │
╰────┴────────┴──────────┴──────────┴────────┴───────┴────────┴─────────┴──────────╯

Top 3
╭───────────────┬─────────┬─────────┬─────────╮
│ Metric        │ JPM     │ NVDA    │ GOOGL   │
├───────────────┼─────────┼─────────┼─────────┤
│ Profit $      │ -87.53  │ -265.59 │ -317.75 │
│ Return        │ -0.88%  │ -2.66%  │ -3.18%  │
│ Max drawdown  │ -5.28%  │ -2.66%  │ -4.31%  │
│ …             │         │         │         │
╰───────────────┴─────────┴─────────┴─────────╯

Trades for JPM (2 of 2)
╭──────┬──────────────────┬─────────┬──────────────────┬────────┬──────────┬──────╮
│ Side │ Entry            │ Entry $ │ Exit             │ Exit $ │ Return % │ Bars │
├──────┼──────────────────┼─────────┼──────────────────┼────────┼──────────┼──────┤
│ long │ 2026-10-16 12:30 │ 340.34  │ 2026-10-19 11:30 │ 337.19 │ -0.97%   │ 22   │
│ long │ 2026-10-26 16:00 │ 334.12  │ 2026-11-06 16:00 │ 334.49 │ +0.07%   │ 234  │
╰──────┴──────────────────┴─────────┴──────────────────┴────────┴──────────┴──────╯
```

## Setup

1. Install [devenv](https://devenv.sh) and add `eval "$(devenv hook zsh)"` (or `bash`) to your shell config.
2. Run `devenv allow` in the checkout once. After that, `cd` into it activates Go, gopls, golangci-lint and gotools.
3. `cp .env.example .env`, then add an OpenRouter key from <https://openrouter.ai/keys>. Set `POLYGON_API_KEY` only if you want Polygon data. The binary reads `.env` from the directory you run it in, and variables already set in your shell take priority.
4. `go build . && ./stratest --help`

## Quickstart

```
./stratest new
```

The wizard asks for:

1. **Name**: a slug such as `sma_15m_x_4h`.
2. **Description**: plain English. See below.
3. **Tickers**: pick from tickers you've already downloaded, and/or type new ones (`AAPL, MSFT, NVDA`). Nothing is downloaded yet.
4. **Review**: Generate, Edit, or Cancel.
5. **Generate**: the LLM writes the code, which is then compiled and checked on synthetic data, including a lookahead probe. If validation fails, the error goes back to the LLM for one retry.
6. **Result**: shows the detected timeframes, the base interval, warm-up days, and the code. Choose Save, Regenerate, Edit description, or Discard.

Then:

```
./stratest run sma_15m_x_4h --period 45d
```

For scripts: `stratest new --name X --desc "..." --tickers A,B --yes`. Each flag you pass skips its own step, and `--yes` skips everything else.

| Command | Purpose |
|---|---|
| `stratest new` | Interactive wizard |
| `stratest run <name> [--period 1y] [--start D --end D] [--tickers A,B] [--provider yahoo\|polygon] [--no-cache] [--top 3] [--trades]` | Backtest. Also `--fee-bps`, `--slip-bps`, `--capital` |
| `stratest list` / `show <name>` | List strategies / print one |
| `stratest regen <name>` | Regenerate the code from the saved description, bypassing the LLM cache |
| `stratest data` | Downloaded data: ticker, provider, interval, range, size |
| `stratest cache clear [--prices] [--llm] [--results]` | Clear caches (all of them without flags) |

Strategies live in `strategies/<name>.yaml`, with the code inline. Edit the code by hand and the next `run` uses it.

## Writing good descriptions

Name the timeframe of every indicator, state the entry and exit rules, and say "short" if you want shorts (strategies are long-only by default).

- `Buy when the daily RSI(14) drops below 30, sell when it rises above 70.`
- `Go long when the 1h close is above the upper Bollinger band (20, 2) and the 1h volume is above its 20-bar average; exit when the close falls back below the middle band.`
- Cross-timeframe: `Buy when the 15m SMA(20) crosses above the 4h SMA(50), sell when it crosses back below. Only take entries when the daily close is above the daily EMA(20).`

Rules like "buy when X, sell when Y" become stateful: the position is held until the exit rule fires.

## Data providers

| Provider | Key | Intraday history |
|---|---|---|
| `yahoo` (default) | none | 1m ≈ 7 days · 5m/15m/30m ≈ 60 days · 1h/4h ≈ 730 days · 1d unlimited |
| `polygon` | `POLYGON_API_KEY` | depends on your plan |

Only the strategy's **base** timeframe (its finest) is downloaded. Every coarser timeframe is resampled locally into buckets anchored at the 09:30 New York open. So a cross-timeframe strategy needs:

- every intraday timeframe to be a whole multiple of the base (`15m` + `4h` works, `1h` + `45m` doesn't; `1d` and `1w` always work), and
- the provider to have base-interval data for the whole window. With Yahoo, a 15m strategy can only test about 60 days back, so `--period 1y` fails right away with a hint. Use `--period 45d`, Polygon, or a coarser base. If the warm-up would reach past the provider's limit, it is clipped and a note is printed.

Prices are regular-session only (09:30–16:00 ET), adjusted where the source allows. SPY is always fetched as the benchmark.

## Caching

Everything lives under `.cache/` (gitignored):

- `prices/<provider>/<interval>/<ticker>.gob`: bars plus the time ranges already requested. Only missing ranges are fetched, and today's open session is refetched until it closes.
- `llm/<prompt_hash>.json`: validated LLM output, keyed by description + prompt template + model.
- `results/<hash>.gob`: backtest results, keyed by code, ticker, window, timeframes, provider, costs, and engine version.

A repeat run makes no network calls. Clear caches with `stratest cache clear [--prices] [--llm] [--results]`, or bypass them with `run --no-cache` / `regen`.

## Extending

- **Provider**: add `internal/data/<name>.go` implementing `Provider` (`Name`, `Fetch`, `MaxHistory`), return normalized bars (`Normalize`), and add one line to `Providers` in `provider.go`.
- **Indicator**: add a function to `internal/ta/ta.go`, run `go generate ./internal/strategy` to refresh the Yaegi symbols, and list its signature in `internal/llm/prompt.md`. Editing the prompt changes `prompt_hash`, so cached LLM output is not reused.
- **Metric**: add a field to `Metrics` and compute it in `internal/backtest/metrics.go`, add a row to `detailRows` in `internal/report/report.go`, and bump `EngineVersion` so cached results are recomputed.
