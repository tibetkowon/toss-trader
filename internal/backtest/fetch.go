package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

// Fetch는 주기적으로, 그리고 정상 종료/조회 오류 시 수집분을 저장합니다.
// API 재시도/백오프는 Source에 맡깁니다.
// maxPages는 이번 실행에서 종목별로 추가 수집할 분봉 페이지 수입니다.
func Fetch(ctx context.Context, src screener.Source, cache Cache, market string, maxPages int, logf func(string, ...any)) error {
	if market != "KR" && market != "US" {
		return fmt.Errorf("시장은 KR 또는 US여야 합니다")
	}
	if maxPages <= 0 {
		return fmt.Errorf("페이지 수는 양수여야 합니다")
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	universe, err := cache.LoadUniverse(market)
	if errors.Is(err, os.ErrNotExist) {
		universe, err = src.Rankings(ctx, "MARKET_TRADING_AMOUNT", "1mo", market)
		if err != nil {
			return err
		}
		sort.SliceStable(universe, func(i, j int) bool { return universe[i].Rank < universe[j].Rank })
		seen := map[string]bool{}
		unique := make([]tossapi.RankingItem, 0, 100)
		for _, r := range universe {
			if r.Symbol == "" || seen[r.Symbol] {
				continue
			}
			seen[r.Symbol] = true
			unique = append(unique, r)
			if len(unique) == 100 {
				break
			}
		}
		universe = unique
		if len(universe) == 0 {
			return fmt.Errorf("유니버스가 비었습니다")
		}
		err = cache.SaveUniverse(market, universe)
	}
	if err != nil {
		return err
	}
	logf("%s 유니버스 %d종목", market, len(universe))
	var errs []error
	for i, u := range universe {
		if err := ctx.Err(); err != nil {
			return err
		}
		logf("[%d/%d] %s 수집 시작", i+1, len(universe), u.Symbol)
		if err := fetchSymbol(ctx, src, cache, market, u.Symbol, maxPages, logf); err != nil {
			logf("%s 수집 실패 (저장된 지점에서 재개 가능): %v", u.Symbol, err)
			errs = append(errs, fmt.Errorf("%s: %w", u.Symbol, err))
		}
	}
	return errors.Join(errs...)
}
func fetchSymbol(ctx context.Context, src screener.Source, cache Cache, market, symbol string, maxPages int, logf func(string, ...any)) error {
	if _, err := cache.loadStocks(market, symbol); errors.Is(err, os.ErrNotExist) {
		v, err := src.Stocks(ctx, symbol)
		if err != nil {
			return err
		}
		if err = cache.saveStocks(market, symbol, v); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var warnings []json.RawMessage
	if err := cache.load(market, symbol, "warnings", &warnings); errors.Is(err, os.ErrNotExist) {
		v, err := src.StockWarnings(ctx, symbol)
		if err != nil {
			return err
		}
		if err = cache.save(market, symbol, "warnings", v); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var daily []tossapi.Candle
	if err := cache.load(market, symbol, "1d", &daily); errors.Is(err, os.ErrNotExist) {
		v, _, err := src.Candles(ctx, symbol, "1d", 200, "")
		if err != nil {
			return err
		}
		if _, err = ordered(v); err != nil {
			return err
		}
		if err = cache.save(market, symbol, "1d", v); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var minutes []tossapi.Candle
	if err := cache.load(market, symbol, "1m", &minutes); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	minutes, err := ordered(minutes)
	if err != nil {
		return err
	}
	var complete bool
	if err := cache.load(market, symbol, "1m-complete", &complete); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if complete {
		logf("%s 분봉 %d개 (수집 완료 캐시)", symbol, len(minutes))
		return nil
	}
	before := ""
	if len(minutes) > 0 {
		before = minutes[len(minutes)-1].Timestamp
	}
	// 페이지마다 8만 봉을 정렬·저장하면 종목당 수 GB를 쓰게 되므로 일정 간격과 종료 시점에만 저장한다.
	const flushEvery = 25
	flush := func() error {
		sorted, err := ordered(minutes)
		if err != nil {
			return err
		}
		minutes = sorted
		return cache.save(market, symbol, "1m", minutes)
	}
	for page := 0; page < maxPages; page++ {
		bars, next, err := src.Candles(ctx, symbol, "1m", 200, before)
		if err != nil {
			return errors.Join(err, flush())
		}
		oldLen := len(minutes)
		minutes = append(minutes, bars...)
		if (page+1)%flushEvery == 0 {
			if err = flush(); err != nil {
				return err
			}
			logf("%s 분봉 페이지 %d/%d, 누적 %d개", symbol, page+1, maxPages, len(minutes))
		}
		if next == "" || len(bars) == 0 {
			if err = flush(); err != nil {
				return err
			}
			return cache.save(market, symbol, "1m-complete", true)
		}
		if next == before || len(minutes) == oldLen {
			return errors.Join(fmt.Errorf("분봉 페이지네이션 진행 없음"), flush())
		}
		before = next
	}
	return flush()
}
