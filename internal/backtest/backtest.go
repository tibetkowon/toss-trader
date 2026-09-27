// Package backtest replays historical price data through the same
// simulator.Simulator engine the paper-trading loop uses, so backtest and
// paper trading share identical fill logic (SPEC.md 6.1). It contains no
// network code — callers fetch candles via internal/tossapi and translate
// them into the types here.
package backtest

import (
	"time"

	"github.com/tibetkowon/toss-trader/internal/simulator"
)

// IntradayTick is one observed price for one symbol at one moment within a
// trading day, in true chronological order. When multiple symbols could
// break out at the same timestamp, the caller resolves the tie by ordering
// same-timestamp ticks by liquidity rank (SPEC.md 4.2) before calling Run.
type IntradayTick struct {
	Symbol     string
	Time       time.Time
	Price      float64
	IsEndOfDay bool
}

// DayReport summarizes one simulated trading day. EndCash is also that
// day's ending equity, since SPEC.md 3.1 never carries a position
// overnight.
type DayReport struct {
	Date      string
	StartCash float64
	EndCash   float64
	Actions   []simulator.Action
}

// Report is the full multi-day backtest result.
type Report struct {
	Days           []DayReport
	StartingCash   float64
	FinalEquity    float64
	MaxDrawdownPct float64 // fraction of the running peak equity, e.g. 0.10 = 10%
}

// Run simulates the watchlist across dates in order. dailySetups maps
// date -> symbol -> Setup, precomputed by the caller from the PRIOR day's
// candles (SPEC.md 3.1's look-ahead-safe target price and trend filter —
// Run itself never looks at future data, it only plays back what it's
// given). intraday maps date -> chronologically sorted ticks across every
// watchlist symbol for that date.
//
// A fresh Simulator is created for each date so SPEC.md 4.3's per-day
// resets (당일 재진입 금지, 연속 손실 카운트, 일일 손실 한도) never leak
// across days, while cash carries forward as SPEC.md 4.1's compounding
// seed.
func Run(cfg simulator.Config, startingCash float64, dates []string, dailySetups map[string]map[string]simulator.Setup, intraday map[string][]IntradayTick) Report {
	cash := startingCash
	peak := startingCash
	maxDD := 0.0
	days := make([]DayReport, 0, len(dates))

	for _, date := range dates {
		startCash := cash
		sim := simulator.New(cfg, cash)
		setups := dailySetups[date]

		var actions []simulator.Action
		var lastPriceBySymbol map[string]float64
		for _, tick := range intraday[date] {
			setup, ok := setups[tick.Symbol]
			if !ok {
				continue
			}
			if lastPriceBySymbol == nil {
				lastPriceBySymbol = make(map[string]float64)
			}
			lastPriceBySymbol[tick.Symbol] = tick.Price
			if action := sim.OnTick(setup, tick.Price, tick.IsEndOfDay); action.Type != simulator.NoAction {
				actions = append(actions, action)
			}
		}

		// Safety net: force-close anything still open at the last price we
		// saw for it, so a day with incomplete EOD data never lets state
		// (or unrealized P&L) leak into the next day's fresh Simulator.
		if pos, ok := sim.Position(); ok {
			price := pos.EntryPrice
			if p, ok := lastPriceBySymbol[pos.Symbol]; ok {
				price = p
			}
			if action := sim.OnTick(setups[pos.Symbol], price, true); action.Type != simulator.NoAction {
				actions = append(actions, action)
			}
		}

		cash = sim.Cash()
		if cash > peak {
			peak = cash
		}
		if peak > 0 {
			if dd := (peak - cash) / peak; dd > maxDD {
				maxDD = dd
			}
		}
		days = append(days, DayReport{Date: date, StartCash: startCash, EndCash: cash, Actions: actions})
	}

	return Report{Days: days, StartingCash: startingCash, FinalEquity: cash, MaxDrawdownPct: maxDD}
}
