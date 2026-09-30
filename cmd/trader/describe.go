package main

import (
	"fmt"

	"github.com/tibetkowon/toss-trader/internal/screener"
)

func describeEntry(e screener.Entry) string {
	switch {
	case !e.Passed:
		return fmt.Sprintf("%s 평가 탈락(%s): %s", e.Symbol, e.Origin, e.Reason)
	case e.HasOpen:
		return fmt.Sprintf("%s 셋업: 시가=%.2f 전일(%s) 고저=%.2f/%.2f 종가=%.2f 노이즈=%.2f%% 추세=%v 목표가=%.2f (%s)",
			e.Symbol, e.Open, e.Prev.Date, e.Prev.High, e.Prev.Low, e.Prev.Close, e.NoiseRatio*100, e.TrendOK, e.Target, e.Origin)
	default:
		return fmt.Sprintf("%s 평가 통과, 정규장 시가 대기(%s): 노이즈=%.2f%%", e.Symbol, e.Origin, e.NoiseRatio*100)
	}
}
