package yahoo

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jarcoal/httpmock"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/finance"
	"github.com/Toshik1978/finance/civil"
)

type mappingSuite struct {
	suite.Suite
}

func (s *mappingSuite) SetupTest() {
	httpmock.Activate()
	registerHandshake()
}

func (s *mappingSuite) TearDownTest() {
	httpmock.DeactivateAndReset()
}

func (s *mappingSuite) newProvider() *Provider {
	return New(slog.New(slog.DiscardHandler), NewClient(slog.New(slog.DiscardHandler), http.DefaultClient))
}

func (s *mappingSuite) TestSatisfiesTheProviderInterface() {
	var _ finance.Provider = s.newProvider()
	s.Equal("Yahoo Finance", s.newProvider().Name())
	s.Zero(httpmock.GetTotalCallCount(), "Name must not call yahoo")
}

func (s *mappingSuite) TestQuoteMapsFixtureOntoDomain() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/TOST",
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/TOST.quote.json").Bytes()))

	q, err := s.newProvider().Quote(context.Background(), "TOST")

	s.Require().NoError(err)
	s.Equal("TOST", q.Ticker)
	s.Equal(finance.TypeEquity, q.Type)
	s.Equal(finance.ExchangeNYSE, q.Exchange)
	s.NotEmpty(q.Currency)
	s.False(q.Price.IsZero())
	s.Equal("Technology", q.Sector)
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one quote")
}

func (s *mappingSuite) TestExchangeCodesAreNormalised() {
	// Yahoo's codes are not the domain's.
	cases := map[string]string{
		"NMS": finance.ExchangeNASDAQ,
		"NAS": finance.ExchangeNASDAQ,
		"NYQ": finance.ExchangeNYSE,
		"CNQ": finance.ExchangeCSE,
		"CSE": finance.ExchangeCSE,
		"GER": finance.ExchangeXETRA,
	}
	p := s.newProvider()
	for in, want := range cases {
		s.Equal(want, p.exchange(context.Background(), in), "yahoo code %q", in)
	}
	s.Empty(p.exchange(context.Background(), "WAT"), "an unknown code maps to empty")
	s.Zero(httpmock.GetTotalCallCount(), "exchange normalisation must not call yahoo")
}

func (s *mappingSuite) TestEmptyResultIsNotFound() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/NOPE",
		httpmock.NewStringResponder(http.StatusOK, `{"quoteSummary":{"result":[],"error":null}}`))

	_, err := s.newProvider().Quote(context.Background(), "NOPE")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one quote")
}

func (s *mappingSuite) TestUpstreamErrorObjectIsReported() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/NOPE",
		httpmock.NewStringResponder(http.StatusOK,
			`{"quoteSummary":{"result":[],"error":{"code":"Not Found","description":"No data found"}}}`))

	_, err := s.newProvider().Quote(context.Background(), "NOPE")

	s.Require().Error(err)
	s.Contains(err.Error(), "No data found")
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one quote")
}

func (s *mappingSuite) TestHistoryBarsAreSortedAscending() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/TOST",
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/TOST.quote.json").Bytes()))
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+chartPath+"/TOST",
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/TOST.chart.json").Bytes()))

	h, err := s.newProvider().History(context.Background(), "TOST",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2026, Month: 1, Day: 1})

	s.Require().NoError(err)
	s.Require().NotEmpty(h.Chart)
	s.Equal("TOST", h.Ticker)
	for i := 1; i < len(h.Chart); i++ {
		s.True(h.Chart[i-1].Date.Before(h.Chart[i].Date), "bars must ascend by date")
	}
	s.Equal(4, httpmock.GetTotalCallCount(), "cookie, crumb, one quote, one chart")
}

// TestHistoryReordersBarsThatArriveOutOfOrder feeds a wire order that is not
// already ascending (Dec, Jan, Jun), so it fails if slices.SortFunc is removed
// — unlike a fixture whose timestamps happen to already be sorted.
func (s *mappingSuite) TestHistoryReordersBarsThatArriveOutOfOrder() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/TOST",
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/TOST.quote.json").Bytes()))
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+chartPath+"/TOST",
		httpmock.NewStringResponder(http.StatusOK, `{"chart":{"result":[{`+
			`"timestamp":[1638316800,1609459200,1622505600],`+
			`"indicators":{"quote":[{`+
			`"open":[100,200,300],"close":[100,200,300],`+
			`"low":[100,200,300],"high":[100,200,300],"volume":[100,200,300]}]}`+
			`}],"error":null}}`))

	h, err := s.newProvider().History(context.Background(), "TOST",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2022, Month: 1, Day: 1})

	s.Require().NoError(err)
	s.Require().Len(h.Chart, 3)
	// Wire order is Dec, Jan, Jun (volumes 100, 200, 300); ascending is Jan, Jun, Dec.
	s.Equal(civil.Date{Year: 2021, Month: 1, Day: 1}, h.Chart[0].Date)
	s.Equal(civil.Date{Year: 2021, Month: 6, Day: 1}, h.Chart[1].Date)
	s.Equal(civil.Date{Year: 2021, Month: 12, Day: 1}, h.Chart[2].Date)
	s.Equal(int64(200), h.Chart[0].Volume, "a bar's own fields must travel with it when reordered")
	s.Equal(int64(300), h.Chart[1].Volume)
	s.Equal(int64(100), h.Chart[2].Volume)
	s.Equal(4, httpmock.GetTotalCallCount(), "cookie, crumb, one quote, one chart")
}

// TestHistoryEmptyQuoteArrayIsNotFound crafts a decodable-but-empty
// indicators.quote array, which used to panic on Quote[0] before the guard.
func (s *mappingSuite) TestHistoryEmptyQuoteArrayIsNotFound() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/TOST",
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/TOST.quote.json").Bytes()))
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+chartPath+"/TOST",
		httpmock.NewStringResponder(http.StatusOK,
			`{"chart":{"result":[{"timestamp":[1609459200],"indicators":{"quote":[]}}],"error":null}}`))

	s.Require().NotPanics(func() {
		_, err := s.newProvider().History(context.Background(), "TOST",
			civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2022, Month: 1, Day: 1})

		s.Require().Error(err)
		s.Require().ErrorIs(err, finance.ErrNotFound)
	})
}

// TestHistoryMismatchedArrayLengthsDoesNotPanic crafts two timestamps against
// a quote series whose close array holds only one value and whose other
// fields are entirely absent — decodable, but disagreeing in length.
func (s *mappingSuite) TestHistoryMismatchedArrayLengthsDoesNotPanic() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/TOST",
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/TOST.quote.json").Bytes()))
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+chartPath+"/TOST",
		httpmock.NewStringResponder(http.StatusOK, `{"chart":{"result":[{`+
			`"timestamp":[1609459200,1609545600],`+
			`"indicators":{"quote":[{"close":[1.1]}]}`+
			`}],"error":null}}`))

	var h finance.History
	var err error
	s.Require().NotPanics(func() {
		h, err = s.newProvider().History(context.Background(), "TOST",
			civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2022, Month: 1, Day: 1})
	})

	s.Require().NoError(err)
	s.Empty(h.Chart, "the shortest array bounds every bar; none is complete here")
}

func (s *mappingSuite) TestRateUsesYahooPairNotation() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/GBPEUR=X",
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/GBPEUR=X.quote.json").Bytes()))

	r, err := s.newProvider().Rate(context.Background(), "GBP", "EUR")

	s.Require().NoError(err)
	s.Equal("GBP", r.From)
	s.Equal("EUR", r.To)
	s.False(r.Price.IsZero())
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one quote")
}

func (s *mappingSuite) TestRateHistorySortsAscendingByDate() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+chartPath+"/GBPEUR=X",
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/GBPEUR=X.chart.json").Bytes()))

	rates, err := s.newProvider().RateHistory(context.Background(), "GBP", "EUR",
		civil.Date{Year: 2019, Month: 1, Day: 1}, civil.Date{Year: 2020, Month: 12, Day: 31})

	s.Require().NoError(err)
	s.Require().NotEmpty(rates)
	for i := 1; i < len(rates); i++ {
		s.True(rates[i-1].Date.Before(rates[i].Date), "rates must ascend by date")
	}
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one chart")
}

// TestRateHistoryReordersRatesThatArriveOutOfOrder feeds a wire order that is
// not already ascending, so it fails if slices.SortFunc is removed.
func (s *mappingSuite) TestRateHistoryReordersRatesThatArriveOutOfOrder() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+chartPath+"/GBPEUR=X",
		httpmock.NewStringResponder(http.StatusOK, `{"chart":{"result":[{`+
			`"timestamp":[1638316800,1609459200,1622505600],`+
			`"indicators":{"quote":[{"close":[1.30,1.10,1.20]}]}`+
			`}],"error":null}}`))

	rates, err := s.newProvider().RateHistory(context.Background(), "GBP", "EUR",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2022, Month: 1, Day: 1})

	s.Require().NoError(err)
	s.Require().Len(rates, 3)
	// Wire order is Dec (1.30), Jan (1.10), Jun (1.20); ascending is Jan, Jun, Dec.
	s.Equal(civil.Date{Year: 2021, Month: 1, Day: 1}, rates[0].Date)
	s.Equal(civil.Date{Year: 2021, Month: 6, Day: 1}, rates[1].Date)
	s.Equal(civil.Date{Year: 2021, Month: 12, Day: 1}, rates[2].Date)
	s.True(decimal.NewFromFloat(1.10).Equal(rates[0].Price), "a rate's own price must travel with it when reordered")
	s.True(decimal.NewFromFloat(1.20).Equal(rates[1].Price))
	s.True(decimal.NewFromFloat(1.30).Equal(rates[2].Price))
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one chart")
}

// TestRateHistoryEmptyQuoteArrayIsNotFound crafts a decodable-but-empty
// indicators.quote array, which used to panic on Quote[0] before the guard.
func (s *mappingSuite) TestRateHistoryEmptyQuoteArrayIsNotFound() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+chartPath+"/GBPEUR=X",
		httpmock.NewStringResponder(http.StatusOK,
			`{"chart":{"result":[{"timestamp":[1609459200],"indicators":{"quote":[]}}],"error":null}}`))

	s.Require().NotPanics(func() {
		_, err := s.newProvider().RateHistory(context.Background(), "GBP", "EUR",
			civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2022, Month: 1, Day: 1})

		s.Require().Error(err)
		s.Require().ErrorIs(err, finance.ErrNotFound)
	})
}

// TestRateHistoryMismatchedArrayLengthsDoesNotPanic crafts two timestamps
// against a close array holding only one value — decodable, but disagreeing
// in length. This is the exact shape that used to panic with "index out of
// range [1] with length 1".
func (s *mappingSuite) TestRateHistoryMismatchedArrayLengthsDoesNotPanic() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+chartPath+"/GBPEUR=X",
		httpmock.NewStringResponder(http.StatusOK, `{"chart":{"result":[{`+
			`"timestamp":[1609459200,1609545600],`+
			`"indicators":{"quote":[{"close":[1.1]}]}`+
			`}],"error":null}}`))

	var rates []finance.ExchangeRate
	var err error
	s.Require().NotPanics(func() {
		rates, err = s.newProvider().RateHistory(context.Background(), "GBP", "EUR",
			civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2022, Month: 1, Day: 1})
	})

	s.Require().NoError(err)
	s.Require().Len(rates, 1, "the shorter close array bounds how many rates are complete")
	s.True(decimal.NewFromFloat(1.1).Equal(rates[0].Price))
}

func (s *mappingSuite) TestSearchMapsFixtureOntoDomain() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+searchPath,
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/TOST.search.json").Bytes()))

	matches, err := s.newProvider().Search(context.Background(), "TOST")

	s.Require().NoError(err)
	s.NotEmpty(matches)
	s.Equal("TOST", matches[0].Ticker)
	s.Equal(finance.TypeEquity, matches[0].Type)
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one search")
}

func (s *mappingSuite) TestSearchEmptyIsNotFound() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+searchPath,
		httpmock.NewStringResponder(http.StatusOK, `{"count":0,"quotes":[]}`))

	_, err := s.newProvider().Search(context.Background(), "NOPE")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one search")
}

func (s *mappingSuite) TestQuoteTransportErrorIsWrapped() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/TOST",
		httpmock.NewStringResponder(http.StatusInternalServerError, "boom"))

	_, err := s.newProvider().Quote(context.Background(), "TOST")

	s.Require().Error(err)
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one failed quote")
}

func (s *mappingSuite) TestSearchTransportErrorIsWrapped() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+searchPath,
		httpmock.NewStringResponder(http.StatusInternalServerError, "boom"))

	_, err := s.newProvider().Search(context.Background(), "TOST")

	s.Require().Error(err)
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one failed search")
}

func (s *mappingSuite) TestRateTransportErrorIsWrapped() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/GBPEUR=X",
		httpmock.NewStringResponder(http.StatusInternalServerError, "boom"))

	_, err := s.newProvider().Rate(context.Background(), "GBP", "EUR")

	s.Require().Error(err)
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one failed quote")
}

func (s *mappingSuite) TestRateHistoryTransportErrorIsWrapped() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+chartPath+"/GBPEUR=X",
		httpmock.NewStringResponder(http.StatusInternalServerError, "boom"))

	_, err := s.newProvider().RateHistory(context.Background(), "GBP", "EUR",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2020, Month: 2, Day: 1})

	s.Require().Error(err)
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one failed chart")
}

func (s *mappingSuite) TestHistoryFailsWhenQuoteFails() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/TOST",
		httpmock.NewStringResponder(http.StatusInternalServerError, "boom"))

	_, err := s.newProvider().History(context.Background(), "TOST",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2020, Month: 2, Day: 1})

	s.Require().Error(err)
	s.Contains(err.Error(), "instrument", "the quote-side failure must say so")
	// The chart is never requested once the instrument lookup has failed.
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one failed quote; no chart call")
}

func (s *mappingSuite) TestHistoryFailsWhenChartFails() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/TOST",
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/TOST.quote.json").Bytes()))
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+chartPath+"/TOST",
		httpmock.NewStringResponder(http.StatusInternalServerError, "boom"))

	_, err := s.newProvider().History(context.Background(), "TOST",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2020, Month: 2, Day: 1})

	s.Require().Error(err)
	s.Contains(err.Error(), "chart", "the chart-side failure must say so")
	s.Equal(4, httpmock.GetTotalCallCount(), "cookie, crumb, one quote, one failed chart")
}

func (s *mappingSuite) TestMultipleResultsIsAnError() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/DUP",
		httpmock.NewStringResponder(http.StatusOK, `{"quoteSummary":{"result":[{},{}],"error":null}}`))

	_, err := s.newProvider().Quote(context.Background(), "DUP")

	s.Require().Error(err)
	s.Require().NotErrorIs(err, finance.ErrNotFound)
	s.Equal(3, httpmock.GetTotalCallCount(), "cookie, crumb, one quote")
}
