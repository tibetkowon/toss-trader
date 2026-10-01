// Package simulator implements SPEC.md 6.1's self-contained fill simulator:
// given a stream of observed prices, it decides buy/stop-loss/end-of-day
// actions using internal/strategy's rules and tracks a virtual
// single-position portfolio. It never calls the real order API. The same
// engine drives both the backtester and the paper-trading loop so both use
// identical, conservative fill assumptions (fill at the actually-observed
// price, never the idealized target/stop price — SPEC.md 6.1).
package simulator

import (
	"math"

	"github.com/tibetkowon/toss-trader/internal/strategy"
)

// Config holds the fixed parameters the simulator enforces (SPEC.md 3.2/4.3).
// K is informational only here — TargetPrice is computed by the caller via
// internal/strategy and passed in through Setup, since it depends on
// per-symbol candle data the simulator itself doesn't fetch.
type Config struct {
	StopLossPct       float64 // e.g. 0.02 for -2% (SPEC.md 4.3)
	DailyLossLimitPct float64 // e.g. 0.05 for 5% of seed (SPEC.md 4.3)
	CommissionRate    float64 // from tossapi.Commission.CommissionRate (SPEC.md 6.1)
}

// Setup is the per-symbol, per-day information computed once before the
// session starts, from data through yesterday's close only (look-ahead
// safe — SPEC.md 3.1).
type Setup struct {
	Symbol      string
	TargetPrice float64
	TrendOK     bool
}

// ActionType classifies what, if anything, OnTick did.
type ActionType int

const (
	NoAction ActionType = iota
	Bought
	StoppedOut
	ClosedEndOfDay
	SkippedZeroShares
)

func (t ActionType) String() string {
	switch t {
	case NoAction:
		return "NoAction"
	case Bought:
		return "Bought"
	case StoppedOut:
		return "StoppedOut"
	case ClosedEndOfDay:
		return "ClosedEndOfDay"
	case SkippedZeroShares:
		return "SkippedZeroShares"
	default:
		return "Unknown"
	}
}

// Action reports the outcome of one OnTick call.
type Action struct {
	Type     ActionType
	Symbol   string
	Price    float64
	Shares   int
	Proceeds float64 // signed cash flow: negative for a buy, positive for a sell
	PnL      float64 // realized P&L net of commission; only meaningful for StoppedOut/ClosedEndOfDay
}

// Position is the simulator's single open holding, if any (SPEC.md 4.2: at
// most one concurrent position).
type Position struct {
	Symbol     string
	Shares     int
	EntryPrice float64
	// CostBasis is notional + buy-side commission actually paid — used as
	// the P&L baseline at close so buy-side commission isn't silently
	// dropped from realized P&L (and therefore from the daily loss limit).
	CostBasis float64
}

// Simulator tracks one trading day's virtual portfolio for a single account.
// It is not safe for concurrent use.
type Simulator struct {
	cfg               Config
	seed              float64 // SPEC.md 4.1's seed, snapshotted once at day start
	cash              float64
	position          *Position
	stoppedOutToday   map[string]bool
	consecutiveLosses int
	realizedPnLToday  float64
	currency          string
	fxRate            float64
}

// New creates a Simulator for one trading day. startingCash is SPEC.md
// 4.1's seed — the real account balance at the start of the day.
func New(cfg Config, startingCash float64) *Simulator {
	return &Simulator{
		cfg:             cfg,
		seed:            startingCash,
		cash:            startingCash,
		stoppedOutToday: make(map[string]bool),
	}
}

// Cash reports the current uninvested cash.
func (s *Simulator) Cash() float64 { return s.cash }

// Position reports the current open position, if any.
func (s *Simulator) Position() (Position, bool) {
	if s.position == nil {
		return Position{}, false
	}
	return *s.position, true
}

// RealizedPnLToday reports the sum of realized P&L (net of commission) from
// positions closed so far today.
func (s *Simulator) RealizedPnLToday() float64 { return s.realizedPnLToday }

// ConsecutiveLosses reports the current consecutive-losing-trade streak.
func (s *Simulator) ConsecutiveLosses() int { return s.consecutiveLosses }

// StoppedOutSymbols reports the symbols banned from re-entry today
// (SPEC.md 3.1), in no particular order.
func (s *Simulator) StoppedOutSymbols() []string {
	symbols := make([]string, 0, len(s.stoppedOutToday))
	for symbol := range s.stoppedOutToday {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// State is a snapshot of everything needed to resume a Simulator exactly —
// used to persist state across process restarts (SPEC.md 5.1: a crash must
// never silently lose the day's position/risk-counter state).
type State struct {
	Seed              float64
	Cash              float64
	Position          *Position
	StoppedOutSymbols []string
	ConsecutiveLosses int
	RealizedPnLToday  float64
	// Currency/FXRate는 이 세션의 금액 단위와, 세션 내내 고정해 쓰는 단위당 원화 환율입니다.
	// 비어 있으면(옛 저장분) 원화로 취급합니다.
	Currency string  `json:",omitempty"`
	FXRate   float64 `json:",omitempty"`
}

// KRWRate는 이 상태의 금액 한 단위가 몇 원인지 돌려줍니다.
func (st State) KRWRate() float64 {
	if st.FXRate > 0 {
		return st.FXRate
	}
	return 1
}

// State captures the current state for persistence.
func (s *Simulator) State() State {
	var position *Position
	if s.position != nil {
		copy := *s.position
		position = &copy
	}
	return State{
		Seed:              s.seed,
		Cash:              s.cash,
		Position:          position,
		StoppedOutSymbols: s.StoppedOutSymbols(),
		ConsecutiveLosses: s.consecutiveLosses,
		RealizedPnLToday:  s.RealizedPnLToday(),
		Currency:          s.currency,
		FXRate:            s.fxRate,
	}
}

// SetCurrency records the session's currency and fixed KRW rate so they persist with State.
func (s *Simulator) SetCurrency(currency string, fxRate float64) {
	s.currency, s.fxRate = currency, fxRate
}

// Restore reconstructs a Simulator from a previously saved State — the
// counterpart to State(), used on process restart (SPEC.md 5.1).
func Restore(cfg Config, state State) *Simulator {
	var position *Position
	if state.Position != nil {
		copy := *state.Position
		position = &copy
	}
	stoppedOutToday := make(map[string]bool, len(state.StoppedOutSymbols))
	for _, symbol := range state.StoppedOutSymbols {
		stoppedOutToday[symbol] = true
	}
	return &Simulator{
		cfg:               cfg,
		seed:              state.Seed,
		cash:              state.Cash,
		position:          position,
		stoppedOutToday:   stoppedOutToday,
		consecutiveLosses: state.ConsecutiveLosses,
		realizedPnLToday:  state.RealizedPnLToday,
		currency:          state.Currency,
		fxRate:            state.FXRate,
	}
}

// DailyPnL reports realized P&L today plus the open position's unrealized
// P&L at currentPriceOfHeld (ignored if there is no open position) — the
// figure SPEC.md 4.3's daily loss limit and SPEC.md 9's dashboard use.
func (s *Simulator) DailyPnL(currentPriceOfHeld float64) float64 {
	unrealized := 0.0
	if s.position != nil {
		unrealized = (currentPriceOfHeld - s.position.EntryPrice) * float64(s.position.Shares)
	}
	return s.realizedPnLToday + unrealized
}

// OnTick feeds one observed price for one symbol and returns what the
// simulator did, if anything.
//
// Callers must feed ticks in true chronological order. When multiple
// watchlist symbols could break out at effectively the same moment, the
// caller is responsible for tie-breaking by liquidity rank (SPEC.md 4.2)
// before calling OnTick — the simulator only ever acts on the first
// qualifying tick it's given, and once a position is open it ignores
// entry signals for every other symbol.
func (s *Simulator) OnTick(setup Setup, price float64, isEndOfDay bool) Action {
	if s.position != nil && s.position.Symbol == setup.Symbol {
		// Stop-loss takes priority over an end-of-day close if both are
		// somehow true on the same tick (SPEC.md 6.1: 손절 우선).
		if strategy.StopLossTriggered(s.position.EntryPrice, price, s.cfg.StopLossPct) {
			return s.closePosition(setup.Symbol, price, true)
		}
		if isEndOfDay {
			return s.closePosition(setup.Symbol, price, false)
		}
		return Action{Type: NoAction, Symbol: setup.Symbol, Price: price}
	}

	// Not our held symbol (or we're flat): end-of-day ticks need no action,
	// and holding a different symbol already blocks any new entry.
	if isEndOfDay || s.position != nil {
		return Action{Type: NoAction, Symbol: setup.Symbol, Price: price}
	}

	if strategy.DailyLossLimitExceeded(s.realizedPnLToday, s.seed, s.cfg.DailyLossLimitPct) ||
		strategy.HaltForConsecutiveLosses(s.consecutiveLosses) ||
		s.stoppedOutToday[setup.Symbol] ||
		!setup.TrendOK ||
		price < setup.TargetPrice {
		return Action{Type: NoAction, Symbol: setup.Symbol, Price: price}
	}

	return s.openPosition(setup.Symbol, price)
}

// openPosition sizes the buy as all available cash (SPEC.md 4.2: 균등분할 ÷
// 최대동시보유(1) = 전액) and fills at the observed price plus commission.
func (s *Simulator) openPosition(symbol string, price float64) Action {
	shares := int(math.Floor(s.cash / (price * (1 + s.cfg.CommissionRate))))
	if shares <= 0 {
		return Action{Type: SkippedZeroShares, Symbol: symbol, Price: price}
	}
	notional := price * float64(shares)
	commission := notional * s.cfg.CommissionRate
	cost := notional + commission
	s.cash -= cost
	s.position = &Position{Symbol: symbol, Shares: shares, EntryPrice: price, CostBasis: cost}
	return Action{Type: Bought, Symbol: symbol, Price: price, Shares: shares, Proceeds: -cost}
}

func (s *Simulator) closePosition(symbol string, price float64, isStopLoss bool) Action {
	pos := s.position
	notional := price * float64(pos.Shares)
	commission := notional * s.cfg.CommissionRate
	proceeds := notional - commission
	pnl := proceeds - pos.CostBasis

	s.cash += proceeds
	s.realizedPnLToday += pnl
	s.position = nil
	if pnl < 0 {
		s.consecutiveLosses++
	} else {
		s.consecutiveLosses = 0
	}

	actionType := ClosedEndOfDay
	if isStopLoss {
		actionType = StoppedOut
		s.stoppedOutToday[symbol] = true
	}
	return Action{Type: actionType, Symbol: symbol, Price: price, Shares: pos.Shares, Proceeds: proceeds, PnL: pnl}
}
