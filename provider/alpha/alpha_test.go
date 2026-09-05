package alpha

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"

	"github.com/jarcoal/httpmock"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/finance"
	"github.com/Toshik1978/finance/civil"
)

const testAPIKey = "test-api-key-a1b2c3"

// TestAlpha is the package's single entry point; every suite is registered here.
func TestAlpha(t *testing.T) {
	suite.Run(t, new(transportSuite))
	suite.Run(t, new(mappingSuite))
}

type transportSuite struct {
	suite.Suite
}

func (s *transportSuite) SetupTest()    { httpmock.Activate() }
func (s *transportSuite) TearDownTest() { httpmock.DeactivateAndReset() }

func (s *transportSuite) newClient() *Client {
	return NewClient(slog.New(slog.DiscardHandler), http.DefaultClient, testAPIKey)
}

func (s *transportSuite) TestQuoteDecodesAFixture() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/KO.GLOBAL_QUOTE.json").Bytes()))

	r, err := s.newClient().Quote(context.Background(), "KO")

	s.Require().NoError(err)
	s.Equal("KO", r.GlobalQuote.Symbol)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestQuoteEmptyEnvelopeFixtureDecodesWithoutError() {
	// AlphaVantage answers an unknown symbol with a bare empty envelope, not an
	// in-band failure; the next task's mapping is what turns this into
	// finance.ErrNotFound.
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/NOSUCH.GLOBAL_QUOTE.json").Bytes()))

	r, err := s.newClient().Quote(context.Background(), "NOSUCH")

	s.Require().NoError(err)
	s.Empty(r.GlobalQuote.Symbol)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestSearchDecodesAFixture() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/WD.SYMBOL_SEARCH.json").Bytes()))

	r, err := s.newClient().Search(context.Background(), "WD")

	s.Require().NoError(err)
	s.NotEmpty(r.BestMatches)
	s.Equal("WD", r.BestMatches[0].Symbol)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestOverviewDecodesAFixture() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/KO.OVERVIEW.json").Bytes()))

	r, err := s.newClient().Overview(context.Background(), "KO")

	s.Require().NoError(err)
	s.Equal("KO", r.Symbol)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestChartDecodesAFixture() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/KO.TIME_SERIES_DAILY.json").Bytes()))

	r, err := s.newClient().Chart(context.Background(), "KO")

	s.Require().NoError(err)
	s.Equal("KO", r.MetaData.Symbol)
	s.NotEmpty(r.TimeSeriesDaily)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestRateDecodesAFixture() {
	httpmock.RegisterResponder(
		http.MethodGet,
		apiURL,
		httpmock.NewBytesResponder(
			http.StatusOK,
			httpmock.File("testdata/USD_EUR.CURRENCY_EXCHANGE_RATE.json").Bytes(),
		),
	)

	r, err := s.newClient().Rate(context.Background(), "USD", "EUR")

	s.Require().NoError(err)
	s.Equal("USD", r.RealtimeCurrencyExchangeRate.FromCurrencyCode)
	s.Equal("EUR", r.RealtimeCurrencyExchangeRate.ToCurrencyCode)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestRateChartDecodesAFixture() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/GBP_EUR.FX_DAILY.json").Bytes()))

	r, err := s.newClient().RateChart(context.Background(), "GBP", "EUR")

	s.Require().NoError(err)
	s.Equal("GBP", r.MetaData.FromSymbol)
	s.Equal("EUR", r.MetaData.ToSymbol)
	s.NotEmpty(r.TimeSeriesFXDaily)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestAPIKeyIsSentInTheQuery() {
	var got string
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		func(req *http.Request) (*http.Response, error) {
			got = req.URL.Query().Get("apikey")

			return httpmock.NewBytesResponse(http.StatusOK, httpmock.File("testdata/KO.GLOBAL_QUOTE.json").Bytes()), nil
		})

	_, err := s.newClient().Quote(context.Background(), "KO")

	s.Require().NoError(err)
	s.Equal(testAPIKey, got)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestAPIKeyNeverReachesATransportFailureError() {
	// Regression: net/http's *url.Error wraps the failed request's full URL,
	// apikey and all; only redacting it stops the key reaching an error message.
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewErrorResponder(errors.New("connection refused")))

	_, err := s.newClient().Quote(context.Background(), "KO")

	s.Require().Error(err)
	s.NotContains(err.Error(), testAPIKey)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestQuotaExhaustionBecomesErrRateLimited() {
	// The free tier is 25 requests/day. Beyond it AlphaVantage answers HTTP 200
	// with a "Note", which is not a parse failure and must not look like one.
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewStringResponder(http.StatusOK,
			`{"Note":"Thank you for using Alpha Vantage! Our standard API rate limit is 25 requests per day."}`))

	_, err := s.newClient().Quote(context.Background(), "KO")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrRateLimited)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestInformationFieldAlsoMeansRateLimited() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewStringResponder(http.StatusOK,
			`{"Information":"the standard API rate limit is 25 requests per day"}`))

	_, err := s.newClient().Quote(context.Background(), "KO")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrRateLimited)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestErrorMessageBecomesErrNotFound() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewStringResponder(http.StatusOK, `{"Error Message":"Invalid API call."}`))

	_, err := s.newClient().Quote(context.Background(), "NOPE")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestRateErrorMessageFixtureBecomesErrNotFound() {
	httpmock.RegisterResponder(
		http.MethodGet,
		apiURL,
		httpmock.NewBytesResponder(
			http.StatusOK,
			httpmock.File("testdata/BAD_PAIR.CURRENCY_EXCHANGE_RATE.json").Bytes(),
		),
	)

	_, err := s.newClient().Rate(context.Background(), "BAD", "PAIR")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestNon200IsAnError() {
	httpmock.RegisterResponder(
		http.MethodGet,
		apiURL,
		httpmock.NewStringResponder(http.StatusInternalServerError, "boom"),
	)

	_, err := s.newClient().Quote(context.Background(), "KO")

	s.Require().Error(err)
	s.NotContains(err.Error(), "invalid character")
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestTooManyRequestsStatusBecomesErrRateLimited() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewStringResponder(http.StatusTooManyRequests, "Too Many Requests"))

	_, err := s.newClient().Quote(context.Background(), "KO")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrRateLimited)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestNotFoundStatusBecomesErrNotFound() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewStringResponder(http.StatusNotFound, ""))

	_, err := s.newClient().Quote(context.Background(), "NOPE")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *transportSuite) TestLenientDecimalToleratesProviderPlaceholders() {
	for _, raw := range []string{`""`, `"-"`, `"None"`, `null`} {
		var d lenientDecimal
		s.Require().NoError(d.UnmarshalJSON([]byte(raw)), "input %q", raw)
		s.True(d.IsZero(), "input %q", raw)
	}

	var d lenientDecimal
	s.Require().NoError(d.UnmarshalJSON([]byte(`"12.5"`)))
	s.Equal("12.5", d.String())

	s.Require().Error(d.UnmarshalJSON([]byte(`"not-a-number"`)))
}

type mappingSuite struct {
	suite.Suite
}

func (s *mappingSuite) SetupTest()    { httpmock.Activate() }
func (s *mappingSuite) TearDownTest() { httpmock.DeactivateAndReset() }

func (s *mappingSuite) newProvider() *Provider {
	log := slog.New(slog.DiscardHandler)

	return New(log, NewClient(log, http.DefaultClient, testAPIKey))
}

// byFunction dispatches on the `function` query parameter, which is how
// AlphaVantage distinguishes its endpoints — they share one URL.
func (s *mappingSuite) byFunction(files map[string]string) {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		func(req *http.Request) (*http.Response, error) {
			f := req.URL.Query().Get("function")
			path, ok := files[f]
			if !ok {
				return httpmock.NewStringResponse(http.StatusOK, `{"Error Message":"Invalid API call."}`), nil
			}

			return httpmock.NewBytesResponse(http.StatusOK, httpmock.File(path).Bytes()), nil
		})
}

func (s *mappingSuite) TestSatisfiesTheProviderInterface() {
	var _ finance.Provider = s.newProvider()
	s.Equal("AlphaVantage", s.newProvider().Name())
	s.Zero(httpmock.GetTotalCallCount(), "Name must not call alphavantage")
}

func (s *mappingSuite) TestQuoteCombinesQuoteAndOverview() {
	s.byFunction(map[string]string{
		"GLOBAL_QUOTE": "testdata/KO.GLOBAL_QUOTE.json",
		"OVERVIEW":     "testdata/KO.OVERVIEW.json",
	})

	q, err := s.newProvider().Quote(context.Background(), "KO")

	s.Require().NoError(err)
	s.Equal("KO", q.Ticker)
	s.NotEmpty(q.Name, "the name comes from OVERVIEW")
	s.False(q.Price.IsZero(), "the price comes from GLOBAL_QUOTE")
	s.Equal(2, httpmock.GetTotalCallCount(), "a quote costs exactly two of the 25 daily calls")
}

func (s *mappingSuite) TestQuoteEmptyGlobalQuoteIsNotFoundWithoutCallingOverview() {
	s.byFunction(map[string]string{"GLOBAL_QUOTE": "testdata/NOSUCH.GLOBAL_QUOTE.json"})

	_, err := s.newProvider().Quote(context.Background(), "NOSUCH")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount(), "an empty quote must not spend the overview call too")
}

func (s *mappingSuite) TestQuoteEmptyOverviewIsNotFound() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		func(req *http.Request) (*http.Response, error) {
			if req.URL.Query().Get("function") == "OVERVIEW" {
				return httpmock.NewStringResponse(http.StatusOK, `{"Symbol":""}`), nil
			}

			return httpmock.NewBytesResponse(http.StatusOK, httpmock.File("testdata/KO.GLOBAL_QUOTE.json").Bytes()), nil
		})

	_, err := s.newProvider().Quote(context.Background(), "KO")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(2, httpmock.GetTotalCallCount(), "the quote succeeded; only the overview was empty")
}

func (s *mappingSuite) TestNeverSynthesisesACrossRate() {
	// Regression: the old provider triangulated an unavailable pair through EUR
	// and returned the product as if it were a quoted rate.
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewStringResponder(http.StatusOK, `{"Error Message":"Invalid API call."}`))

	_, err := s.newProvider().Rate(context.Background(), "GBP", "JPY")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount(), "exactly one attempt: no EUR triangulation")
}

func (s *mappingSuite) TestRateMapsFixture() {
	s.byFunction(map[string]string{"CURRENCY_EXCHANGE_RATE": "testdata/GBP_EUR.CURRENCY_EXCHANGE_RATE.json"})

	r, err := s.newProvider().Rate(context.Background(), "GBP", "EUR")

	s.Require().NoError(err)
	s.Equal("GBP", r.From)
	s.Equal("EUR", r.To)
	s.Equal(civil.Date{Year: 2024, Month: 2, Day: 11}, r.Date)
	s.False(r.Price.IsZero())
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *mappingSuite) TestRateEmptyEnvelopeIsNotFound() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewStringResponder(http.StatusOK, `{"Realtime Currency Exchange Rate":{}}`))

	_, err := s.newProvider().Rate(context.Background(), "GBP", "EUR")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *mappingSuite) TestRateBadTimestampIsWrapped() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewStringResponder(http.StatusOK, `{"Realtime Currency Exchange Rate":{`+
			`"1. From_Currency Code":"GBP","3. To_Currency Code":"EUR",`+
			`"5. Exchange Rate":"1.2","6. Last Refreshed":"not-a-timestamp"}}`))

	_, err := s.newProvider().Rate(context.Background(), "GBP", "EUR")

	s.Require().Error(err)
	s.Require().NotErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *mappingSuite) TestRateHistoryFiltersToTheRange() {
	s.byFunction(map[string]string{"FX_DAILY": "testdata/GBP_EUR.FX_DAILY.json"})

	begin := civil.Date{Year: 2020, Month: 1, Day: 1}
	end := civil.Date{Year: 2020, Month: 6, Day: 30}

	rates, err := s.newProvider().RateHistory(context.Background(), "GBP", "EUR", begin, end)

	s.Require().NoError(err)
	s.Require().NotEmpty(rates)
	for _, r := range rates {
		s.True(r.Date.Between(begin, end), "%s is outside the requested range", r.Date)
	}
	for i := 1; i < len(rates); i++ {
		s.True(rates[i-1].Date.Before(rates[i].Date), "rates must ascend by date")
	}
	s.Equal(1, httpmock.GetTotalCallCount())
}

// TestRateHistoryReordersRatesThatArriveOutOfOrder feeds a wire order that is
// not already ascending (Dec, Jan, Jun), so it fails if slices.SortFunc is
// removed — unlike a fixture whose dates happen to already be sorted.
func (s *mappingSuite) TestRateHistoryReordersRatesThatArriveOutOfOrder() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewStringResponder(http.StatusOK, `{"Time Series FX (Daily)":{`+
			`"2021-12-01":{"4. close":"1.30"},`+
			`"2021-01-01":{"4. close":"1.10"},`+
			`"2021-06-01":{"4. close":"1.20"}`+
			`}}`))

	rates, err := s.newProvider().RateHistory(context.Background(), "GBP", "EUR",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2022, Month: 1, Day: 1})

	s.Require().NoError(err)
	s.Require().Len(rates, 3)
	s.Equal(civil.Date{Year: 2021, Month: 1, Day: 1}, rates[0].Date)
	s.Equal(civil.Date{Year: 2021, Month: 6, Day: 1}, rates[1].Date)
	s.Equal(civil.Date{Year: 2021, Month: 12, Day: 1}, rates[2].Date)
	s.True(decimal.NewFromFloat(1.10).Equal(rates[0].Price), "a rate's own price must travel with it when reordered")
	s.True(decimal.NewFromFloat(1.20).Equal(rates[1].Price))
	s.True(decimal.NewFromFloat(1.30).Equal(rates[2].Price))
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *mappingSuite) TestRateHistoryEmptyIsNotFound() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewStringResponder(http.StatusOK, `{"Time Series FX (Daily)":{}}`))

	_, err := s.newProvider().RateHistory(context.Background(), "GBP", "EUR",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2020, Month: 2, Day: 1})

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *mappingSuite) TestRateHistoryOutsideRangeIsNotFound() {
	s.byFunction(map[string]string{"FX_DAILY": "testdata/GBP_EUR.FX_DAILY.json"})

	_, err := s.newProvider().RateHistory(context.Background(), "GBP", "EUR",
		civil.Date{Year: 1900, Month: 1, Day: 1}, civil.Date{Year: 1900, Month: 2, Day: 1})

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *mappingSuite) TestHistoryBarsAreSortedAscending() {
	s.byFunction(map[string]string{
		"OVERVIEW":          "testdata/TOST.OVERVIEW.json",
		"TIME_SERIES_DAILY": "testdata/TOST.TIME_SERIES_DAILY.json",
	})

	h, err := s.newProvider().History(context.Background(), "TOST",
		civil.Date{Year: 2021, Month: 1, Day: 1}, civil.Date{Year: 2024, Month: 12, Day: 31})

	s.Require().NoError(err)
	s.Require().NotEmpty(h.Chart)
	s.Equal("TOST", h.Ticker)
	s.NotEmpty(h.Name)
	for i := 1; i < len(h.Chart); i++ {
		s.True(h.Chart[i-1].Date.Before(h.Chart[i].Date), "bars must ascend by date")
	}
	s.Equal(2, httpmock.GetTotalCallCount(), "overview, chart")
}

// TestHistoryReordersBarsThatArriveOutOfOrder feeds a wire order that is not
// already ascending (Dec, Jan, Jun), so it fails if slices.SortFunc is removed.
func (s *mappingSuite) TestHistoryReordersBarsThatArriveOutOfOrder() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		func(req *http.Request) (*http.Response, error) {
			if req.URL.Query().Get("function") == "OVERVIEW" {
				return httpmock.NewBytesResponse(
					http.StatusOK,
					httpmock.File("testdata/TOST.OVERVIEW.json").Bytes(),
				), nil
			}

			return httpmock.NewStringResponse(http.StatusOK, `{"Time Series (Daily)":{`+
				`"2021-12-01":{"5. volume":"100"},`+
				`"2021-01-01":{"5. volume":"200"},`+
				`"2021-06-01":{"5. volume":"300"}`+
				`}}`), nil
		})

	h, err := s.newProvider().History(context.Background(), "TOST",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2022, Month: 1, Day: 1})

	s.Require().NoError(err)
	s.Require().Len(h.Chart, 3)
	s.Equal(civil.Date{Year: 2021, Month: 1, Day: 1}, h.Chart[0].Date)
	s.Equal(civil.Date{Year: 2021, Month: 6, Day: 1}, h.Chart[1].Date)
	s.Equal(civil.Date{Year: 2021, Month: 12, Day: 1}, h.Chart[2].Date)
	s.Equal(int64(200), h.Chart[0].Volume, "a bar's own fields must travel with it when reordered")
	s.Equal(int64(300), h.Chart[1].Volume)
	s.Equal(int64(100), h.Chart[2].Volume)
	s.Equal(2, httpmock.GetTotalCallCount(), "overview, chart")
}

func (s *mappingSuite) TestHistoryFailsWhenOverviewIsEmpty() {
	s.byFunction(map[string]string{"OVERVIEW": "testdata/NOSUCH.GLOBAL_QUOTE.json"})

	_, err := s.newProvider().History(context.Background(), "NOSUCH",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2020, Month: 2, Day: 1})

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount(), "the empty overview must not spend the chart call too")
}

func (s *mappingSuite) TestHistoryFailsWhenChartIsEmpty() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		func(req *http.Request) (*http.Response, error) {
			if req.URL.Query().Get("function") == "OVERVIEW" {
				return httpmock.NewBytesResponse(
					http.StatusOK,
					httpmock.File("testdata/TOST.OVERVIEW.json").Bytes(),
				), nil
			}

			return httpmock.NewStringResponse(http.StatusOK, `{"Time Series (Daily)":{}}`), nil
		})

	_, err := s.newProvider().History(context.Background(), "TOST",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2020, Month: 2, Day: 1})

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(2, httpmock.GetTotalCallCount())
}

func (s *mappingSuite) TestSearchMapsBestMatches() {
	s.byFunction(map[string]string{"SYMBOL_SEARCH": "testdata/WD.SYMBOL_SEARCH.json"})

	m, err := s.newProvider().Search(context.Background(), "WD")

	s.Require().NoError(err)
	s.Require().NotEmpty(m)
	s.Equal("WD", m[0].Ticker)
	s.NotEmpty(m[0].Name)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *mappingSuite) TestEmptySearchIsNotFound() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		httpmock.NewStringResponder(http.StatusOK, `{"bestMatches":[]}`))

	_, err := s.newProvider().Search(context.Background(), "zzzz")

	s.Require().Error(err)
	s.Require().ErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *mappingSuite) TestSearchTransportErrorIsWrapped() {
	httpmock.RegisterResponder(
		http.MethodGet,
		apiURL,
		httpmock.NewStringResponder(http.StatusInternalServerError, "boom"),
	)

	_, err := s.newProvider().Search(context.Background(), "WD")

	s.Require().Error(err)
	s.Require().NotErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *mappingSuite) TestRateHistoryTransportErrorIsWrapped() {
	httpmock.RegisterResponder(
		http.MethodGet,
		apiURL,
		httpmock.NewStringResponder(http.StatusInternalServerError, "boom"),
	)

	_, err := s.newProvider().RateHistory(context.Background(), "GBP", "EUR",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2020, Month: 2, Day: 1})

	s.Require().Error(err)
	s.Require().NotErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount())
}

func (s *mappingSuite) TestQuoteTransportErrorIsWrapped() {
	httpmock.RegisterResponder(
		http.MethodGet,
		apiURL,
		httpmock.NewStringResponder(http.StatusInternalServerError, "boom"),
	)

	_, err := s.newProvider().Quote(context.Background(), "KO")

	s.Require().Error(err)
	s.Require().NotErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount(), "the failed quote call must not spend the overview call too")
}

func (s *mappingSuite) TestQuoteFailsWhenOverviewTransportErrors() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		func(req *http.Request) (*http.Response, error) {
			if req.URL.Query().Get("function") == "OVERVIEW" {
				return httpmock.NewStringResponse(http.StatusInternalServerError, "boom"), nil
			}

			return httpmock.NewBytesResponse(http.StatusOK, httpmock.File("testdata/KO.GLOBAL_QUOTE.json").Bytes()), nil
		})

	_, err := s.newProvider().Quote(context.Background(), "KO")

	s.Require().Error(err)
	s.Require().NotErrorIs(err, finance.ErrNotFound)
	s.Equal(2, httpmock.GetTotalCallCount())
}

func (s *mappingSuite) TestHistoryFailsWhenOverviewTransportErrors() {
	httpmock.RegisterResponder(
		http.MethodGet,
		apiURL,
		httpmock.NewStringResponder(http.StatusInternalServerError, "boom"),
	)

	_, err := s.newProvider().History(context.Background(), "TOST",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2020, Month: 2, Day: 1})

	s.Require().Error(err)
	s.Require().NotErrorIs(err, finance.ErrNotFound)
	s.Equal(1, httpmock.GetTotalCallCount(), "the failed overview call must not spend the chart call too")
}

func (s *mappingSuite) TestHistoryFailsWhenChartTransportErrors() {
	httpmock.RegisterResponder(http.MethodGet, apiURL,
		func(req *http.Request) (*http.Response, error) {
			if req.URL.Query().Get("function") == "OVERVIEW" {
				return httpmock.NewBytesResponse(
					http.StatusOK,
					httpmock.File("testdata/TOST.OVERVIEW.json").Bytes(),
				), nil
			}

			return httpmock.NewStringResponse(http.StatusInternalServerError, "boom"), nil
		})

	_, err := s.newProvider().History(context.Background(), "TOST",
		civil.Date{Year: 2020, Month: 1, Day: 1}, civil.Date{Year: 2020, Month: 2, Day: 1})

	s.Require().Error(err)
	s.Require().NotErrorIs(err, finance.ErrNotFound)
	s.Equal(2, httpmock.GetTotalCallCount())
}
