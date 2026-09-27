package strategy

// DailyBar is one day's OHLC for one symbol.
type DailyBar struct {
	Date                   string
	Open, High, Low, Close float64
}

// ComputeDaySetup derives today's breakout target price and trend-filter
// result from yesterday's data only (SPEC.md 3.1 — never today's own
// high/low/close, only its open, which is known before the session starts).
// closesEndingYesterday must be the trailing window of closes ending at
// yesterday's close (oldest first or any order — MovingAverage doesn't
// care), sized to the configured MA window.
func ComputeDaySetup(todayOpen float64, yesterday DailyBar, closesEndingYesterday []float64, k float64) (targetPrice float64, trendOK bool, err error) {
	ma, err := MovingAverage(closesEndingYesterday)
	if err != nil {
		return 0, false, err
	}
	targetPrice = TargetPrice(todayOpen, yesterday.High, yesterday.Low, k)
	trendOK = TrendFilterPasses(yesterday.Close, ma)
	return targetPrice, trendOK, nil
}
