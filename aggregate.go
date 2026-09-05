package finance

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/Toshik1978/finance/civil"
)

// filterByLength drops instruments that cannot possibly supply the requested
// number of observations, so they do not constrain the shared timeline. Each
// drop is logged at Warn: it otherwise vanishes from the result silently.
func (c *Client) filterByLength(ctx context.Context, hs []History, days, interval int) []History {
	out := make([]History, 0, len(hs))

	for i := range hs {
		if len(hs[i].Chart) < days*interval {
			c.log.WarnContext(ctx, "dropping instrument with insufficient history",
				slog.String("ticker", hs[i].Ticker), slog.Int("bars", len(hs[i].Chart)))

			continue
		}
		out = append(out, hs[i])
	}

	return out
}

// timeline picks the shared dates instruments are sampled on, spaced at least
// interval days apart. A short timeline is refused, not silently returned.
func timeline(hs []History, days, interval int) (map[civil.Date]bool, error) {
	if len(hs) == 0 {
		return nil, fmt.Errorf("no instruments supplied: %w", ErrInsufficientHistory)
	}

	dates, seen := collectDates(hs)
	if len(dates) < 3 {
		return nil, fmt.Errorf(
			"only %d distinct trading dates available, need at least 3: %w",
			len(dates), ErrInsufficientHistory,
		)
	}

	tl := pickTimeline(dates, seen, len(hs), days, interval)
	if len(tl) < days {
		return nil, fmt.Errorf(
			"need %d observations at %d-day spacing, found %d: %w",
			days, interval, len(tl), ErrInsufficientHistory,
		)
	}

	return tl, nil
}

// collectDates gathers every date, ascending, on which any instrument in hs has
// a non-zero close, alongside how many instruments trade on each one.
func collectDates(hs []History) ([]civil.Date, map[civil.Date]int) {
	dates := make([]civil.Date, 0)
	seen := make(map[civil.Date]int)

	for i := range hs {
		for _, b := range hs[i].Chart {
			if b.Close.IsZero() { // a gap, not a price
				continue
			}
			if _, ok := seen[b.Date]; !ok {
				dates = append(dates, b.Date)
			}
			seen[b.Date]++
		}
	}

	slices.SortFunc(dates, compareDates)

	return dates, seen
}

// pickTimeline walks dates back from the most recent, keeping ones shared by
// want instruments and at least interval days apart, until days are picked.
func pickTimeline(dates []civil.Date, seen map[civil.Date]int, want, days, interval int) map[civil.Date]bool {
	tl := make(map[civil.Date]bool, days)
	next := civil.Date{Year: 9999, Month: 12, Day: 31}

	for i := len(dates) - 1; i >= 0 && len(tl) < days; i-- {
		d := dates[i]
		if seen[d] < want || d.After(next) {
			continue
		}
		tl[d] = true
		next = d.AddDays(-interval)
	}

	return tl
}

// keepTimeline trims each chart to the timeline's dates, keeping ascending
// order. It allocates a new Chart per instrument; that allocation is what
// stops normalize from overwriting the caller's own bar arrays.
func keepTimeline(hs []History, tl map[civil.Date]bool) {
	for i := range hs {
		h := &hs[i]

		kept := make([]Bar, 0, len(tl))
		for _, b := range h.Chart {
			if tl[b.Date] {
				kept = append(kept, b)
			}
		}
		h.Chart = kept
	}
}

// compareDates orders dates ascending, for slices.SortFunc.
func compareDates(a, b civil.Date) int {
	switch {
	case a.Before(b):
		return -1
	case b.Before(a):
		return 1
	default:
		return 0
	}
}

// Align resamples the given histories onto one shared set of dates, converting to currency
// when non-empty; days and interval set the count and minimum spacing (52, 7 = a year of
// weekly bars). Thin instruments are dropped; too few shared dates left yields ErrInsufficientHistory.
func (c *Client) Align(
	ctx context.Context,
	hs []History,
	currency string,
	days, interval int,
) ([]History, error) {
	if days <= 0 || interval <= 0 {
		return nil, fmt.Errorf("days=%d and interval=%d must both be positive", days, interval)
	}

	c.log.DebugContext(ctx, "aligning histories",
		slog.Int("instruments", len(hs)),
		slog.String("currency", currency),
		slog.Int("days", days),
		slog.Int("interval", interval))

	kept := c.filterByLength(ctx, hs, days, interval)

	tl, err := timeline(kept, days, interval)
	if err != nil {
		return nil, fmt.Errorf("failed to build a common timeline: %w", err)
	}

	keepTimeline(kept, tl)

	if err := c.normalize(ctx, kept, currency); err != nil {
		return nil, fmt.Errorf("failed to normalize to %s: %w", currency, err)
	}

	return kept, nil
}

// normalize converts instruments not already priced in currency, at each bar's
// own date. An empty currency leaves prices untouched.
func (c *Client) normalize(ctx context.Context, hs []History, currency string) error {
	if currency == "" {
		return nil
	}

	for i := range hs {
		if hs[i].Currency == currency {
			continue
		}

		for j, b := range hs[i].Chart {
			rate, err := c.Rate(ctx, hs[i].Currency, currency, b.Date)
			if err != nil {
				return fmt.Errorf("no %s/%s rate for %s: %w", hs[i].Currency, currency, b.Date, err)
			}

			hs[i].Chart[j].Open = b.Open.Mul(rate)
			hs[i].Chart[j].Close = b.Close.Mul(rate)
			hs[i].Chart[j].High = b.High.Mul(rate)
			hs[i].Chart[j].Low = b.Low.Mul(rate)
		}

		hs[i].Currency = currency
	}

	return nil
}
