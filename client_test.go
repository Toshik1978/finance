package finance

import (
	"context"
	"errors"
	"log/slog"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/finance/civil"
)

// discardLogger is the logger every suite uses; slog has no global in this
// project, so each Client gets one explicitly.
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// fakeProvider is a Provider whose every method is scripted. Unset function
// fields return ErrNotFound, which is the useful default for a fallback chain.
type fakeProvider struct {
	name    string
	calls   int
	search  func() ([]Match, error)
	quote   func() (Quote, error)
	history func() (History, error)
	rate    func() (ExchangeRate, error)
	rates   func() ([]ExchangeRate, error)
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Search(context.Context, string) ([]Match, error) {
	f.calls++
	if f.search == nil {
		return nil, ErrNotFound
	}
	return f.search()
}

func (f *fakeProvider) Quote(context.Context, string) (Quote, error) {
	f.calls++
	if f.quote == nil {
		return Quote{}, ErrNotFound
	}
	return f.quote()
}

func (f *fakeProvider) History(context.Context, string, civil.Date, civil.Date) (History, error) {
	f.calls++
	if f.history == nil {
		return History{}, ErrNotFound
	}
	return f.history()
}

func (f *fakeProvider) Rate(context.Context, string, string) (ExchangeRate, error) {
	f.calls++
	if f.rate == nil {
		return ExchangeRate{}, ErrNotFound
	}
	return f.rate()
}

func (f *fakeProvider) RateHistory(
	context.Context, string, string, civil.Date, civil.Date,
) ([]ExchangeRate, error) {
	f.calls++
	if f.rates == nil {
		return nil, ErrNotFound
	}
	return f.rates()
}

type clientSuite struct {
	suite.Suite
}

func (s *clientSuite) TestFirstProviderWinsAndSecondIsNotCalled() {
	first := &fakeProvider{name: "first", quote: func() (Quote, error) {
		return Quote{Ticker: "AAPL"}, nil
	}}
	second := &fakeProvider{name: "second"}

	c := NewClient(discardLogger(), []Provider{first, second}, nil)
	q, err := c.Quote(context.Background(), "AAPL")

	s.Require().NoError(err)
	s.Equal("AAPL", q.Ticker)
	s.Equal(1, first.calls)
	s.Zero(second.calls, "a satisfied request must not reach the fallback")
}

func (s *clientSuite) TestFallsThroughToSecondProvider() {
	first := &fakeProvider{name: "first", quote: func() (Quote, error) {
		return Quote{}, ErrRateLimited
	}}
	second := &fakeProvider{name: "second", quote: func() (Quote, error) {
		return Quote{Ticker: "AAPL"}, nil
	}}

	c := NewClient(discardLogger(), []Provider{first, second}, nil)
	q, err := c.Quote(context.Background(), "AAPL")

	s.Require().NoError(err)
	s.Equal("AAPL", q.Ticker)
	s.Equal(1, second.calls)
}

func (s *clientSuite) TestAllProvidersFailingYieldsAJoinedError() {
	// The bug this replaces: the old chain returned (nil, nil) here, and the
	// caller then dereferenced it. See the RateToday regression in Task 6.
	first := &fakeProvider{name: "yahoo", quote: func() (Quote, error) {
		return Quote{}, ErrRateLimited
	}}
	second := &fakeProvider{name: "alpha"}

	c := NewClient(discardLogger(), []Provider{first, second}, nil)
	q, err := c.Quote(context.Background(), "AAPL")

	s.Require().Error(err)
	s.Equal(Quote{}, q, "the zero value, never a nil dereference")
	s.Require().ErrorIs(err, ErrRateLimited, "the first provider's cause must survive")
	s.Require().ErrorIs(err, ErrNotFound, "so must the second's")
	s.Contains(err.Error(), "yahoo", "each provider must be named")
	s.Contains(err.Error(), "alpha")
}

func (s *clientSuite) TestNoProvidersIsNotFound() {
	c := NewClient(discardLogger(), nil, nil)
	_, err := c.Quote(context.Background(), "AAPL")

	s.Require().Error(err)
	s.ErrorIs(err, ErrNotFound)
}

func (s *clientSuite) TestSearchAndHistoryUseTheSameChain() {
	p := &fakeProvider{
		name:    "p",
		search:  func() ([]Match, error) { return []Match{{Ticker: "AAPL"}}, nil },
		history: func() (History, error) { return History{Chart: []Bar{{}}}, nil },
	}
	c := NewClient(discardLogger(), []Provider{p}, nil)

	m, err := c.Search(context.Background(), "apple")
	s.Require().NoError(err)
	s.Len(m, 1)

	h, err := c.History(context.Background(), "AAPL", civil.Date{}, civil.Date{})
	s.Require().NoError(err)
	s.Len(h.Chart, 1)
}

func (s *clientSuite) TestErrorsAreNotSwallowedWhenALaterProviderSucceeds() {
	boom := errors.New("boom")
	first := &fakeProvider{name: "first", quote: func() (Quote, error) { return Quote{}, boom }}
	second := &fakeProvider{name: "second", quote: func() (Quote, error) {
		return Quote{Ticker: "AAPL"}, nil
	}}

	c := NewClient(discardLogger(), []Provider{first, second}, nil)
	q, err := c.Quote(context.Background(), "AAPL")

	// A success must return a nil error — never a value AND an error together,
	// which the old chain did.
	s.Require().NoError(err)
	s.Equal("AAPL", q.Ticker)
}
