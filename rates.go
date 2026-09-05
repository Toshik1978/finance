package finance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/shopspring/decimal"

	"github.com/Toshik1978/finance/civil"
)

const (
	// lookupWindow is how many dates Rate inspects for a cached rate: the date
	// itself plus up to 4 days back, spanning a long weekend.
	lookupWindow = 5

	// historyWindow is how far back a sync fetches.
	historyWindow = 3 * 365
)

// RateToday returns the current exchange rate for the pair, straight from a
// provider. It does not consult or populate the cache.
func (c *Client) RateToday(ctx context.Context, from, to string) (decimal.Decimal, error) {
	r, err := query(ctx, c, "rate_today", func(ctx context.Context, p Provider) (ExchangeRate, error) {
		return p.Rate(ctx, from, to)
	})
	if err != nil {
		return decimal.Decimal{}, err
	}

	return r.Price, nil
}

// Rate returns the exchange rate for the pair on date, refreshing the cache at
// most once a day and walking back up to lookupWindow-1 days over non-trading days.
func (c *Client) Rate(ctx context.Context, from, to string, date civil.Date) (decimal.Decimal, error) {
	if err := c.updateRates(ctx, from, to); err != nil {
		return decimal.Decimal{}, fmt.Errorf("failed to update rates: %w", err)
	}

	for i := range lookupWindow {
		v, err := c.storage.Rate(ctx, date.AddDays(-i), from, to)
		if err == nil {
			return v, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return decimal.Decimal{}, fmt.Errorf("failed to read cached rate: %w", err)
		}
	}

	return decimal.Decimal{}, fmt.Errorf(
		"no rate for %s/%s within %d days of %s: %w", from, to, lookupWindow-1, date, ErrNotFound,
	)
}

// updateRates refreshes a pair's cached history at most once per UTC day. The
// lock covers only the decision and the result, never the fetch.
func (c *Client) updateRates(ctx context.Context, from, to string) error {
	now := c.now().UTC()
	today := civil.DateOf(now)

	if !c.syncDue(ctx, from, to, today) {
		return nil
	}

	rates, err := query(ctx, c, "rate_history",
		func(ctx context.Context, p Provider) ([]ExchangeRate, error) {
			return p.RateHistory(ctx, from, to, today.AddDays(-historyWindow), today)
		})
	if err != nil {
		return fmt.Errorf("failed to fetch exchange rates: %w", err)
	}

	if err := c.storage.SetRates(ctx, rates); err != nil {
		return fmt.Errorf("failed to cache exchange rates: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.storage.SetSyncTime(ctx, from, to, now); err != nil {
		// The rates are cached; only the bookkeeping failed. Warn and carry on,
		// accepting that the next call re-syncs.
		c.log.WarnContext(ctx, "failed to record sync time",
			slog.String("from", from), slog.String("to", to), slog.Any("error", err))

		return nil
	}
	c.synced[pairKey(from, to)] = now

	return nil
}

// syncDue reports whether the pair needs refreshing, holding c.mu only for the
// decision so a panic inside it can never leave the client-wide lock stuck.
func (c *Client) syncDue(ctx context.Context, from, to string, today civil.Date) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.syncDueLocked(ctx, from, to, today)
}

// syncDueLocked reports whether the pair needs refreshing. The caller holds c.mu.
func (c *Client) syncDueLocked(ctx context.Context, from, to string, today civil.Date) bool {
	key := pairKey(from, to)

	if c.synced[key].IsZero() {
		t, err := c.storage.SyncTime(ctx, from, to)
		if err != nil {
			// A miss and a read failure lead to the same place: sync. Only a
			// real failure is worth a line in the log.
			if !errors.Is(err, ErrNotFound) {
				c.log.WarnContext(ctx, "failed to read sync time",
					slog.String("from", from), slog.String("to", to), slog.Any("error", err))
			}

			return true
		}
		c.synced[key] = t
	}

	// Sync once per UTC day; both sides are UTC.
	return today != civil.DateOf(c.synced[key].UTC())
}

func pairKey(from, to string) string {
	return from + ":" + to
}
