package main

import (
	"testing"
	"time"
)

func TestDetectMarket(t *testing.T) {
	kst, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		time time.Time
		want string
	}{
		{"국장 기동 시각(08:50 KST)", time.Date(2026, 9, 28, 8, 50, 0, 0, kst), "KR"},
		{"국장 정규장 도중(12:00 KST)", time.Date(2026, 9, 28, 12, 0, 0, 0, kst), "KR"},
		{"미장 기동 시각(22:20 KST)", time.Date(2026, 9, 28, 22, 20, 0, 0, kst), "US"},
		{"미장 정규장 새벽(03:00 KST)", time.Date(2026, 9, 28, 3, 0, 0, 0, kst), "US"},
		{"국장 백스톱 직후(16:00 KST)", time.Date(2026, 9, 28, 16, 0, 0, 0, kst), "KR"},
		{"경계값(05:59 KST -> US)", time.Date(2026, 9, 28, 5, 59, 0, 0, kst), "US"},
		{"경계값(06:00 KST -> KR)", time.Date(2026, 9, 28, 6, 0, 0, 0, kst), "KR"},
		{"경계값(17:59 KST -> KR)", time.Date(2026, 9, 28, 17, 59, 0, 0, kst), "KR"},
		{"경계값(18:00 KST -> US)", time.Date(2026, 9, 28, 18, 0, 0, 0, kst), "US"},
		{"다른 타임존 입력도 KST로 변환해서 판단", time.Date(2026, 9, 28, 3, 50, 0, 0, time.UTC), "KR"}, // 03:50 UTC = 12:50 KST
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := detectMarket(c.time); got != c.want {
				t.Errorf("detectMarket(%v) = %q, want %q", c.time, got, c.want)
			}
		})
	}
}
