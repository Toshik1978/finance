package memory

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/finance"
	"github.com/Toshik1978/finance/civil"
)

// TestMemory is the package's single entry point; every suite is registered here.
func TestMemory(t *testing.T) {
	suite.Run(t, new(storageSuite))
}

type storageSuite struct {
	suite.Suite
}

func day(d int) civil.Date {
	return civil.Date{Year: 2026, Month: time.March, Day: d}
}

func (s *storageSuite) TestMissingRateIsNotFound() {
	_, err := New().Rate(context.Background(), day(1), "USD", "EUR")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Contains(err.Error(), "USD/EUR", "the miss must carry the pair, not just the sentinel")
	s.Contains(err.Error(), "2026-03-01", "the miss must carry the date, not just the sentinel")
}

func (s *storageSuite) TestRoundTripsARate() {
	st := New()
	ctx := context.Background()

	err := st.SetRates(ctx, []finance.ExchangeRate{
		{Date: day(1), From: "USD", To: "EUR", Price: decimal.NewFromFloat(0.92)},
	})
	s.Require().NoError(err)

	v, err := st.Rate(ctx, day(1), "USD", "EUR")
	s.Require().NoError(err)
	s.True(decimal.NewFromFloat(0.92).Equal(v))
}

func (s *storageSuite) TestRatesAreKeyedByDateAndBothCodes() {
	st := New()
	ctx := context.Background()

	s.Require().NoError(st.SetRates(ctx, []finance.ExchangeRate{
		{Date: day(1), From: "USD", To: "EUR", Price: decimal.NewFromInt(75)},
		{Date: day(2), From: "USD", To: "EUR", Price: decimal.NewFromInt(76)},
		{Date: day(1), From: "GBP", To: "EUR", Price: decimal.NewFromInt(90)},
	}))

	v, err := st.Rate(ctx, day(2), "USD", "EUR")
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(76).Equal(v))

	v, err = st.Rate(ctx, day(1), "GBP", "EUR")
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(90).Equal(v))

	_, err = st.Rate(ctx, day(1), "EUR", "USD")
	s.ErrorIs(err, finance.ErrNotFound, "the pair is directional")
}

func (s *storageSuite) TestSetRatesDoesNotOverwriteAnExistingRate() {
	// Client's narrow-lock design relies on this: two goroutines racing on the
	// same pair may both fetch and write, and that is only benign because the
	// second write is a no-op.
	st := New()
	ctx := context.Background()

	s.Require().NoError(st.SetRates(ctx, []finance.ExchangeRate{
		{Date: day(1), From: "USD", To: "EUR", Price: decimal.NewFromInt(75)},
	}))
	s.Require().NoError(st.SetRates(ctx, []finance.ExchangeRate{
		{Date: day(1), From: "USD", To: "EUR", Price: decimal.NewFromInt(999)},
	}))

	v, err := st.Rate(ctx, day(1), "USD", "EUR")
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(75).Equal(v), "the first write wins; the second is a no-op")
}

func (s *storageSuite) TestEmptySetRatesIsANoOp() {
	s.Require().NoError(New().SetRates(context.Background(), nil))
}

func (s *storageSuite) TestMissingSyncTimeIsNotFound() {
	_, err := New().SyncTime(context.Background(), "USD", "EUR")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Contains(err.Error(), "USD/EUR", "the miss must carry the pair, not just the sentinel")
}

func (s *storageSuite) TestSyncTimeRoundTripsInUTC() {
	st := New()
	ctx := context.Background()

	// The same instant, handed in through a non-UTC location, to prove the
	// round trip normalises rather than merely happening to pass on a UTC input.
	tokyo := time.FixedZone("UTC+9", 9*60*60)
	want := time.Date(2026, time.March, 3, 8, 30, 0, 0, tokyo)

	s.Require().NoError(st.SetSyncTime(ctx, "USD", "EUR", want))

	got, err := st.SyncTime(ctx, "USD", "EUR")
	s.Require().NoError(err)
	s.True(want.Equal(got), "must denote the same instant")
	s.Equal(time.UTC, got.Location(), "sync times are stored and returned in UTC")
	s.Equal(2026, got.Year())
	s.Equal(time.March, got.Month())
	s.Equal(2, got.Day())
	s.Equal(23, got.Hour(), "8:30 JST on the 3rd is 23:30 UTC on the 2nd")
}

func (s *storageSuite) TestConcurrentUseIsSafe() {
	st := New()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			_ = st.SetRates(ctx, []finance.ExchangeRate{
				{Date: day(i + 1), From: "USD", To: "EUR", Price: decimal.NewFromInt(int64(70 + i))},
			})
			_, _ = st.Rate(ctx, day(i+1), "USD", "EUR")
		})
	}
	wg.Wait()
}
