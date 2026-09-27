// Package tradingloop holds the testable core of the live polling loop
// (SPEC.md 5.1): turning one round of polled prices into simulator actions,
// with the staleness/error guards SPEC.md 5's kill-switch requires. The
// actual network polling and sleep loop live in cmd/trader, which this
// package has no dependency on.
package tradingloop

import (
	"time"

	"github.com/tibetkowon/toss-trader/internal/simulator"
)

// PriceObservation is one polled price for one symbol. Err is set when
// fetching that symbol's price failed this tick; Timestamp is the price's
// own reported time (not the poll time), used for staleness detection.
type PriceObservation struct {
	Symbol    string
	Price     float64
	Timestamp time.Time
	Err       error
}

// ProcessTick feeds one polling round's observations into sim, in the order
// given — callers must pre-sort observations by watchlist/liquidity rank
// (SPEC.md 4.2) so that when multiple symbols qualify at once, the
// higher-ranked one is tried first. An observation is skipped (no effect on
// sim) when it errored, when no Setup exists for its symbol, or when it's
// older than maxAge (SPEC.md 5: stale prices must not drive new decisions —
// applied here to every action, not just new entries, since acting on
// stale data is not obviously safer for exits either).
//
// Returns every action sim actually took (Type != simulator.NoAction), in
// order, for the caller to persist/publish.
func ProcessTick(sim *simulator.Simulator, setups map[string]simulator.Setup, observations []PriceObservation, now time.Time, maxAge time.Duration) []simulator.Action {
	var actions []simulator.Action
	for _, obs := range observations {
		if obs.Err != nil {
			continue
		}
		if now.Sub(obs.Timestamp) > maxAge {
			continue
		}
		setup, ok := setups[obs.Symbol]
		if !ok {
			continue
		}
		if action := sim.OnTick(setup, obs.Price, false); action.Type != simulator.NoAction {
			actions = append(actions, action)
		}
	}
	return actions
}
