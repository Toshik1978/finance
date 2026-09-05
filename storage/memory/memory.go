// Package memory provides an in-process currency cache implementing
// finance.CurrencyStorage: the reference behaviour the SQL storage must match.
package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Toshik1978/finance"
	"github.com/Toshik1978/finance/civil"
)

// Storage is an in-memory currency cache, safe for concurrent use.
type Storage struct {
	mu    sync.RWMutex
	rates map[string]decimal.Decimal
	sync  map[string]time.Time
}

var _ finance.CurrencyStorage = (*Storage)(nil)

// New creates an empty in-memory currency cache.
func New() *Storage {
	return &Storage{
		rates: make(map[string]decimal.Decimal),
		sync:  make(map[string]time.Time),
	}
}

// Rate reads one cached rate, reporting a miss as finance.ErrNotFound.
func (s *Storage) Rate(_ context.Context, date civil.Date, from, to string) (decimal.Decimal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.rates[rateKey(date, from, to)]
	if !ok {
		return decimal.Decimal{}, fmt.Errorf("no rate for %s/%s on %s: %w", from, to, date, finance.ErrNotFound)
	}

	return v, nil
}

// SetRates writes rates not already cached; a key already present is left
// untouched, so a racing duplicate write is a harmless no-op.
func (s *Storage) SetRates(_ context.Context, rates []finance.ExchangeRate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range rates {
		key := rateKey(rates[i].Date, rates[i].From, rates[i].To)
		if _, ok := s.rates[key]; !ok {
			s.rates[key] = rates[i].Price
		}
	}

	return nil
}

// SyncTime reads when the pair was last refreshed, reporting a miss as
// finance.ErrNotFound.
func (s *Storage) SyncTime(_ context.Context, from, to string) (time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.sync[pairKey(from, to)]
	if !ok {
		return time.Time{}, fmt.Errorf("no sync time for %s/%s: %w", from, to, finance.ErrNotFound)
	}

	return t, nil
}

// SetSyncTime records when the pair was last refreshed, converting t to UTC so
// that later comparisons never depend on the host's zone.
func (s *Storage) SetSyncTime(_ context.Context, from, to string, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sync[pairKey(from, to)] = t.UTC()

	return nil
}

func rateKey(date civil.Date, from, to string) string {
	return date.String() + ":" + from + ":" + to
}

func pairKey(from, to string) string {
	return from + ":" + to
}
