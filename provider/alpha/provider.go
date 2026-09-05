package alpha

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/Toshik1978/finance"
	"github.com/Toshik1978/finance/civil"
)

// Provider adapts the AlphaVantage API to finance.Provider.
type Provider struct {
	log *slog.Logger
	c   *Client
}

// New creates an AlphaVantage provider over the given client.
func New(log *slog.Logger, c *Client) *Provider {
	return &Provider{log: log, c: c}
}

// Name identifies the provider in logs and aggregated errors.
func (p *Provider) Name() string { return "AlphaVantage" }

// Search finds instruments matching free-text keywords.
func (p *Provider) Search(ctx context.Context, keywords string) ([]finance.Match, error) {
	resp, err := p.c.Search(ctx, keywords)
	if err != nil {
		return nil, fmt.Errorf("failed to search alphavantage for %q: %w", keywords, err)
	}
	if len(resp.BestMatches) == 0 {
		return nil, fmt.Errorf("alphavantage has no matches for %q: %w", keywords, finance.ErrNotFound)
	}

	matches := make([]finance.Match, len(resp.BestMatches))
	for i := range resp.BestMatches {
		m := &resp.BestMatches[i]
		matches[i] = finance.Match{Ticker: m.Symbol, Type: m.Type, Name: m.Name}
	}

	return matches, nil
}

// Rate retrieves the current exchange rate for the pair. A pair AlphaVantage
// cannot source is ErrNotFound: it is never triangulated through a third currency.
func (p *Provider) Rate(ctx context.Context, from, to string) (finance.ExchangeRate, error) {
	resp, err := p.c.Rate(ctx, from, to)
	if err != nil {
		return finance.ExchangeRate{}, fmt.Errorf("failed to get alphavantage rate %s/%s: %w", from, to, err)
	}

	r := resp.RealtimeCurrencyExchangeRate
	if r.FromCurrencyCode == "" {
		return finance.ExchangeRate{}, fmt.Errorf(
			"alphavantage has no rate for %s/%s: %w",
			from,
			to,
			finance.ErrNotFound,
		)
	}

	// LastRefreshed is "2024-02-11 17:59:08"; only the date part is a civil.Date.
	date, err := civil.ParseDate(strings.SplitN(r.LastRefreshed, " ", 2)[0])
	if err != nil {
		return finance.ExchangeRate{}, fmt.Errorf(
			"failed to parse alphavantage rate timestamp %q: %w",
			r.LastRefreshed,
			err,
		)
	}

	return finance.ExchangeRate{
		Date:  date,
		From:  from,
		To:    to,
		Price: r.ExchangeRate.Decimal,
	}, nil
}

// RateHistory retrieves daily exchange rates for the pair over the range.
func (p *Provider) RateHistory(
	ctx context.Context, from, to string, begin, end civil.Date,
) ([]finance.ExchangeRate, error) {
	resp, err := p.c.RateChart(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to get alphavantage rate history %s/%s: %w", from, to, err)
	}
	if len(resp.TimeSeriesFXDaily) == 0 {
		return nil, fmt.Errorf("alphavantage has no rate history for %s/%s: %w", from, to, finance.ErrNotFound)
	}

	rates := make([]finance.ExchangeRate, 0, len(resp.TimeSeriesFXDaily))
	for date, bar := range resp.TimeSeriesFXDaily {
		if !date.Between(begin, end) {
			continue
		}

		rates = append(rates, finance.ExchangeRate{Date: date, From: from, To: to, Price: bar.Close.Decimal})
	}
	if len(rates) == 0 {
		return nil, fmt.Errorf("alphavantage has no rate history for %s/%s in range: %w", from, to, finance.ErrNotFound)
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

// Quote retrieves the current price and fundamentals for a ticker, combining
// AlphaVantage's GLOBAL_QUOTE and OVERVIEW calls.
func (p *Provider) Quote(ctx context.Context, ticker string) (finance.Quote, error) {
	q, err := p.c.Quote(ctx, ticker)
	if err != nil {
		return finance.Quote{}, fmt.Errorf("failed to get alphavantage quote for %q: %w", ticker, err)
	}
	if q.GlobalQuote.Symbol == "" {
		return finance.Quote{}, fmt.Errorf("alphavantage has no quote for %q: %w", ticker, finance.ErrNotFound)
	}

	o, err := p.c.Overview(ctx, ticker)
	if err != nil {
		return finance.Quote{}, fmt.Errorf("failed to get alphavantage overview for %q: %w", ticker, err)
	}

	inst, err := instrument(o, ticker)
	if err != nil {
		return finance.Quote{}, err
	}
	p.log.DebugContext(ctx, "combined alphavantage quote and overview", slog.String("ticker", ticker))

	g := q.GlobalQuote

	return finance.Quote{
		Instrument: inst,

		PreviousDayClose: g.PreviousClose.Decimal,
		DayOpen:          g.Open.Decimal,
		DayLow:           g.Low.Decimal,
		DayHigh:          g.High.Decimal,
		Price:            g.Price.Decimal,
		FiftyTwoWeekLow:  o.FiftyTwoWeekLow.Decimal,
		FiftyTwoWeekHigh: o.FiftyTwoWeekHigh.Decimal,
		Volume:           g.Volume.IntPart(),

		Industry:      o.Industry,
		Sector:        o.Sector,
		Beta:          o.Beta.Decimal,
		PriceToBook:   o.PriceToBookRatio.Decimal,
		TrailingPE:    o.TrailingPE.Decimal,
		ForwardPE:     o.ForwardPE.Decimal,
		DividendYield: o.DividendYield.Decimal,
	}, nil
}

// History retrieves daily price bars for a ticker over the range, using
// OVERVIEW for instrument fields and TIME_SERIES_DAILY for the bars.
func (p *Provider) History(ctx context.Context, ticker string, begin, end civil.Date) (finance.History, error) {
	o, err := p.c.Overview(ctx, ticker)
	if err != nil {
		return finance.History{}, fmt.Errorf("failed to get alphavantage overview for history of %q: %w", ticker, err)
	}

	inst, err := instrument(o, ticker)
	if err != nil {
		return finance.History{}, err
	}

	bars, err := p.dailyBars(ctx, ticker, begin, end)
	if err != nil {
		return finance.History{}, err
	}

	return finance.History{Instrument: inst, Chart: bars}, nil
}

// dailyBars retrieves TIME_SERIES_DAILY for ticker, filtered to the range and
// sorted ascending by date.
func (p *Provider) dailyBars(ctx context.Context, ticker string, begin, end civil.Date) ([]finance.Bar, error) {
	resp, err := p.c.Chart(ctx, ticker)
	if err != nil {
		return nil, fmt.Errorf("failed to get alphavantage chart for history of %q: %w", ticker, err)
	}
	if len(resp.TimeSeriesDaily) == 0 {
		return nil, fmt.Errorf("alphavantage has no chart for %q: %w", ticker, finance.ErrNotFound)
	}

	bars := make([]finance.Bar, 0, len(resp.TimeSeriesDaily))
	for date, bar := range resp.TimeSeriesDaily {
		if !date.Between(begin, end) {
			continue
		}

		bars = append(bars, finance.Bar{
			Date: date, Open: bar.Open.Decimal, Close: bar.Close.Decimal,
			Low: bar.Low.Decimal, High: bar.High.Decimal, Volume: bar.Volume.IntPart(),
		})
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("alphavantage has no bars for %q in range: %w", ticker, finance.ErrNotFound)
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

	return bars, nil
}

// instrument maps an OVERVIEW response onto the domain Instrument, or
// ErrNotFound if AlphaVantage has no overview for the ticker.
func instrument(o overviewResponse, ticker string) (finance.Instrument, error) {
	if o.Symbol == "" {
		return finance.Instrument{}, fmt.Errorf("alphavantage has no overview for %q: %w", ticker, finance.ErrNotFound)
	}

	return finance.Instrument{
		Ticker:      o.Symbol,
		Type:        o.AssetType,
		Name:        o.Name,
		Description: o.Description,
		Exchange:    o.Exchange,
		Country:     o.Country,
		Currency:    o.Currency,
	}, nil
}
