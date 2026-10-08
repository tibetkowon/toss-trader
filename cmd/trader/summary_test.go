package main

import (
	"math"
	"strings"
	"testing"

	"github.com/tibetkowon/toss-trader/internal/report"
	"github.com/tibetkowon/toss-trader/internal/session"
	"github.com/tibetkowon/toss-trader/internal/simulator"
)

func sharedEraRecords(lastUSCash float64) []session.DailyRecord {
	return []session.DailyRecord{
		{Date: "2026-10-01", Market: "KR", State: simulator.State{Seed: 100000, Cash: 100000}},
		{Date: "2026-10-02", Market: "KR", State: simulator.State{Seed: 100000, Cash: 96000}},
		{Date: "2026-10-02", Market: "US", State: simulator.State{Seed: 70, Cash: lastUSCash, Currency: "USD", FXRate: 1000}},
	}
}

func TestBuildDaySummary(t *testing.T) {
	// 고점 100000 → 마지막 US 세션 후 92000 (낙폭 8%) → warn
	sum := buildDaySummary(sharedEraRecords(92), 2, -2120.14)
	if sum.Trades != 2 || math.Abs(sum.DayPnLKRW-(-2120.14)) > 1e-9 {
		t.Fatalf("체결 수/일 손익 불일치: %+v", sum)
	}
	if math.Abs(sum.AccountEquityKRW-92000) > 1e-6 || math.Abs(sum.CumulativeReturn-(-0.08)) > 1e-9 {
		t.Fatalf("자산/누적 수익률 불일치: %+v", sum)
	}
	if math.Abs(sum.CurrentDrawdown-0.08) > 1e-9 || math.Abs(sum.MaxDrawdown-0.08) > 1e-9 {
		t.Fatalf("낙폭 불일치: %+v", sum)
	}
	if sum.DrawdownLevel != string(report.DrawdownWarn) {
		t.Fatalf("8%%면 warn이어야 합니다: %q", sum.DrawdownLevel)
	}
}

func TestBuildDaySummaryHealthyAccountIsOK(t *testing.T) {
	sum := buildDaySummary(sharedEraRecords(99), 0, 0)
	if sum.DrawdownLevel != string(report.DrawdownOK) || sum.Trades != 0 {
		t.Fatalf("낙폭 1%%는 ok여야 합니다: %+v", sum)
	}
}

func TestBuildDaySummaryWithNoRecords(t *testing.T) {
	sum := buildDaySummary(nil, 1, 5)
	if sum.Trades != 1 || sum.DayPnLKRW != 5 || sum.AccountEquityKRW != 0 || sum.DrawdownLevel != string(report.DrawdownOK) {
		t.Fatalf("기록이 없어도 패닉 없이 요약해야 합니다: %+v", sum)
	}
}

func TestDrawdownAlertLine(t *testing.T) {
	if line := drawdownAlertLine(report.Status{MaxDrawdown: 0.05, Level: report.DrawdownOK}); line != "" {
		t.Fatalf("ok 단계는 경고가 없어야 합니다: %q", line)
	}
	warn := drawdownAlertLine(report.Status{MaxDrawdown: 0.0812, CurrentDrawdown: 0.0812, Level: report.DrawdownWarn})
	if !strings.Contains(warn, "경고") || !strings.Contains(warn, "8.12%") || !strings.Contains(warn, "10%") {
		t.Fatalf("warn 문구에 단계·수치·기준이 있어야 합니다: %q", warn)
	}
	crit := drawdownAlertLine(report.Status{MaxDrawdown: 0.095, Level: report.DrawdownCritical})
	if !strings.Contains(crit, "위험") || !strings.Contains(crit, "9.50%") {
		t.Fatalf("critical 문구 불일치: %q", crit)
	}
}
