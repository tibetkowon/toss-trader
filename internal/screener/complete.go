package screener

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/tibetkowon/toss-trader/internal/strategy"
)

// Complete는 통과한 종목에 오늘 시가를 붙여 목표가와 추세 결과를 확정합니다(4단계).
// 시가를 아직 못 찾으면 HasOpen=false인 항목을 오류 없이 돌려주며 캐시하지 않습니다.
func (e *Evaluator) Complete(ctx context.Context, symbol string) (Entry, error) {
	entry, ok := e.cache[symbol]
	if !ok || !entry.Passed {
		return Entry{}, fmt.Errorf("%s: 정적 평가를 통과한 항목이 없습니다", symbol)
	}
	if entry.HasOpen {
		return entry, nil
	}
	open, found, err := e.regularOpen(ctx, symbol)
	if err != nil {
		return entry, fmt.Errorf("%s 1분봉 조회: %w", symbol, err)
	}
	if !found {
		return entry, nil
	}
	target, trendOK, err := strategy.ComputeDaySetup(open, entry.Prev, entry.Closes, e.cfg.K)
	if err != nil {
		return entry, fmt.Errorf("%s 셋업 계산: %w", symbol, err)
	}
	entry.HasOpen, entry.Open, entry.Target, entry.TrendOK = true, open, target, trendOK
	e.store(entry)
	return entry, nil
}

// regularOpen은 정규장 시작 이후 가장 이른 1분봉의 시가를 찾습니다. 장전(NXT 등) 봉은 무시하고,
// 정규장 첫 봉이 없는 저유동 종목은 그 이후 가장 이른 봉으로 대신합니다.
func (e *Evaluator) regularOpen(ctx context.Context, symbol string) (float64, bool, error) {
	before := ""
	var bestTime time.Time
	var bestOpen float64
	found := false
	for page := 0; page < 4; page++ {
		candles, next, err := e.src.Candles(ctx, symbol, "1m", 200, before)
		if err != nil {
			return 0, false, err
		}
		reachedStart := false
		for _, c := range candles {
			ts, err := time.Parse(time.RFC3339, c.Timestamp)
			if err != nil {
				continue
			}
			if ts.Before(e.sessionStart) {
				reachedStart = true
				continue
			}
			open, err := strconv.ParseFloat(c.OpenPrice, 64)
			if err != nil || open <= 0 {
				continue
			}
			if !found || ts.Before(bestTime) {
				bestTime, bestOpen, found = ts, open, true
			}
		}
		if reachedStart || next == "" {
			break
		}
		before = next
	}
	return bestOpen, found, nil
}
