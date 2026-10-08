// Package session persists internal/simulator.Simulator state to a local
// SQLite file, keyed by trading date and market, so a process restart never
// silently loses the day's position or risk-counter state (SPEC.md 5.1).
package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	_ "modernc.org/sqlite"

	"github.com/tibetkowon/toss-trader/internal/simulator"
)

// Store persists a Simulator's State across process restarts.
type Store struct {
	db *sql.DB
}

// Open opens (creating the file and schema if necessary) a SQLite-backed
// Store at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// A single connection avoids independent connections to the same
	// SQLite file racing/locking each other.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS session_state (
		trading_date TEXT NOT NULL,
		market TEXT NOT NULL,
		state_json TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (trading_date, market)
	)`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS screener_eval (
		trading_date TEXT NOT NULL,
		market TEXT NOT NULL,
		symbol TEXT NOT NULL,
		eval_json TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (trading_date, market, symbol)
	)`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS session_settings (
		trading_date TEXT NOT NULL,
		market TEXT NOT NULL,
		settings_json TEXT NOT NULL,
		saved_at TEXT NOT NULL,
		PRIMARY KEY (trading_date, market)
	)`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// Save persists state for (date, market), overwriting any previous save for
// the same key.
func (s *Store) Save(ctx context.Context, date, market string, state simulator.State) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO session_state (trading_date, market, state_json, updated_at)
		VALUES (?, ?, ?, datetime('now'))
		ON CONFLICT(trading_date, market) DO UPDATE SET state_json = excluded.state_json, updated_at = excluded.updated_at`,
		date, market, string(data))
	return err
}

// LatestKRWCash returns the account's cash in KRW as of the most recently saved
// session of any market — KR and US sessions share one KRW account, so a new
// session starts from whatever the previous one (either market) ended with
// (SPEC.md 4.1). US sessions run in USD at a fixed rate, converted back here.
// US rows saved before currency tracking existed hold KRW-seeded numbers
// mixed with USD prices, so they are skipped.
func (s *Store) LatestKRWCash(ctx context.Context) (cash float64, ok bool, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT market, state_json FROM session_state ORDER BY trading_date DESC, updated_at DESC, rowid DESC`)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var market, data string
		if err := rows.Scan(&market, &data); err != nil {
			return 0, false, err
		}
		var state simulator.State
		if err := json.Unmarshal([]byte(data), &state); err != nil {
			return 0, false, err
		}
		if market == "US" && state.Currency == "" {
			continue
		}
		return state.Cash * state.KRWRate(), true, nil
	}
	return 0, false, rows.Err()
}

// Load retrieves the saved state for (date, market). ok is false when
// nothing has ever been saved for that key (the normal case at the start of
// a fresh trading day) — that is not an error.
func (s *Store) Load(ctx context.Context, date, market string) (state simulator.State, ok bool, err error) {
	row := s.db.QueryRowContext(ctx, `SELECT state_json FROM session_state WHERE trading_date = ? AND market = ?`, date, market)
	var data string
	if err := row.Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return simulator.State{}, false, nil
		}
		return simulator.State{}, false, err
	}
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return simulator.State{}, false, err
	}
	return state, true, nil
}

// DailyRecord is one saved day's ending state for one market.
type DailyRecord struct {
	Date   string
	Market string
	State  simulator.State
}

// All returns every saved (date, market) row, ordered by market then by
// trading_date ascending — the raw material for a day-over-day equity
// curve (SPEC.md 11's "4주 누적 최대손실 계산 도구" gap).
func (s *Store) All(ctx context.Context) ([]DailyRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT trading_date, market, state_json FROM session_state ORDER BY market ASC, trading_date ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []DailyRecord
	for rows.Next() {
		var date, market, data string
		if err := rows.Scan(&date, &market, &data); err != nil {
			return nil, err
		}
		var state simulator.State
		if err := json.Unmarshal([]byte(data), &state); err != nil {
			return nil, err
		}
		records = append(records, DailyRecord{Date: date, Market: market, State: state})
	}
	return records, rows.Err()
}
