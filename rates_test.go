package finance

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/finance/civil"
)

// fakeStorage is an honest in-memory CurrencyStorage: it stores and returns
// what it is given, rather than recording calls.
type fakeStorage struct {
	rates     map[string]decimal.Decimal
	syncTimes map[string]time.Time
	setCalls  int
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{
		rates:     make(map[string]decimal.Decimal),
		syncTimes: make(map[string]time.Time),
	}
}

func (f *fakeStorage) key(d civil.Date, from, to string) string {
	return d.String() + ":" + from + ":" + to
}

func (f *fakeStorage) Rate(_ context.Context, d civil.Date, from, to string) (decimal.Decimal, error) {
	if v, ok := f.rates[f.key(d, from, to)]; ok {
		return v, nil
	}
	return decimal.Decimal{}, ErrNotFound
}

func (f *fakeStorage) SetRates(_ context.Context, rates []ExchangeRate) error {
	f.setCalls++
	for _, r := range rates {
		key := f.key(r.Date, r.From, r.To)
		if _, ok := f.rates[key]; !ok {
			f.rates[key] = r.Price
		}
	}

	return nil
}

func (f *fakeStorage) SyncTime(_ context.Context, from, to string) (time.Time, error) {
	if t, ok := f.syncTimes[from+":"+to]; ok {
		return t, nil
	}
	return time.Time{}, ErrNotFound
}

func (f *fakeStorage) SetSyncTime(_ context.Context, from, to string, t time.Time) error {
	f.syncTimes[from+":"+to] = t
	return nil
}

type ratesSuite struct {
	suite.Suite
}

// fixedNow pins the clock so sync-window behaviour is deterministic.
func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func (s *ratesSuite) TestRateTodayReturnsThePrice() {
	p := &fakeProvider{name: "p", rate: func() (ExchangeRate, error) {
		return ExchangeRate{From: "USD", To: "EUR", Price: decimal.NewFromInt(75)}, nil
	}}
	c := NewClient(discardLogger(), []Provider{p}, newFakeStorage())

	v, err := c.RateToday(context.Background(), "USD", "EUR")
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(75).Equal(v))
}

func (s *ratesSuite) TestRateTodayDoesNotPanicWhenEveryProviderFails() {
	// Regression: the old RateToday dereferenced a nil *ExchangeRate here and
	// panicked. Reachable whenever every provider rate-limited.
	c := NewClient(discardLogger(), []Provider{&fakeProvider{name: "p"}}, newFakeStorage())

	s.Require().NotPanics(func() {
		v, err := c.RateToday(context.Background(), "USD", "EUR")
		s.Require().Error(err)
		s.True(v.IsZero())
	})
}

func (s *ratesSuite) TestRateSyncsOnceThenServesFromStorage() {
	day := civil.Date{Year: 2026, Month: time.March, Day: 2}
	p := &fakeProvider{name: "p", rates: func() ([]ExchangeRate, error) {
		return []ExchangeRate{
			{Date: day, From: "USD", To: "EUR", Price: decimal.NewFromInt(75)},
		}, nil
	}}
	st := newFakeStorage()
	c := NewClient(discardLogger(), []Provider{p}, st)
	c.now = fixedNow(time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC))

	v, err := c.Rate(context.Background(), "USD", "EUR", day)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(75).Equal(v))
	s.Equal(1, st.setCalls)

	// A second call on the same day must not re-sync.
	_, err = c.Rate(context.Background(), "USD", "EUR", day)
	s.Require().NoError(err)
	s.Equal(1, st.setCalls, "sync happens at most once per day per pair")
}

func (s *ratesSuite) TestRateWalksBackOverNonTradingDays() {
	// Friday has a rate; the caller asks for Sunday. Walking back finds Friday.
	friday := civil.Date{Year: 2026, Month: time.February, Day: 27}
	sunday := civil.Date{Year: 2026, Month: time.March, Day: 1}

	st := newFakeStorage()
	st.rates[st.key(friday, "USD", "EUR")] = decimal.NewFromInt(75)
	st.syncTimes["USD:EUR"] = time.Date(2026, time.March, 1, 6, 0, 0, 0, time.UTC)

	c := NewClient(discardLogger(), nil, st)
	c.now = fixedNow(time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC))

	v, err := c.Rate(context.Background(), "USD", "EUR", sunday)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(75).Equal(v))
}

func (s *ratesSuite) TestRateGivesUpBeyondTheLookupWindow() {
	old := civil.Date{Year: 2026, Month: time.January, Day: 1}
	st := newFakeStorage()
	st.rates[st.key(old, "USD", "EUR")] = decimal.NewFromInt(75)
	st.syncTimes["USD:EUR"] = time.Date(2026, time.March, 1, 6, 0, 0, 0, time.UTC)

	c := NewClient(discardLogger(), nil, st)
	c.now = fixedNow(time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC))

	_, err := c.Rate(context.Background(), "USD", "EUR", civil.Date{Year: 2026, Month: time.March, Day: 1})
	s.Require().Error(err)
	s.ErrorIs(err, ErrNotFound)
}

func (s *ratesSuite) TestRateWalkBackReachesTheOldestDateInTheWindow() {
	// lookupWindow (5) inspects date down to date-4; date-4 is the oldest
	// reachable day.
	date := civil.Date{Year: 2026, Month: time.March, Day: 10}
	oldest := date.AddDays(-4)

	st := newFakeStorage()
	st.rates[st.key(oldest, "USD", "EUR")] = decimal.NewFromInt(75)
	st.syncTimes["USD:EUR"] = time.Date(2026, time.March, 10, 6, 0, 0, 0, time.UTC)

	c := NewClient(discardLogger(), nil, st)
	c.now = fixedNow(time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC))

	v, err := c.Rate(context.Background(), "USD", "EUR", date)
	s.Require().NoError(err)
	s.True(decimal.NewFromInt(75).Equal(v))
}

func (s *ratesSuite) TestRateWalkBackDoesNotReachOneDayBeyondTheWindow() {
	// date-5 is one day past the window; it must not be found.
	date := civil.Date{Year: 2026, Month: time.March, Day: 10}
	tooOld := date.AddDays(-5)

	st := newFakeStorage()
	st.rates[st.key(tooOld, "USD", "EUR")] = decimal.NewFromInt(75)
	st.syncTimes["USD:EUR"] = time.Date(2026, time.March, 10, 6, 0, 0, 0, time.UTC)

	c := NewClient(discardLogger(), nil, st)
	c.now = fixedNow(time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC))

	_, err := c.Rate(context.Background(), "USD", "EUR", date)
	s.Require().Error(err)
	s.ErrorIs(err, ErrNotFound)
}

func (s *ratesSuite) TestSyncTimeComparisonIsUTC() {
	// Stored sync time is 23:30 UTC on 1 March; "now" is 00:30 UTC on 2 March —
	// a different UTC day, so a sync is due despite being 1 hour apart.
	st := newFakeStorage()
	st.syncTimes["USD:EUR"] = time.Date(2026, time.March, 1, 23, 30, 0, 0, time.UTC)

	p := &fakeProvider{name: "p", rates: func() ([]ExchangeRate, error) {
		return []ExchangeRate{{
			Date:  civil.Date{Year: 2026, Month: time.March, Day: 2},
			From:  "USD",
			To:    "EUR",
			Price: decimal.NewFromInt(76),
		}}, nil
	}}

	c := NewClient(discardLogger(), []Provider{p}, st)
	c.now = fixedNow(time.Date(2026, time.March, 2, 0, 30, 0, 0, time.UTC))

	_, err := c.Rate(context.Background(), "USD", "EUR", civil.Date{Year: 2026, Month: time.March, Day: 2})
	s.Require().NoError(err)
	s.Equal(1, st.setCalls, "crossing midnight UTC must trigger a sync")
}

func (s *ratesSuite) TestCancelledContextAbortsTheUpdate() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := &fakeProvider{name: "p", rates: func() ([]ExchangeRate, error) {
		return nil, context.Canceled
	}}
	c := NewClient(discardLogger(), []Provider{p}, newFakeStorage())
	c.now = fixedNow(time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC))

	_, err := c.Rate(ctx, "USD", "EUR", civil.Date{Year: 2026, Month: time.March, Day: 2})
	s.Require().Error(err)
}
