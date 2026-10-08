package main

import (
	"time"

	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/snapshot"
)

// 손절·일일 손실 한도 기본값(SPEC.md 4.3). 시뮬레이터 설정과 스냅샷 표시가 같은 값을 쓰도록 한 곳에 둡니다.
const (
	stopLossPct       = 0.02
	dailyLossLimitPct = 0.05
)

// effectiveConfig는 이번 프로세스가 실제로 적용하는 설정을 스냅샷 표시용 값으로 옮깁니다.
// 환경변수 덮어쓰기까지 끝난 뒤의 값을 받아야 합니다.
func effectiveConfig(scfg screener.Config, chaseLimit float64, poll, heartbeat, eod time.Duration) *snapshot.EffectiveConfig {
	return &snapshot.EffectiveConfig{
		StopLossPct:          stopLossPct,
		DailyLossLimitPct:    dailyLossLimitPct,
		K:                    scfg.K,
		MAWindow:             scfg.MAWindow,
		NoiseWindow:          scfg.NoiseWindow,
		NoiseMin:             scfg.NoiseMin,
		NoiseMax:             scfg.NoiseMax,
		MinAffordable:        scfg.MinAffordable,
		RankDepth:            scfg.RankDepth,
		ActiveCount:          scfg.ActiveCount,
		ActiveKeepRank:       scfg.KeepRank,
		EvalPerMin:           scfg.EvalPerMin,
		LazyExpand:           scfg.LazyExpand,
		RankStartDelayMin:    int(scfg.StartDelay / time.Minute),
		RankRefreshSec:       int(scfg.RefreshEvery / time.Second),
		ChaseLimitPct:        chaseLimit,
		PollIntervalSec:      int(poll / time.Second),
		HeartbeatIntervalSec: int(heartbeat / time.Second),
		EODBufferMin:         int(eod / time.Minute),
	}
}
