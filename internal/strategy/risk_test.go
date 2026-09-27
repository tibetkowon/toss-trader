package strategy

import "testing"

func TestStopLossTriggered(t *testing.T) {
	tests := []struct {
		name         string
		entryPrice   float64
		currentPrice float64
		stopLossPct  float64
		want         bool
	}{
		{name: "손절 기준과 동일", entryPrice: 100, currentPrice: 98, stopLossPct: 0.02, want: true},
		{name: "손절 기준 직전", entryPrice: 100, currentPrice: 98.01, stopLossPct: 0.02, want: false},
		{name: "손절 기준보다 크게 하락", entryPrice: 100, currentPrice: 80, stopLossPct: 0.02, want: true},
		{name: "매수가보다 상승", entryPrice: 100, currentPrice: 110, stopLossPct: 0.02, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StopLossTriggered(tt.entryPrice, tt.currentPrice, tt.stopLossPct); got != tt.want {
				t.Errorf("StopLossTriggered() = %v, 기대값 %v", got, tt.want)
			}
		})
	}
}

func TestDailyLossLimitExceeded(t *testing.T) {
	tests := []struct {
		name     string
		dailyPnL float64
		seed     float64
		limitPct float64
		want     bool
	}{
		{name: "손실 한도와 동일", dailyPnL: -50, seed: 1000, limitPct: 0.05, want: false},
		{name: "손실 한도 소폭 초과", dailyPnL: -50.01, seed: 1000, limitPct: 0.05, want: true},
		{name: "작은 수익", dailyPnL: 1, seed: 1000, limitPct: 0.05, want: false},
		{name: "시드보다 큰 수익", dailyPnL: 1000000, seed: 1000, limitPct: 0.05, want: false},
		{name: "손익 없음", dailyPnL: 0, seed: 1000, limitPct: 0.05, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DailyLossLimitExceeded(tt.dailyPnL, tt.seed, tt.limitPct); got != tt.want {
				t.Errorf("DailyLossLimitExceeded() = %v, 기대값 %v", got, tt.want)
			}
		})
	}
}

func TestHaltForConsecutiveLosses(t *testing.T) {
	tests := []struct {
		name              string
		consecutiveLosses int
		want              bool
	}{
		{name: "연속 손실 없음", consecutiveLosses: 0, want: false},
		{name: "연속 손실 1회", consecutiveLosses: 1, want: false},
		{name: "연속 손실 2회", consecutiveLosses: 2, want: true},
		{name: "연속 손실 3회", consecutiveLosses: 3, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HaltForConsecutiveLosses(tt.consecutiveLosses); got != tt.want {
				t.Errorf("HaltForConsecutiveLosses() = %v, 기대값 %v", got, tt.want)
			}
		})
	}
}
