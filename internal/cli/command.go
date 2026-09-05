// Package cli implements the finance command-line interface. Commands write to
// an injected io.Writer so they can be tested without capturing stdout.
package cli

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"

	"github.com/Toshik1978/finance"
)

// ErrUsage means the command was invoked wrongly — wrong number of arguments,
// unparsable input. main exits 2 on it; every other error exits 1.
var ErrUsage = errors.New("invalid usage")

// Client is the slice of *finance.Client the commands use. Declaring it here,
// rather than depending on the concrete type, is what lets tests supply a fake.
type Client interface {
	// RateToday returns the current exchange rate for the pair.
	RateToday(ctx context.Context, from, to string) (decimal.Decimal, error)
	// Quote retrieves the current price and fundamentals for a ticker.
	Quote(ctx context.Context, ticker string) (finance.Quote, error)
}
