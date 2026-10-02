package main

import (
	"strings"
	"testing"

	"github.com/tibetkowon/toss-trader/internal/simulator"
)

func TestSkipTrackerDropsZeroShareSkipsButKeepsTradesAndCounts(t *testing.T) {
	tr := newSkipTracker()
	bought := simulator.Action{Type: simulator.Bought, Symbol: "A", Price: 10, Shares: 1.5}
	var kept []simulator.Action
	for i := 0; i < 3; i++ {
		kept = tr.filter([]simulator.Action{
			{Type: simulator.SkippedZeroShares, Symbol: "SPCX", Price: 100 + float64(i)},
			bought,
		}, 73.94)
		if len(kept) != 1 || kept[0].Type != simulator.Bought {
			t.Fatalf("tick %d: 스킵만 걸러져야 합니다: %+v", i, kept)
		}
	}
	lines := tr.summary()
	if len(lines) != 1 || !strings.Contains(lines[0], "SPCX 3회") || !strings.Contains(lines[0], "100.00~102.00") {
		t.Fatalf("요약이 이상합니다: %v", lines)
	}
}

func TestFormatSharesKeepsFractionsWithoutTrailingZeros(t *testing.T) {
	for in, want := range map[float64]string{3: "3", 0.5: "0.5", 0.123456: "0.123456"} {
		if got := formatShares(in); got != want {
			t.Errorf("formatShares(%v) = %q, want %q", in, got, want)
		}
	}
}
