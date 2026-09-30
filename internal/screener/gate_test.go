package screener

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

// gateFixture는 모두 평가를 통과하는 종목들과, 시가 110(목표가 112)이 확정 가능한 1분봉을 만듭니다.
func gateFixture(symbols ...string) *fakeSource {
	f := passingSource(symbols...)
	for _, s := range symbols {
		f.minute[s] = []tossapi.Candle{minuteCandle(testSessionStart(), "110")}
	}
	return f
}

func setRanking(f *fakeSource, symbols ...string) {
	f.rankings = nil
	for i, s := range symbols {
		f.rankings = append(f.rankings, rankingItem(i+1, s, "50"))
	}
}

func newTestGate(f *fakeSource, mutate func(*Config)) *Gate {
	cfg := testConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	return NewGate(cfg, f, NewEvaluator(f, cfg, testSessionStart()), testSessionStart())
}

var afterDelay = testSessionStart().Add(6 * time.Minute)

func TestRefreshWaitsForStartDelay(t *testing.T) {
	f := gateFixture("A", "B")
	setRanking(f, "A", "B")
	g := newTestGate(f, nil)
	if up := g.Refresh(context.Background(), testSessionStart().Add(4*time.Minute)); up.Ran {
		t.Fatal("지연 전에 갱신했습니다")
	}
	if f.calls["Rankings"] != 0 || len(g.Active()) != 0 {
		t.Fatalf("지연 전 호출/활성: %d %v", f.calls["Rankings"], g.Active())
	}
}

func TestRefreshSelectsTopActiveAndSetup(t *testing.T) {
	f := gateFixture("A", "B", "C")
	setRanking(f, "A", "B", "C")
	g := newTestGate(f, nil)
	up := g.Refresh(context.Background(), afterDelay)
	if !up.Ran || !reflect.DeepEqual(up.Added, []string{"A", "B"}) || len(up.Removed) != 0 {
		t.Fatalf("Update = %+v", up)
	}
	if !reflect.DeepEqual(g.Active(), []string{"A", "B"}) || g.Rank("A") != 1 || g.Rank("B") != 2 {
		t.Fatalf("active=%v ranks=%d,%d", g.Active(), g.Rank("A"), g.Rank("B"))
	}
	setup, ok := g.Setup("A")
	if !ok || setup.TargetPrice != 112 || !setup.TrendOK {
		t.Errorf("Setup = %+v %v", setup, ok)
	}
	if _, ok := g.Setup("ZZZ"); ok {
		t.Error("모르는 종목의 Setup이 있습니다")
	}
}

func TestRefreshIntervalAndRankChange(t *testing.T) {
	f := gateFixture("A", "B", "C")
	setRanking(f, "A", "B", "C")
	g := newTestGate(f, nil)
	g.Refresh(context.Background(), afterDelay)
	setRanking(f, "C", "A", "B")
	if up := g.Refresh(context.Background(), afterDelay.Add(30*time.Second)); up.Ran {
		t.Fatal("RefreshEvery 안에 다시 갱신했습니다")
	}
	up := g.Refresh(context.Background(), afterDelay.Add(61*time.Second))
	if !up.Ran || !reflect.DeepEqual(up.Added, []string{"C"}) || !reflect.DeepEqual(up.Removed, []string{"B"}) {
		t.Fatalf("Update = %+v", up)
	}
	if !reflect.DeepEqual(g.Active(), []string{"C", "A"}) {
		t.Errorf("active = %v", g.Active())
	}
}

func TestRefreshSkipsRejectedSymbols(t *testing.T) {
	f := gateFixture("B", "C")
	f.addStock("LEV")
	f.stocks["LEV"] = tossapi.Stock{Symbol: "LEV", Raw: []byte(`{"leverageFactor":"3"}`)}
	setRanking(f, "LEV", "B", "C")
	g := newTestGate(f, nil)
	g.Refresh(context.Background(), afterDelay)
	if !reflect.DeepEqual(g.Active(), []string{"B", "C"}) {
		t.Fatalf("active = %v", g.Active())
	}
	if e, _ := g.Evaluator().Cached("LEV"); e.Passed || e.Reason == "" {
		t.Errorf("탈락 사유가 기록되지 않았습니다: %+v", e)
	}
}

func TestRefreshRankingFailureKeepsPrevious(t *testing.T) {
	f := gateFixture("A", "B")
	setRanking(f, "A", "B")
	g := newTestGate(f, nil)
	g.Refresh(context.Background(), afterDelay)

	f.rankingErr = errors.New("503")
	up := g.Refresh(context.Background(), afterDelay.Add(2*time.Minute))
	if !up.Ran || len(up.Errs) == 0 || up.UsedFallback {
		t.Fatalf("Update = %+v", up)
	}
	if !reflect.DeepEqual(g.Active(), []string{"A", "B"}) {
		t.Errorf("실패 시 직전 집합을 유지해야 합니다: %v", g.Active())
	}

	f.rankingErr = nil
	f.rankings = nil // 빈 응답도 실패로 취급
	up = g.Refresh(context.Background(), afterDelay.Add(4*time.Minute))
	if len(up.Errs) == 0 || !reflect.DeepEqual(g.Active(), []string{"A", "B"}) {
		t.Errorf("빈 랭킹: %+v %v", up, g.Active())
	}
}

func TestRefreshFallsBackToPrewarmedWhenNeverRanked(t *testing.T) {
	f := gateFixture("A", "B", "C")
	setRanking(f, "A", "B", "C") // 전일 랭킹
	g := newTestGate(f, nil)
	res := g.PreWarm(context.Background(), time.Now().Add(time.Minute))
	if res.Err != nil || res.Passed != 3 || res.Evaluated != 3 || res.TimedOut || !reflect.DeepEqual(res.Top, []string{"A", "B", "C"}) {
		t.Fatalf("PreWarm = %+v", res)
	}
	f.rankingErr = errors.New("503")
	up := g.Refresh(context.Background(), afterDelay)
	if !up.UsedFallback || !reflect.DeepEqual(g.Active(), []string{"A", "B"}) {
		t.Fatalf("대체 집합: %+v %v", up, g.Active())
	}
}

func TestPreWarmDeadlineAndFailure(t *testing.T) {
	f := gateFixture("A")
	setRanking(f, "A")
	g := newTestGate(f, nil)
	res := g.PreWarm(context.Background(), time.Now().Add(-time.Second))
	if !res.TimedOut || res.Evaluated != 0 || f.calls["Stocks"] != 0 {
		t.Errorf("마감 초과 시 평가하지 않아야 합니다: %+v", res)
	}
	f.rankingErr = errors.New("503")
	if res := g.PreWarm(context.Background(), time.Now().Add(time.Minute)); res.Err == nil {
		t.Error("랭킹 오류가 전달되지 않았습니다")
	}
}

func TestRefreshBudgetSpreadsEvaluationsAcrossMinutes(t *testing.T) {
	f := gateFixture("A", "B", "C")
	setRanking(f, "A", "B", "C")
	g := newTestGate(f, func(c *Config) { c.EvalPerMin = 1; c.ActiveCount = 3 })
	up := g.Refresh(context.Background(), afterDelay)
	if up.Skipped != 2 || !reflect.DeepEqual(g.Active(), []string{"A"}) {
		t.Fatalf("첫 갱신: %+v %v", up, g.Active())
	}
	g.Refresh(context.Background(), afterDelay.Add(61*time.Second))
	g.Refresh(context.Background(), afterDelay.Add(122*time.Second))
	if !reflect.DeepEqual(g.Active(), []string{"A", "B", "C"}) {
		t.Errorf("세 번째 갱신 후 active = %v", g.Active())
	}
}

func TestRefreshLazyExpandOffFreezesCandidates(t *testing.T) {
	f := gateFixture("A", "B", "C")
	setRanking(f, "A", "B")
	g := newTestGate(f, func(c *Config) { c.LazyExpand = false })
	g.Refresh(context.Background(), afterDelay)
	setRanking(f, "C", "A", "B") // C가 장중에 떠오름
	g.Refresh(context.Background(), afterDelay.Add(2*time.Minute))
	if !reflect.DeepEqual(g.Active(), []string{"A", "B"}) {
		t.Errorf("고정 후보 밖의 C가 편입되었습니다: %v", g.Active())
	}
	if _, cached := g.Evaluator().Cached("C"); cached {
		t.Error("고정 모드에서 새 종목을 평가했습니다")
	}
}

func TestRefreshSkipsSymbolWithoutOpenYet(t *testing.T) {
	f := gateFixture("A", "B")
	f.minute["A"] = nil // 시가를 아직 못 구함
	setRanking(f, "A", "B")
	g := newTestGate(f, nil)
	g.Refresh(context.Background(), afterDelay)
	if !reflect.DeepEqual(g.Active(), []string{"B"}) {
		t.Fatalf("active = %v", g.Active())
	}
	f.minute["A"] = []tossapi.Candle{minuteCandle(testSessionStart(), "110")}
	g.Refresh(context.Background(), afterDelay.Add(61*time.Second))
	if !reflect.DeepEqual(g.Active(), []string{"A", "B"}) {
		t.Errorf("시가 확정 후 편입되지 않았습니다: %v", g.Active())
	}
}

func TestRefreshMinAffordableUsesSeed(t *testing.T) {
	f := gateFixture("A", "B", "C")
	f.rankings = []tossapi.RankingItem{rankingItem(1, "A", "900000"), rankingItem(2, "B", "800000"), rankingItem(3, "C", "5000")}
	g := newTestGate(f, func(c *Config) { c.MinAffordable = 1 })
	g.SetSeed(100000)
	g.Refresh(context.Background(), afterDelay)
	if !reflect.DeepEqual(g.Active(), []string{"A", "C"}) {
		t.Errorf("살 수 있는 종목이 최소 1개 포함되어야 합니다: %v", g.Active())
	}
}
