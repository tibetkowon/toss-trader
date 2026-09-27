package snapshot

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDailyLossProgress(t *testing.T) {
	for _, tc := range []struct {
		name             string
		pnl, seed, limit float64
		want             float64
	}{
		{"수익", 100, 100000, 0.05, 0},
		{"손익 없음", 0, 100000, 0.05, 0},
		{"일부 손실", -2500, 100000, 0.05, 0.5},
		{"한도 도달", -5000, 100000, 0.05, 1},
		{"한도 초과", -7500, 100000, 0.05, 1.5},
		{"잔고 없음", -100, 0, 0.05, 0},
		{"음수 잔고", -100, -100000, 0.05, 0},
		{"한도 없음", -100, 100000, 0, 0},
		{"음수 한도", -100, 100000, -0.05, 0},
		{"잘못된 손익", math.NaN(), 100000, 0.05, 0},
		{"무한 손실", math.Inf(-1), 100000, 0.05, 0},
		{"무한 수익", math.Inf(1), 100000, 0.05, 0},
		{"잘못된 잔고", -100, math.NaN(), 0.05, 0},
		{"무한 잔고", -100, math.Inf(1), 0.05, 0},
		{"잘못된 한도", -100, 100000, math.NaN(), 0},
		{"무한 한도", -100, 100000, math.Inf(1), 0},
		{"한도 곱 오버플로", -100, math.MaxFloat64, 2, 0},
		{"한도 곱 언더플로", -100, math.SmallestNonzeroFloat64, 0.01, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Snapshot{DailyPnL: tc.pnl, Seed: tc.seed, DailyLossLimitPct: tc.limit}
			if got := s.DailyLossProgress(); got != tc.want {
				t.Errorf("진행률: %v, 기대: %v", got, tc.want)
			}
		})
	}
}

func TestRenderJSON(t *testing.T) {
	now := time.Date(2026, 9, 27, 1, 2, 3, 0, time.UTC)
	want := Snapshot{
		Positions:         []Position{{Symbol: "005930", Quantity: 1, UnrealizedPnL: -200}},
		DailyPnL:          -2500,
		Seed:              100000,
		DailyLossLimitPct: 0.05,
		KillSwitch:        KillSwitchStatus{Halted: true, Reason: "연속 손실"},
		RecentOrders:      []Order{{Symbol: "005930", Side: "BUY", Quantity: 1, Price: 50000, Status: "FILLED", CreatedAt: now}},
		UpdatedAt:         now,
	}
	data, err := want.RenderJSON()
	if err != nil {
		t.Fatal(err)
	}
	var got Snapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JSON 왕복 결과: %+v, 기대: %+v", got, want)
	}
	if _, err := (Snapshot{DailyPnL: math.NaN()}).RenderJSON(); err == nil {
		t.Error("JSON으로 표현할 수 없는 값의 오류가 필요합니다")
	}
}

func TestRenderHTML(t *testing.T) {
	attack := "<script>alert(1)</script>"
	s := Snapshot{
		Positions:         []Position{{Symbol: attack, Quantity: 2, UnrealizedPnL: -123}},
		DailyPnL:          -2500,
		Seed:              100000,
		DailyLossLimitPct: 0.05,
		KillSwitch:        KillSwitchStatus{Halted: true, Reason: attack},
		RecentOrders:      []Order{{Symbol: attack, Side: attack, Status: attack, Quantity: 3, Price: 456}},
		UpdatedAt:         time.Date(2026, 9, 27, 1, 2, 3, 0, time.UTC),
	}
	data, err := s.RenderHTML()
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	if strings.Contains(html, attack) || strings.Contains(html, "<script>") {
		t.Fatal("실행 가능한 스크립트가 포함되었습니다")
	}
	if got := strings.Count(html, "&lt;script&gt;alert(1)&lt;/script&gt;"); got != 5 {
		t.Errorf("이스케이프된 필드 수: %d, 기대: 5", got)
	}
	for _, want := range []string{"50.00%", "-2500", "-123", "456", "중단", "2026-09-27T01:02:03Z"} {
		if !strings.Contains(html, want) {
			t.Errorf("필수 표시 내용 누락: %s", want)
		}
	}
	empty, err := (Snapshot{}).RenderHTML()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"정상", "보유 포지션 없음", "최근 주문 없음", "0.00%"} {
		if !strings.Contains(string(empty), want) {
			t.Errorf("빈 상태 표시 누락: %s", want)
		}
	}
}
