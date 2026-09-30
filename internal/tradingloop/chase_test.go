package tradingloop

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/simulator"
)

var chaseNow = time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)

func chaseObs(symbol string, price float64) PriceObservation {
	return PriceObservation{Symbol: symbol, Price: price, Timestamp: chaseNow}
}

func chaseSetups() map[string]simulator.Setup {
	return map[string]simulator.Setup{
		"A":  {Symbol: "A", TargetPrice: 100, TrendOK: true},
		"B":  {Symbol: "B", TargetPrice: 200, TrendOK: true},
		"NT": {Symbol: "NT", TargetPrice: 100, TrendOK: false},
	}
}

func symbols(obs []PriceObservation) []string {
	var out []string
	for _, o := range obs {
		out = append(out, o.Symbol)
	}
	return out
}

func TestChaseGuardBlocksSymbolFarAboveTarget(t *testing.T) {
	g := NewChaseGuard(0.01)
	kept, blocked := g.Filter([]PriceObservation{chaseObs("A", 102), chaseObs("B", 200)}, chaseSetups(), false, chaseNow, 30*time.Second)
	if !reflect.DeepEqual(symbols(kept), []string{"B"}) || !reflect.DeepEqual(blocked, []string{"A"}) || !g.Blocked("A") {
		t.Fatalf("kept=%v blocked=%v", symbols(kept), blocked)
	}
}

func TestChaseGuardWithinLimitPasses(t *testing.T) {
	g := NewChaseGuard(0.01)
	kept, blocked := g.Filter([]PriceObservation{chaseObs("A", 100.9)}, chaseSetups(), false, chaseNow, 30*time.Second)
	if len(kept) != 1 || len(blocked) != 0 {
		t.Fatalf("kept=%v blocked=%v", symbols(kept), blocked)
	}
}

func TestChaseGuardBlockIsStickyAndReportedOnce(t *testing.T) {
	g := NewChaseGuard(0.01)
	g.Filter([]PriceObservation{chaseObs("A", 105)}, chaseSetups(), false, chaseNow, 30*time.Second)
	kept, blocked := g.Filter([]PriceObservation{chaseObs("A", 100)}, chaseSetups(), false, chaseNow, 30*time.Second)
	if len(kept) != 0 || len(blocked) != 0 {
		t.Fatalf("가격이 내려와도 차단은 유지되고 다시 보고되지 않아야 합니다: kept=%v blocked=%v", symbols(kept), blocked)
	}
}

func TestChaseGuardDisabledOrHolding(t *testing.T) {
	obs := []PriceObservation{chaseObs("A", 150)}
	for name, g := range map[string]*ChaseGuard{"off": NewChaseGuard(0), "negative": NewChaseGuard(-1)} {
		if kept, blocked := g.Filter(obs, chaseSetups(), false, chaseNow, 30*time.Second); len(kept) != 1 || len(blocked) != 0 {
			t.Errorf("%s: kept=%v blocked=%v", name, symbols(kept), blocked)
		}
	}
	g := NewChaseGuard(0.01)
	if kept, blocked := g.Filter(obs, chaseSetups(), true, chaseNow, 30*time.Second); len(kept) != 1 || len(blocked) != 0 || g.Blocked("A") {
		t.Errorf("보유 중에는 걸러내면 안 됩니다: kept=%v blocked=%v", symbols(kept), blocked)
	}
}

func TestChaseGuardIgnoresStaleErroredAndUntradable(t *testing.T) {
	g := NewChaseGuard(0.01)
	stale := PriceObservation{Symbol: "A", Price: 150, Timestamp: chaseNow.Add(-time.Minute)}
	errored := PriceObservation{Symbol: "B", Err: errors.New("503")}
	noTrend := chaseObs("NT", 150)
	unknown := chaseObs("ZZ", 150)
	kept, blocked := g.Filter([]PriceObservation{stale, errored, noTrend, unknown}, chaseSetups(), false, chaseNow, 30*time.Second)
	if len(kept) != 4 || len(blocked) != 0 {
		t.Fatalf("kept=%v blocked=%v", symbols(kept), blocked)
	}
}
