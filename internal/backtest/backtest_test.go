package backtest

import (
	"math"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/simulator"
)

func approxEqual(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func testConfig() simulator.Config {
	return simulator.Config{StopLossPct: 0.02, DailyLossLimitPct: 0.05, CommissionRate: 0}
}

func tick(symbol string, hhmm string, price float64, eod bool) IntradayTick {
	t, _ := time.Parse("15:04", hhmm)
	return IntradayTick{Symbol: symbol, Time: t, Price: price, IsEndOfDay: eod}
}

func TestCashCarriesForwardAndDailyReset(t *testing.T) {
	dates := []string{"2026-09-01", "2026-09-02"}
	setups := map[string]map[string]simulator.Setup{
		"2026-09-01": {"A": {Symbol: "A", TargetPrice: 1000, TrendOK: true}},
		"2026-09-02": {"A": {Symbol: "A", TargetPrice: 1000, TrendOK: true}},
	}
	intraday := map[string][]IntradayTick{
		"2026-09-01": {
			tick("A", "09:00", 1000, false), // buy, 100 shares
			tick("A", "09:01", 980, true),   // -2% boundary: stop-loss fires before EOD check
		},
		"2026-09-02": {
			tick("A", "09:00", 1000, false), // must NOT be blocked by yesterday's stop-out
			tick("A", "15:30", 1050, true),
		},
	}

	report := Run(testConfig(), 100000, dates, setups, intraday)

	if len(report.Days) != 2 {
		t.Fatalf("일수 불일치: %d", len(report.Days))
	}
	day1, day2 := report.Days[0], report.Days[1]
	if !approxEqual(day1.EndCash, 98000) {
		t.Fatalf("1일차 종료 현금 불일치: %v", day1.EndCash)
	}
	if !approxEqual(day2.StartCash, 98000) {
		t.Fatalf("2일차 시작 현금(전일 이월) 불일치: %v", day2.StartCash)
	}
	if len(day2.Actions) == 0 || day2.Actions[0].Type != simulator.Bought {
		t.Fatalf("전일 손절이 당일 재진입을 잘못 차단했습니다(리셋 실패): %+v", day2.Actions)
	}
	if !approxEqual(day2.EndCash, 102900) {
		t.Fatalf("2일차 종료 현금 불일치: %v", day2.EndCash)
	}
	if !approxEqual(report.FinalEquity, 102900) {
		t.Fatalf("최종 자산 불일치: %v", report.FinalEquity)
	}
}

func TestMaxDrawdownTracksRunningPeak(t *testing.T) {
	dates := []string{"2026-09-01", "2026-09-02"}
	setups := map[string]map[string]simulator.Setup{
		"2026-09-01": {"A": {Symbol: "A", TargetPrice: 1000, TrendOK: true}},
		"2026-09-02": {"A": {Symbol: "A", TargetPrice: 1000, TrendOK: true}},
	}
	intraday := map[string][]IntradayTick{
		"2026-09-01": {tick("A", "09:00", 1000, false), tick("A", "09:01", 980, true)},
		"2026-09-02": {tick("A", "09:00", 1000, false), tick("A", "15:30", 1050, true)},
	}

	report := Run(testConfig(), 100000, dates, setups, intraday)

	if !approxEqual(report.MaxDrawdownPct, 0.02) {
		t.Fatalf("최대 낙폭 불일치: got %v want 0.02", report.MaxDrawdownPct)
	}
}

func TestForceClosesOpenPositionWithoutExplicitEndOfDayTick(t *testing.T) {
	dates := []string{"2026-09-01"}
	setups := map[string]map[string]simulator.Setup{
		"2026-09-01": {"A": {Symbol: "A", TargetPrice: 1000, TrendOK: true}},
	}
	intraday := map[string][]IntradayTick{
		"2026-09-01": {
			tick("A", "09:00", 1000, false),
			tick("A", "09:05", 1010, false), // no EOD-flagged tick at all this day
		},
	}

	report := Run(testConfig(), 100000, dates, setups, intraday)

	day := report.Days[0]
	if len(day.Actions) != 2 {
		t.Fatalf("액션 개수 불일치(매수+강제청산이어야 함): %+v", day.Actions)
	}
	last := day.Actions[len(day.Actions)-1]
	if last.Type != simulator.ClosedEndOfDay {
		t.Fatalf("데이터 누락 시 안전장치로 강제청산되지 않았습니다: %+v", last)
	}
	if !approxEqual(day.EndCash, 101000) {
		t.Fatalf("강제청산 가격(마지막 관측가) 반영 불일치: %v", day.EndCash)
	}
}

func TestTickWithoutSetupIsIgnored(t *testing.T) {
	dates := []string{"2026-09-01"}
	setups := map[string]map[string]simulator.Setup{
		"2026-09-01": {}, // 이 날짜에는 아무 종목도 설정이 없음(데이터 누락 상황 가정)
	}
	intraday := map[string][]IntradayTick{
		"2026-09-01": {tick("B", "09:00", 1000, false)},
	}

	report := Run(testConfig(), 100000, dates, setups, intraday)

	if len(report.Days[0].Actions) != 0 {
		t.Fatalf("설정 없는 틱이 처리되었습니다: %+v", report.Days[0].Actions)
	}
	if !approxEqual(report.FinalEquity, 100000) {
		t.Fatalf("자산이 변하지 않아야 합니다: %v", report.FinalEquity)
	}
}
