package session

import "context"

// SaveEval stores one symbol's screening result for (date, market). The payload is opaque to this
// package; callers own its encoding.
func (s *Store) SaveEval(ctx context.Context, date, market, symbol string, data []byte) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO screener_eval (trading_date, market, symbol, eval_json, updated_at)
		VALUES (?, ?, ?, ?, datetime('now'))
		ON CONFLICT(trading_date, market, symbol) DO UPDATE SET eval_json = excluded.eval_json, updated_at = excluded.updated_at`,
		date, market, symbol, string(data))
	return err
}

// LoadEvals returns every stored screening result for (date, market), ordered by symbol.
func (s *Store) LoadEvals(ctx context.Context, date, market string) ([][]byte, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT eval_json FROM screener_eval WHERE trading_date = ? AND market = ? ORDER BY symbol ASC`, date, market)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([][]byte, 0)
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, []byte(data))
	}
	return out, rows.Err()
}
