package tradingloop

import (
	"errors"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/simulator"
)

func testConfig() simulator.Config {
	return simulator.Config{StopLossPct: 0.02, DailyLossLimitPct: 0.05, CommissionRate: 0}
}

func TestProcessTickBuysOnQualifyingObservation(t *testing.T) {
	sim := simulator.New(testConfig(), 100000)
	setups := map[string]simulator.Setup{"A": {Symbol: "A", TargetPrice: 1000, TrendOK: true}}
	now := time.Now()
	obs := []PriceObservation{{Symbol: "A", Price: 1000, Timestamp: now}}

	actions := ProcessTick(sim, setups, obs, now, 30*time.Second)

	if len(actions) != 1 || actions[0].Type != simulator.Bought {
		t.Fatalf("매수가 발생하지 않았습니다: %+v", actions)
	}
}

func TestProcessTickSkipsStaleObservation(t *testing.T) {
	sim := simulator.New(testConfig(), 100000)
	setups := map[string]simulator.Setup{"A": {Symbol: "A", TargetPrice: 1000, TrendOK: true}}
	now := time.Now()
	obs := []PriceObservation{{Symbol: "A", Price: 1000, Timestamp: now.Add(-1 * time.Minute)}}

	actions := ProcessTick(sim, setups, obs, now, 30*time.Second)

	if len(actions) != 0 {
		t.Fatalf("오래된 시세로 매수가 발생했습니다: %+v", actions)
	}
	if _, ok := sim.Position(); ok {
		t.Fatal("오래된 시세로 포지션이 생겼습니다")
	}
}

func TestProcessTickSkipsErroredObservation(t *testing.T) {
	sim := simulator.New(testConfig(), 100000)
	setups := map[string]simulator.Setup{"A": {Symbol: "A", TargetPrice: 1000, TrendOK: true}}
	now := time.Now()
	obs := []PriceObservation{{Symbol: "A", Price: 1000, Timestamp: now, Err: errors.New("조회 실패")}}

	actions := ProcessTick(sim, setups, obs, now, 30*time.Second)

	if len(actions) != 0 {
		t.Fatalf("조회 실패한 관측이 처리되었습니다: %+v", actions)
	}
}

func TestProcessTickSkipsObservationWithoutSetup(t *testing.T) {
	sim := simulator.New(testConfig(), 100000)
	setups := map[string]simulator.Setup{} // "A"에 대한 설정 없음
	now := time.Now()
	obs := []PriceObservation{{Symbol: "A", Price: 1000, Timestamp: now}}

	actions := ProcessTick(sim, setups, obs, now, 30*time.Second)

	if len(actions) != 0 {
		t.Fatalf("설정 없는 관측이 처리되었습니다: %+v", actions)
	}
}

func TestProcessTickRespectsObservationOrder(t *testing.T) {
	sim := simulator.New(testConfig(), 100000)
	setups := map[string]simulator.Setup{
		"A": {Symbol: "A", TargetPrice: 1000, TrendOK: true},
		"B": {Symbol: "B", TargetPrice: 500, TrendOK: true},
	}
	now := time.Now()
	// B가 먼저 나열되어 있으므로 B가 매수되어야 합니다(SPEC.md 4.2의 우선순위는
	// 호출자가 관측 목록 순서로 이미 반영해서 넘긴다는 계약).
	obs := []PriceObservation{
		{Symbol: "B", Price: 500, Timestamp: now},
		{Symbol: "A", Price: 1000, Timestamp: now},
	}

	actions := ProcessTick(sim, setups, obs, now, 30*time.Second)

	if len(actions) != 1 || actions[0].Symbol != "B" {
		t.Fatalf("목록 순서상 우선순위가 지켜지지 않았습니다: %+v", actions)
	}
}

func TestProcessTickStopLossOnHeldPosition(t *testing.T) {
	sim := simulator.New(testConfig(), 100000)
	setups := map[string]simulator.Setup{"A": {Symbol: "A", TargetPrice: 1000, TrendOK: true}}
	now := time.Now()
	ProcessTick(sim, setups, []PriceObservation{{Symbol: "A", Price: 1000, Timestamp: now}}, now, 30*time.Second)

	actions := ProcessTick(sim, setups, []PriceObservation{{Symbol: "A", Price: 980, Timestamp: now}}, now, 30*time.Second)

	if len(actions) != 1 || actions[0].Type != simulator.StoppedOut {
		t.Fatalf("손절이 처리되지 않았습니다: %+v", actions)
	}
}
