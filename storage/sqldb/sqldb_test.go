package sqldb

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
	_ "modernc.org/sqlite" // test-only: a real database to exercise.

	"github.com/Toshik1978/finance"
	"github.com/Toshik1978/finance/civil"
)

// TestSQLDB is the package's single entry point; every suite is registered here.
func TestSQLDB(t *testing.T) {
	suite.Run(t, new(storageSuite))
}

type storageSuite struct {
	suite.Suite

	db *sql.DB
	st *Storage
}

func (s *storageSuite) SetupTest() {
	db, err := sql.Open("sqlite", ":memory:")
	s.Require().NoError(err)
	db.SetMaxOpenConns(1) // a second :memory: connection is its own empty database

	_, err = db.ExecContext(context.Background(), SchemaSQLite)
	s.Require().NoError(err)

	s.db = db
	s.st = New(db)
}

func (s *storageSuite) TearDownTest() {
	s.Require().NoError(s.db.Close())
}

func day(d int) civil.Date {
	return civil.Date{Year: 2026, Month: time.March, Day: d}
}

// The cases below mirror storage/memory's suite: both implement the same
// interface and must behave identically.

func (s *storageSuite) TestMissingRateIsNotFound() {
	_, err := s.st.Rate(context.Background(), day(1), "USD", "EUR")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Contains(err.Error(), "USD/EUR", "the miss must carry the pair, not just the sentinel")
	s.Contains(err.Error(), "2026-03-01", "the miss must carry the date, not just the sentinel")
}

func (s *storageSuite) TestRoundTripsARate() {
	ctx := context.Background()

	s.Require().NoError(s.st.SetRates(ctx, []finance.ExchangeRate{
		{Date: day(1), From: "USD", To: "EUR", Price: decimal.NewFromFloat(0.92)},
	}))

	v, err := s.st.Rate(ctx, day(1), "USD", "EUR")
	s.Require().NoError(err)
	s.True(decimal.NewFromFloat(0.92).Equal(v), "got %s", v)
}

func (s *storageSuite) TestRatesAreKeyedByDateAndBothCodes() {
	ctx := context.Background()

	s.Require().NoError(s.st.SetRates(ctx, []finance.ExchangeRate{
		{Date: day(1), From: "USD", To: "EUR", Price: decimal.NewFromInt(75)},
		{Date: day(2), From: "USD", To: "EUR", Price: decimal.NewFromInt(76)},
		{Date: day(1), From: "GBP", To: "EUR", Price: decimal.NewFromInt(90)},
	}))

	v, err := s.st.Rate(ctx, day(2), "USD", "EUR")
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(76).Equal(v))

	v, err = s.st.Rate(ctx, day(1), "GBP", "EUR")
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(90).Equal(v))

	_, err = s.st.Rate(ctx, day(1), "EUR", "USD")
	s.ErrorIs(err, finance.ErrNotFound, "the pair is directional; the unique index must not canonicalise it")
}

func (s *storageSuite) TestSetRatesDoesNotOverwriteAnExistingRate() {
	// Client's narrow-lock design relies on this: two callers racing on the
	// same pair may both fetch and write, and that is only benign because the
	// second write is a no-op. SQL equivalent is ON CONFLICT DO NOTHING, never
	// DO UPDATE.
	ctx := context.Background()

	s.Require().NoError(s.st.SetRates(ctx, []finance.ExchangeRate{
		{Date: day(1), From: "USD", To: "EUR", Price: decimal.NewFromInt(75)},
	}))
	s.Require().NoError(s.st.SetRates(ctx, []finance.ExchangeRate{
		{Date: day(1), From: "USD", To: "EUR", Price: decimal.NewFromInt(999)},
	}))

	v, err := s.st.Rate(ctx, day(1), "USD", "EUR")
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(75).Equal(v), "the first write wins; the second is a no-op")
}

func (s *storageSuite) TestSetRatesIgnoresDuplicatesWithinOneBatch() {
	// No-overwrite is per-key, not per-batch: a batch mixing a cached and a new
	// key must still commit the new one rather than failing the transaction.
	ctx := context.Background()

	s.Require().NoError(s.st.SetRates(ctx, []finance.ExchangeRate{
		{Date: day(1), From: "USD", To: "EUR", Price: decimal.NewFromInt(75)},
		{Date: day(1), From: "USD", To: "EUR", Price: decimal.NewFromInt(75)},
		{Date: day(2), From: "USD", To: "EUR", Price: decimal.NewFromInt(76)},
	}))

	var n int
	s.Require().NoError(
		s.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM currencies").Scan(&n),
	)
	s.Equal(2, n, "the duplicate must be ignored, not fail the whole batch")
}

func (s *storageSuite) TestEmptySetRatesIsANoOp() {
	s.Require().NoError(s.st.SetRates(context.Background(), nil))
}

func (s *storageSuite) TestMissingSyncTimeIsNotFound() {
	_, err := s.st.SyncTime(context.Background(), "USD", "EUR")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Contains(err.Error(), "USD/EUR", "the miss must carry the pair, not just the sentinel")
}

func (s *storageSuite) TestSyncTimeRoundTripsInUTC() {
	ctx := context.Background()

	// The same instant, handed in through a non-UTC location, to prove the
	// round trip normalises rather than merely happening to pass on a UTC input.
	tokyo := time.FixedZone("UTC+9", 9*60*60)
	want := time.Date(2026, time.March, 3, 8, 30, 0, 0, tokyo)

	s.Require().NoError(s.st.SetSyncTime(ctx, "USD", "EUR", want))

	got, err := s.st.SyncTime(ctx, "USD", "EUR")
	s.Require().NoError(err)
	s.True(want.Equal(got), "must denote the same instant")
	s.Equal(time.UTC, got.Location(), "sync times are stored and returned in UTC")
	s.Equal(2026, got.Year())
	s.Equal(time.March, got.Month())
	s.Equal(2, got.Day())
	s.Equal(23, got.Hour(), "8:30 JST on the 3rd is 23:30 UTC on the 2nd")
}

func (s *storageSuite) TestSetSyncTimeOverwrites() {
	// Unlike SetRates, a second SetSyncTime for the same pair replaces the
	// prior value: ON CONFLICT DO UPDATE, not DO NOTHING.
	ctx := context.Background()
	first := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	second := time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC)

	s.Require().NoError(s.st.SetSyncTime(ctx, "USD", "EUR", first))
	s.Require().NoError(s.st.SetSyncTime(ctx, "USD", "EUR", second))

	got, err := s.st.SyncTime(ctx, "USD", "EUR")
	s.Require().NoError(err)
	s.True(second.Equal(got))
}

func (s *storageSuite) TestConcurrentAccessSharesTheSameDatabase() {
	// An uncapped pool may hand a query a second, distinct ":memory:"
	// connection - its own empty database, seen here as a non-ErrNotFound error.
	const parallel = 8

	start := make(chan struct{})
	errs := make([]error, parallel)

	var wg sync.WaitGroup
	wg.Add(parallel)
	for i := range errs {
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = s.st.Rate(context.Background(), day(1), "USD", "EUR")
		}(i)
	}
	close(start)
	wg.Wait()

	for _, err := range errs {
		s.Require().ErrorIs(err, finance.ErrNotFound)
	}
}
