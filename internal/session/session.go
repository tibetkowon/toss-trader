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

// LatestCash returns the ending cash from the most recently saved day for
// market, if any — used to seed a brand new trading day with the prior
// day's compounded balance (SPEC.md 4.1) rather than always resetting to a
// fixed starting amount.
func (s *Store) LatestCash(ctx context.Context, market string) (cash float64, ok bool, err error) {
	row := s.db.QueryRowContext(ctx, `SELECT state_json FROM session_state WHERE market = ? ORDER BY trading_date DESC LIMIT 1`, market)
	var data string
	if err := row.Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	var state simulator.State
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return 0, false, err
	}
	return state.Cash, true, nil
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
