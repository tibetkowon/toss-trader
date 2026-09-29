package main

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/simulator"
	"github.com/tibetkowon/toss-trader/internal/snapshot"
	"github.com/tibetkowon/toss-trader/internal/tradingloop"
)

func TestDetectMarket(t *testing.T) {
	kst, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		time time.Time
		want string
	}{
		{"국장 기동 시각(08:50 KST)", time.Date(2026, 9, 28, 8, 50, 0, 0, kst), "KR"},
		{"국장 정규장 도중(12:00 KST)", time.Date(2026, 9, 28, 12, 0, 0, 0, kst), "KR"},
		{"미장 기동 시각(22:20 KST)", time.Date(2026, 9, 28, 22, 20, 0, 0, kst), "US"},
		{"미장 정규장 새벽(03:00 KST)", time.Date(2026, 9, 28, 3, 0, 0, 0, kst), "US"},
		{"국장 백스톱 직후(16:00 KST)", time.Date(2026, 9, 28, 16, 0, 0, 0, kst), "KR"},
		{"경계값(05:59 KST -> US)", time.Date(2026, 9, 28, 5, 59, 0, 0, kst), "US"},
		{"경계값(06:00 KST -> KR)", time.Date(2026, 9, 28, 6, 0, 0, 0, kst), "KR"},
		{"경계값(17:59 KST -> KR)", time.Date(2026, 9, 28, 17, 59, 0, 0, kst), "KR"},
		{"경계값(18:00 KST -> US)", time.Date(2026, 9, 28, 18, 0, 0, 0, kst), "US"},
		{"다른 타임존 입력도 KST로 변환해서 판단", time.Date(2026, 9, 28, 3, 50, 0, 0, time.UTC), "KR"}, // 03:50 UTC = 12:50 KST
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := detectMarket(c.time); got != c.want {
				t.Errorf("detectMarket(%v) = %q, want %q", c.time, got, c.want)
			}
		})
	}
}

func TestDescribeSkippedObservations(t *testing.T) {
	now := time.Date(2026, 9, 28, 5, 0, 0, 0, time.UTC)
	maxAge := 30 * time.Second

	cases := []struct {
		name  string
		obs   []tradingloop.PriceObservation
		wantN int
	}{
		{
			name:  "정상 관측치는 로그 없음",
			obs:   []tradingloop.PriceObservation{{Symbol: "005930", Price: 70000, Timestamp: now}},
			wantN: 0,
		},
		{
			name:  "에러난 관측치는 한 줄",
			obs:   []tradingloop.PriceObservation{{Symbol: "005930", Err: errors.New("rate limited")}},
			wantN: 1,
		},
		{
			name:  "스테일 관측치도 한 줄",
			obs:   []tradingloop.PriceObservation{{Symbol: "005930", Price: 70000, Timestamp: now.Add(-time.Minute)}},
			wantN: 1,
		},
		{
			name: "8개 전부 에러면 8줄",
			obs: []tradingloop.PriceObservation{
				{Symbol: "005930", Err: errors.New("x")}, {Symbol: "000660", Err: errors.New("x")},
				{Symbol: "066570", Err: errors.New("x")}, {Symbol: "005380", Err: errors.New("x")},
				{Symbol: "034020", Err: errors.New("x")}, {Symbol: "396500", Err: errors.New("x")},
				{Symbol: "229200", Err: errors.New("x")}, {Symbol: "069500", Err: errors.New("x")},
			},
			wantN: 8,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := describeSkippedObservations(c.obs, now, maxAge)
			if len(got) != c.wantN {
				t.Errorf("describeSkippedObservations() = %d lines, want %d (%v)", len(got), c.wantN, got)
			}
		})
	}
}

func TestAppendRecentOrderCapsLength(t *testing.T) {
	var orders []snapshot.Order
	for i := 0; i < 15; i++ {
		orders = appendRecentOrder(orders, snapshot.Order{Symbol: fmt.Sprintf("%d", i)}, 10)
	}
	if len(orders) != 10 {
		t.Fatalf("len(orders) = %d, want 10", len(orders))
	}
	if orders[0].Symbol != "5" {
		t.Errorf("oldest kept order = %q, want %q (should have dropped 0-4)", orders[0].Symbol, "5")
	}
	if orders[9].Symbol != "14" {
		t.Errorf("newest order = %q, want %q", orders[9].Symbol, "14")
	}
}

func TestToOrderMapsActionType(t *testing.T) {
	at := time.Date(2026, 9, 28, 0, 12, 0, 0, time.UTC)
	action := simulator.Action{Type: simulator.StoppedOut, Symbol: "396500", Price: 38465, Shares: 2, PnL: -1571.5}
	got := toOrder(action, at)
	want := snapshot.Order{Symbol: "396500", Side: "SELL", Quantity: 2, Price: 38465, Status: "StoppedOut", CreatedAt: at}
	if got != want {
		t.Errorf("toOrder() = %+v, want %+v", got, want)
	}
}

func TestToOrderBoughtIsBuySide(t *testing.T) {
	at := time.Date(2026, 9, 29, 0, 0, 17, 0, time.UTC)
	action := simulator.Action{Type: simulator.Bought, Symbol: "229200", Price: 14145, Shares: 6}
	got := toOrder(action, at)
	if got.Side != "BUY" {
		t.Errorf("Side = %q, want BUY", got.Side)
	}
}
