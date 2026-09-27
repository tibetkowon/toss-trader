package strategy

// StopLossTriggered reports whether the current price has fallen far enough
// from the entry price to trigger a stop-loss. stopLossPct is a positive
// fraction (e.g. 0.02 for -2%).
func StopLossTriggered(entryPrice, currentPrice, stopLossPct float64) bool {
	return currentPrice <= entryPrice*(1-stopLossPct)
}

// DailyLossLimitExceeded reports whether today's P&L (realized + unrealized
// + costs, as a signed value where losses are negative) has breached the
// daily loss limit, expressed as a fraction of the seed (e.g. 0.05 for 5%
// of the seed). Only losses count — a positive dailyPnL never exceeds the
// limit.
func DailyLossLimitExceeded(dailyPnL, seed, limitPct float64) bool {
	if dailyPnL >= 0 {
		return false
	}
	return -dailyPnL > seed*limitPct
}

// ConsecutiveLossHaltThreshold is the number of consecutive losing trades
// that halts new entries for the rest of the trading day (SPEC: 2).
const ConsecutiveLossHaltThreshold = 2

// HaltForConsecutiveLosses reports whether new entries should be halted for
// the rest of the day given the count of consecutive losing trades so far.
func HaltForConsecutiveLosses(consecutiveLosses int) bool {
	return consecutiveLosses >= ConsecutiveLossHaltThreshold
}
