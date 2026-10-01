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
