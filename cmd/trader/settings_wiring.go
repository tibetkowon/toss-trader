package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/session"
	"github.com/tibetkowon/toss-trader/internal/settings"
)

// settingsLoader는 설정 문서를 Firestore에서 읽고, 마지막 정상 값은 로컬 파일에 둡니다.
// SETTINGS_PROJECT_ID가 없으면 원격을 읽지 않고 캐시나 기본값만 씁니다(로컬 개발).
func settingsLoader() settings.Loader {
	cache := os.Getenv("SETTINGS_CACHE_PATH")
	if cache == "" {
		cache = "settings-last-good.json"
	}
	loader := settings.Loader{CachePath: cache}
	if project := os.Getenv("SETTINGS_PROJECT_ID"); project != "" {
		loader.Source = settings.FirestoreSource{ProjectID: project}
	}
	return loader
}

// resolveSettings는 (date, market) 세션에 적용할 설정을 정합니다. 그날 처음 기동할 때는 로더로 읽어
// DB에 기록하고, 같은 날 재기동하면 기록된 값을 그대로 씁니다. 세션 중 바뀐 설정은 다음 세션부터 적용됩니다(SPEC.md 9.2).
func resolveSettings(ctx context.Context, store *session.Store, loader settings.Loader, date, market string) (settings.Result, error) {
	data, ok, err := store.LoadDaySettings(ctx, date, market)
	if err != nil {
		return settings.Result{}, fmt.Errorf("오늘 설정 기록 조회 실패: %w", err)
	}
	if ok {
		doc, err := settings.Decode(data)
		if err != nil {
			return settings.Result{}, fmt.Errorf("오늘 저장된 설정을 읽을 수 없습니다: %w", err)
		}
		return settings.Result{Document: doc, Origin: "session"}, nil
	}
	res := loader.Load(ctx)
	data, err = settings.Encode(res.Document)
	if err != nil {
		return settings.Result{}, err
	}
	if err := store.SaveDaySettings(ctx, date, market, data); err != nil {
		return settings.Result{}, fmt.Errorf("오늘 설정 기록 저장 실패: %w", err)
	}
	return res, nil
}

// logSettings는 이번 세션에 적용하는 설정의 출처와 문제를 로그로 남깁니다.
func logSettings(res settings.Result) {
	switch res.Origin {
	case "session":
		log.Printf("오늘 세션 설정을 재사용합니다(버전 %d)", res.Version)
	case "remote":
		log.Printf("설정을 적용합니다: 원격 버전 %d", res.Version)
	case "cache":
		log.Printf("원격 설정 대신 마지막 정상 설정을 적용합니다(버전 %d)", res.Version)
	default:
		log.Printf("설정 문서가 없어 기본값을 적용합니다")
	}
	if res.Problem != nil {
		log.Printf("설정 경고: %v", res.Problem)
	}
}

// screenerConfigFrom은 설정을 스크리너 설정으로 옮깁니다. 미국은 소수점 매수라 "살 수 있는 종목"
// 규칙을 끕니다.
func screenerConfigFrom(s settings.Settings, market string, commissionRate float64) screener.Config {
	c := screener.Config{
		Market:         market,
		NoiseMin:       s.NoiseMin,
		NoiseMax:       s.NoiseMax,
		NoiseWindow:    s.NoiseWindow,
		MAWindow:       s.MAWindow,
		K:              s.K,
		RankDepth:      s.RankDepth,
		ActiveCount:    s.ActiveCount,
		MinAffordable:  s.MinAffordable,
		EvalPerMin:     s.EvalPerMin,
		StartDelay:     time.Duration(s.RankStartDelayMin) * time.Minute,
		RefreshEvery:   time.Duration(s.RankRefreshSec) * time.Second,
		KeepRank:       s.ActiveKeepRank,
		LazyExpand:     s.LazyExpand,
		CommissionRate: commissionRate,
	}
	if market == "US" {
		c.MinAffordable = 0
	}
	return c
}
