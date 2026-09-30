package main

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/session"
	"github.com/tibetkowon/toss-trader/internal/simulator"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestScreenerConfigDefaultsAndOverrides(t *testing.T) {
	def := screener.DefaultConfig("KR")
	def.CommissionRate = 0.001
	if got := screenerConfig("KR", 0.001, envOf(nil)); !reflect.DeepEqual(got, def) {
		t.Fatalf("기본값: %+v, 기대: %+v", got, def)
	}
	got := screenerConfig("KR", 0.001, envOf(map[string]string{
		"RANK_DEPTH": "40", "ACTIVE_COUNT": "8", "EVAL_PER_MIN": "0", "ACTIVE_KEEP_RANK": "20",
		"NOISE_MIN": "3", "NOISE_MAX": "5.5", "RANK_START_DELAY_MINUTES": "10", "RANK_REFRESH_SECONDS": "30",
		"LAZY_EXPAND": "false",
	}))
	if got.RankDepth != 40 || got.ActiveCount != 8 || got.EvalPerMin != 0 || got.KeepRank != 20 ||
		math.Abs(got.NoiseMin-0.03) > 1e-12 || math.Abs(got.NoiseMax-0.055) > 1e-12 ||
		got.StartDelay != 10*time.Minute || got.RefreshEvery != 30*time.Second || got.LazyExpand {
		t.Errorf("덮어쓰기 결과: %+v", got)
	}
}

func TestScreenerConfigRejectsBadValues(t *testing.T) {
	def := screenerConfig("KR", 0, envOf(nil))
	for name, env := range map[string]map[string]string{
		"not a number":   {"RANK_DEPTH": "abc", "ACTIVE_COUNT": "x"},
		"zero active":    {"ACTIVE_COUNT": "0"},
		"negative depth": {"RANK_DEPTH": "-5"},
		"inverted noise": {"NOISE_MIN": "6", "NOISE_MAX": "2"},
		"negative delay": {"RANK_START_DELAY_MINUTES": "-1"},
		"zero refresh":   {"RANK_REFRESH_SECONDS": "0"},
	} {
		if got := screenerConfig("KR", 0, envOf(env)); !reflect.DeepEqual(got, def) {
			t.Errorf("%s: 잘못된 값이 기본값을 대체했습니다: %+v", name, got)
		}
	}
}

func TestChaseLimitFromEnv(t *testing.T) {
	for _, tc := range []struct {
		env  map[string]string
		want float64
	}{
		{nil, 0.01},
		{map[string]string{"CHASE_LIMIT_PCT": "2.5"}, 0.025},
		{map[string]string{"CHASE_LIMIT_PCT": "0"}, 0},
		{map[string]string{"CHASE_LIMIT_PCT": "-1"}, 0.01},
		{map[string]string{"CHASE_LIMIT_PCT": "abc"}, 0.01},
	} {
		if got := chaseLimitFromEnv(envOf(tc.env)); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("%v: %v, 기대: %v", tc.env, got, tc.want)
		}
	}
}

func TestPollSymbolsAlwaysIncludesHeld(t *testing.T) {
	cases := []struct {
		active []string
		held   string
		want   []string
	}{
		{[]string{"A", "B"}, "", []string{"A", "B"}},
		{[]string{"A", "B"}, "B", []string{"A", "B"}},
		{[]string{"A", "B"}, "Z", []string{"Z", "A", "B"}},
		{nil, "Z", []string{"Z"}},
		{nil, "", nil},
	}
	for _, tc := range cases {
		if got := pollSymbols(tc.active, tc.held); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("pollSymbols(%v, %q) = %v, 기대: %v", tc.active, tc.held, got, tc.want)
		}
	}
}

func TestSetupsForGivesHeldSymbolAFallback(t *testing.T) {
	known := map[string]simulator.Setup{"A": {Symbol: "A", TargetPrice: 100, TrendOK: true}}
	setupOf := func(s string) (simulator.Setup, bool) { v, ok := known[s]; return v, ok }

	got := setupsFor(setupOf, []string{"A", "B"}, "")
	if len(got) != 1 || got["A"].TargetPrice != 100 {
		t.Errorf("Setup이 없는 종목은 빠져야 합니다: %+v", got)
	}
	got = setupsFor(setupOf, []string{"Z", "A"}, "Z")
	if z, ok := got["Z"]; !ok || z.TrendOK || z.Symbol != "Z" {
		t.Errorf("보유 종목 대체 Setup: %+v %v", z, ok)
	}
}

type fakePrices map[string]*tossapi.Price

func (f fakePrices) Price(_ context.Context, symbol string) (*tossapi.Price, error) {
	p, ok := f[symbol]
	if !ok {
		return nil, errors.New("503")
	}
	return p, nil
}

func TestPollPricesKeepsOrderAndReportsErrors(t *testing.T) {
	src := fakePrices{
		"A": {LastPrice: "100", Timestamp: "2026-10-01T01:00:00Z"},
		"C": {LastPrice: "abc", Timestamp: "2026-10-01T01:00:00Z"},
		"D": {LastPrice: "100", Timestamp: "not-a-time"},
	}
	obs := pollPrices(context.Background(), src, []string{"A", "B", "C", "D"})
	if len(obs) != 4 {
		t.Fatalf("관찰값 %d개", len(obs))
	}
	if obs[0].Symbol != "A" || obs[0].Err != nil || obs[0].Price != 100 {
		t.Errorf("A: %+v", obs[0])
	}
	for i, sym := range []string{"B", "C", "D"} {
		if obs[i+1].Symbol != sym || obs[i+1].Err == nil {
			t.Errorf("%s는 오류로 보고되어야 합니다: %+v", sym, obs[i+1])
		}
	}
}

func TestRestoreAndSaveEvals(t *testing.T) {
	ctx := context.Background()
	store, err := session.Open(filepath.Join(t.TempDir(), "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	save := evalSaver(ctx, store, "2026-10-01", "KR")
	save(screener.Entry{Symbol: "A", Passed: true, HasOpen: true, Open: 110, Target: 112, TrendOK: true})
	save(screener.Entry{Symbol: "B", Passed: false, Reason: "레버리지/인버스"})
	if err := store.SaveEval(ctx, "2026-10-01", "KR", "BAD", []byte("not json")); err != nil {
		t.Fatal(err)
	}

	ev := screener.NewEvaluator(nil, screener.DefaultConfig("KR"), time.Now())
	restored, skipped, err := restoreEvals(ctx, store, ev, "2026-10-01", "KR")
	if err != nil || restored != 2 || skipped != 1 {
		t.Fatalf("restored=%d skipped=%d err=%v", restored, skipped, err)
	}
	if a, ok := ev.Cached("A"); !ok || a.Target != 112 || !a.HasOpen {
		t.Errorf("A 복구: %+v %v", a, ok)
	}
	if b, ok := ev.Cached("B"); !ok || b.Passed || b.Reason == "" {
		t.Errorf("탈락 항목도 복구되어야 합니다: %+v %v", b, ok)
	}
}

func TestBuildScreenerStatus(t *testing.T) {
	entries := []screener.Entry{
		{Symbol: "A", Passed: true, Origin: "prewarm", Target: 112},
		{Symbol: "B", Passed: true, Origin: "intraday", Target: 55},
		{Symbol: "X", Passed: false, Reason: "노이즈 1.2% (2.5~6%)"},
		{Symbol: "Y", Passed: false, Reason: "레버리지/인버스"},
		{Symbol: "Z", Passed: false, Reason: "유의사항 1건"},
	}
	rank := func(s string) int { return map[string]int{"A": 2, "B": 5}[s] }
	st := buildScreenerStatus([]string{"A", "B"}, rank, entries, 2)
	if len(st.Active) != 2 || st.Active[0].Symbol != "A" || st.Active[0].Rank != 2 || st.Active[0].Target != 112 || st.Active[1].Origin != "intraday" {
		t.Errorf("Active = %+v", st.Active)
	}
	if len(st.Rejections) != 2 || st.Rejections[0].Symbol != "X" || st.Rejections[1].Symbol != "Y" {
		t.Errorf("Rejections는 종목순으로 상한까지만: %+v", st.Rejections)
	}
	if empty := buildScreenerStatus(nil, rank, nil, 5); empty == nil || len(empty.Active) != 0 {
		t.Errorf("빈 상태에서도 nil이 아니어야 합니다: %+v", empty)
	}
}

func TestDescribeUpdate(t *testing.T) {
	rank := func(string) int { return 3 }
	if got := describeUpdate(screener.Update{}, rank); len(got) != 0 {
		t.Errorf("Ran=false는 로그가 없어야 합니다: %v", got)
	}
	lines := describeUpdate(screener.Update{Ran: true, Added: []string{"A"}, Removed: []string{"B"}, Skipped: 2,
		UsedFallback: true, Errs: []error{errors.New("랭킹 조회: 503")}}, rank)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"편입", "A", "제외", "B", "예산", "2", "사전 평가", "503"} {
		if !strings.Contains(joined, want) {
			t.Errorf("로그에 %q가 없습니다:\n%s", want, joined)
		}
	}
}
