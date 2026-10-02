package simulator

import (
	"math"
	"testing"
)

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

func testConfig() Config {
	return Config{StopLossPct: 0.02, DailyLossLimitPct: 0.05, CommissionRate: 0.001}
}

func TestBuyOnBreakout(t *testing.T) {
	sim := New(testConfig(), 100000)
	setup := Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}

	action := sim.OnTick(setup, 1000, false)

	if action.Type != Bought || action.Shares != 99 {
		t.Fatalf("매수 액션 불일치: %+v", action)
	}
	wantCost := 99*1000.0 + 99*1000.0*0.001
	if !approxEqual(-action.Proceeds, wantCost) {
		t.Fatalf("매수 비용 불일치: got %v want %v", -action.Proceeds, wantCost)
	}
	if !approxEqual(sim.Cash(), 100000-wantCost) {
		t.Fatalf("잔여 현금 불일치: %v", sim.Cash())
	}
	pos, ok := sim.Position()
	if !ok || pos.Symbol != "A" || pos.Shares != 99 || pos.EntryPrice != 1000 {
		t.Fatalf("포지션 불일치: %+v ok=%v", pos, ok)
	}
}

func TestNoBuyIfTrendFails(t *testing.T) {
	sim := New(testConfig(), 100000)
	setup := Setup{Symbol: "A", TargetPrice: 1000, TrendOK: false}

	action := sim.OnTick(setup, 1500, false)

	if action.Type != NoAction {
		t.Fatalf("추세 필터 실패에도 매수했습니다: %+v", action)
	}
	if _, ok := sim.Position(); ok {
		t.Fatal("포지션이 생겼습니다")
	}
}

func TestNoBuyIfBelowTarget(t *testing.T) {
	sim := New(testConfig(), 100000)
	setup := Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}

	action := sim.OnTick(setup, 999.99, false)

	if action.Type != NoAction {
		t.Fatalf("목표가 미달인데도 매수했습니다: %+v", action)
	}
}

func TestNoBuyIfAlreadyHoldingDifferentSymbol(t *testing.T) {
	sim := New(testConfig(), 100000)
	sim.OnTick(Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}, 1000, false)

	action := sim.OnTick(Setup{Symbol: "B", TargetPrice: 500, TrendOK: true}, 600, false)

	if action.Type != NoAction {
		t.Fatalf("이미 보유 중인데 다른 종목을 매수했습니다: %+v", action)
	}
	pos, _ := sim.Position()
	if pos.Symbol != "A" {
		t.Fatalf("보유 종목이 바뀌었습니다: %+v", pos)
	}
}

func TestStopLossTriggersAndBansReentrySameDay(t *testing.T) {
	sim := New(testConfig(), 100000)
	setup := Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}
	sim.OnTick(setup, 1000, false) // entry at 1000, 99 shares

	action := sim.OnTick(setup, 980, false) // exactly -2%: boundary triggers

	if action.Type != StoppedOut {
		t.Fatalf("손절이 발동하지 않았습니다: %+v", action)
	}
	if action.PnL >= 0 {
		t.Fatalf("손절인데 손실이 아닙니다: %v", action.PnL)
	}
	if sim.ConsecutiveLosses() != 1 {
		t.Fatalf("연속 손실 카운트 불일치: %d", sim.ConsecutiveLosses())
	}
	if _, ok := sim.Position(); ok {
		t.Fatal("손절 후에도 포지션이 남아있습니다")
	}

	// 같은 날 같은 종목 재진입 금지 (SPEC.md 3.1).
	reentry := sim.OnTick(setup, 1500, false)
	if reentry.Type != NoAction {
		t.Fatalf("당일 재진입 금지를 위반했습니다: %+v", reentry)
	}
}

func TestEndOfDayCloseWithProfitDoesNotBanReentry(t *testing.T) {
	sim := New(testConfig(), 100000)
	setup := Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}
	sim.OnTick(setup, 1000, false)

	action := sim.OnTick(setup, 1050, true)

	if action.Type != ClosedEndOfDay {
		t.Fatalf("마감 강제청산이 아닙니다: %+v", action)
	}
	if action.PnL <= 0 {
		t.Fatalf("수익 청산인데 손실로 계산되었습니다: %v", action.PnL)
	}
	if sim.ConsecutiveLosses() != 0 {
		t.Fatalf("수익 청산인데 연속 손실이 증가했습니다: %d", sim.ConsecutiveLosses())
	}
}

func TestStopLossPriorityOverEndOfDaySameTick(t *testing.T) {
	sim := New(testConfig(), 100000)
	setup := Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}
	sim.OnTick(setup, 1000, false)

	// 손절가 이하 + 마감 시각이 동시에 참인 틱: 손절이 우선해야 함 (SPEC.md 6.1).
	action := sim.OnTick(setup, 970, true)

	if action.Type != StoppedOut {
		t.Fatalf("마감 강제청산이 손절보다 우선 처리되었습니다: %+v", action)
	}
}

func TestZeroSharesSkipWhenPriceExceedsCash(t *testing.T) {
	sim := New(testConfig(), 500)
	setup := Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}

	action := sim.OnTick(setup, 1000, false)

	if action.Type != SkippedZeroShares {
		t.Fatalf("0주 스킵이 아닙니다: %+v", action)
	}
	if sim.Cash() != 500 {
		t.Fatalf("스킵인데 현금이 변경되었습니다: %v", sim.Cash())
	}
	if _, ok := sim.Position(); ok {
		t.Fatal("스킵인데 포지션이 생겼습니다")
	}
}

func TestConsecutiveLossesHaltsNewEntry(t *testing.T) {
	sim := New(testConfig(), 100000)
	a := Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}
	b := Setup{Symbol: "B", TargetPrice: 1000, TrendOK: true}
	c := Setup{Symbol: "C", TargetPrice: 1000, TrendOK: true}

	sim.OnTick(a, 1000, false)
	if sim.OnTick(a, 980, false).Type != StoppedOut {
		t.Fatal("A 손절 실패")
	}
	sim.OnTick(b, 1000, false)
	if sim.OnTick(b, 980, false).Type != StoppedOut {
		t.Fatal("B 손절 실패")
	}
	if sim.ConsecutiveLosses() != 2 {
		t.Fatalf("연속 손실 카운트 불일치: %d", sim.ConsecutiveLosses())
	}

	action := sim.OnTick(c, 1500, false)
	if action.Type != NoAction {
		t.Fatalf("연속 손실 2회 후에도 신규 매수가 허용되었습니다: %+v", action)
	}
}

func TestConsecutiveLossResetsAfterProfitableClose(t *testing.T) {
	sim := New(testConfig(), 100000)
	a := Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}
	b := Setup{Symbol: "B", TargetPrice: 1000, TrendOK: true}
	c := Setup{Symbol: "C", TargetPrice: 1000, TrendOK: true}

	sim.OnTick(a, 1000, false)
	sim.OnTick(a, 980, false) // loss #1
	sim.OnTick(b, 1000, false)
	sim.OnTick(b, 1100, true) // profitable EOD close resets the streak

	if sim.ConsecutiveLosses() != 0 {
		t.Fatalf("연속 손실이 리셋되지 않았습니다: %d", sim.ConsecutiveLosses())
	}

	action := sim.OnTick(c, 1500, false)
	if action.Type != Bought {
		t.Fatalf("리셋 후 신규 매수가 차단되었습니다: %+v", action)
	}
}

func TestDailyLossLimitHaltsNewEntry(t *testing.T) {
	sim := New(testConfig(), 100000)
	a := Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}
	b := Setup{Symbol: "B", TargetPrice: 1000, TrendOK: true}

	sim.OnTick(a, 1000, false)
	// 큰 갭 하락으로 -2% 손절 폭을 훨씬 초과하는 단일 손실을 시뮬레이션합니다.
	closeAction := sim.OnTick(a, 500, false)
	if closeAction.Type != StoppedOut {
		t.Fatalf("손절 실패: %+v", closeAction)
	}
	if closeAction.PnL >= -5000 { // 시드(100000)의 5%보다 훨씬 큰 손실이어야 함
		t.Fatalf("테스트 전제가 깨졌습니다(손실이 충분히 크지 않음): %v", closeAction.PnL)
	}
	if sim.ConsecutiveLosses() != 1 {
		t.Fatalf("연속 손실 카운트: %d (2회가 아니라 일일 한도로 차단되는지 확인하는 테스트)", sim.ConsecutiveLosses())
	}

	action := sim.OnTick(b, 1500, false)
	if action.Type != NoAction {
		t.Fatalf("일일 손실 한도 초과 후에도 신규 매수가 허용되었습니다: %+v", action)
	}
}

func TestCommissionAppliedOnBuyAndSell(t *testing.T) {
	sim := New(testConfig(), 100000)
	setup := Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}

	buy := sim.OnTick(setup, 1000, false)
	sell := sim.OnTick(setup, 1000, true) // 시가 변동 없이 청산 — 수수료만큼만 손실이어야 함

	wantLoss := -(float64(buy.Shares) * 1000 * 0.001 * 2) // 매수+매도 양쪽 수수료
	if !approxEqual(sell.PnL, wantLoss) {
		t.Fatalf("수수료 반영 손익 불일치: got %v want %v", sell.PnL, wantLoss)
	}
}

func TestDailyPnLReflectsUnrealized(t *testing.T) {
	sim := New(testConfig(), 100000)
	setup := Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}
	sim.OnTick(setup, 1000, false) // 99 shares @ 1000

	dailyPnL := sim.DailyPnL(1010) // 아직 보유 중, 현재가 1010으로 평가

	wantUnrealized := 99 * (1010 - 1000.0)
	if !approxEqual(dailyPnL, wantUnrealized) {
		t.Fatalf("평가손익 반영 불일치: got %v want %v", dailyPnL, wantUnrealized)
	}
}

func TestStateRoundTrip(t *testing.T) {
	cfg := testConfig()
	sim := New(cfg, 100000)
	a := Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}
	b := Setup{Symbol: "B", TargetPrice: 1000, TrendOK: true}
	sim.OnTick(a, 1000, false)
	sim.OnTick(a, 980, false) // stop-loss -> A banned for re-entry, consecutiveLosses=1
	sim.OnTick(b, 1000, false)

	state := sim.State()
	restored := Restore(cfg, state)

	pos, ok := restored.Position()
	wantPos, wantOK := sim.Position()
	if ok != wantOK || pos != wantPos {
		t.Fatalf("포지션 복구 불일치: got %+v ok=%v want %+v ok=%v", pos, ok, wantPos, wantOK)
	}
	if !approxEqual(restored.Cash(), sim.Cash()) {
		t.Fatalf("현금 복구 불일치: got %v want %v", restored.Cash(), sim.Cash())
	}
	if !approxEqual(restored.RealizedPnLToday(), sim.RealizedPnLToday()) {
		t.Fatalf("실현손익 복구 불일치: got %v want %v", restored.RealizedPnLToday(), sim.RealizedPnLToday())
	}
	if restored.ConsecutiveLosses() != sim.ConsecutiveLosses() {
		t.Fatalf("연속손실 복구 불일치: got %d want %d", restored.ConsecutiveLosses(), sim.ConsecutiveLosses())
	}

	// 복구 후에도 A는 여전히 당일 재진입 금지여야 합니다.
	action := restored.OnTick(a, 1500, false)
	if action.Type != NoAction {
		t.Fatalf("복구 후 재진입 금지가 유지되지 않았습니다: %+v", action)
	}
}

func TestActionTypeString(t *testing.T) {
	cases := map[ActionType]string{
		NoAction:          "NoAction",
		Bought:            "Bought",
		StoppedOut:        "StoppedOut",
		ClosedEndOfDay:    "ClosedEndOfDay",
		SkippedZeroShares: "SkippedZeroShares",
		ActionType(99):    "Unknown",
	}
	for actionType, want := range cases {
		if got := actionType.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", actionType, got, want)
		}
	}
}

func TestFractionalBuyUsesAllCashDownToSixDecimals(t *testing.T) {
	cfg := testConfig()
	cfg.Fractional = true
	sim := New(cfg, 73.94)
	action := sim.OnTick(Setup{Symbol: "A", TargetPrice: 100, TrendOK: true}, 150, false)
	if action.Type != Bought {
		t.Fatalf("소수점 모드에서는 1주 미만도 매수해야 합니다: %+v", action)
	}
	want := math.Floor(73.94/(150*(1+cfg.CommissionRate))*1e6) / 1e6
	if action.Shares != want || action.Shares >= 1 {
		t.Fatalf("shares = %v, want %v", action.Shares, want)
	}
	if sim.Cash() < 0 || sim.Cash() > 0.01 {
		t.Fatalf("현금이 거의 남지 않아야 합니다: %v", sim.Cash())
	}
}

func TestIntegerModeStillSkipsWhenOneShareUnaffordable(t *testing.T) {
	sim := New(testConfig(), 73.94)
	if a := sim.OnTick(Setup{Symbol: "A", TargetPrice: 100, TrendOK: true}, 150, false); a.Type != SkippedZeroShares {
		t.Fatalf("정수 모드는 스킵해야 합니다: %+v", a)
	}
}
