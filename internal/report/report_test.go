package report

import (
	"math"
	"reflect"
	"testing"

	"github.com/tibetkowon/toss-trader/internal/session"
	"github.com/tibetkowon/toss-trader/internal/simulator"
)

func TestBuildEquityCurve(t *testing.T) {
	records := []session.DailyRecord{
		{Date: "2026-09-28", Market: "KR", State: simulator.State{Cash: 1000}},
		{Date: "2026-09-29", Market: "KR", State: simulator.State{
			Cash: 600,
			Position: &simulator.Position{
				Symbol: "A", Shares: 4, EntryPrice: 100, CostBasis: 401,
			},
		}},
	}
	want := []EquityPoint{
		{Date: "2026-09-28", Equity: 1000},
		{Date: "2026-09-29", Equity: 1001},
	}
	if got := BuildEquityCurve(records); !reflect.DeepEqual(got, want) {
		t.Fatalf("자산 곡선 불일치: got %+v want %+v", got, want)
	}
}

func TestBuildEquityCurveStartsAtSeed(t *testing.T) {
	records := []session.DailyRecord{
		{Date: "2026-09-28", Market: "KR", State: simulator.State{Seed: 100000, Cash: 97441}},
		{Date: "2026-09-29", Market: "KR", State: simulator.State{Seed: 97441, Cash: 98225}},
	}
	got := BuildEquityCurve(records)
	want := []EquityPoint{
		{Date: StartLabel, Equity: 100000},
		{Date: "2026-09-28", Equity: 97441},
		{Date: "2026-09-29", Equity: 98225},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("자산 곡선 불일치: got %+v want %+v", got, want)
	}
	if dd := MaxDrawdown(got); math.Abs(dd-0.02559) > 1e-4 {
		t.Fatalf("첫날 손실도 낙폭으로 잡혀야 합니다: %v", dd)
	}
}

func TestBuildEquityCurveEmpty(t *testing.T) {
	if got := BuildEquityCurve(nil); len(got) != 0 {
		t.Fatalf("빈 기록의 자산 곡선이 비어 있지 않습니다: %+v", got)
	}
}

func TestMaxDrawdown(t *testing.T) {
	tests := []struct {
		name     string
		equities []float64
		want     float64
	}{
		{name: "빈 곡선"},
		{name: "하루", equities: []float64{100}},
		{name: "상승", equities: []float64{100, 110, 120}},
		{name: "보합", equities: []float64{100, 100, 100}},
		{name: "연속 하락", equities: []float64{100, 90, 80}, want: 0.2},
		{name: "새 고점", equities: []float64{100, 90, 200, 150}, want: 0.25},
		{name: "회복 후 최대 낙폭 유지", equities: []float64{100, 50, 200, 180}, want: 0.5},
		{name: "전액 손실", equities: []float64{100, 0}, want: 1},
		{name: "음수 자산", equities: []float64{100, -20}, want: 1.2},
		{name: "영 고점", equities: []float64{0, 0, -10}},
		{name: "음수 고점", equities: []float64{-10, -20}},
		{name: "양수 고점으로 전환", equities: []float64{-10, 0, 100, 80}, want: 0.2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			points := make([]EquityPoint, len(tt.equities))
			for i, equity := range tt.equities {
				points[i] = EquityPoint{Equity: equity}
			}
			got := MaxDrawdown(points)
			if math.IsNaN(got) || math.Abs(got-tt.want) > 1e-12 {
				t.Fatalf("최대 낙폭 불일치: got %v want %v", got, tt.want)
			}
		})
	}
}

func TestBuildEquityCurveConvertsUSDSessionsToKRW(t *testing.T) {
	records := []session.DailyRecord{
		{Date: "2026-10-01", Market: "US", State: simulator.State{Seed: 70, Cash: 71, Currency: "USD", FXRate: 1400}},
	}
	got := BuildEquityCurve(records)
	want := []EquityPoint{
		{Date: StartLabel, Equity: 98000},
		{Date: "2026-10-01", Equity: 99400},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestBuildAccountCurveInterleavesMarketsChronologically(t *testing.T) {
	// Store.All은 시장별로 묶어서 돌려주지만, 계좌는 하나이므로 날짜순(같은 날은 KR→US)으로 이어야 한다.
	records := []session.DailyRecord{
		{Date: "2026-10-06", Market: "KR", State: simulator.State{Seed: 1000, Cash: 990}},
		{Date: "2026-10-07", Market: "KR", State: simulator.State{Seed: 980, Cash: 980}},
		{Date: "2026-10-06", Market: "US", State: simulator.State{Seed: 0.99, Cash: 0.98, Currency: "USD", FXRate: 1000}},
		{Date: "2026-10-07", Market: "US", State: simulator.State{Seed: 0.98, Cash: 0.95, Currency: "USD", FXRate: 1000}},
	}
	got := BuildAccountCurve(records)
	want := []EquityPoint{
		{Date: StartLabel, Equity: 1000},
		{Date: "2026-10-06 KR", Equity: 990},
		{Date: "2026-10-06 US", Equity: 980},
		{Date: "2026-10-07 KR", Equity: 980},
		{Date: "2026-10-07 US", Equity: 950},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("합산 곡선 불일치:\ngot  %+v\nwant %+v", got, want)
	}
}

func TestBuildAccountCurveSkipsLegacyIndependentUSSessions(t *testing.T) {
	// 계좌 공유 이전의 US 세션은 통화가 비어 있고 독립 시드(100000)로 돌았다. 합산 곡선에 섞으면 안 된다.
	records := []session.DailyRecord{
		{Date: "2026-09-30", Market: "KR", State: simulator.State{Seed: 100000, Cash: 98000}},
		{Date: "2026-09-30", Market: "US", State: simulator.State{Seed: 100000, Cash: 100463}},
		{Date: "2026-10-01", Market: "KR", State: simulator.State{Seed: 98000, Cash: 99000}},
		{Date: "2026-10-01", Market: "US", State: simulator.State{Seed: 72, Cash: 72, Currency: "USD", FXRate: 1375}},
	}
	got := BuildAccountCurve(records)
	want := []EquityPoint{
		{Date: StartLabel, Equity: 100000},
		{Date: "2026-09-30 KR", Equity: 98000},
		{Date: "2026-10-01 KR", Equity: 99000},
		{Date: "2026-10-01 US", Equity: 99000},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("레거시 US 세션이 섞였습니다:\ngot  %+v\nwant %+v", got, want)
	}
}

func TestBuildAccountCurveEmpty(t *testing.T) {
	if got := BuildAccountCurve(nil); len(got) != 0 {
		t.Fatalf("빈 기록의 합산 곡선이 비어 있지 않습니다: %+v", got)
	}
}

func TestAssess(t *testing.T) {
	// 100 → 110(고점) → 99(저점, MDD 10%) → 104.5(현재, 고점 대비 -5%)
	curve := []EquityPoint{{Equity: 100}, {Equity: 110}, {Equity: 99}, {Equity: 104.5}}
	got := Assess(curve)
	if math.Abs(got.Equity-104.5) > 1e-9 || math.Abs(got.Peak-110) > 1e-9 {
		t.Fatalf("자산/고점 불일치: %+v", got)
	}
	if math.Abs(got.CumulativeReturn-0.045) > 1e-9 {
		t.Fatalf("누적 수익률 불일치: %v", got.CumulativeReturn)
	}
	if math.Abs(got.CurrentDrawdown-0.05) > 1e-9 || math.Abs(got.MaxDrawdown-0.10) > 1e-9 {
		t.Fatalf("낙폭 불일치: cur=%v max=%v", got.CurrentDrawdown, got.MaxDrawdown)
	}
	if got.Level != DrawdownCritical {
		t.Fatalf("MDD 10%%면 critical이어야 합니다: %q", got.Level)
	}
}

func TestAssessEmptyAndSinglePoint(t *testing.T) {
	if got := Assess(nil); got != (Status{}) {
		t.Fatalf("빈 곡선은 영값이어야 합니다: %+v", got)
	}
	got := Assess([]EquityPoint{{Equity: 100}})
	if got.Equity != 100 || got.CumulativeReturn != 0 || got.MaxDrawdown != 0 || got.Level != DrawdownOK {
		t.Fatalf("점 하나: %+v", got)
	}
}

func TestDrawdownLevelThresholds(t *testing.T) {
	tests := []struct {
		maxDD float64
		want  DrawdownLevel
	}{
		{0, DrawdownOK},
		{0.0799, DrawdownOK},
		{0.08, DrawdownWarn},
		{0.0899, DrawdownWarn},
		{0.09, DrawdownCritical},
		{0.25, DrawdownCritical},
	}
	for _, tt := range tests {
		if got := LevelFor(tt.maxDD); got != tt.want {
			t.Errorf("LevelFor(%v) = %q, want %q", tt.maxDD, got, tt.want)
		}
	}
}
