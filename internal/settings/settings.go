// Package settings는 웹에서 수정하는 트레이더 설정을 다룹니다(SPEC.md 9.2).
// 값은 Firestore 설정 문서에서 읽고, 실패하면 마지막으로 정상 적용된 값을 쓰며,
// 그것도 없으면 기본값을 씁니다. 범위 검증은 웹 화면과 Firestore 보안 규칙에도 같은 값으로 적용합니다.
package settings

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Settings는 웹에서 수정할 수 있는 값입니다. 비율은 소수(0.02 = 2%)입니다.
type Settings struct {
	StopLossPct          float64 `json:"stop_loss_pct"`
	DailyLossLimitPct    float64 `json:"daily_loss_limit_pct"`
	K                    float64 `json:"k"`
	MAWindow             int     `json:"ma_window"`
	NoiseWindow          int     `json:"noise_window"`
	NoiseMin             float64 `json:"noise_min"`
	NoiseMax             float64 `json:"noise_max"`
	MinAffordable        int     `json:"min_affordable"`
	RankDepth            int     `json:"rank_depth"`
	ActiveCount          int     `json:"active_count"`
	ActiveKeepRank       int     `json:"active_keep_rank"`
	EvalPerMin           int     `json:"eval_per_min"`
	LazyExpand           bool    `json:"lazy_expand"`
	RankStartDelayMin    int     `json:"rank_start_delay_minutes"`
	RankRefreshSec       int     `json:"rank_refresh_seconds"`
	ChaseLimitPct        float64 `json:"chase_limit_pct"`
	PollIntervalSec      int     `json:"poll_interval_seconds"`
	HeartbeatIntervalSec int     `json:"heartbeat_interval_seconds"`
	EODBufferMin         int     `json:"eod_buffer_minutes"`
}

// Defaults는 설정 문서가 없을 때 쓰는 값입니다. screener.DefaultConfig 및 기존 코드의 값과 같아야 합니다.
func Defaults() Settings {
	return Settings{
		StopLossPct:          0.02,
		DailyLossLimitPct:    0.05,
		K:                    0.5,
		MAWindow:             5,
		NoiseWindow:          20,
		NoiseMin:             0.025,
		NoiseMax:             0.06,
		MinAffordable:        2,
		RankDepth:            30,
		ActiveCount:          10,
		ActiveKeepRank:       30,
		EvalPerMin:           3,
		LazyExpand:           true,
		RankStartDelayMin:    5,
		RankRefreshSec:       60,
		ChaseLimitPct:        0.01,
		PollIntervalSec:      4,
		HeartbeatIntervalSec: 60,
		EODBufferMin:         15,
	}
}

// Validate는 모든 값이 허용 범위 안이고 서로 모순되지 않는지 확인합니다.
// 실패하면 문제 항목을 모두 담은 에러를 돌려줍니다.
func (s Settings) Validate() error {
	var bad []string
	check := func(name string, v, lo, hi float64) {
		if math.IsNaN(v) || v < lo || v > hi {
			bad = append(bad, fmt.Sprintf("%s=%v (허용 %v~%v)", name, v, lo, hi))
		}
	}
	check("stop_loss_pct", s.StopLossPct, 0.01, 0.02)
	check("daily_loss_limit_pct", s.DailyLossLimitPct, 0.01, 0.05)
	check("k", s.K, 0.1, 1.0)
	check("ma_window", float64(s.MAWindow), 2, 20)
	check("noise_window", float64(s.NoiseWindow), 5, 60)
	check("noise_min", s.NoiseMin, 0.005, 0.2)
	check("noise_max", s.NoiseMax, 0.005, 0.2)
	check("min_affordable", float64(s.MinAffordable), 0, 10)
	check("rank_depth", float64(s.RankDepth), 1, 100)
	check("active_count", float64(s.ActiveCount), 1, 30)
	check("active_keep_rank", float64(s.ActiveKeepRank), 0, 100)
	check("eval_per_min", float64(s.EvalPerMin), 0, 60)
	check("rank_start_delay_minutes", float64(s.RankStartDelayMin), 0, 60)
	check("rank_refresh_seconds", float64(s.RankRefreshSec), 10, 600)
	check("chase_limit_pct", s.ChaseLimitPct, 0, 0.05)
	check("poll_interval_seconds", float64(s.PollIntervalSec), 2, 60)
	check("heartbeat_interval_seconds", float64(s.HeartbeatIntervalSec), 10, 600)
	check("eod_buffer_minutes", float64(s.EODBufferMin), 0, 60)
	if s.NoiseMin >= s.NoiseMax {
		bad = append(bad, "noise_min은 noise_max보다 작아야 합니다")
	}
	if s.ActiveCount > s.RankDepth {
		bad = append(bad, "active_count는 rank_depth 이하여야 합니다")
	}
	if len(bad) > 0 {
		return errors.New(strings.Join(bad, "; "))
	}
	return nil
}

// PollInterval은 시세 폴링 주기입니다.
func (s Settings) PollInterval() time.Duration {
	return time.Duration(s.PollIntervalSec) * time.Second
}

// HeartbeatInterval은 상태 스냅샷의 주기적 갱신 간격입니다(SPEC.md 9).
func (s Settings) HeartbeatInterval() time.Duration {
	return time.Duration(s.HeartbeatIntervalSec) * time.Second
}

// EODBuffer는 정규장 종료 전 강제청산을 시작하는 여유 시간입니다.
func (s Settings) EODBuffer() time.Duration {
	return time.Duration(s.EODBufferMin) * time.Minute
}
