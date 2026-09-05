// Package yahoo is the raw HTTP transport for the Yahoo Finance API: it
// authenticates, issues requests and decodes them into wire types.
package yahoo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/Toshik1978/finance"
	"github.com/Toshik1978/finance/civil"
)

const (
	apiURL    = "https://query2.finance.yahoo.com"
	cookieURL = "https://fc.yahoo.com"
	crumbPath = "v1/test/getcrumb"
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"

	searchPath = "v1/finance/search"
	chartPath  = "v8/finance/chart"
	quotePath  = "v10/finance/quoteSummary"

	quoteModules = "assetProfile,quoteType,summaryDetail,financialData,defaultKeyStatistics,price,fundPerformance"

	oneDay    = "1d"
	allEvents = "div,split"

	// crumbTTL bounds how long a cookie/crumb pair is reused before a fresh one
	// is fetched.
	crumbTTL = 24 * time.Hour
)

// Client is the raw Yahoo Finance transport: it authenticates and decodes
// responses into wire types, without mapping them onto the domain model.
type Client struct {
	log *slog.Logger
	hc  *http.Client

	mu      sync.Mutex
	fetched time.Time
	cookie  *http.Cookie
	crumb   string
}

// NewClient creates a Yahoo Finance transport using hc to issue requests.
func NewClient(log *slog.Logger, hc *http.Client) *Client {
	return &Client{log: log, hc: hc}
}

// Search finds instruments matching free-text keywords.
func (c *Client) Search(ctx context.Context, keywords string) (searchResponse, error) {
	c.log.DebugContext(ctx, "calling yahoo search", slog.String("keywords", keywords))

	query := url.Values{}
	query.Set("q", keywords)
	query.Set("quotesCount", "10")
	query.Set("newsCount", "0")

	var response searchResponse
	if err := c.decode(ctx, apiURL+"/"+searchPath, query, &response); err != nil {
		return searchResponse{}, fmt.Errorf("failed to search yahoo: %w", err)
	}

	return response, nil
}

// Quote retrieves the current price and fundamentals for a symbol.
func (c *Client) Quote(ctx context.Context, symbol string) (quoteResponse, error) {
	c.log.DebugContext(ctx, "calling yahoo quote", slog.String("symbol", symbol))

	query := url.Values{}
	query.Set("modules", quoteModules)

	var response quoteResponse
	if err := c.decode(ctx, apiURL+"/"+quotePath+"/"+symbol, query, &response); err != nil {
		return quoteResponse{}, fmt.Errorf("failed to get yahoo quote: %w", err)
	}

	return response, nil
}

// Chart retrieves daily price bars for a symbol over [from, to].
func (c *Client) Chart(ctx context.Context, symbol string, from, to civil.Date) (chartResponse, error) {
	c.log.DebugContext(ctx, "calling yahoo chart",
		slog.String("symbol", symbol), slog.String("from", from.String()), slog.String("to", to.String()))

	query := url.Values{}
	query.Set("period1", strconv.FormatInt(from.In(time.UTC).Unix(), 10))
	query.Set("period2", strconv.FormatInt(to.In(time.UTC).Unix(), 10))
	query.Set("interval", oneDay)
	query.Set("events", allEvents)

	var response chartResponse
	if err := c.decode(ctx, apiURL+"/"+chartPath+"/"+symbol, query, &response); err != nil {
		return chartResponse{}, fmt.Errorf("failed to get yahoo chart: %w", err)
	}

	return response, nil
}

// decode issues an authenticated request and unmarshals its body into v.
func (c *Client) decode(ctx context.Context, reqURL string, query url.Values, v any) error {
	body, err := c.call(ctx, reqURL, query)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("failed to unmarshal yahoo response: %w", err)
	}

	return nil
}

// statusError maps an HTTP status onto the package's sentinel errors so the
// client chain can tell a quota refusal from a genuine absence.
func statusError(status int, body []byte) error {
	switch {
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("yahoo refused the request (%d): %w", status, finance.ErrRateLimited)
	case status == http.StatusNotFound:
		return fmt.Errorf("yahoo has no such instrument (%d): %w", status, finance.ErrNotFound)
	case status >= http.StatusBadRequest:
		return fmt.Errorf("yahoo returned %d: %s", status, bytes.TrimSpace(body))
	}

	return nil
}

// call issues one authenticated request and returns the raw body. The status is
// checked first: Yahoo answers 429 with plain text, not JSON.
func (c *Client) call(ctx context.Context, reqURL string, query url.Values) ([]byte, error) {
	cookie, crumb, err := c.handshake(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate with yahoo: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.AddCookie(cookie)
	query.Set("crumb", crumb)
	req.URL.RawQuery = query.Encode()

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call yahoo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read yahoo response: %w", err)
	}

	if err := statusError(resp.StatusCode, body); err != nil {
		return nil, err
	}

	return body, nil
}

// handshake returns a cookie and crumb, refreshed at most once a day. A failed
// fetch caches nothing — a poisoned crumb used to break every call for a day.
func (c *Client) handshake(ctx context.Context) (*http.Cookie, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.fetched.IsZero() && time.Since(c.fetched) < crumbTTL {
		return c.cookie, c.crumb, nil
	}

	cookie, err := c.fetchCookie(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get cookie: %w", err)
	}

	crumb, err := c.fetchCrumb(ctx, cookie)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get crumb: %w", err)
	}

	// Only now, with both halves known good, is anything cached.
	c.cookie, c.crumb, c.fetched = cookie, crumb, time.Now()

	return c.cookie, c.crumb, nil
}

// fetchCookie retrieves the session cookie that precedes every crumb request.
func (c *Client) fetchCookie(ctx context.Context) (*http.Cookie, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cookieURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call yahoo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	cookies := resp.Cookies()
	if len(cookies) != 1 {
		return nil, errors.New("yahoo did not return exactly one cookie")
	}

	return cookies[0], nil
}

// fetchCrumb retrieves the crumb value required alongside cookie on every
// request. A non-2xx status or an empty body never becomes the cached crumb.
func (c *Client) fetchCrumb(ctx context.Context, cookie *http.Cookie) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/"+crumbPath, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.AddCookie(cookie)

	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call yahoo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read yahoo response: %w", err)
	}

	if err := statusError(resp.StatusCode, body); err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return "", errors.New("yahoo returned an empty crumb")
	}

	return string(body), nil
}
