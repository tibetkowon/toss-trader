package strategy

import "errors"

// TargetPrice computes the breakout entry price: today's open plus k times
// yesterday's high-low range. If the current price crosses this during the
// session, a buy signal fires.
func TargetPrice(todayOpen, prevHigh, prevLow, k float64) float64 {
	return todayOpen + (prevHigh-prevLow)*k
}

// MovingAverage computes the simple moving average of closes. closes must
// already end at "yesterday" — callers must never include today's close
// (that would be look-ahead bias). Returns an error if closes is empty.
func MovingAverage(closes []float64) (float64, error) {
	if len(closes) == 0 {
		return 0, errors.New("closes가 비어 있습니다")
	}
	sum := 0.0
	for _, c := range closes {
		sum += c
	}
	return sum / float64(len(closes)), nil
}

// TrendFilterPasses reports whether yesterday's close was above the trend
// moving average — the look-ahead-safe entry filter. Only yesterday's data
// is used, never today's.
func TrendFilterPasses(prevClose, movingAverage float64) bool {
	return prevClose > movingAverage
}
