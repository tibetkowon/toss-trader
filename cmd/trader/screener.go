package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/session"
	"github.com/tibetkowon/toss-trader/internal/simulator"
	"github.com/tibetkowon/toss-trader/internal/snapshot"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
	"github.com/tibetkowon/toss-trader/internal/tradingloop"
)

func envFloat(getenv func(string) string, key string) (float64, bool) {
	v := getenv(key)
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n < 0 || n != n {
		return 0, false
	}
	return n, true
}

func envInt(getenv func(string) string, key string, min int) (int, bool) {
	v := getenv(key)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min {
		return 0, false
	}
	return n, true
}

func screenerConfig(market string, commissionRate float64, getenv func(string) string) screener.Config {
	c := screener.DefaultConfig(market)
	c.CommissionRate = commissionRate
	if n, ok := envInt(getenv, "RANK_DEPTH", 1); ok {
		c.RankDepth = n
	}
	if n, ok := envInt(getenv, "ACTIVE_COUNT", 1); ok {
		c.ActiveCount = n
	}
	if n, ok := envInt(getenv, "EVAL_PER_MIN", 0); ok {
		c.EvalPerMin = n
	}
	if n, ok := envInt(getenv, "ACTIVE_KEEP_RANK", 0); ok {
		c.KeepRank = n
	}
	if n, ok := envInt(getenv, "RANK_START_DELAY_MINUTES", 0); ok {
		c.StartDelay = time.Duration(n) * time.Minute
	}
	if n, ok := envInt(getenv, "RANK_REFRESH_SECONDS", 1); ok {
		c.RefreshEvery = time.Duration(n) * time.Second
	}
	lo, hi := c.NoiseMin, c.NoiseMax
	if v, ok := envFloat(getenv, "NOISE_MIN"); ok {
		lo = v / 100
	}
	if v, ok := envFloat(getenv, "NOISE_MAX"); ok {
		hi = v / 100
	}
	if lo < hi {
		c.NoiseMin, c.NoiseMax = lo, hi
	}
	if v := getenv("LAZY_EXPAND"); v == "false" || v == "0" {
		c.LazyExpand = false
	}
	return c
}

func chaseLimitFromEnv(getenv func(string) string) float64 {
	if v, ok := envFloat(getenv, "CHASE_LIMIT_PCT"); ok {
		return v / 100
	}
	return 0.01
}

func pollSymbols(active []string, held string) []string {
	if held == "" {
		return active
	}
	for _, s := range active {
		if s == held {
			return active
		}
	}
	return append([]string{held}, active...)
}

func setupsFor(setupOf func(string) (simulator.Setup, bool), symbols []string, held string) map[string]simulator.Setup {
	setups := make(map[string]simulator.Setup, len(symbols))
	for _, s := range symbols {
		if setup, ok := setupOf(s); ok {
			setups[s] = setup
		}
	}
	if _, ok := setups[held]; held != "" && !ok {
		setups[held] = simulator.Setup{Symbol: held}
	}
	return setups
}

type priceSource interface {
	Price(ctx context.Context, symbol string) (*tossapi.Price, error)
}

// 보유 종목은 손절 감시가 걸려 있어 한 틱이라도 못 보면 안 되므로, 실패 시 짧게 재시도합니다.
const heldRetries = 2

var heldRetryDelay = 300 * time.Millisecond

func pollPrices(ctx context.Context, src priceSource, symbols []string, held string) []tradingloop.PriceObservation {
	observations := make([]tradingloop.PriceObservation, 0, len(symbols))
	for _, symbol := range symbols {
		obs := fetchObservation(ctx, src, symbol)
		for attempt := 0; symbol == held && obs.Err != nil && attempt < heldRetries; attempt++ {
			select {
			case <-ctx.Done():
				attempt = heldRetries
				continue
			case <-time.After(heldRetryDelay):
			}
			obs = fetchObservation(ctx, src, symbol)
		}
		observations = append(observations, obs)
	}
	return observations
}

func fetchObservation(ctx context.Context, src priceSource, symbol string) tradingloop.PriceObservation {
	price, err := src.Price(ctx, symbol)
	if err != nil {
		return tradingloop.PriceObservation{Symbol: symbol, Err: err}
	}
	last, err := strconv.ParseFloat(price.LastPrice, 64)
	if err != nil {
		return tradingloop.PriceObservation{Symbol: symbol, Err: err}
	}
	ts, err := time.Parse(time.RFC3339, price.Timestamp)
	if err != nil {
		return tradingloop.PriceObservation{Symbol: symbol, Err: err}
	}
	return tradingloop.PriceObservation{Symbol: symbol, Price: last, Timestamp: ts}
}

func restoreEvals(ctx context.Context, store *session.Store, ev *screener.Evaluator, date, market string) (restored, skipped int, err error) {
	blobs, err := store.LoadEvals(ctx, date, market)
	if err != nil {
		return 0, 0, err
	}
	entries := make([]screener.Entry, 0, len(blobs))
	for _, blob := range blobs {
		var e screener.Entry
		if err := json.Unmarshal(blob, &e); err != nil || e.Symbol == "" {
			skipped++
			continue
		}
		entries = append(entries, e)
	}
	ev.Seed(entries)
	return len(entries), skipped, nil
}

func evalSaver(ctx context.Context, store *session.Store, date, market string) func(screener.Entry) {
	return func(e screener.Entry) {
		data, err := json.Marshal(e)
		if err == nil {
			err = store.SaveEval(ctx, date, market, e.Symbol, data)
		}
		if err != nil {
			log.Printf("%s 평가 결과 저장 실패(재시작 시 다시 평가합니다): %v", e.Symbol, err)
		}
	}
}

func buildScreenerStatus(active []string, rank func(string) int, entries []screener.Entry, maxRejections int) *snapshot.ScreenerStatus {
	bysymbol := make(map[string]screener.Entry, len(entries))
	for _, e := range entries {
		bysymbol[e.Symbol] = e
	}
	st := &snapshot.ScreenerStatus{}
	for _, s := range active {
		e := bysymbol[s]
		st.Active = append(st.Active, snapshot.ScreenerActive{Symbol: s, Rank: rank(s), Target: e.Target, Origin: e.Origin})
	}
	for _, e := range entries {
		if e.Passed || len(st.Rejections) >= maxRejections {
			continue
		}
		st.Rejections = append(st.Rejections, snapshot.ScreenerRejection{Symbol: e.Symbol, Reason: e.Reason})
	}
	return st
}

func describeUpdate(up screener.Update, rank func(string) int) []string {
	if !up.Ran {
		return nil
	}
	var lines []string
	if len(up.Added) > 0 {
		parts := make([]string, len(up.Added))
		for i, s := range up.Added {
			parts[i] = fmt.Sprintf("%s(순위 %d)", s, rank(s))
		}
		lines = append(lines, "활성 종목 편입: "+strings.Join(parts, ", "))
	}
	if len(up.Removed) > 0 {
		lines = append(lines, "활성 종목 제외: "+strings.Join(up.Removed, ", "))
	}
	if up.Skipped > 0 {
		lines = append(lines, fmt.Sprintf("분당 평가 예산 때문에 %d개 종목 평가를 다음 갱신으로 미뤘습니다", up.Skipped))
	}
	if up.UsedFallback {
		lines = append(lines, "당일 랭킹을 한 번도 받지 못해 사전 평가 통과 종목을 활성 종목으로 대신 사용합니다")
	}
	for _, err := range up.Errs {
		lines = append(lines, fmt.Sprintf("스크리너 갱신 중 오류(직전 활성 집합 유지): %v", err))
	}
	return lines
}
