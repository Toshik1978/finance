package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/fatih/color"

	"github.com/Toshik1978/finance"
)

type quoteCommand struct {
	log    *slog.Logger
	client Client
}

// NewQuote creates the quote command, reporting a ticker's price and
// fundamentals via c.
func NewQuote(log *slog.Logger, c Client) *quoteCommand {
	return &quoteCommand{log: log, client: c}
}

func (c *quoteCommand) Name() string { return "quote" }

func (c *quoteCommand) Short() string { return "Show the current quote for a ticker" }

func (c *quoteCommand) Long() string {
	return "quote TICKER shows the current price and, where the provider supplies them, fundamentals for TICKER."
}

// Run reports the quote for the single ticker given in args.
func (c *quoteCommand) Run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 1 {
		err := fmt.Errorf("quote requires exactly one ticker, got %d: %w", len(args), ErrUsage)
		c.log.WarnContext(ctx, "invalid quote arguments", slog.Any("error", err))

		return err
	}

	q, err := c.client.Quote(ctx, args[0])
	if err != nil {
		c.log.WarnContext(ctx, "failed to fetch quote", slog.String("ticker", args[0]), slog.Any("error", err))

		return fmt.Errorf("failed to fetch quote for %q: %w", args[0], err)
	}

	if _, err := fmt.Fprint(out, renderQuote(q)); err != nil {
		return fmt.Errorf("failed to write quote output: %w", err)
	}

	return nil
}

// renderQuote formats q, omitting fields the provider left unset rather than
// printing them as zero.
func renderQuote(q finance.Quote) string {
	bold := color.New(color.FgHiCyan, color.Bold)

	var sb strings.Builder

	line := func(label, value string) {
		fmt.Fprintf(&sb, "%s: %s\n", bold.Sprint(label), value)
	}

	line("Symbol", q.Ticker)
	line("Name", q.Name)
	line("Type", q.Type)
	line("Currency", q.Currency)
	line("Current Price", q.Price.String())

	if !q.PreviousDayClose.IsZero() {
		line("Previous Day Close", q.PreviousDayClose.StringFixed(4))
	}
	if !q.DayOpen.IsZero() {
		line("Day Open", q.DayOpen.StringFixed(4))
	}
	if !q.DayLow.IsZero() {
		line("Day Low", q.DayLow.StringFixed(4))
	}
	if !q.DayHigh.IsZero() {
		line("Day High", q.DayHigh.StringFixed(4))
	}
	if q.Volume != 0 {
		line("Volume", Thousands(q.Volume))
	}
	if !q.TrailingPE.IsZero() {
		line("P/E", q.TrailingPE.StringFixed(4))
	}
	if !q.ForwardPE.IsZero() {
		line("Forward P/E", q.ForwardPE.StringFixed(4))
	}

	return sb.String()
}
