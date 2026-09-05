package cli

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/finance"
)

// TestMain disables fatih/color's ANSI codes before any test runs: it decides
// based on os.Stdout, which is not the buffer these tests write to.
func TestMain(m *testing.M) {
	color.NoColor = true

	m.Run()
}

// TestCLI is the package's single entry point; every suite is registered here.
func TestCLI(t *testing.T) {
	suite.Run(t, new(currencySuite))
	suite.Run(t, new(quoteSuite))
	suite.Run(t, new(formatSuite))
	suite.Run(t, new(configSuite))
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// fakeClient is the scripted double for the Client interface.
type fakeClient struct {
	rate     decimal.Decimal
	rateErr  error
	quote    finance.Quote
	quoteErr error
	pairs    [][2]string
}

func (f *fakeClient) RateToday(_ context.Context, from, to string) (decimal.Decimal, error) {
	f.pairs = append(f.pairs, [2]string{from, to})

	return f.rate, f.rateErr
}

func (f *fakeClient) Quote(context.Context, string) (finance.Quote, error) {
	return f.quote, f.quoteErr
}

type currencySuite struct{ suite.Suite }

func (s *currencySuite) TestMetadata() {
	cmd := NewCurrency(discardLogger(), &fakeClient{})

	s.Equal("currency", cmd.Name())
	s.NotEmpty(cmd.Short())
	s.NotEmpty(cmd.Long())
}

func (s *currencySuite) TestExplicitPair() {
	c := &fakeClient{rate: decimal.NewFromFloat(75.8949)}
	var out bytes.Buffer

	err := NewCurrency(discardLogger(), c).Run(context.Background(), []string{"USD", "EUR"}, &out)

	s.Require().NoError(err)
	s.Contains(out.String(), "USD")
	s.Contains(out.String(), "EUR")
	s.Contains(out.String(), "75.8949")
	s.Equal([][2]string{{"USD", "EUR"}}, c.pairs)
}

func (s *currencySuite) TestNoArgumentsUsesTheDefaultPairs() {
	c := &fakeClient{rate: decimal.NewFromInt(75)}
	var out bytes.Buffer

	err := NewCurrency(discardLogger(), c).Run(context.Background(), nil, &out)

	s.Require().NoError(err)
	s.Equal([][2]string{{"USD", "EUR"}, {"GBP", "USD"}}, c.pairs)
}

// TestOutputEndsWithExactlyOneNewline regresses a shell prompt resuming
// mid-line: the old output never wrote a trailing newline at all.
func (s *currencySuite) TestOutputEndsWithExactlyOneNewline() {
	c := &fakeClient{rate: decimal.NewFromInt(75)}
	var out bytes.Buffer

	err := NewCurrency(discardLogger(), c).Run(context.Background(), nil, &out)

	s.Require().NoError(err)
	s.True(strings.HasSuffix(out.String(), "\n"), "output must end with a newline")
	s.False(strings.HasSuffix(out.String(), "\n\n"), "output must not end with a blank line")
}

func (s *currencySuite) TestWrongArgumentCountIsUsage() {
	var out bytes.Buffer

	err := NewCurrency(discardLogger(), &fakeClient{}).
		Run(context.Background(), []string{"USD"}, &out)

	s.Require().ErrorIs(err, ErrUsage)
	s.Empty(out.String(), "a usage error must print nothing to the output stream")
}

func (s *currencySuite) TestProviderFailureReachesTheCaller() {
	// Regression: the old wrapCommand swallowed this and printed usage, so a
	// rate-limited provider was indistinguishable from a typo.
	c := &fakeClient{rateErr: finance.ErrRateLimited}
	var out bytes.Buffer

	err := NewCurrency(discardLogger(), c).Run(context.Background(), []string{"USD", "EUR"}, &out)

	s.Require().NotErrorIs(err, ErrUsage, "a provider failure is not a usage error")
	s.ErrorIs(err, finance.ErrRateLimited, "the cause must survive")
}

type quoteSuite struct{ suite.Suite }

func (s *quoteSuite) TestMetadata() {
	cmd := NewQuote(discardLogger(), &fakeClient{})

	s.Equal("quote", cmd.Name())
	s.NotEmpty(cmd.Short())
	s.NotEmpty(cmd.Long())
}

func (s *quoteSuite) TestRendersTheCoreFields() {
	c := &fakeClient{quote: finance.Quote{
		Instrument: finance.Instrument{
			Ticker: "AAPL", Name: "Apple Inc.", Type: finance.TypeEquity, Currency: "USD",
		},
		Price:  decimal.NewFromFloat(190.5),
		Volume: 1234567,
	}}
	var out bytes.Buffer

	err := NewQuote(discardLogger(), c).Run(context.Background(), []string{"AAPL"}, &out)

	s.Require().NoError(err)
	got := out.String()
	s.Contains(got, "AAPL")
	s.Contains(got, "Apple Inc.")
	s.Contains(got, finance.TypeEquity)
	s.Contains(got, "USD")
	s.Contains(got, "190.5")
	s.Contains(got, "1,234,567", "volume is thousands-separated")
	s.True(strings.HasSuffix(got, "\n"), "output must end with a newline")
	s.False(strings.HasSuffix(got, "\n\n"), "output must not end with a blank line")
}

func (s *quoteSuite) TestOmitsUnsetOptionalFields() {
	c := &fakeClient{quote: finance.Quote{
		Instrument: finance.Instrument{Ticker: "AAPL", Currency: "USD"},
		Price:      decimal.NewFromInt(190),
	}}
	var out bytes.Buffer

	s.Require().NoError(NewQuote(discardLogger(), c).Run(context.Background(), []string{"AAPL"}, &out))

	got := out.String()
	s.NotContains(got, "P/E", "an unset ratio must not be printed as zero")
	s.NotContains(got, "Volume")
}

func (s *quoteSuite) TestMissingTickerIsUsage() {
	var out bytes.Buffer

	err := NewQuote(discardLogger(), &fakeClient{}).Run(context.Background(), nil, &out)

	s.Require().ErrorIs(err, ErrUsage)
	s.Empty(out.String(), "a usage error must print nothing to the output stream")
}

func (s *quoteSuite) TestNotFoundIsReportedNotSwallowed() {
	c := &fakeClient{quoteErr: finance.ErrNotFound}
	var out bytes.Buffer

	err := NewQuote(discardLogger(), c).Run(context.Background(), []string{"NOPE"}, &out)

	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.NotErrorIs(err, ErrUsage)
}

type formatSuite struct{ suite.Suite }

func (s *formatSuite) TestThousands() {
	cases := map[int64]string{
		0: "0", 1: "1", 999: "999", 1000: "1,000", 1234567: "1,234,567",
		-1234567: "-1,234,567", 1000000000: "1,000,000,000",
	}
	for in, want := range cases {
		s.Equal(want, Thousands(in), "input %d", in)
	}
}
