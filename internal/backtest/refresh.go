package backtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

// Refresh는 캐시된 종목에 최신 일봉과 분봉을 이어 붙입니다. Fetch가 과거 방향으로만 받으므로 새 거래일은 이 함수로 채웁니다.
// 분봉은 최신부터 가져오다 캐시의 가장 새로운 봉에 닿으면 멈춥니다. maxPages 안에 못 닿으면 구멍이 생기므로 오류입니다.
func Refresh(ctx context.Context, src screener.Source, cache Cache, market string, maxPages int, logf func(string, ...any)) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	universe, err := cache.LoadUniverse(market)
	if err != nil {
		return fmt.Errorf("유니버스 캐시가 없습니다(먼저 fetch): %w", err)
	}
	var errs []error
	for i, u := range universe {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := refreshSymbol(ctx, src, cache, market, u.Symbol, maxPages); err != nil {
			logf("%s 갱신 실패: %v", u.Symbol, err)
			errs = append(errs, fmt.Errorf("%s: %w", u.Symbol, err))
			continue
		}
		logf("[%d/%d] %s 갱신", i+1, len(universe), u.Symbol)
	}
	return errors.Join(errs...)
}

func refreshSymbol(ctx context.Context, src screener.Source, cache Cache, market, symbol string, maxPages int) error {
	var daily []tossapi.Candle
	if err := cache.load(market, symbol, "1d", &daily); err != nil {
		return err
	}
	fresh, _, err := src.Candles(ctx, symbol, "1d", 200, "")
	if err != nil {
		return err
	}
	if daily, err = ordered(append(daily, fresh...)); err != nil {
		return err
	}
	if err = cache.save(market, symbol, "1d", daily); err != nil {
		return err
	}
	var minutes []tossapi.Candle
	if err = cache.load(market, symbol, "1m", &minutes); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if minutes, err = ordered(minutes); err != nil {
		return err
	}
	var newest time.Time
	if len(minutes) > 0 {
		if newest, err = time.Parse(time.RFC3339, minutes[0].Timestamp); err != nil {
			return err
		}
	}
	before, joined := "", len(minutes) == 0
	for page := 0; page < maxPages && !joined; page++ {
		bars, next, err := src.Candles(ctx, symbol, "1m", 200, before)
		if err != nil {
			return err
		}
		for _, b := range bars {
			if t, err := time.Parse(time.RFC3339, b.Timestamp); err == nil && !t.After(newest) {
				joined = true
			}
		}
		minutes = append(minutes, bars...)
		if next == "" || len(bars) == 0 || next == before {
			break
		}
		before = next
	}
	if !joined {
		return fmt.Errorf("분봉 %d페이지 안에 캐시와 이어지지 않습니다(BACKTEST_MAX_PAGES 증가 필요)", maxPages)
	}
	if minutes, err = ordered(minutes); err != nil {
		return err
	}
	return cache.save(market, symbol, "1m", minutes)
}
