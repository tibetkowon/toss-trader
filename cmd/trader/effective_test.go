package main

import (
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/screener"
)

func TestEffectiveConfigReflectsDefaults(t *testing.T) {
	got := effectiveConfig(screener.DefaultConfig("KR"), 0.01, 4*time.Second, 60*time.Second, 15*time.Minute)

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"stop_loss_pct", got.StopLossPct, 0.02},
		{"daily_loss_limit_pct", got.DailyLossLimitPct, 0.05},
		{"k", got.K, 0.5},
		{"ma_window", got.MAWindow, 5},
		{"noise_window", got.NoiseWindow, 20},
		{"noise_min", got.NoiseMin, 0.025},
		{"noise_max", got.NoiseMax, 0.06},
		{"min_affordable", got.MinAffordable, 2},
		{"rank_depth", got.RankDepth, 30},
		{"active_count", got.ActiveCount, 10},
		{"active_keep_rank", got.ActiveKeepRank, 30},
		{"eval_per_min", got.EvalPerMin, 3},
		{"lazy_expand", got.LazyExpand, true},
		{"rank_start_delay_minutes", got.RankStartDelayMin, 5},
		{"rank_refresh_seconds", got.RankRefreshSec, 60},
		{"chase_limit_pct", got.ChaseLimitPct, 0.01},
		{"poll_interval_seconds", got.PollIntervalSec, 4},
		{"heartbeat_interval_seconds", got.HeartbeatIntervalSec, 60},
		{"eod_buffer_minutes", got.EODBufferMin, 15},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestEffectiveConfigFollowsOverrides(t *testing.T) {
	scfg := screener.DefaultConfig("KR")
	scfg.ActiveCount = 7
	scfg.StartDelay = 10 * time.Minute
	scfg.RefreshEvery = 90 * time.Second

	got := effectiveConfig(scfg, 0.025, 5*time.Second, 30*time.Second, 0)

	if got.ActiveCount != 7 || got.RankStartDelayMin != 10 || got.RankRefreshSec != 90 {
		t.Errorf("screener overrides not reflected: %+v", got)
	}
	if got.ChaseLimitPct != 0.025 || got.PollIntervalSec != 5 || got.HeartbeatIntervalSec != 30 || got.EODBufferMin != 0 {
		t.Errorf("runtime overrides not reflected: %+v", got)
	}
}
