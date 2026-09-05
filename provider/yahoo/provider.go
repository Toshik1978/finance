package yahoo

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/Toshik1978/finance"
	"github.com/Toshik1978/finance/civil"
)

// exchanges maps Yahoo's exchange codes onto the domain's.
var exchanges = map[string]string{ //nolint:gochecknoglobals // immutable lookup table, not state.
	"NMS": finance.ExchangeNASDAQ,
	"NAS": finance.ExchangeNASDAQ,
	"NYQ": finance.ExchangeNYSE,
	"CNQ": finance.ExchangeCSE,
	"CSE": finance.ExchangeCSE,
	"GER": finance.ExchangeXETRA,
}

// Provider adapts the Yahoo Finance API to finance.Provider.
type Provider struct {
	log *slog.Logger
	c   *Client
}

// New creates a Yahoo Finance provider over the given client.
func New(log *slog.Logger, c *Client) *Provider {
	return &Provider{log: log, c: c}
}

// Name identifies the provider in logs and aggregated errors.
func (p *Provider) Name() string { return "Yahoo Finance" }

// Search finds instruments matching free-text keywords.
func (p *Provider) Search(ctx context.Context, keywords string) ([]finance.Match, error) {
	resp, err := p.c.Search(ctx, keywords)
	if err != nil {
		return nil, fmt.Errorf("failed to search yahoo for %q: %w", keywords, err)
	}
	if len(resp.Quotes) == 0 {
		return nil, fmt.Errorf("yahoo has no matches for %q: %w", keywords, finance.ErrNotFound)
	}

	matches := make([]finance.Match, len(resp.Quotes))
	for i, q := range resp.Quotes {
		matches[i] = finance.Match{Ticker: q.Symbol, Type: q.Type, Name: q.Name}
	}

	return matches, nil
}

// Rate retrieves the current exchange rate for the pair, using Yahoo's pair notation.
func (p *Provider) Rate(ctx context.Context, from, to string) (finance.ExchangeRate, error) {
	symbol := p.pair(from, to)

	resp, err := p.c.Quote(ctx, symbol)
	if err != nil {
		return finance.ExchangeRate{}, fmt.Errorf("failed to get yahoo rate %s/%s: %w", from, to, err)
	}

	r, err := p.result(resp, symbol)
	if err != nil {
		return finance.ExchangeRate{}, err
	}

	return finance.ExchangeRate{
		Date:  civil.DateOf(time.Unix(r.Price.Timestamp, 0).UTC()),
		From:  from,
		To:    to,
		Price: r.Price.Price.decimal(),
	}, nil
}

// RateHistory retrieves daily exchange rates for the pair over the range.
func (p *Provider) RateHistory(
	ctx context.Context, from, to string, begin, end civil.Date,
) ([]finance.ExchangeRate, error) {
	symbol := p.pair(from, to)

	resp, err := p.c.Chart(ctx, symbol, begin, end)
	if err != nil {
		return nil, fmt.Errorf("failed to get yahoo rate history %s/%s: %w", from, to, err)
	}

	r, err := p.series(resp, symbol)
	if err != nil {
		return nil, err
	}

	series, err := chartSeries(r, symbol)
	if err != nil {
		return nil, err
	}
	n := min(len(r.Timestamp), len(series.Close))

	rates := make([]finance.ExchangeRate, n)
	for i := range n {
		rates[i] = finance.ExchangeRate{
			Date:  civil.DateOf(time.Unix(r.Timestamp[i], 0).UTC()),
			From:  from,
			To:    to,
			Price: series.Close[i].Decimal,
		}
	}

	slices.SortFunc(rates, func(a, b finance.ExchangeRate) int {
		switch {
		case a.Date.Before(b.Date):
			return -1
		case b.Date.Before(a.Date):
			return 1
		default:
			return 0
		}
	})

	return rates, nil
}

// Quote retrieves the current price and fundamentals for a ticker.
func (p *Provider) Quote(ctx context.Context, ticker string) (finance.Quote, error) {
	resp, err := p.c.Quote(ctx, ticker)
	if err != nil {
		return finance.Quote{}, fmt.Errorf("failed to get yahoo quote for %q: %w", ticker, err)
	}

	r, err := p.result(resp, ticker)
	if err != nil {
		return finance.Quote{}, err
	}

	return finance.Quote{
		Ticker:      r.QuoteType.Symbol,
		Type:        r.QuoteType.Type,
		Name:        r.QuoteType.Name,
		Description: r.QuoteType.LongName,
		Exchange:    p.exchange(ctx, r.QuoteType.Exchange),
		Currency:    r.Price.Currency,

		PreviousDayClose: r.Price.PreviousDayClose.decimal(),
		DayOpen:          r.Price.DayOpen.decimal(),
		DayLow:           r.Price.DayLow.decimal(),
		DayHigh:          r.Price.DayHigh.decimal(),
		Price:            r.Price.Price.decimal(),
		FiftyTwoWeekLow:  r.SummaryDetail.FiftyTwoWeekLow.decimal(),
		FiftyTwoWeekHigh: r.SummaryDetail.FiftyTwoWeekHigh.decimal(),
		Volume:           r.Price.Volume.decimal().IntPart(),

		Industry:      r.AssetProfile.Industry,
		Sector:        r.AssetProfile.Sector,
		Beta:          r.SummaryDetail.Beta.decimal(),
		PriceToBook:   r.DefaultKeyStatistics.PriceToBook.decimal(),
		TrailingPE:    r.SummaryDetail.TrailingPE.decimal(),
		ForwardPE:     r.SummaryDetail.ForwardPE.decimal(),
		DividendYield: r.SummaryDetail.DividendYield.decimal(),
	}, nil
}

// History retrieves daily price bars for a ticker over the range.
func (p *Provider) History(ctx context.Context, ticker string, begin, end civil.Date) (finance.History, error) {
	q, err := p.Quote(ctx, ticker)
	if err != nil {
		return finance.History{}, fmt.Errorf("failed to get yahoo instrument for history of %q: %w", ticker, err)
	}

	resp, err := p.c.Chart(ctx, ticker, begin, end)
	if err != nil {
		return finance.History{}, fmt.Errorf("failed to get yahoo chart for history of %q: %w", ticker, err)
	}

	r, err := p.series(resp, ticker)
	if err != nil {
		return finance.History{}, err
	}

	series, err := chartSeries(r, ticker)
	if err != nil {
		return finance.History{}, err
	}
	n := min(
		len(r.Timestamp), len(series.Open), len(series.Close), len(series.Low), len(series.High), len(series.Volume),
	)

	bars := make([]finance.Bar, n)
	for i := range n {
		bars[i] = finance.Bar{
			Date:   civil.DateOf(time.Unix(r.Timestamp[i], 0).UTC()),
			Open:   series.Open[i].Decimal,
			Close:  series.Close[i].Decimal,
			Low:    series.Low[i].Decimal,
			High:   series.High[i].Decimal,
			Volume: series.Volume[i],
		}
	}

	slices.SortFunc(bars, func(a, b finance.Bar) int {
		switch {
		case a.Date.Before(b.Date):
			return -1
		case b.Date.Before(a.Date):
			return 1
		default:
			return 0
		}
	})

	return finance.History{
		Instrument: q.Instrument,
		Chart:      bars,
	}, nil
}

// result unwraps the single expected element of a quote response, turning
// Yahoo's several ways of saying "nothing" into ErrNotFound.
func (p *Provider) result(r quoteResponse, ticker string) (quoteResult, error) {
	return singleResult(r.Inner.Result, r.Inner.Error, ticker)
}

// series unwraps the single expected element of a chart response, turning
// Yahoo's several ways of saying "nothing" into ErrNotFound.
func (p *Provider) series(r chartResponse, ticker string) (chartResult, error) {
	return singleResult(r.Inner.Result, r.Inner.Error, ticker)
}

// singleResult picks the one expected element out of a Yahoo result envelope,
// turning an upstream error object or an empty envelope into ErrNotFound.
//
//nolint:ireturn // T is the caller's own result type, not an interface.
func singleResult[T any](result []T, upstreamErr *yahooError, ticker string) (T, error) {
	var zero T

	if upstreamErr != nil {
		return zero, fmt.Errorf("yahoo rejected %q: %s", ticker, upstreamErr.Description)
	}
	if len(result) == 0 {
		return zero, fmt.Errorf("yahoo has no %q: %w", ticker, finance.ErrNotFound)
	}
	if len(result) > 1 {
		return zero, fmt.Errorf("yahoo returned %d results for %q", len(result), ticker)
	}

	return result[0], nil
}

// chartSeries validates that r holds a decoded quote series and returns it.
// The caller must still bound its own reads against len(r.Timestamp): Yahoo's
// arrays can decode successfully while disagreeing in length.
func chartSeries(r chartResult, ticker string) (quoteSeries, error) {
	if len(r.Indicators.Quote) == 0 {
		return quoteSeries{}, fmt.Errorf("yahoo returned no series for %q: %w", ticker, finance.ErrNotFound)
	}

	return r.Indicators.Quote[0], nil
}

// exchange normalises a Yahoo exchange code, warning on an unknown code.
func (p *Provider) exchange(ctx context.Context, code string) string {
	x, ok := exchanges[code]
	if !ok {
		p.log.WarnContext(ctx, "unsupported yahoo exchange", slog.String("exchange", code))
	}

	return x
}

// pair renders a currency pair in Yahoo's notation: USD -> EUR is "USDEUR=X".
func (p *Provider) pair(from, to string) string {
	return from + to + "=X"
}
