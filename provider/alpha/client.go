// Package alpha is the raw HTTP transport for the AlphaVantage API: it
// authenticates, issues requests and decodes them into wire types.
package alpha

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

	"github.com/Toshik1978/finance"
)

const (
	apiURL = "https://www.alphavantage.co/query"

	paramFunction   = "function"
	paramAPIKey     = "apikey"
	paramSymbol     = "symbol"
	paramOutputSize = "outputsize"
	outputSizeFull  = "full"
	paramKeywords   = "keywords"
	paramFromCode   = "from_currency"
	paramToCode     = "to_currency"
	paramFromSymbol = "from_symbol"
	paramToSymbol   = "to_symbol"

	funcSearch    = "SYMBOL_SEARCH"
	funcOverview  = "OVERVIEW"
	funcQuote     = "GLOBAL_QUOTE"
	funcDaily     = "TIME_SERIES_DAILY"
	funcRate      = "CURRENCY_EXCHANGE_RATE"
	funcRateChart = "FX_DAILY"
)

// Client is the raw AlphaVantage transport: it authenticates and decodes
// responses into wire types, without mapping them onto the domain model.
type Client struct {
	log    *slog.Logger
	hc     *http.Client
	apiKey string
}

// NewClient creates an AlphaVantage transport using hc to issue requests,
// authenticated with apiKey.
func NewClient(log *slog.Logger, hc *http.Client, apiKey string) *Client {
	return &Client{log: log, hc: hc, apiKey: apiKey}
}

// Search finds instruments matching free-text keywords.
func (c *Client) Search(ctx context.Context, keywords string) (searchResponse, error) {
	c.log.DebugContext(ctx, "calling alphavantage search", slog.String("keywords", keywords))

	query := url.Values{}
	query.Set(paramFunction, funcSearch)
	query.Set(paramKeywords, keywords)

	var response searchResponse
	if err := c.decode(ctx, query, &response); err != nil {
		return searchResponse{}, fmt.Errorf("failed to search alphavantage: %w", err)
	}

	return response, nil
}

// Quote retrieves the current price for a symbol.
func (c *Client) Quote(ctx context.Context, symbol string) (quoteResponse, error) {
	c.log.DebugContext(ctx, "calling alphavantage quote", slog.String("symbol", symbol))

	query := url.Values{}
	query.Set(paramFunction, funcQuote)
	query.Set(paramSymbol, symbol)

	var response quoteResponse
	if err := c.decode(ctx, query, &response); err != nil {
		return quoteResponse{}, fmt.Errorf("failed to get alphavantage quote: %w", err)
	}

	return response, nil
}

// Overview retrieves company fundamentals for a symbol.
func (c *Client) Overview(ctx context.Context, symbol string) (overviewResponse, error) {
	c.log.DebugContext(ctx, "calling alphavantage overview", slog.String("symbol", symbol))

	query := url.Values{}
	query.Set(paramFunction, funcOverview)
	query.Set(paramSymbol, symbol)

	var response overviewResponse
	if err := c.decode(ctx, query, &response); err != nil {
		return overviewResponse{}, fmt.Errorf("failed to get alphavantage overview: %w", err)
	}

	return response, nil
}

// Chart retrieves the full daily price history for a symbol.
func (c *Client) Chart(ctx context.Context, symbol string) (chartResponse, error) {
	c.log.DebugContext(ctx, "calling alphavantage chart", slog.String("symbol", symbol))

	query := url.Values{}
	query.Set(paramFunction, funcDaily)
	query.Set(paramOutputSize, outputSizeFull)
	query.Set(paramSymbol, symbol)

	var response chartResponse
	if err := c.decode(ctx, query, &response); err != nil {
		return chartResponse{}, fmt.Errorf("failed to get alphavantage chart: %w", err)
	}

	return response, nil
}

// Rate retrieves the current exchange rate from one currency to another.
func (c *Client) Rate(ctx context.Context, from, to string) (rateResponse, error) {
	c.log.DebugContext(ctx, "calling alphavantage rate", slog.String("from", from), slog.String("to", to))

	query := url.Values{}
	query.Set(paramFunction, funcRate)
	query.Set(paramFromCode, from)
	query.Set(paramToCode, to)

	var response rateResponse
	if err := c.decode(ctx, query, &response); err != nil {
		return rateResponse{}, fmt.Errorf("failed to get alphavantage rate: %w", err)
	}

	return response, nil
}

// RateChart retrieves the full daily exchange rate history between two currencies.
func (c *Client) RateChart(ctx context.Context, from, to string) (rateChartResponse, error) {
	c.log.DebugContext(ctx, "calling alphavantage rate chart", slog.String("from", from), slog.String("to", to))

	query := url.Values{}
	query.Set(paramFunction, funcRateChart)
	query.Set(paramOutputSize, outputSizeFull)
	query.Set(paramFromSymbol, from)
	query.Set(paramToSymbol, to)

	var response rateChartResponse
	if err := c.decode(ctx, query, &response); err != nil {
		return rateChartResponse{}, fmt.Errorf("failed to get alphavantage rate chart: %w", err)
	}

	return response, nil
}

// statusError maps an HTTP status onto the package's sentinel errors so the
// client chain can tell a quota refusal from a genuine absence.
func statusError(status int, body []byte) error {
	switch {
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("alphavantage refused the request (%d): %w", status, finance.ErrRateLimited)
	case status == http.StatusNotFound:
		return fmt.Errorf("alphavantage has no such instrument (%d): %w", status, finance.ErrNotFound)
	case status != http.StatusOK:
		return fmt.Errorf("alphavantage returned %d: %s", status, bytes.TrimSpace(body))
	}

	return nil
}

// decode issues one request built from query and unmarshals the body into v.
func (c *Client) decode(ctx context.Context, query url.Values, v any) error {
	body, err := c.call(ctx, query)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("failed to unmarshal alphavantage response: %w", err)
	}

	return nil
}

// call issues one request and returns the raw body, having checked both the
// HTTP status and AlphaVantage's in-band failure envelope.
func (c *Client) call(ctx context.Context, query url.Values) ([]byte, error) {
	query.Set(paramAPIKey, c.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.URL.RawQuery = query.Encode()

	resp, err := c.hc.Do(req)
	if err != nil {
		// *url.Error carries the full request URL, apikey and all: net/http's own
		// redaction strips only userinfo passwords, never query parameters.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			return nil, fmt.Errorf("failed to call alphavantage (%s): %w", ue.Op, ue.Err)
		}

		return nil, fmt.Errorf("failed to call alphavantage: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read alphavantage response: %w", err)
	}

	if err := statusError(resp.StatusCode, body); err != nil {
		return nil, err
	}

	// AlphaVantage answers 200 even when it is refusing; the envelope decides.
	var f failure
	if err := json.Unmarshal(body, &f); err == nil {
		if err := f.err(); err != nil {
			return nil, err
		}
	}

	return body, nil
}
