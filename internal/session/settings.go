package session

import (
	"context"
	"database/sql"
	"errors"
)

// SaveDaySettings는 (date, market) 세션에 적용하기로 정한 설정 문서를 저장합니다. 이미 저장된 값이
// 있으면 덮어쓰지 않습니다. 같은 날 재기동할 때 처음 정한 설정을 그대로 쓰기 위해서입니다(SPEC.md 9.2).
func (s *Store) SaveDaySettings(ctx context.Context, date, market string, data []byte) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO session_settings (trading_date, market, settings_json, saved_at)
		VALUES (?, ?, ?, datetime('now'))`, date, market, string(data))
	return err
}

// LoadDaySettings는 (date, market) 세션에 저장된 설정 문서를 돌려줍니다. 없으면 ok=false입니다.
func (s *Store) LoadDaySettings(ctx context.Context, date, market string) (data []byte, ok bool, err error) {
	var raw string
	err = s.db.QueryRowContext(ctx, `SELECT settings_json FROM session_settings WHERE trading_date = ? AND market = ?`,
		date, market).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return []byte(raw), true, nil
}
