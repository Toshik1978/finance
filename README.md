# finance

[![Lint, Test & Build](https://github.com/Toshik1978/finance/actions/workflows/ci.yml/badge.svg)](https://github.com/Toshik1978/finance/actions/workflows/ci.yml)
[![Tests](https://img.shields.io/endpoint?url=https%3A%2F%2Fgist.githubusercontent.com%2FToshik1978%2F1688c3648d3bcfaa71ac95808ee3f084%2Fraw%2Ftests.json)](https://github.com/Toshik1978/finance/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/endpoint?url=https%3A%2F%2Fgist.githubusercontent.com%2FToshik1978%2F1688c3648d3bcfaa71ac95808ee3f084%2Fraw%2Fcoverage.json)](https://github.com/Toshik1978/finance/actions/workflows/ci.yml)

A Go library and CLI for currency exchange rates and equity quotes. Data comes
from Yahoo Finance, with AlphaVantage as an optional fallback provider.

## Commands

### `finance currency [from] [to]`

Prints the current exchange rate between two currency codes (e.g.
`finance currency USD EUR`). With no arguments, prints USD/EUR and GBP/USD.

### `finance quote [ticker]`

Prints the current price and fundamentals for an equity ticker (e.g.
`finance quote AAPL`): name, type, currency, current price, and whatever of
previous close, day open/low/high, volume and P/E ratios the provider
returned.

## Environment Variables

| Variable                     | Description                                                                                     |
|-------------------------------|---------------------------------------------------------------------------------------------------|
| `FINANCE_DB_PATH`             | Path to the SQLite database holding cached exchange rates. Defaults to `$HOME/.local/share/finance/finance.db` when unset. |
| `FINANCE_ALPHAVANTAGE_KEY`    | AlphaVantage API key. Leave empty to run with Yahoo Finance alone; set it to register AlphaVantage as a fallback provider. Free tier is 25 requests/day: https://www.alphavantage.co/support/#api-key |
| `FINANCE_HTTP_TIMEOUT`        | Per-request HTTP timeout, in Go duration syntax (e.g. `15s`).                                     |

Copy `.env.dist` to `.env` and fill in what you need.

## Build

Requires Go 1.27 (see `.mise.toml`). Tasks run via [go-task](https://taskfile.dev):

```bash
task setup   # download modules
task build   # compile bin/finance, CGO_ENABLED=0
```

`task lint` and `task test` run the same gates CI enforces.

## Library

```go
log := slog.New(slog.NewTextHandler(os.Stderr, nil))
providers := []finance.Provider{yahooProvider, alphaProvider}
client := finance.NewClient(log, providers, storage)

quote, err := client.Quote(ctx, "AAPL")
rate, err := client.RateToday(ctx, "USD", "EUR")
past, err := client.Rate(ctx, "USD", "EUR", someDate)
matches, err := client.Search(ctx, "apple")
history, err := client.History(ctx, "AAPL", begin, end)
```

`finance.Client` tries each provider in order, falling back on the next when one
returns `finance.ErrNotFound` or a transport error. When all of them fail it
returns every error joined, each tagged with its provider.

The chain is sequential rather than fan-out on purpose: the first good answer
wins, and querying every provider at once would spend AlphaVantage's 25 requests
a day even when Yahoo answers. There is no concurrency anywhere in the library.

Only exchange rates are cached, through `finance.CurrencyStorage` —
`storage/memory` for tests and callers without a database, `storage/sqldb` for a
`*sql.DB` you supply. Quotes and history are always fetched live.

The library packages (`finance`, `civil`, `provider/...`, `storage/...`) depend
on exactly one non-stdlib module, `github.com/shopspring/decimal`; see
`CLAUDE.md` for the full approved dependency list.

### `Client.Align`

```go
series, err := client.Align(ctx, histories, "USD", 52, 7)
```

`Align` resamples several instruments' histories onto one shared set of dates and
converts them into a single currency, using the rate on each bar's own date so
the result reflects the FX move as well as the price move. Instruments with too
little history are dropped and logged; if the rest share too few dates it fails
with `ErrInsufficientHistory`.

Nothing in this repository calls it. It exists to feed a covariance matrix: that
calculation is meaningless unless every series is sampled on identical dates in a
common currency, which is exactly what `Align` guarantees and what makes it
awkward to reproduce ad hoc. Its test suite is the only thing keeping it correct
— treat those tests as the specification.

One sharp edge: an instrument is dropped when it has fewer than `days * interval`
bars, which counts trading bars against a calendar-day threshold. A full year of
daily history is about 252 bars, so a request for 52 weekly observations
(`days=52, interval=7`, i.e. 364) will drop it. Each drop is logged at `Warn`
with the ticker and bar count.

## Out of scope

Deliberate non-goals, recorded so they need not be re-argued:

- **Portfolio analytics.** CAPM, VaR, Sharpe, Sortino, Treynor and covariance
  are left to whatever consumes the data. This tool's output is terminal
  quotes; the library hands you series and stops there.
- **OpenFIGI identifier mapping**, broker holdings integration, and any
  benchmark-index concept beyond the `TypeIndex` constant.
- **Generalised caching.** No code path generates the quota pressure that would
  justify caching quotes or history.
- **Capability interfaces on `Provider`.** Both providers implement all six
  methods, so nothing is stubbed and the ceremony would buy nothing.
