package screener

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

func TestCompleteComputesTargetFromRegularSessionOpen(t *testing.T) {
	f := passingSource("A")
	start := testSessionStart()
	f.minute["A"] = []tossapi.Candle{
		minuteCandle(start.Add(2*time.Minute), "120"),
		minuteCandle(start.Add(time.Minute), "111"),
		minuteCandle(start, "110"),
		minuteCandle(start.Add(-time.Minute), "90"), // 장전(NXT) 봉은 무시
	}
	ev := newTestEvaluator(f)
	ev.Static(context.Background(), "A", "prewarm", time.Now(), false)

	e, err := ev.Complete(context.Background(), "A")
	if err != nil || !e.HasOpen {
		t.Fatalf("Complete = %+v %v", e, err)
	}
	// 시가 110 + (104-100)*0.5 = 112, 전일 종가 103 > MA5 100.6
	if e.Open != 110 || e.Target != 112 || !e.TrendOK {
		t.Errorf("open/target/trend = %v %v %v", e.Open, e.Target, e.TrendOK)
	}
	if again, _ := ev.Complete(context.Background(), "A"); !again.HasOpen || f.calls["Minute"] != 1 {
		t.Errorf("확정된 항목은 다시 조회하지 않아야 합니다: 1분봉 호출 %d회", f.calls["Minute"])
	}
}

func TestCompleteFallsBackToEarliestBarAfterStart(t *testing.T) {
	f := passingSource("A")
	start := testSessionStart()
	f.minute["A"] = []tossapi.Candle{
		minuteCandle(start.Add(3*time.Minute), "130"),
		minuteCandle(start.Add(time.Minute), "115"), // 09:00 봉이 없는 저유동 종목
	}
	ev := newTestEvaluator(f)
	ev.Static(context.Background(), "A", "prewarm", time.Now(), false)
	e, err := ev.Complete(context.Background(), "A")
	if err != nil || !e.HasOpen || e.Open != 115 {
		t.Fatalf("Complete = %+v %v", e, err)
	}
}

func TestCompleteNoBarsYetIsRetriable(t *testing.T) {
	f := passingSource("A")
	f.minute["A"] = []tossapi.Candle{minuteCandle(testSessionStart().Add(-time.Minute), "90")}
	ev := newTestEvaluator(f)
	ev.Static(context.Background(), "A", "prewarm", time.Now(), false)
	e, err := ev.Complete(context.Background(), "A")
	if err != nil || e.HasOpen {
		t.Fatalf("시가가 없는데 확정되었습니다: %+v %v", e, err)
	}
	if cached, _ := ev.Cached("A"); cached.HasOpen {
		t.Error("미확정 상태가 캐시에 확정으로 남았습니다")
	}
	f.minute["A"] = []tossapi.Candle{minuteCandle(testSessionStart(), "100")}
	if e, err := ev.Complete(context.Background(), "A"); err != nil || !e.HasOpen {
		t.Fatalf("재시도 실패: %+v %v", e, err)
	}
}

func TestCompleteRequiresPassedEntry(t *testing.T) {
	f := newFakeSource()
	ev := newTestEvaluator(f)
	if _, err := ev.Complete(context.Background(), "NOPE"); err == nil {
		t.Error("평가되지 않은 종목에 오류가 없습니다")
	}
	ev.Seed([]Entry{{Symbol: "R", Passed: false, Reason: "x"}})
	if _, err := ev.Complete(context.Background(), "R"); err == nil {
		t.Error("탈락 종목에 오류가 없습니다")
	}
}

func TestCompleteMinuteAPIErrorPropagates(t *testing.T) {
	f := passingSource("A")
	f.errOn["Minute:A"] = errors.New("429")
	ev := newTestEvaluator(f)
	ev.Static(context.Background(), "A", "prewarm", time.Now(), false)
	if _, err := ev.Complete(context.Background(), "A"); err == nil {
		t.Fatal("API 오류가 전달되지 않았습니다")
	}
}
