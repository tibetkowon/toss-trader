package main

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/session"
	"github.com/tibetkowon/toss-trader/internal/settings"
)

type fakeSettingsSource struct {
	doc settings.Document
	err error
}

func (f *fakeSettingsSource) Fetch(context.Context) (settings.Document, error) { return f.doc, f.err }

func TestScreenerConfigFromDefaultsMatchScreenerDefaults(t *testing.T) {
	want := screener.DefaultConfig("KR")
	want.CommissionRate = 0.001
	if got := screenerConfigFrom(settings.Defaults(), "KR", 0.001); !reflect.DeepEqual(got, want) {
		t.Fatalf("기본 설정이 스크리너 기본값과 다릅니다: %+v, 기대: %+v", got, want)
	}
}

func TestScreenerConfigFromAppliesEditedValues(t *testing.T) {
	s := settings.Defaults()
	s.ActiveCount = 8
	s.RankDepth = 40
	s.LazyExpand = false
	s.RankStartDelayMin = 10
	s.RankRefreshSec = 30
	s.ActiveKeepRank = 20
	got := screenerConfigFrom(s, "KR", 0)
	if got.ActiveCount != 8 || got.RankDepth != 40 || got.LazyExpand || got.StartDelay.Minutes() != 10 ||
		got.RefreshEvery.Seconds() != 30 || got.KeepRank != 20 {
		t.Fatalf("수정한 값이 반영되지 않았습니다: %+v", got)
	}
}

func TestScreenerConfigFromDisablesAffordabilityRuleForFractionalUS(t *testing.T) {
	if screenerConfigFrom(settings.Defaults(), "US", 0).MinAffordable != 0 {
		t.Error("US는 소수점 매수라 살 수 있는 종목 보장 규칙을 꺼야 합니다")
	}
	if screenerConfigFrom(settings.Defaults(), "KR", 0).MinAffordable == 0 {
		t.Error("KR은 정수 주식이라 규칙을 유지해야 합니다")
	}
}

func TestResolveSettingsKeepsTheDaysFirstSettings(t *testing.T) {
	ctx := context.Background()
	store, err := session.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	src := &fakeSettingsSource{doc: settings.Document{Version: 1, Settings: settings.Defaults()}}
	loader := settings.Loader{Source: src}

	first, err := resolveSettings(ctx, store, loader, "2026-10-08", "KR")
	if err != nil || first.Origin != "remote" || first.Version != 1 {
		t.Fatalf("그날 첫 기동은 원격 설정을 읽어야 합니다: %+v err=%v", first, err)
	}

	// 장중에 설정이 바뀌어도 같은 날 재기동하면 처음 정한 값을 씁니다.
	changed := settings.Defaults()
	changed.ActiveCount = 5
	src.doc = settings.Document{Version: 2, Settings: changed}
	again, err := resolveSettings(ctx, store, loader, "2026-10-08", "KR")
	if err != nil || again.Origin != "session" || again.Version != 1 || again.ActiveCount != 10 {
		t.Fatalf("같은 날 재기동이 처음 설정을 쓰지 않았습니다: %+v err=%v", again, err)
	}

	// 다음 날 세션은 바뀐 설정을 읽습니다.
	next, err := resolveSettings(ctx, store, loader, "2026-10-09", "KR")
	if err != nil || next.Version != 2 || next.ActiveCount != 5 {
		t.Fatalf("다음 세션이 새 설정을 쓰지 않았습니다: %+v err=%v", next, err)
	}
}

func TestSettingsLoaderUsesRemoteOnlyWhenProjectIsSet(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "last-good.json")
	t.Setenv("SETTINGS_CACHE_PATH", cache)

	t.Setenv("SETTINGS_PROJECT_ID", "")
	if l := settingsLoader(); l.Source != nil || l.CachePath != cache {
		t.Fatalf("프로젝트 ID가 없으면 원격을 쓰면 안 됩니다: %+v", l)
	}

	t.Setenv("SETTINGS_PROJECT_ID", "demo-project")
	l := settingsLoader()
	src, ok := l.Source.(settings.FirestoreSource)
	if !ok || src.ProjectID != "demo-project" {
		t.Fatalf("프로젝트 ID가 있으면 Firestore 원격을 써야 합니다: %+v", l.Source)
	}
}
