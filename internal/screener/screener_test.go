package screener

import (
	"math"
	"testing"

	"github.com/tibetkowon/toss-trader/internal/strategy"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

func TestBarsFromCandlesSkipsBadBars(t *testing.T) {
	candles := []tossapi.Candle{
		{Timestamp: "2026-09-30T00:00:00.000+09:00", OpenPrice: "100", HighPrice: "104", LowPrice: "100", ClosePrice: "102"},
		{Timestamp: "2026-09-29", OpenPrice: "x", HighPrice: "104", LowPrice: "100", ClosePrice: "102"},
		{Timestamp: "short", OpenPrice: "100", HighPrice: "104", LowPrice: "100", ClosePrice: "102"},
		{Timestamp: "2026-09-26T00:00:00.000+09:00", OpenPrice: "0", HighPrice: "104", LowPrice: "100", ClosePrice: "102"},
		{Timestamp: "2026-09-25T00:00:00.000+09:00", OpenPrice: "90", HighPrice: "95", LowPrice: "89", ClosePrice: "94"},
	}
	got := BarsFromCandles(candles)
	if len(got) != 2 || got[0].Date != "2026-09-30" || got[1].Date != "2026-09-25" {
		t.Fatalf("변환 결과: %+v", got)
	}
	if got[1].Open != 90 || got[1].High != 95 || got[1].Low != 89 || got[1].Close != 94 {
		t.Errorf("값 불일치: %+v", got[1])
	}
}

func TestNoiseRatio(t *testing.T) {
	prior := []strategy.DailyBar{
		{Open: 100, High: 104, Low: 100}, // 4%
		{Open: 200, High: 208, Low: 200}, // 4%
		{Open: 100, High: 110, Low: 100}, // 10% — 윈도 밖
	}
	got, ok := NoiseRatio(prior, 2)
	if !ok || math.Abs(got-0.04) > 1e-9 {
		t.Fatalf("NoiseRatio = %v, %v", got, ok)
	}
	if _, ok := NoiseRatio(prior, 4); ok {
		t.Error("봉이 부족한데 ok=true")
	}
	if _, ok := NoiseRatio(prior, 0); ok {
		t.Error("window 0을 허용했습니다")
	}
}

func TestEntrySetup(t *testing.T) {
	e := Entry{Symbol: "A", Passed: true, HasOpen: true, Target: 105, TrendOK: true}
	s := e.Setup()
	if s.Symbol != "A" || s.TargetPrice != 105 || !s.TrendOK {
		t.Fatalf("Setup = %+v", s)
	}
}

func TestDefaultConfigEnablesHysteresisAtRankDepth(t *testing.T) {
	c := DefaultConfig("KR")
	if c.KeepRank != c.RankDepth {
		t.Fatalf("KeepRank=%d, RankDepth=%d: 기본값은 랭킹 목록 전체에서 활성 종목을 유지해야 합니다", c.KeepRank, c.RankDepth)
	}
}
