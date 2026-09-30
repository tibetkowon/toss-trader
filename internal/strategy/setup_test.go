package strategy

import "testing"

func TestComputeDaySetup(t *testing.T) {
	yesterday := DailyBar{Date: "2026-09-26", Open: 990, High: 1020, Low: 980, Close: 1010}
	closes := []float64{1000, 1005, 1010, 1010, 1010} // MA = 1007

	target, trendOK, err := ComputeDaySetup(1000, yesterday, closes, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	wantTarget := 1000 + (1020-980)*0.5 // = 1020
	if target != wantTarget {
		t.Fatalf("목표가 불일치: got %v want %v", target, wantTarget)
	}
	if !trendOK {
		t.Fatalf("전일 종가(1010) > MA(1007)인데 추세 필터가 false입니다")
	}
}

func TestComputeDaySetupTrendFails(t *testing.T) {
	yesterday := DailyBar{Date: "2026-09-26", Open: 990, High: 1020, Low: 980, Close: 990}
	closes := []float64{1010, 1010, 1010, 1010, 1010} // MA = 1010, 990 < 1010

	_, trendOK, err := ComputeDaySetup(1000, yesterday, closes, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if trendOK {
		t.Fatal("전일 종가가 MA보다 낮은데 추세 필터를 통과했습니다")
	}
}

func TestComputeDaySetupEmptyClosesErrors(t *testing.T) {
	yesterday := DailyBar{Date: "2026-09-26", Open: 990, High: 1020, Low: 980, Close: 1010}
	if _, _, err := ComputeDaySetup(1000, yesterday, nil, 0.5); err == nil {
		t.Fatal("빈 closes를 허용했습니다")
	}
}

func TestSplitBarsSeparatesTodayFromPrior(t *testing.T) {
	bars := []DailyBar{ // API returns newest first, and includes today's still-forming bar
		{Date: "2026-09-30", Open: 100, High: 101, Low: 100, Close: 101},
		{Date: "2026-09-29", Open: 90, High: 110, Low: 85, Close: 100},
		{Date: "2026-09-28", Open: 80, High: 95, Low: 78, Close: 90},
	}

	today, prior := SplitBars(bars, "2026-09-30")

	if today == nil || today.Open != 100 {
		t.Fatalf("오늘 봉을 찾지 못했습니다: %+v", today)
	}
	if len(prior) != 2 || prior[0].Date != "2026-09-29" || prior[1].Date != "2026-09-28" {
		t.Fatalf("어제 이전 봉이 잘못 분리됨: %+v", prior)
	}
}

func TestSplitBarsWithoutTodayBar(t *testing.T) {
	bars := []DailyBar{
		{Date: "2026-09-29", Close: 100},
		{Date: "2026-09-28", Close: 90},
	}

	today, prior := SplitBars(bars, "2026-09-30")

	if today != nil {
		t.Fatalf("오늘 봉이 없는데 %+v를 반환했습니다", today)
	}
	if len(prior) != 2 || prior[0].Date != "2026-09-29" {
		t.Fatalf("prior가 잘못됨: %+v", prior)
	}
}
