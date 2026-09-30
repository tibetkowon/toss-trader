package main

import (
	"strings"
	"testing"

	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/strategy"
)

func TestDescribeEntry(t *testing.T) {
	ready := describeEntry(screener.Entry{Symbol: "005930", Origin: "prewarm", Passed: true, HasOpen: true,
		Open: 110, Target: 112, TrendOK: true, NoiseRatio: 0.035, Prev: strategy.DailyBar{Date: "2026-09-30", High: 114, Low: 108, Close: 111}})
	for _, want := range []string{"005930", "셋업: 시가=110.00", "목표가=112.00", "prewarm", "추세=true", "3.50%"} {
		if !strings.Contains(ready, want) {
			t.Errorf("%q가 없습니다: %s", want, ready)
		}
	}
	waiting := describeEntry(screener.Entry{Symbol: "A", Origin: "intraday", Passed: true, NoiseRatio: 0.04})
	if !strings.Contains(waiting, "시가 대기") || strings.Contains(waiting, "셋업:") {
		t.Errorf("시가 대기 줄: %s", waiting)
	}
	rejected := describeEntry(screener.Entry{Symbol: "B", Origin: "intraday", Reason: "레버리지/인버스"})
	if !strings.Contains(rejected, "탈락") || !strings.Contains(rejected, "레버리지/인버스") {
		t.Errorf("탈락 줄: %s", rejected)
	}
}
