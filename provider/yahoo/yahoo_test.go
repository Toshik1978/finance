package yahoo

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/finance"
	"github.com/Toshik1978/finance/civil"
)

const (
	testCookieName  = "A3"
	testCookieValue = "test-cookie-a1b2c3"
	testCrumb       = "test-crumb-d4e5f6"
)

// TestYahoo is the package's single entry point; every suite is registered here.
func TestYahoo(t *testing.T) {
	suite.Run(t, new(transportSuite))
	suite.Run(t, new(mappingSuite))
}

// registerHandshake wires the cookie and crumb calls that precede every request.
func registerHandshake() {
	httpmock.RegisterResponder(http.MethodGet, cookieURL,
		func(*http.Request) (*http.Response, error) {
			resp := httpmock.NewStringResponse(http.StatusOK, "")
			resp.Header.Add("Set-Cookie", testCookieName+"="+testCookieValue)

			return resp, nil
		})
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+crumbPath,
		httpmock.NewStringResponder(http.StatusOK, testCrumb))
}

type transportSuite struct {
	suite.Suite
}

func (s *transportSuite) SetupTest() {
	httpmock.Activate()
	registerHandshake()
}

func (s *transportSuite) TearDownTest() {
	httpmock.DeactivateAndReset()
}

func (s *transportSuite) newClient() *Client {
	return NewClient(slog.New(slog.DiscardHandler), http.DefaultClient)
}

func (s *transportSuite) TestQuoteDecodesAFixture() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/TOST",
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/TOST.quote.json").Bytes()))

	r, err := s.newClient().Quote(context.Background(), "TOST")

	s.Require().NoError(err)
	s.Require().Len(r.Inner.Result, 1)
	s.Equal("TOST", r.Inner.Result[0].QuoteType.Symbol)
	s.Positive(httpmock.GetTotalCallCount(), "the test must exercise the transport")
}

func (s *transportSuite) TestSearchDecodesAFixture() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+searchPath,
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/TOST.search.json").Bytes()))

	r, err := s.newClient().Search(context.Background(), "TOST")

	s.Require().NoError(err)
	s.Positive(r.Count)
	s.NotEmpty(r.Quotes)
}

func (s *transportSuite) TestQuoteNotFoundFixtureDecodesTheErrorEnvelope() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/NOSUCH",
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/NOSUCH.quote.json").Bytes()))

	r, err := s.newClient().Quote(context.Background(), "NOSUCH")

	s.Require().NoError(err)
	s.Empty(r.Inner.Result)
	s.Require().NotNil(r.Inner.Error)
	s.Equal("Not Found", r.Inner.Error.Code)
}

func (s *transportSuite) TestRateLimitBecomesErrRateLimited() {
	// Regression: a 429 body of plain text "Too Many Requests" used to reach
	// json.Unmarshal directly, producing "invalid character 'T'".
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/TOST",
		httpmock.NewStringResponder(http.StatusTooManyRequests, "Too Many Requests"))

	_, err := s.newClient().Quote(context.Background(), "TOST")

	s.Require().ErrorIs(err, finance.ErrRateLimited)
	s.NotContains(err.Error(), "invalid character")
}

func (s *transportSuite) TestNotFoundBecomesErrNotFound() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/NOPE",
		httpmock.NewStringResponder(http.StatusNotFound, ""))

	_, err := s.newClient().Quote(context.Background(), "NOPE")

	s.Require().ErrorIs(err, finance.ErrNotFound)
}

func (s *transportSuite) TestFailedCrumbIsNotCached() {
	// Regression: a failed crumb fetch used to be cached as the crumb itself,
	// breaking every call for the next 24 hours.
	httpmock.Reset()
	httpmock.RegisterResponder(http.MethodGet, cookieURL,
		func(*http.Request) (*http.Response, error) {
			resp := httpmock.NewStringResponse(http.StatusOK, "")
			resp.Header.Add("Set-Cookie", testCookieName+"="+testCookieValue)

			return resp, nil
		})
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+crumbPath,
		httpmock.NewStringResponder(http.StatusTooManyRequests, "Too Many Requests"))

	c := s.newClient()
	_, err := c.Quote(context.Background(), "TOST")
	s.Require().Error(err)

	s.Empty(c.crumb, "a failed handshake must not poison the cache")
	s.True(c.fetched.IsZero(), "and must not start the 24-hour clock")
}

func (s *transportSuite) TestEmptyCrumbIsRejectedAndNotCached() {
	httpmock.Reset()
	httpmock.RegisterResponder(http.MethodGet, cookieURL,
		func(*http.Request) (*http.Response, error) {
			resp := httpmock.NewStringResponse(http.StatusOK, "")
			resp.Header.Add("Set-Cookie", testCookieName+"="+testCookieValue)

			return resp, nil
		})
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+crumbPath,
		httpmock.NewStringResponder(http.StatusOK, ""))

	c := s.newClient()
	_, err := c.Quote(context.Background(), "TOST")

	s.Require().Error(err)
	s.Empty(c.crumb)
	s.True(c.fetched.IsZero())
}

func (s *transportSuite) TestHandshakeHappensOncePerClient() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/TOST",
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/TOST.quote.json").Bytes()))

	c := s.newClient()
	_, err := c.Quote(context.Background(), "TOST")
	s.Require().NoError(err)
	_, err = c.Quote(context.Background(), "TOST")
	s.Require().NoError(err)

	info := httpmock.GetCallCountInfo()
	s.Equal(1, info["GET "+apiURL+"/"+crumbPath], "the crumb must be reused")
}

func (s *transportSuite) TestChartPassesTheDateRange() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+chartPath+"/TOST",
		httpmock.NewBytesResponder(http.StatusOK, httpmock.File("testdata/TOST.chart.json").Bytes()))

	_, err := s.newClient().Chart(context.Background(), "TOST",
		civil.Date{Year: 2026, Month: 1, Day: 1}, civil.Date{Year: 2026, Month: 3, Day: 1})

	s.Require().NoError(err)
}

func (s *transportSuite) TestCrumbReachesTheOutgoingQuery() {
	// Responders match on path alone, so nothing else asserts the crumb is
	// actually attached to the request rather than merely fetched.
	var got string
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/TOST",
		func(req *http.Request) (*http.Response, error) {
			got = req.URL.Query().Get("crumb")

			return httpmock.NewBytesResponse(http.StatusOK, httpmock.File("testdata/TOST.quote.json").Bytes()), nil
		})

	_, err := s.newClient().Quote(context.Background(), "TOST")

	s.Require().NoError(err)
	s.Equal(testCrumb, got)
}

func (s *transportSuite) TestServerErrorIsWrapped() {
	httpmock.RegisterResponder(http.MethodGet, apiURL+"/"+quotePath+"/TOST",
		httpmock.NewStringResponder(http.StatusInternalServerError, "boom"))

	_, err := s.newClient().Quote(context.Background(), "TOST")

	s.Require().Error(err)
	s.Require().NotErrorIs(err, finance.ErrRateLimited)
	s.Require().NotErrorIs(err, finance.ErrNotFound)
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
