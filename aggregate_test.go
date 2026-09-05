package finance

import (
	"context"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/finance/civil"
)

// recordingHandler is a slog.Handler that appends every record it handles,
// so a suite can assert on what was logged without a real sink.
type recordingHandler struct {
	records *[]slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	*h.records = append(*h.records, r)

	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// attrMap collects a record's attributes into a map keyed by name, for
// assertions that do not care about attribute order.
func attrMap(r slog.Record) map[string]any {
	m := make(map[string]any, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()

		return true
	})

	return m
}

type alignSuite struct {
	suite.Suite
}

// day builds a date in March 2026, the month every case in this suite uses.
func day(d int) civil.Date {
	return civil.Date{Year: 2026, Month: time.March, Day: d}
}

// hist builds a History whose bars all close at price, on the given days.
func hist(ticker, currency string, price float64, days ...int) History {
	h := History{Ticker: ticker, Currency: currency}
	for _, d := range days {
		h.Chart = append(h.Chart, Bar{
			Date:  day(d),
			Open:  decimal.NewFromFloat(price),
			Close: decimal.NewFromFloat(price),
			Low:   decimal.NewFromFloat(price),
			High:  decimal.NewFromFloat(price),
		})
	}

	return h
}

func (s *alignSuite) TestFilterByLengthDropsShortSeries() {
	long := hist("LONG", "USD", 1, 1, 2, 3, 4, 5, 6)
	short := hist("SHORT", "USD", 1, 1, 2)

	got := s.newClient(newFakeStorage()).filterByLength(context.Background(), []History{long, short}, 3, 2)

	s.Require().Len(got, 1)
	s.Equal("LONG", got[0].Ticker)
}

func (s *alignSuite) TestFilterByLengthLogsEachDrop() {
	long := hist("LONG", "USD", 1, 1, 2, 3, 4, 5, 6)
	short := hist("SHORT", "USD", 1, 1, 2)

	var records []slog.Record
	c := NewClient(slog.New(&recordingHandler{records: &records}), nil, newFakeStorage())

	got := c.filterByLength(context.Background(), []History{long, short}, 3, 2)

	s.Require().Len(got, 1, "the kept instrument must still be returned")
	s.Require().Len(records, 1, "exactly the dropped instrument must be logged, not the kept one")
	s.Equal(slog.LevelWarn, records[0].Level)
	s.Contains(records[0].Message, "dropping instrument")

	attrs := attrMap(records[0])
	s.Equal("SHORT", attrs["ticker"])
	s.Equal(int64(2), attrs["bars"])
}

func (s *alignSuite) TestTimelinePicksCommonDatesAtTheRequestedSpacing() {
	// Two instruments trading on the same seven days; ask for 3 observations
	// spaced 2 days apart. Counting back from the latest: 7, 5, 3.
	a := hist("A", "USD", 10, 1, 2, 3, 4, 5, 6, 7)
	b := hist("B", "USD", 20, 1, 2, 3, 4, 5, 6, 7)

	tl, err := timeline([]History{a, b}, 3, 2)

	s.Require().NoError(err)
	s.Len(tl, 3)
	s.True(tl[day(7)])
	s.True(tl[day(5)])
	s.True(tl[day(3)])
	s.False(tl[day(6)], "dates closer than the interval are skipped")
}

func (s *alignSuite) TestTimelineSortsBarsGivenOutOfOrder() {
	// Chart bars arrive scrambled, not chronological. If timeline relied on
	// input order instead of sorting, it would pick the wrong "latest" dates.
	a := hist("A", "USD", 10, 7, 3, 5, 1, 6, 2, 4)
	b := hist("B", "USD", 20, 4, 1, 6, 2, 7, 3, 5)

	tl, err := timeline([]History{a, b}, 3, 2)

	s.Require().NoError(err)
	s.Len(tl, 3)
	s.True(tl[day(7)])
	s.True(tl[day(5)])
	s.True(tl[day(3)])
	s.False(tl[day(6)], "dates closer than the interval are skipped")
}

func (s *alignSuite) TestTimelineExcludesDatesNotSharedByAll() {
	// B does not trade on day 5 — a different holiday calendar. Day 5 cannot
	// appear in the timeline, because a covariance needs paired observations.
	a := hist("A", "USD", 10, 1, 2, 3, 4, 5, 6, 7)
	b := hist("B", "USD", 20, 1, 2, 3, 4, 6, 7)

	tl, err := timeline([]History{a, b}, 3, 1)

	s.Require().NoError(err)
	s.False(tl[day(5)], "a date missing from one series must be excluded")
	for d := range tl {
		s.NotEqual(day(5), d)
	}
}

func (s *alignSuite) TestTimelineIgnoresZeroCloses() {
	// A zero close is a gap, not a price. It must not anchor the timeline.
	a := hist("A", "USD", 10, 1, 2, 3, 4, 5)
	a.Chart[4].Close = decimal.Decimal{}
	b := hist("B", "USD", 20, 1, 2, 3, 4, 5)

	tl, err := timeline([]History{a, b}, 2, 1)

	s.Require().NoError(err)
	s.False(tl[day(5)], "a zero close must not appear in the timeline")
}

func (s *alignSuite) TestTimelineFailsOnTooFewDistinctDates() {
	// Regression: this returned a nil map, and keepTimeline then silently
	// emptied every chart instead of reporting the problem.
	a := hist("A", "USD", 10, 1, 2)
	b := hist("B", "USD", 20, 1, 2)

	_, err := timeline([]History{a, b}, 2, 1)

	s.Require().Error(err)
	s.ErrorIs(err, ErrInsufficientHistory)
}

func (s *alignSuite) TestTimelineFailsWhenItCannotFillTheRequest() {
	// Only 3 common dates at spacing 1, but 10 observations were asked for.
	a := hist("A", "USD", 10, 1, 2, 3)
	b := hist("B", "USD", 20, 1, 2, 3)

	_, err := timeline([]History{a, b}, 10, 1)

	s.Require().Error(err)
	s.ErrorIs(err, ErrInsufficientHistory)
}

func (s *alignSuite) TestTimelineFailsWithNoInstruments() {
	_, err := timeline(nil, 2, 1)

	s.Require().Error(err)
	s.ErrorIs(err, ErrInsufficientHistory)
}

func (s *alignSuite) TestCompareDatesOrdersAscending() {
	a, b := day(1), day(2)

	s.Negative(compareDates(a, b))
	s.Positive(compareDates(b, a))
	s.Zero(compareDates(a, a))
}

func (s *alignSuite) TestKeepTimelineRetainsOrderAndOnlyListedDates() {
	a := hist("A", "USD", 10, 1, 2, 3, 4, 5)
	tl := map[civil.Date]bool{day(1): true, day(3): true, day(5): true}

	hs := []History{a}
	keepTimeline(hs, tl)

	s.Require().Len(hs[0].Chart, 3)
	s.Equal(day(1), hs[0].Chart[0].Date, "bars must stay in ascending order")
	s.Equal(day(3), hs[0].Chart[1].Date)
	s.Equal(day(5), hs[0].Chart[2].Date)
}

// newClient builds a client pinned to a fixed clock so rate syncing is deterministic.
func (s *alignSuite) newClient(st CurrencyStorage) *Client {
	c := NewClient(discardLogger(), nil, st)
	c.now = fixedNow(time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC))

	return c
}

func (s *alignSuite) TestAlignProducesIdenticalDatesForEveryInstrument() {
	a := hist("A", "USD", 10, 1, 2, 3, 4, 5, 6, 7)
	b := hist("B", "USD", 20, 1, 2, 3, 4, 5, 6, 7)

	out, err := s.newClient(newFakeStorage()).
		Align(context.Background(), []History{a, b}, "", 3, 2)

	s.Require().NoError(err)
	s.Require().Len(out, 2)
	s.Require().Len(out[0].Chart, 3)
	s.Require().Len(out[1].Chart, 3)
	for i := range out[0].Chart {
		s.Equal(out[0].Chart[i].Date, out[1].Chart[i].Date,
			"every instrument must be sampled on the same dates")
	}
}

func (s *alignSuite) TestAlignDropsAnInstrumentWithTooFewBars() {
	a := hist("A", "USD", 10, 1, 2, 3, 4, 5, 6, 7)
	b := hist("B", "USD", 20, 1, 2, 3, 4, 5, 6, 7)
	short := hist("SHORT", "USD", 30, 6, 7)

	out, err := s.newClient(newFakeStorage()).
		Align(context.Background(), []History{a, b, short}, "", 3, 2)

	s.Require().NoError(err)
	s.Len(out, 2, "the short series must be dropped, not shorten everyone else")
	for i := range out {
		s.NotEqual("SHORT", out[i].Ticker)
	}
}

func (s *alignSuite) TestAlignNormalisesUsingEachBarsOwnRate() {
	// A is priced in USD and must be converted to EUR. The rate differs by day,
	// so a single series-wide rate would give the wrong answer. OHLC differ from
	// one another so a field mix-up (e.g. High written with Low's factor) fails.
	ohlcBar := func(d int) Bar {
		return Bar{
			Date: day(d), Open: decimal.NewFromInt(11), Close: decimal.NewFromInt(10),
			Low: decimal.NewFromInt(9), High: decimal.NewFromInt(12),
		}
	}
	a := History{Ticker: "A", Currency: "USD", Chart: []Bar{ohlcBar(1), ohlcBar(2), ohlcBar(3)}}
	b := hist("B", "EUR", 20, 1, 2, 3)

	st := newFakeStorage()
	st.rates[st.key(day(1), "USD", "EUR")] = decimal.NewFromInt(70)
	st.rates[st.key(day(2), "USD", "EUR")] = decimal.NewFromInt(80)
	st.rates[st.key(day(3), "USD", "EUR")] = decimal.NewFromInt(90)
	st.syncTimes["USD:EUR"] = time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)

	out, err := s.newClient(st).Align(context.Background(), []History{a, b}, "EUR", 3, 1)

	s.Require().NoError(err)
	s.Require().Len(out, 2)

	converted := out[0]
	s.Require().Len(converted.Chart, 3)
	s.True(decimal.NewFromInt(700).Equal(converted.Chart[0].Close), "10 USD at 70")
	s.True(decimal.NewFromInt(800).Equal(converted.Chart[1].Close), "10 USD at 80")
	s.True(decimal.NewFromInt(900).Equal(converted.Chart[2].Close), "10 USD at 90")
	s.True(decimal.NewFromInt(770).Equal(converted.Chart[0].Open), "11 USD at 70")
	s.True(decimal.NewFromInt(630).Equal(converted.Chart[0].Low), "9 USD at 70")
	s.True(decimal.NewFromInt(840).Equal(converted.Chart[0].High), "12 USD at 70")
	s.Equal("EUR", converted.Currency, "converted instrument's currency updates to the target")

	s.True(decimal.NewFromInt(20).Equal(out[1].Chart[0].Close),
		"an instrument already in the target currency is untouched")
	s.Equal("EUR", out[1].Currency)
}

// TestAlignDoesNotMutateTheCallersInput pins keepTimeline's allocation: it is
// the only thing stopping normalize from writing converted prices into the
// caller's own bar arrays.
func (s *alignSuite) TestAlignDoesNotMutateTheCallersInput() {
	a := hist("A", "USD", 10, 1, 2, 3)
	b := hist("B", "EUR", 20, 1, 2, 3)
	hs := []History{a, b}

	st := newFakeStorage()
	st.rates[st.key(day(1), "USD", "EUR")] = decimal.NewFromInt(70)
	st.rates[st.key(day(2), "USD", "EUR")] = decimal.NewFromInt(80)
	st.rates[st.key(day(3), "USD", "EUR")] = decimal.NewFromInt(90)
	st.syncTimes["USD:EUR"] = time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)

	_, err := s.newClient(st).Align(context.Background(), hs, "EUR", 3, 1)

	s.Require().NoError(err)
	s.Require().Len(hs, 2)
	s.Require().Len(hs[0].Chart, 3, "the caller's own chart length must be unchanged")
	s.Equal("USD", hs[0].Currency, "the caller's own instrument currency must be unchanged")
	for _, bar := range hs[0].Chart {
		s.True(decimal.NewFromInt(10).Equal(bar.Close), "the caller's own prices must be unchanged")
		s.True(decimal.NewFromInt(10).Equal(bar.Open))
	}
}

func (s *alignSuite) TestAlignWithEmptyCurrencySkipsNormalisation() {
	a := hist("A", "USD", 10, 1, 2, 3)
	b := hist("B", "EUR", 20, 1, 2, 3)

	out, err := s.newClient(newFakeStorage()).
		Align(context.Background(), []History{a, b}, "", 3, 1)

	s.Require().NoError(err)
	s.True(decimal.NewFromInt(10).Equal(out[0].Chart[0].Close), "prices stay as provided")
	s.Equal("USD", out[0].Currency, "currency is unchanged on the no-op path")
}

func (s *alignSuite) TestAlignPropagatesARateLookupFailure() {
	// A missing rate must fail the whole call. Returning partially converted
	// series would mix currencies inside one covariance matrix.
	a := hist("A", "USD", 10, 1, 2, 3)
	b := hist("B", "EUR", 20, 1, 2, 3)

	st := newFakeStorage()
	st.syncTimes["USD:EUR"] = time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	// no rates loaded at all

	_, err := s.newClient(st).Align(context.Background(), []History{a, b}, "EUR", 3, 1)

	s.Require().Error(err)
	s.ErrorIs(err, ErrNotFound)
}

func (s *alignSuite) TestAlignAtWeeklySpacing() {
	// The shape a covariance matrix needs: 3 observations, 7 days apart.
	// filterByLength needs days*interval=21 daily bars to keep an instrument.
	days := make([]int, 21)
	for i := range days {
		days[i] = i + 1
	}
	a := hist("A", "USD", 10, days...)
	b := hist("B", "USD", 20, days...)

	out, err := s.newClient(newFakeStorage()).
		Align(context.Background(), []History{a, b}, "", 3, 7)

	s.Require().NoError(err)
	s.Require().Len(out[0].Chart, 3)
	s.Equal(day(7), out[0].Chart[0].Date)
	s.Equal(day(14), out[0].Chart[1].Date)
	s.Equal(day(21), out[0].Chart[2].Date)
}

func (s *alignSuite) TestAlignFailsRatherThanReturningEmptyCharts() {
	// Regression for the nil-map bug: two instruments sharing only two dates
	// used to come back with every chart silently emptied.
	a := hist("A", "USD", 10, 1, 2)
	b := hist("B", "USD", 20, 1, 2)

	_, err := s.newClient(newFakeStorage()).
		Align(context.Background(), []History{a, b}, "", 2, 1)

	s.Require().Error(err)
	s.ErrorIs(err, ErrInsufficientHistory)
}

func (s *alignSuite) TestAlignRejectsNonPositiveDaysOrInterval() {
	a := hist("A", "USD", 10, 1, 2, 3)

	_, err := s.newClient(newFakeStorage()).Align(context.Background(), []History{a}, "", 0, 1)
	s.Require().Error(err)

	_, err = s.newClient(newFakeStorage()).Align(context.Background(), []History{a}, "", 1, 0)
	s.Require().Error(err)
}

func (s *alignSuite) TestAlignExcludesDatesMissingFromAnyInstrumentsCalendar() {
	// B's calendar skips day 5 — a different trading holiday. Align's shared
	// timeline must exclude it end-to-end, not just inside timeline's own tests.
	a := hist("A", "USD", 10, 1, 2, 3, 4, 5, 6, 7)
	b := hist("B", "USD", 20, 1, 2, 3, 4, 6, 7)

	out, err := s.newClient(newFakeStorage()).
		Align(context.Background(), []History{a, b}, "", 3, 1)

	s.Require().NoError(err)
	for i := range out {
		for j := range out[i].Chart {
			s.NotEqual(day(5), out[i].Chart[j].Date, "a date missing from one calendar must not reach the output")
		}
	}
}

func (s *alignSuite) TestAlignExcludesZeroClosesFromTheOutput() {
	// A's day 5 is a zero close (a gap, not a price). If it still counted
	// towards the shared timeline, day 5 would wrongly appear in the output.
	a := hist("A", "USD", 10, 1, 2, 3, 4, 5)
	a.Chart[4].Close = decimal.Decimal{}
	b := hist("B", "USD", 20, 1, 2, 3, 4, 5)

	out, err := s.newClient(newFakeStorage()).
		Align(context.Background(), []History{a, b}, "", 2, 1)

	s.Require().NoError(err)
	for i := range out {
		for j := range out[i].Chart {
			s.NotEqual(day(5), out[i].Chart[j].Date, "a zero close must not survive into the aligned output")
		}
	}
}
