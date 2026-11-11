# stratest

A personal terminal tool. An LLM translates a plain-English strategy into one Go function, which is run in Yaegi and backtested over US equities.

## Goals and non-goals

- Goals: simple, readable code; cache everything; adding a provider, indicator or metric takes one function or file.
- Non-goals: production hardening, live or paper trading, portfolio simulation, non-US sessions, charts, web UI. Error handling stays minimal: return errors up to `cmd`, which prints them.

## Directory map

- `main.go`: calls `cmd.Execute()`.
- `cmd/`: cobra commands. `root.go` (root, ticker parsing), `new.go` (huh wizard + `--yes` path), `run.go` (window, fetch, parallel backtest), `list.go` (list/show/regen/data/cache).
- `internal/config`: paths, env vars, defaults.
- `internal/data`: `Bar`, `Frame`, `Data`, `Session` (buckets, `Resample`, `Align`, `Normalize`; `session.go`), the providers (`yahoo.go`, `polygon.go`, `polygon_futures.go`), and the gob price cache.
- `internal/ta`: indicators exposed to strategy code.
- `internal/strategy`: YAML load/save, the Yaegi sandbox and validation, generated Yaegi symbols.
- `internal/llm`: OpenRouter client, embedded `prompt.md`, the generate → validate → retry loop, and the LLM cache.
- `internal/backtest`: single-ticker engine and metrics.
- `internal/report`: lipgloss tables.
- `strategies/`: saved strategies (committed).

## Design decisions

- **Yaegi for LLM code.** Strategies are plain Go, editable by hand, and run without a compile step. The interpreter only sees `math`, `sort`, `time` (minus `Now`, `Sleep`, timers) and the API package `stratest/sandboxapi` (ta + `Data`/`Frame`, dot-imported). Source is read from an empty filesystem, so no other packages can load. Interpreter output is discarded. `checkSource` rejects other imports and `go` statements before compiling. `Call` recovers panics and enforces a timeout (an infinite loop leaks a goroutine, which is fine for a CLI).
- **Host methods work in Yaegi.** `d.TF`, `d.Ind` with an interpreted closure, and dot imports were prototyped first and all work, so no plain-function fallback was needed.
- **Yaegi concurrency (verified with `-race`).** Calling one compiled `Signals` from many goroutines is race-free only if the strategy has no package-level state. As soon as generated code declares a global var, races appear. So `run` gives each worker goroutine its own interpreter, compiled lazily (cache hits never compile).
- **Strategy as code in YAML.** One portable file per strategy. `Save` swaps tabs for spaces so yaml.v3 writes a readable `code: |` block.
- **Execution rule (hard-coded).** `pos[i+1] = signal[i]` and `ret[i] = pos[i]*(close[i]/close[i-1]-1) - |Δpos|*cost`. The signal computed at the close of bar i is filled at that close and earns bar i+1's move. It never earns the move of the bar it was computed on. The position is flat at the window start, so entering costs one fee. Trades record fill times (bar close times).
- **Fetch the finest timeframe, resample the rest.** One cached series per ticker keeps the cache small and all timeframes consistent. Polygon's 1h/4h bars are clock-aligned, so they're built from 30m bars. Yahoo's 4h is built from 1h bars.
- **Session-anchored buckets.** A `data.Session` is an open offset from the trading day's NY midnight plus a length. `RegularHours` (stocks) is 09:30–16:00; `Globex` (futures, any `=F` ticker, via `SessionOf`) is 18:00 the prior evening to 17:00. `Session.Bucket` anchors intraday buckets at the open (stocks 1h: 09:30–10:30 … 15:30–16:00; 4h: 09:30–13:30, 13:30–16:00), so a bucket never spans sessions. `1d` is a session; `1w` runs Monday to Friday trading days. `Data` carries its ticker's session; the lookahead probe uses `RegularHours`.
- **Completed-bar alignment.** Base bar i sees the latest higher-timeframe bar whose close time is at or before base bar i's close time (a single two-pointer pass, `Session.Align`). Close time is the bucket's *scheduled* end (`End`), not its last bar's end. For complete buckets these are the same. For a partial bucket at the edge of the data, it means the bucket never looks complete early. The lookahead probe depends on this.
- **Lookahead probe.** `Validate` runs the strategy on 2,000 synthetic bars, then on the same bars truncated at three random points k. Signals at bars ≤ k must match exactly.
- **Cache keys.** Prices: `<provider>/<interval>/<ticker>.gob` holds bars plus the *requested* ranges, so weekends and holidays aren't treated as gaps. Coverage never extends past today 00:00 NY, since futures sessions are almost always open. LLM: sha256(description, prompt.md, model), and only validated output is cached. Results: sha256(code, ticker, start, end, base, timeframes, provider, costs, capital, warm-up, `EngineVersion`). The default window ends at today 00:00 NY, so keys stay stable all day.

## Strategy function contract

This is the contract given to the LLM in `internal/llm/prompt.md` (the source of truth):

- Exactly one `func Signals(d *Data) []float64`, plus optional helpers. No `package` line, no imports, no I/O, goroutines or randomness.
- API: `Frame{Time []time.Time; Open, High, Low, Close, Volume []float64}`, `d.Len()`, `d.TF(tf)` (aligned, length `d.Len()`), `d.Native(tf)`, `d.Align(tf, x)`, `d.Ind(tf, fn)` (recommended for indicators). Timeframes: 1m 5m 15m 30m 1h 4h 1d 1w.
- Indicators: SMA, EMA, RMA, RSI, ATR, StdDev, Highest, Lowest, MACD, Bollinger, CrossOver, CrossUnder. Same length as the input, with NaN warm-up.
- Output: a target position per base bar: 1 long, 0 flat, -1 short (only when asked). NaN means 0. No lookahead.
- Response JSON: `{"timeframes": [...], "base": "<finest>", "warmup_days": N, "code": "..."}`.

## Code style

Small functions. Only one interface (`Provider`). No frameworks. Errors are returned and printed in `cmd`. No panics in library code except for programmer errors. The exception is `Data.Native` on an undeclared timeframe, which panics *inside the sandbox* and is recovered as a validation error. Full names, no abbreviations, functional where Go allows.

## How to

- Dev shell: devenv (`devenv.nix`). It activates automatically when you `cd` into the checkout. Build with `go build .`, which writes `./stratest`.
- Test: `go test ./...` (no network). `go test -race ./...` before touching sandbox concurrency.
- Regenerate Yaegi symbols after changing exported `ta` functions or `Data`/`Frame`: `go generate ./internal/strategy`. This runs `yaegi extract` for `ta` and `data` (filtered to `Frame` and `Data`), then strips the Go version build tag and gofmts the output. `yaegi extract` treats the dot-less module path `stratest/...` as stdlib and would otherwise pin the files to the current Go version.
- Add a provider: one file implementing `Provider` plus one line in `data.Providers`.
- Add a metric: a field plus computation in `backtest/metrics.go`, a row in `report.detailRows`, and bump `backtest.EngineVersion`.
- Add an indicator: a function in `ta.go`, then `go generate`, then document it in `prompt.md`.

## Gotchas

- Yahoo's chart endpoint is unofficial and can break or rate-limit. Limits: 1m ≈ 7d, 5m/15m/30m ≈ 60d, 1h ≈ 730d, measured back from *now*, not from the window length. The run checks `today - start` against `MaxHistory` and clips the warm-up. Fetches run 4 at a time.
- Bars are stored in UTC with their *start* time. All bucketing is done in America/New_York (tzdata is embedded), so DST shifts the UTC open between 13:30 and 14:30.
- Partial last bars: the 15:30 1h bar and the 13:30 4h bar are short. Half-days end early, but their daily bar only becomes visible on the next session's first bar.
- The current day is incomplete. The default `--end` is today 00:00 NY, and the cache never marks today covered.
- Futures: `/NQ` is parsed to Yahoo's `NQ=F` and `futures` expands to `data.LiquidFutures`. Neither provider's series is back-adjusted, so rolls appear as price jumps. Yahoo stamps futures daily bars at 00:00 ET of the trading day.
- Polygon futures (`/futures/v1`) only has individual contracts, so `fetchFuture` stitches a series. It takes a contract snapshot (`date=`) every `rollHorizon` (180 days), keeps singles expiring in [from, to + rollHorizon], fetches each one's daily bars, then picks per session the highest-volume contract that doesn't expire earlier than the one already held (`frontContracts`), and fetches intraday bars per held segment. Date filters on `window_start` are midnight UTC, so requests are widened by a day and trimmed. Daily bars are dated by `session_end_date`. Resolutions are `1min`…`1hour`, `1day` (not `1d`); results default to newest first. Without `date=`, the contracts endpoint returns one row per contract per day, which is far too many pages.
- `getJSON` waits out HTTP 429 (15 s, up to 12 tries): Polygon's free plan allows 5 calls a minute.
- Yaegi: `var _, _, _ = ...` in the header failed with "constant definition loop". The header has no blank vars, and Yaegi doesn't reject unused imports. Exports must be built lazily (`sync.OnceValue`), because package vars initialize before the generated `init()` fills `Symbols`.
