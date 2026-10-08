package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tibetkowon/toss-trader/internal/tradingloop"
)

// staleTracker는 "시세가 오래돼 건너뜀"을 틱마다가 아니라 에피소드(stale이 시작해서 해소될 때까지)
// 단위로 기록합니다. 2026-10-07 01:42 KST에 10종목이 45초 동안 stale이어서 같은 내용의 로그가
// 98줄 쌓였고, 정작 중요한 "보유 포지션의 손절 판정이 멈춰 있었다"는 사실은 묻혔기 때문입니다.
type staleTracker struct {
	open  map[string]*staleEpisode
	stats map[string]*staleStat
}

type staleEpisode struct {
	since time.Time
	held  bool // 에피소드 중 한 번이라도 보유 포지션이었던 종목
}

type staleStat struct {
	episodes int
	longest  time.Duration
}

func newStaleTracker() *staleTracker {
	return &staleTracker{open: map[string]*staleEpisode{}, stats: map[string]*staleStat{}}
}

// observe는 이번 틱의 관측치를 반영하고, 새로 시작했거나 해소된 에피소드에 대해서만 로그 줄을
// 돌려줍니다. 조회 오류 관측치는 stale 여부를 알 수 없으므로 에피소드를 시작하지도 끊지도 않고,
// 관측 대상에서 빠진 종목(활성 종목에서 제외됨)은 해소로 취급합니다.
func (t *staleTracker) observe(observations []tradingloop.PriceObservation, now time.Time, maxAge time.Duration, held string) []string {
	staleNow := map[string]time.Duration{}
	errored := map[string]bool{}
	for _, obs := range observations {
		if obs.Err != nil {
			errored[obs.Symbol] = true
			continue
		}
		if age := now.Sub(obs.Timestamp); age > maxAge {
			staleNow[obs.Symbol] = age
		}
	}

	var started []string
	var worst time.Duration
	heldStarted := false
	for _, symbol := range sortedKeys(staleNow) {
		ep, isOpen := t.open[symbol]
		if !isOpen {
			ep = &staleEpisode{since: now}
			t.open[symbol] = ep
			started = append(started, symbol)
			worst = max(worst, staleNow[symbol])
			if symbol == held {
				heldStarted = true
			}
		}
		if symbol == held {
			ep.held = true
		}
	}

	var resolved []string
	for _, symbol := range sortedKeys(t.open) {
		if _, stale := staleNow[symbol]; stale || errored[symbol] {
			continue
		}
		ep := t.open[symbol]
		d := now.Sub(ep.since)
		st := t.statFor(symbol)
		st.episodes++
		st.longest = max(st.longest, d)
		label := formatDuration(d)
		if ep.held {
			label += ", 보유 포지션"
		}
		resolved = append(resolved, fmt.Sprintf("%s(%s)", symbol, label))
		delete(t.open, symbol)
	}

	var lines []string
	if len(started) > 0 {
		lines = append(lines, fmt.Sprintf("시세 stale 시작: %s (%d종목, 최대 age=%s) — 해소될 때까지 해당 종목 판정을 건너뜁니다",
			strings.Join(started, ", "), len(started), formatDuration(worst)))
	}
	if heldStarted {
		lines = append(lines, fmt.Sprintf("⚠ 보유 포지션 %s 시세가 stale입니다 — 시세가 회복될 때까지 손절 판정이 멈춥니다", held))
	}
	if len(resolved) > 0 {
		lines = append(lines, "시세 stale 해소: "+strings.Join(resolved, ", "))
	}
	return lines
}

// summary는 마감 때 종목별 stale 횟수와 최장 지속 시간을 돌려줍니다. 마감 시점에 아직 진행 중인
// 에피소드도 now까지의 길이로 포함합니다.
func (t *staleTracker) summary(now time.Time) []string {
	merged := map[string]staleStat{}
	for symbol, st := range t.stats {
		merged[symbol] = *st
	}
	for symbol, ep := range t.open {
		st := merged[symbol]
		st.episodes++
		st.longest = max(st.longest, now.Sub(ep.since))
		merged[symbol] = st
	}
	lines := make([]string, 0, len(merged))
	for _, symbol := range sortedKeys(merged) {
		st := merged[symbol]
		lines = append(lines, fmt.Sprintf("시세 stale 요약: %s %d회 (최장 %s)", symbol, st.episodes, formatDuration(st.longest)))
	}
	return lines
}

func (t *staleTracker) statFor(symbol string) *staleStat {
	st, ok := t.stats[symbol]
	if !ok {
		st = &staleStat{}
		t.stats[symbol] = st
	}
	return st
}

func formatDuration(d time.Duration) string { return d.Round(time.Second).String() }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
