// Package report computes day-over-day account performance (equity curve,
// max drawdown) from internal/session's persisted daily state — the tool
// SPEC.md 11 flags as missing for the 4-week paper-trading verification.
package report

import "github.com/tibetkowon/toss-trader/internal/session"

// EquityPoint is one day's ending account equity for one market.
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
		points = append(points, EquityPoint{Date: StartLabel, Equity: records[0].State.Seed})
	}
	for _, r := range records {
		equity := r.State.Cash
		if r.State.Position != nil {
			equity += r.State.Position.CostBasis
		}
		points = append(points, EquityPoint{Date: r.Date, Equity: equity})
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
