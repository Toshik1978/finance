package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/fatih/color"
)

// defaultPairs is what currency reports when invoked with no arguments.
//
//nolint:gochecknoglobals // immutable fixture, not mutable state.
var defaultPairs = [][2]string{{"USD", "EUR"}, {"GBP", "USD"}}

type currencyCommand struct {
	log    *slog.Logger
	client Client
}

// NewCurrency creates the currency command, reporting exchange rates via c.
func NewCurrency(log *slog.Logger, c Client) *currencyCommand {
	return &currencyCommand{log: log, client: c}
}

func (c *currencyCommand) Name() string { return "currency" }

func (c *currencyCommand) Short() string { return "Show the current exchange rate for a currency pair" }

func (c *currencyCommand) Long() string {
	return "currency [FROM TO] shows the current exchange rate for a currency pair, " +
		"or the default USD/EUR and GBP/USD pairs when none is given."
}

// Run reports the rate for the given pair, or the default pairs when args is empty.
func (c *currencyCommand) Run(ctx context.Context, args []string, out io.Writer) error {
	pairs, err := currencyPairs(args)
	if err != nil {
		c.log.WarnContext(ctx, "invalid currency arguments", slog.Any("error", err))

		return err
	}

	var sb strings.Builder

	bold := color.New(color.FgHiGreen, color.Bold)

	for _, pair := range pairs {
		rate, err := c.client.RateToday(ctx, pair[0], pair[1])
		if err != nil {
			c.log.WarnContext(ctx, "failed to fetch currency rate",
				slog.String("from", pair[0]), slog.String("to", pair[1]), slog.Any("error", err))

			return fmt.Errorf("failed to fetch rate for %s/%s: %w", pair[0], pair[1], err)
		}

		fmt.Fprintf(&sb, "1 %s = %s %s\n", bold.Sprint(pair[0]), rate.StringFixed(4), bold.Sprint(pair[1]))
	}

	if _, err := fmt.Fprint(out, sb.String()); err != nil {
		return fmt.Errorf("failed to write currency output: %w", err)
	}

	return nil
}

// currencyPairs validates args and resolves it to the pairs to report: the
// default pairs for no arguments, or the single pair given by exactly two.
func currencyPairs(args []string) ([][2]string, error) {
	switch len(args) {
	case 0:
		return slices.Clone(defaultPairs), nil
	case 2:
		return [][2]string{{args[0], args[1]}}, nil
	default:
		return nil, fmt.Errorf("currency takes 0 or 2 arguments (from, to), got %d: %w", len(args), ErrUsage)
	}
}
