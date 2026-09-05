package finance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Toshik1978/finance/civil"
)

// Client retrieves financial data, trying each provider in turn until one
// answers. Providers are supplied in preference order.
type Client struct {
	log       *slog.Logger
	providers []Provider
	storage   CurrencyStorage

	// now is the injectable clock; tests replace it to control sync timing.
	now func() time.Time

	mu     sync.Mutex
	synced map[string]time.Time
}

// NewClient creates a client over the given providers, in preference order.
func NewClient(log *slog.Logger, providers []Provider, storage CurrencyStorage) *Client {
	return &Client{
		log:       log,
		providers: providers,
		storage:   storage,
		now:       time.Now,
		synced:    make(map[string]time.Time),
	}
}

// Search finds instruments matching free-text keywords.
func (c *Client) Search(ctx context.Context, keywords string) ([]Match, error) {
	return query(ctx, c, "search", func(ctx context.Context, p Provider) ([]Match, error) {
		return p.Search(ctx, keywords)
	})
}

// Quote retrieves the current price and fundamentals for a ticker.
func (c *Client) Quote(ctx context.Context, ticker string) (Quote, error) {
	return query(ctx, c, "quote", func(ctx context.Context, p Provider) (Quote, error) {
		return p.Quote(ctx, ticker)
	})
}

// History retrieves daily price bars for a ticker over the range.
func (c *Client) History(ctx context.Context, ticker string, begin, end civil.Date) (History, error) {
	return query(ctx, c, "history", func(ctx context.Context, p Provider) (History, error) {
		return p.History(ctx, ticker, begin, end)
	})
}

// query runs op against each provider in turn, returning the first success. When
// all fail it returns their errors joined, each tagged with its provider.
//
//nolint:ireturn // T is a caller-supplied type parameter, not a chosen interface return.
func query[T any](
	ctx context.Context,
	c *Client,
	op string,
	fn func(context.Context, Provider) (T, error),
) (T, error) {
	var (
		zero T
		errs []error
	)

	for _, p := range c.providers {
		start := time.Now()

		v, err := fn(ctx, p)
		c.logOutcome(ctx, op, p.Name(), err, time.Since(start))

		if err == nil {
			return v, nil
		}

		errs = append(errs, fmt.Errorf("%s: %w", p.Name(), err))
	}

	if len(errs) == 0 {
		return zero, fmt.Errorf("%s: %w", op, ErrNotFound)
	}

	return zero, fmt.Errorf("%s: %w", op, errors.Join(errs...))
}

// logOutcome emits one structured line per provider call, classifying how it
// resolved so a fallback chain is legible in the log.
func (c *Client) logOutcome(ctx context.Context, op, provider string, err error, d time.Duration) {
	status := "ok"
	level := slog.LevelDebug

	switch {
	case err == nil:
	case errors.Is(err, ErrNotFound):
		status = "not_found"
	case errors.Is(err, ErrRateLimited):
		status, level = "rate_limited", slog.LevelWarn
	default:
		status, level = "error", slog.LevelWarn
	}

	attrs := []any{
		slog.String("op", op),
		slog.String("provider", provider),
		slog.String("status", status),
		slog.Int64("duration_ms", d.Milliseconds()),
	}
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}

	c.log.Log(ctx, level, "provider call finished", attrs...)
}
