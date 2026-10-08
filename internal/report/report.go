// Package report computes day-over-day account performance (equity curve,
// max drawdown) from internal/session's persisted daily state — the tool
// SPEC.md 11 flags as missing for the 4-week paper-trading verification.
package report

import (
	"sort"

	"github.com/tibetkowon/toss-trader/internal/session"
)

// EquityPoint is one day's ending account equity for one market, in KRW
// (USD sessions are converted at the session's fixed rate).
type EquityPoint struct {
	Date   string
	Equity float64
}

// StartLabel is the Date of the synthetic first point holding the starting seed.
const StartLabel = "시작"

// BuildEquityCurve turns raw daily records (already ordered by market then
// date — see Store.All) into an equity curve, each ordered by date
// ascending. When the first record knows its starting seed, the curve opens
// with that seed so a loss on day one counts toward the drawdown (otherwise
// the first day's close would be mistaken for the peak).
func BuildEquityCurve(records []session.DailyRecord) []EquityPoint {
	points := make([]EquityPoint, 0, len(records)+1)
	if len(records) > 0 && records[0].State.Seed > 0 {
		points = append(points, EquityPoint{Date: StartLabel, Equity: records[0].State.Seed * records[0].State.KRWRate()})
	}
	for _, r := range records {
		equity := r.State.Cash
		if r.State.Position != nil {
			equity += r.State.Position.CostBasis
		}
		points = append(points, EquityPoint{Date: r.Date, Equity: equity * r.State.KRWRate()})
	}
	return points
}

// MaxDrawdown returns the largest peak-to-trough decline across points, as
// a fraction of the peak (e.g. 0.1 for -10%). Fewer than two points can't
// show a drawdown, so it returns 0.
func MaxDrawdown(points []EquityPoint) float64 {
	if len(points) < 2 {
		return 0
	}
	peak := points[0].Equity
	maxDD := 0.0
	for _, p := range points[1:] {
		if p.Equity > peak {
			peak = p.Equity
			continue
		}
		if peak <= 0 {
			continue
		}
		if dd := (peak - p.Equity) / peak; dd > maxDD {
			maxDD = dd
		}
	}
	return maxDD
}

// BuildAccountCurve는 KR/US 기록을 하나의 원화 계좌 자산 곡선으로 합칩니다. 두 시장은 같은 원화
// 계좌를 공유하므로(직전 세션의 원화 잔고가 다음 세션의 시드) 시장별로 따로 보면 서로의 손익이
// 빠집니다. 날짜순(같은 날은 KR 다음 US)으로 이어 붙이고 점 이름은 "날짜 시장"입니다.
//
// 통화가 비어 있는 US 기록은 계좌 공유 이전(2026-10-01 전)에 별도 시드 100000원으로 따로 돌던
// 독립 세션이라 합산에서 제외합니다. KR 기록은 처음부터 계좌 잔고를 이어 왔으므로 모두 씁니다.
func BuildAccountCurve(records []session.DailyRecord) []EquityPoint {
	shared := make([]session.DailyRecord, 0, len(records))
	for _, r := range records {
		if r.Market == "US" && r.State.Currency == "" {
			continue
		}
		shared = append(shared, r)
	}
	sort.SliceStable(shared, func(i, j int) bool {
		if shared[i].Date != shared[j].Date {
			return shared[i].Date < shared[j].Date
		}
		return shared[i].Market < shared[j].Market
	})
	points := BuildEquityCurve(shared)
	// 맨 앞 시작점(StartLabel)은 그대로 두고, 나머지 점에 시장 이름을 붙입니다.
	offset := len(points) - len(shared)
	for i, r := range shared {
		points[offset+i].Date = r.Date + " " + r.Market
	}
	return points
}

// DrawdownLevel은 최대 낙폭이 SPEC.md 6.2의 10% 기준에 얼마나 가까운지를 나타냅니다.
type DrawdownLevel string

const (
	DrawdownOK       DrawdownLevel = "ok"
	DrawdownWarn     DrawdownLevel = "warn"
	DrawdownCritical DrawdownLevel = "critical"
)

// 경고 임계치. 10% 기준을 넘고 나서 알면 이미 검증이 끝난 뒤라, 그 전에 미리 알리기 위한 값입니다.
const (
	DrawdownWarnAt     = 0.08
	DrawdownCriticalAt = 0.09
)

// LevelFor는 최대 낙폭(비율)의 경고 단계를 돌려줍니다.
func LevelFor(maxDrawdown float64) DrawdownLevel {
	switch {
	case maxDrawdown >= DrawdownCriticalAt:
		return DrawdownCritical
	case maxDrawdown >= DrawdownWarnAt:
		return DrawdownWarn
	default:
		return DrawdownOK
	}
}

// Status는 자산 곡선의 현재 상태 요약입니다. 모든 비율은 소수(0.1 = 10%)입니다.
type Status struct {
	Equity           float64
	Peak             float64
	CumulativeReturn float64 // 첫 점(시작 시드) 대비 현재 자산
	CurrentDrawdown  float64 // 고점 대비 현재 자산
	MaxDrawdown      float64
	Level            DrawdownLevel // MaxDrawdown 기준(한 번 넘으면 검증 기준에서 되돌릴 수 없으므로)
}

// Assess는 곡선의 마지막 점을 기준으로 현재 상태를 요약합니다. 빈 곡선은 영값을 돌려줍니다.
func Assess(points []EquityPoint) Status {
	if len(points) == 0 {
		return Status{}
	}
	st := Status{Equity: points[len(points)-1].Equity, Peak: points[0].Equity}
	for _, p := range points[1:] {
		if p.Equity > st.Peak {
			st.Peak = p.Equity
		}
	}
	if first := points[0].Equity; first > 0 {
		st.CumulativeReturn = st.Equity/first - 1
	}
	if st.Peak > 0 && st.Equity < st.Peak {
		st.CurrentDrawdown = (st.Peak - st.Equity) / st.Peak
	}
	st.MaxDrawdown = MaxDrawdown(points)
	st.Level = LevelFor(st.MaxDrawdown)
	return st
}
