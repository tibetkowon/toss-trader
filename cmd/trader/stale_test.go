package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/tradingloop"
)

const staleMax = 30 * time.Second

func obsAt(symbol string, now time.Time, age time.Duration) tradingloop.PriceObservation {
	return tradingloop.PriceObservation{Symbol: symbol, Price: 100, Timestamp: now.Add(-age)}
}

func TestStaleTrackerLogsOnePerEpisodeNotPerTick(t *testing.T) {
	tr := newStaleTracker()
	t0 := time.Date(2026, 10, 7, 16, 42, 15, 0, time.UTC)

	var lines []string
	for i := 0; i < 5; i++ { // 4초 간격으로 5틱 동안 계속 stale
		now := t0.Add(time.Duration(i) * 4 * time.Second)
		lines = append(lines, tr.observe([]tradingloop.PriceObservation{
			obsAt("MU", now, 45*time.Second), obsAt("AMD", now, 50*time.Second), obsAt("TSLA", now, time.Second),
		}, now, staleMax, "")...)
	}
	if len(lines) != 1 {
		t.Fatalf("에피소드 시작 한 줄만 나와야 합니다: %d줄 %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "AMD") || !strings.Contains(lines[0], "MU") || strings.Contains(lines[0], "TSLA") {
		t.Fatalf("stale 종목(AMD, MU)만 나열해야 합니다: %q", lines[0])
	}
}

func TestStaleTrackerLogsRecoveryWithDuration(t *testing.T) {
	tr := newStaleTracker()
	t0 := time.Date(2026, 10, 7, 16, 42, 15, 0, time.UTC)
	tr.observe([]tradingloop.PriceObservation{obsAt("MU", t0, 40*time.Second)}, t0, staleMax, "")

	t1 := t0.Add(45 * time.Second)
	lines := tr.observe([]tradingloop.PriceObservation{obsAt("MU", t1, time.Second)}, t1, staleMax, "")
	if len(lines) != 1 || !strings.Contains(lines[0], "해소") || !strings.Contains(lines[0], "MU") || !strings.Contains(lines[0], "45s") {
		t.Fatalf("해소 한 줄(종목·지속시간 포함)이 나와야 합니다: %v", lines)
	}
	if again := tr.observe([]tradingloop.PriceObservation{obsAt("MU", t1, time.Second)}, t1, staleMax, ""); len(again) != 0 {
		t.Fatalf("정상 틱은 로그가 없어야 합니다: %v", again)
	}
}

func TestStaleTrackerWarnsWhenHeldPositionGoesStale(t *testing.T) {
	now := time.Date(2026, 10, 7, 16, 42, 15, 0, time.UTC)

	held := newStaleTracker().observe([]tradingloop.PriceObservation{obsAt("SPCX", now, 35*time.Second), obsAt("MU", now, 35*time.Second)}, now, staleMax, "SPCX")
	var warn string
	for _, l := range held {
		if strings.Contains(l, "보유 포지션") {
			warn = l
		}
	}
	if warn == "" || !strings.Contains(warn, "SPCX") || !strings.Contains(warn, "손절") {
		t.Fatalf("보유 종목이 stale이면 손절 판정 중단을 경고해야 합니다: %v", held)
	}

	notHeld := newStaleTracker().observe([]tradingloop.PriceObservation{obsAt("MU", now, 35*time.Second)}, now, staleMax, "SPCX")
	for _, l := range notHeld {
		if strings.Contains(l, "보유 포지션") {
			t.Fatalf("보유하지 않은 종목에는 포지션 경고가 없어야 합니다: %v", notHeld)
		}
	}
}

func TestStaleTrackerIgnoresFetchErrorsForEpisodeState(t *testing.T) {
	tr := newStaleTracker()
	t0 := time.Date(2026, 10, 7, 16, 42, 15, 0, time.UTC)
	tr.observe([]tradingloop.PriceObservation{obsAt("MU", t0, 40*time.Second)}, t0, staleMax, "")

	// 조회 오류는 stale 여부를 알 수 없으므로 에피소드를 끊지도, 새로 시작하지도 않는다.
	t1 := t0.Add(4 * time.Second)
	if lines := tr.observe([]tradingloop.PriceObservation{{Symbol: "MU", Err: errors.New("429")}}, t1, staleMax, ""); len(lines) != 0 {
		t.Fatalf("오류 관측치는 stale 로그를 만들면 안 됩니다: %v", lines)
	}
	t2 := t0.Add(10 * time.Second)
	if lines := tr.observe([]tradingloop.PriceObservation{obsAt("MU", t2, 50*time.Second)}, t2, staleMax, ""); len(lines) != 0 {
		t.Fatalf("오류 틱을 사이에 둬도 같은 에피소드여야 합니다: %v", lines)
	}
}

func TestStaleTrackerEndsEpisodeWhenSymbolLeavesActiveSet(t *testing.T) {
	tr := newStaleTracker()
	t0 := time.Date(2026, 10, 7, 16, 42, 15, 0, time.UTC)
	tr.observe([]tradingloop.PriceObservation{obsAt("MU", t0, 40*time.Second)}, t0, staleMax, "")

	t1 := t0.Add(8 * time.Second)
	lines := tr.observe(nil, t1, staleMax, "") // MU가 활성 종목에서 빠져 관측되지 않음
	if len(lines) != 1 || !strings.Contains(lines[0], "해소") {
		t.Fatalf("관측 대상에서 빠지면 에피소드를 닫아야 합니다: %v", lines)
	}
}

func TestStaleTrackerSummaryCountsEpisodesAndLongest(t *testing.T) {
	tr := newStaleTracker()
	t0 := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	step := func(at time.Time, age time.Duration) {
		tr.observe([]tradingloop.PriceObservation{obsAt("MU", at, age)}, at, staleMax, "")
	}
	step(t0, 40*time.Second)                      // 1번째 시작
	step(t0.Add(20*time.Second), time.Second)     // 20s 만에 해소
	step(t0.Add(100*time.Second), 40*time.Second) // 2번째 시작
	step(t0.Add(160*time.Second), time.Second)    // 60s 만에 해소

	got := tr.summary(t0.Add(200 * time.Second))
	if len(got) != 1 || !strings.Contains(got[0], "MU") || !strings.Contains(got[0], "2회") || !strings.Contains(got[0], "1m0s") {
		t.Fatalf("요약(MU 2회, 최장 1m0s) 불일치: %v", got)
	}
	if none := newStaleTracker().summary(t0); len(none) != 0 {
		t.Fatalf("stale이 없으면 요약도 없어야 합니다: %v", none)
	}
}

func TestStaleTrackerSummaryIncludesStillOpenEpisode(t *testing.T) {
	tr := newStaleTracker()
	t0 := time.Date(2026, 10, 7, 19, 40, 0, 0, time.UTC)
	tr.observe([]tradingloop.PriceObservation{obsAt("MU", t0, 40*time.Second)}, t0, staleMax, "")
	got := tr.summary(t0.Add(30 * time.Second))
	if len(got) != 1 || !strings.Contains(got[0], "1회") || !strings.Contains(got[0], "30s") {
		t.Fatalf("마감 시점에 진행 중인 에피소드도 요약해야 합니다: %v", got)
	}
}
