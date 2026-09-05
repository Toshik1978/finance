// Package sqldb provides a SQL-backed currency cache. It takes a *sql.DB and
// never imports a driver — the database choice belongs to the caller.
package sqldb

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Toshik1978/finance"
	"github.com/Toshik1978/finance/civil"
)

// SchemaSQLite creates the tables and index this package reads and writes, in
// SQLite dialect. The caller applies it once at startup.
//
//go:embed schema/sqlite.sql
var SchemaSQLite string

// SchemaPostgres is SchemaSQLite's PostgreSQL equivalent.
//
//go:embed schema/pg.sql
var SchemaPostgres string

// syncTimeKey namespaces a pair's sync time inside currency_settings.
const syncTimeKey = "sync_time:%s:%s"

// Storage is a SQL-backed currency cache, safe for concurrent use to the
// extent *sql.DB is.
type Storage struct {
	db *sql.DB
}

// New creates a currency cache over an already-open database. The caller must
// apply SchemaSQLite or SchemaPostgres to it first.
func New(db *sql.DB) *Storage {
	return &Storage{db: db}
}

// Rate reads one cached rate, reporting a miss as finance.ErrNotFound.
func (s *Storage) Rate(ctx context.Context, date civil.Date, from, to string) (decimal.Decimal, error) {
	const query = `SELECT value FROM currencies WHERE date = $1 AND base_code = $2 AND code = $3`

	var v decimal.Decimal

	err := s.db.QueryRowContext(ctx, query, date, from, to).Scan(&v)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return decimal.Decimal{}, fmt.Errorf("no rate for %s/%s on %s: %w", from, to, date, finance.ErrNotFound)
	case err != nil:
		return decimal.Decimal{}, fmt.Errorf("failed to read rate: %w", err)
	}

	return v, nil
}

// SetRates writes rates not already cached, in one transaction; the unique
// index leaves an already-present key untouched, so a racing write is a no-op.
func (s *Storage) SetRates(ctx context.Context, rates []finance.ExchangeRate) error {
	if len(rates) == 0 {
		return nil
	}

	const query = `
		INSERT INTO currencies (date, base_code, code, value)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (date, base_code, code) DO NOTHING`

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for i := range rates {
		if _, err := stmt.ExecContext(ctx, rates[i].Date, rates[i].From, rates[i].To, rates[i].Price); err != nil {
			return fmt.Errorf(
				"failed to write rate for %s/%s on %s: %w",
				rates[i].From,
				rates[i].To,
				rates[i].Date,
				err,
			)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit rates: %w", err)
	}

	return nil
}

// SyncTime reads when the pair was last refreshed, reporting a miss as
// finance.ErrNotFound. The returned time is always in UTC.
func (s *Storage) SyncTime(ctx context.Context, from, to string) (time.Time, error) {
	const query = `SELECT value FROM currency_settings WHERE key = $1`

	var raw string

	err := s.db.QueryRowContext(ctx, query, fmt.Sprintf(syncTimeKey, from, to)).Scan(&raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return time.Time{}, fmt.Errorf("no sync time for %s/%s: %w", from, to, finance.ErrNotFound)
	case err != nil:
		return time.Time{}, fmt.Errorf("failed to read sync time: %w", err)
	}

	sec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("malformed sync time %q: %w", raw, err)
	}

	// .UTC() is load-bearing: time.Unix returns local time, and the client
	// compares this against a UTC-derived date.
	return time.Unix(sec, 0).UTC(), nil
}

// SetSyncTime records when the pair was last refreshed, converting t to UTC so
// that later comparisons never depend on the host's zone.
func (s *Storage) SetSyncTime(ctx context.Context, from, to string, t time.Time) error {
	const query = `
		INSERT INTO currency_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = CURRENT_TIMESTAMP`

	key := fmt.Sprintf(syncTimeKey, from, to)
	if _, err := s.db.ExecContext(ctx, query, key, strconv.FormatInt(t.UTC().Unix(), 10)); err != nil {
		return fmt.Errorf("failed to write sync time for %s/%s: %w", from, to, err)
	}

	return nil
}
