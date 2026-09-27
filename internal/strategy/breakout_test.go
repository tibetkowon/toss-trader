package strategy

import "testing"

func TestTargetPrice(t *testing.T) {
	tests := []struct {
		name      string
		todayOpen float64
		prevHigh  float64
		prevLow   float64
		k         float64
		want      float64
	}{
		{name: "기본 계수", todayOpen: 100, prevHigh: 120, prevLow: 80, k: 0.5, want: 120},
		{name: "다른 계수", todayOpen: 200, prevHigh: 240, prevLow: 200, k: 0.25, want: 210},
		{name: "변동폭 없음", todayOpen: 150, prevHigh: 100, prevLow: 100, k: 0.5, want: 150},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TargetPrice(tt.todayOpen, tt.prevHigh, tt.prevLow, tt.k); got != tt.want {
				t.Errorf("TargetPrice() = %v, 기대값 %v", got, tt.want)
			}
		})
	}
}

func TestMovingAverage(t *testing.T) {
	tests := []struct {
		name    string
		closes  []float64
		want    float64
		wantErr bool
	}{
		{name: "일반", closes: []float64{10, 20, 30}, want: 20},
		{name: "단일 원소", closes: []float64{15}, want: 15},
		{name: "빈 슬라이스", closes: []float64{}, wantErr: true},
		{name: "nil 슬라이스", closes: nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MovingAverage(tt.closes)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MovingAverage() 오류 = %v, 오류 기대 여부 %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("MovingAverage() = %v, 기대값 %v", got, tt.want)
			}
		})
	}
}

func TestTrendFilterPasses(t *testing.T) {
	tests := []struct {
		name          string
		prevClose     float64
		movingAverage float64
		want          bool
	}{
		{name: "이동평균 초과", prevClose: 21, movingAverage: 20, want: true},
		{name: "이동평균 미만", prevClose: 19, movingAverage: 20, want: false},
		{name: "이동평균과 동일", prevClose: 20, movingAverage: 20, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TrendFilterPasses(tt.prevClose, tt.movingAverage); got != tt.want {
				t.Errorf("TrendFilterPasses() = %v, 기대값 %v", got, tt.want)
			}
		})
	}
}
