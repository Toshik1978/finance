package finance

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Toshik1978/finance/civil"
)

// Provider is a source of financial data. Every method returns a value and
// reports absence as ErrNotFound; none may return a nil-nil pair.
type Provider interface {
	// Name identifies the provider in logs and in aggregated errors.
	Name() string

	// Search finds instruments matching free-text keywords. An empty result is
	// ErrNotFound, not an empty slice with a nil error.
	Search(ctx context.Context, keywords string) ([]Match, error)

	// Rate retrieves the current exchange rate for the pair.
	Rate(ctx context.Context, from, to string) (ExchangeRate, error)
	// RateHistory retrieves daily exchange rates for the pair over the range.
	RateHistory(ctx context.Context, from, to string, begin, end civil.Date) ([]ExchangeRate, error)

	// Quote retrieves the current price and fundamentals for a ticker.
	Quote(ctx context.Context, ticker string) (Quote, error)
	// History retrieves daily price bars for a ticker over the range.
	History(ctx context.Context, ticker string, begin, end civil.Date) (History, error)
}

// CurrencyStorage caches exchange rates so that historical lookups do not hit a
// provider once per bar. Implementations return ErrNotFound on a cache miss.
type CurrencyStorage interface {
	// Rate reads one cached rate.
	Rate(ctx context.Context, date civil.Date, from, to string) (decimal.Decimal, error)
	// SetRates writes rates, ignoring ones already present.
	SetRates(ctx context.Context, rates []ExchangeRate) error

	// SyncTime reads when the pair was last refreshed.
	SyncTime(ctx context.Context, from, to string) (time.Time, error)
	// SetSyncTime records when the pair was last refreshed.
	SetSyncTime(ctx context.Context, from, to string, t time.Time) error
}
