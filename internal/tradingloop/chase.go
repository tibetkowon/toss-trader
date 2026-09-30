package tradingloop

import (
	"time"

	"github.com/tibetkowon/toss-trader/internal/simulator"
)

// ChaseGuard는 처음 관찰한 가격이 이미 목표가를 limitPct 넘게 웃도는 종목을 당일 진입 후보에서
// 영구히 제외합니다. 시뮬레이터 앞단에서 관찰값만 거르므로 청산에는 관여하지 않습니다.
type ChaseGuard struct {
	limitPct float64
	blocked  map[string]bool
}

func NewChaseGuard(limitPct float64) *ChaseGuard {
	return &ChaseGuard{limitPct: limitPct, blocked: map[string]bool{}}
}

func (c *ChaseGuard) Blocked(symbol string) bool { return c.blocked[symbol] }

func (c *ChaseGuard) Filter(obs []PriceObservation, setups map[string]simulator.Setup, holding bool, now time.Time, maxAge time.Duration) (kept []PriceObservation, newlyBlocked []string) {
	if c.limitPct <= 0 || holding {
		return obs, nil
	}
	kept = make([]PriceObservation, 0, len(obs))
	for _, o := range obs {
		if c.blocked[o.Symbol] {
			continue
		}
		if o.Err != nil || now.Sub(o.Timestamp) > maxAge {
			kept = append(kept, o)
			continue
		}
		setup, ok := setups[o.Symbol]
		if ok && setup.TrendOK && o.Price > setup.TargetPrice*(1+c.limitPct) {
			c.blocked[o.Symbol] = true
			newlyBlocked = append(newlyBlocked, o.Symbol)
			continue
		}
		kept = append(kept, o)
	}
	return kept, newlyBlocked
}
