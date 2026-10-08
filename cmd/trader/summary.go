package main

import (
	"fmt"

	"github.com/tibetkowon/toss-trader/internal/report"
	"github.com/tibetkowon/toss-trader/internal/session"
	"github.com/tibetkowon/toss-trader/internal/snapshot"
)

// drawdownLimit은 SPEC.md 6.2의 검증 기준 최대 낙폭입니다.
const drawdownLimit = 0.10

// buildDaySummary는 저장된 세션 기록(오늘 세션 포함)으로 하루 결과와 계좌 누적 상태를 만듭니다.
// 낙폭은 KR/US를 합친 계좌 곡선 기준입니다(report.BuildAccountCurve).
func buildDaySummary(records []session.DailyRecord, trades int, dayPnLKRW float64) snapshot.DaySummary {
	st := report.Assess(report.BuildAccountCurve(records))
	level := st.Level
	if level == "" {
		level = report.DrawdownOK
	}
	return snapshot.DaySummary{
		Trades:           trades,
		DayPnLKRW:        dayPnLKRW,
		AccountEquityKRW: st.Equity,
		CumulativeReturn: st.CumulativeReturn,
		CurrentDrawdown:  st.CurrentDrawdown,
		MaxDrawdown:      st.MaxDrawdown,
		DrawdownLevel:    string(level),
	}
}

// drawdownAlertLine은 낙폭이 경고 단계 이상일 때만 로그 한 줄을 돌려줍니다(정상이면 빈 문자열).
// 푸시 알림 채널이 없으므로 journald 로그와 대시보드 요약이 알림 수단입니다.
func drawdownAlertLine(st report.Status) string {
	var tag string
	switch st.Level {
	case report.DrawdownWarn:
		tag = "낙폭 경고"
	case report.DrawdownCritical:
		tag = "낙폭 위험"
	default:
		return ""
	}
	return fmt.Sprintf("[%s] 최대 낙폭 %.2f%% (현재 %.2f%%) — 검증 기준 %.0f%%까지 %.2f%%p 남았습니다",
		tag, st.MaxDrawdown*100, st.CurrentDrawdown*100, drawdownLimit*100, (drawdownLimit-st.MaxDrawdown)*100)
}

// summaryLine은 마감 로그용 한 줄 요약입니다.
func summaryLine(s snapshot.DaySummary) string {
	return fmt.Sprintf("일일 요약: 체결 %d건, 일 손익 %.0f원, 계좌 자산 %.0f원(누적 %+.2f%%), 최대 낙폭 %.2f%%(현재 %.2f%%)",
		s.Trades, s.DayPnLKRW, s.AccountEquityKRW, s.CumulativeReturn*100, s.MaxDrawdown*100, s.CurrentDrawdown*100)
}
