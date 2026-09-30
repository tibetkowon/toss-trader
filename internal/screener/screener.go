// Package screener는 매일 활성 감시 종목을 고릅니다(설계: docs/superpowers/specs/
// 2026-09-30-daily-screener-design.md). 종목 평가는 어제까지 확정된 데이터로만 하고,
// 활성 집합은 실시간 당일 거래대금 랭킹으로 정합니다.
package screener

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/tibetkowon/toss-trader/internal/simulator"
	"github.com/tibetkowon/toss-trader/internal/strategy"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

const dateLayout = "2006-01-02"

// Source는 스크리너가 쓰는 API 표면입니다. *tossapi.Client가 그대로 만족합니다.
type Source interface {
	Rankings(ctx context.Context, rankingType, duration, marketCountry string) ([]tossapi.RankingItem, error)
	Stocks(ctx context.Context, symbols ...string) ([]tossapi.Stock, error)
	StockWarnings(ctx context.Context, symbol string) ([]json.RawMessage, error)
	Candles(ctx context.Context, symbol, interval string, count int, before string) ([]tossapi.Candle, string, error)
}

var _ Source = (*tossapi.Client)(nil)

// Config의 0값 의미: EvalPerMin 0 = 제한 없음, StartDelay 0 = 지연 없음,
// KeepRank 0 = 히스테리시스 끔, MinAffordable 0 = 규칙 끔.
type Config struct {
	Market         string
	NoiseMin       float64 // 비율(0.025 = 2.5%)
	NoiseMax       float64
	NoiseWindow    int
	MAWindow       int
	K              float64
	RankDepth      int
	ActiveCount    int
	MinAffordable  int
	EvalPerMin     int
	StartDelay     time.Duration
	RefreshEvery   time.Duration
	KeepRank       int
	LazyExpand     bool // false면 첫 갱신의 랭킹 상위 종목으로 후보를 고정
	Seed           float64
	CommissionRate float64
}

func DefaultConfig(market string) Config {
	return Config{
		Market: market, NoiseMin: 0.025, NoiseMax: 0.06, NoiseWindow: 20, MAWindow: 5, K: 0.5,
		RankDepth: 30, ActiveCount: 10, MinAffordable: 2, EvalPerMin: 3,
		StartDelay: 5 * time.Minute, RefreshEvery: time.Minute, KeepRank: 0, LazyExpand: true,
	}
}

// Entry는 종목 하나의 하루치 평가 결과입니다(탈락 포함). JSON으로 저장·복구됩니다.

type Entry struct {
	Symbol     string            `json:"symbol"`
	Origin     string            `json:"origin"` // "prewarm" | "intraday"
	Passed     bool              `json:"passed"` // 1~3단계 통과
	Reason     string            `json:"reason,omitempty"`
	NoiseRatio float64           `json:"noise_ratio"`
	Prev       strategy.DailyBar `json:"prev"`
	Closes     []float64         `json:"closes,omitempty"` // 어제까지 MAWindow개, 최신순
	HasOpen    bool              `json:"has_open"`
	Open       float64           `json:"open"`
	Target     float64           `json:"target"`
	TrendOK    bool              `json:"trend_ok"`
}

// Setup은 시뮬레이터용 Setup입니다. HasOpen이 아니면 목표가가 0이므로 호출자가 걸러야 합니다.
func (e Entry) Setup() simulator.Setup {
	return simulator.Setup{Symbol: e.Symbol, TargetPrice: e.Target, TrendOK: e.TrendOK}
}

// BarsFromCandles는 일봉 캔들을 DailyBar로 바꿉니다(입력 순서 유지).
// 날짜나 가격을 해석할 수 없거나 가격이 0 이하인 봉은 버립니다.
func BarsFromCandles(candles []tossapi.Candle) []strategy.DailyBar {
	bars := make([]strategy.DailyBar, 0, len(candles))
	for _, c := range candles {
		if len(c.Timestamp) < len(dateLayout) {
			continue
		}
		open, e1 := strconv.ParseFloat(c.OpenPrice, 64)
		high, e2 := strconv.ParseFloat(c.HighPrice, 64)
		low, e3 := strconv.ParseFloat(c.LowPrice, 64)
		closePrice, e4 := strconv.ParseFloat(c.ClosePrice, 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || open <= 0 || high <= 0 || low <= 0 || closePrice <= 0 {
			continue
		}
		bars = append(bars, strategy.DailyBar{Date: c.Timestamp[:len(dateLayout)], Open: open, High: high, Low: low, Close: closePrice})
	}
	return bars
}

// NoiseRatio는 최신순 prior의 앞 window개에 대한 평균 (고가-저가)/시가입니다.
func NoiseRatio(prior []strategy.DailyBar, window int) (float64, bool) {
	if window <= 0 || len(prior) < window {
		return 0, false
	}
	sum := 0.0
	for _, b := range prior[:window] {
		sum += (b.High - b.Low) / b.Open
	}
	return sum / float64(window), true
}
