package backtest

import (
	"math"
	"time"
)

// Trade는 진입부터 청산까지 한 건의 거래입니다. 금액 단위는 시장 통화(US는 달러)입니다.
type Trade struct {
	Symbol                string
	EntryTime, ExitTime   time.Time
	EntryPrice, ExitPrice float64
	Shares                float64
	Cost                  float64 // 매수 대금 + 매수 수수료
	PnL                   float64 // 비용 차감 후 실현손익
	Reason                string  // StoppedOut | ClosedEndOfDay
}

// Return은 거래 하나의 비용 포함 수익률(소수)입니다.
func (t Trade) Return() float64 {
	if t.Cost == 0 {
		return 0
	}
	return t.PnL / t.Cost
}

// TradeStats는 거래 목록의 요약입니다. 수익률은 모두 소수(0.01 = 1%)입니다.
type TradeStats struct {
	Trades, Wins, Losses int
	WinRate              float64
	AvgWin, AvgLoss      float64 // AvgLoss는 음수
	BreakevenWinRate     float64 // 평균 수익·손실 기준 본전 승률. 수익이나 손실이 없으면 0
	Expectancy           float64 // 거래당 평균 수익률
}

func summarizeTrades(trades []Trade) TradeStats {
	s := TradeStats{Trades: len(trades)}
	var winSum, lossSum, all float64
	for _, t := range trades {
		r := t.Return()
		all += r
		if t.PnL > 0 {
			s.Wins++
			winSum += r
		} else {
			s.Losses++
			lossSum += r
		}
	}
	if s.Trades == 0 {
		return s
	}
	s.WinRate = float64(s.Wins) / float64(s.Trades)
	s.Expectancy = all / float64(s.Trades)
	if s.Wins > 0 {
		s.AvgWin = winSum / float64(s.Wins)
	}
	if s.Losses > 0 {
		s.AvgLoss = lossSum / float64(s.Losses)
	}
	if s.AvgWin > 0 && s.AvgLoss < 0 {
		s.BreakevenWinRate = -s.AvgLoss / (s.AvgWin - s.AvgLoss)
	}
	return s
}

// Benchmark는 같은 구간의 단순 보유 수익률입니다(첫날 정규장 첫 봉 시가 → 마지막날 마지막 봉 종가).
type Benchmark struct {
	Symbol string
	Return float64
}

func replayBenchmark(src *HistSource, days []string) *Benchmark {
	symbol := "069500"
	if src.market == "US" {
		symbol = "SPY"
	}
	x := src.idx[symbol]
	if x == nil || len(days) == 0 {
		return nil // 유니버스에 없으면 생략
	}
	bars := src.data[symbol].Minutes
	var first, last float64
	var firstT, lastT time.Time
	for i := range bars { // 최신순
		t := x.minT[i]
		d := t.In(src.loc).Format("2006-01-02")
		if d < days[0] || d > days[len(days)-1] || !src.regular(t) {
			continue
		}
		if lastT.IsZero() || t.After(lastT) {
			lastT, last = t, parseNum(bars[i].ClosePrice)
		}
		if firstT.IsZero() || t.Before(firstT) {
			firstT, first = t, parseNum(bars[i].OpenPrice)
		}
	}
	if first <= 0 || last <= 0 || math.IsNaN(first) || math.IsNaN(last) {
		return nil
	}
	return &Benchmark{Symbol: symbol, Return: last/first - 1}
}
