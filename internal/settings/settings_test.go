package settings

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsAreValid(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatalf("기본값이 검증에 실패했습니다: %v", err)
	}
}

func TestValidateBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Settings)
		valid  bool
	}{
		{"손절 하한 1%", func(s *Settings) { s.StopLossPct = 0.01 }, true},
		{"손절 상한 2%", func(s *Settings) { s.StopLossPct = 0.02 }, true},
		{"손절 2% 초과는 완화라 거부", func(s *Settings) { s.StopLossPct = 0.025 }, false},
		{"손절 1% 미만 거부", func(s *Settings) { s.StopLossPct = 0.005 }, false},
		{"일일 한도 하한 1%", func(s *Settings) { s.DailyLossLimitPct = 0.01 }, true},
		{"일일 한도 5% 초과 거부", func(s *Settings) { s.DailyLossLimitPct = 0.06 }, false},
		{"NaN 거부", func(s *Settings) { s.K = math.NaN() }, false},
		{"노이즈 최소가 최대 이상이면 거부", func(s *Settings) { s.NoiseMin = 0.06; s.NoiseMax = 0.06 }, false},
		{"활성 종목 수가 랭킹 깊이를 넘으면 거부", func(s *Settings) { s.ActiveCount = 31; s.RankDepth = 30 }, false},
		{"추격 상한 0은 끔으로 허용", func(s *Settings) { s.ChaseLimitPct = 0 }, true},
		{"폴링 주기 1초는 거부", func(s *Settings) { s.PollIntervalSec = 1 }, false},
	}
	for _, c := range cases {
		s := Defaults()
		c.mutate(&s)
		if err := s.Validate(); (err == nil) != c.valid {
			t.Errorf("%s: valid=%v, err=%v", c.name, err == nil, err)
		}
	}
}

func TestValidateReportsEveryBadField(t *testing.T) {
	s := Defaults()
	s.StopLossPct = 0.5
	s.DailyLossLimitPct = 0.5
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "stop_loss_pct") || !strings.Contains(err.Error(), "daily_loss_limit_pct") {
		t.Fatalf("문제 항목이 모두 보고되지 않았습니다: %v", err)
	}
}

type fakeSource struct {
	doc Document
	err error
}

func (f fakeSource) Fetch(context.Context) (Document, error) { return f.doc, f.err }

func remoteDoc(version int, mutate func(*Settings)) Document {
	d := Document{Version: version, Settings: Defaults()}
	if mutate != nil {
		mutate(&d.Settings)
	}
	return d
}

func TestLoadPrefersRemoteAndWritesCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-good.json")
	remote := remoteDoc(3, func(s *Settings) { s.ActiveCount = 7 })

	res := Loader{Source: fakeSource{doc: remote}, CachePath: path}.Load(context.Background())
	if res.Origin != "remote" || res.Version != 3 || res.ActiveCount != 7 || res.Problem != nil {
		t.Fatalf("원격 설정이 적용되지 않았습니다: %+v", res)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("마지막 정상 설정이 캐시로 저장되지 않았습니다: %v", err)
	}

	// 원격이 실패하면 방금 저장한 마지막 정상 값을 씁니다.
	down := Loader{Source: fakeSource{err: errors.New("network")}, CachePath: path}.Load(context.Background())
	if down.Origin != "cache" || down.Version != 3 || down.ActiveCount != 7 || down.Problem == nil {
		t.Fatalf("캐시 대체가 동작하지 않았습니다: %+v", down)
	}
}

func TestLoadFallsBackWhenRemoteIsOutOfRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-good.json")
	good := remoteDoc(2, nil)
	if _, err := os.Stat(path); err == nil {
		t.Fatal("테스트 시작 시 캐시가 있으면 안 됩니다")
	}
	Loader{Source: fakeSource{doc: good}, CachePath: path}.Load(context.Background())

	bad := remoteDoc(5, func(s *Settings) { s.StopLossPct = 0.05 })
	res := Loader{Source: fakeSource{doc: bad}, CachePath: path}.Load(context.Background())
	if res.Origin != "cache" || res.Version != 2 {
		t.Fatalf("범위 밖 원격 값을 거부하지 않았습니다: %+v", res)
	}
	if res.Problem == nil {
		t.Fatal("거부 사유가 기록되지 않았습니다")
	}
}

func TestLoadUsesDefaultsWhenNothingIsAvailable(t *testing.T) {
	res := Loader{CachePath: filepath.Join(t.TempDir(), "missing.json")}.Load(context.Background())
	if res.Origin != "default" || res.Version != 0 || res.Settings != Defaults() || res.Problem != nil {
		t.Fatalf("기본값 대체가 잘못되었습니다: %+v", res)
	}
}

func TestLoadReportsCorruptCacheAndUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-good.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := Loader{CachePath: path}.Load(context.Background())
	if res.Origin != "default" || res.Problem == nil {
		t.Fatalf("깨진 캐시를 기본값 대체 사유로 남기지 않았습니다: %+v", res)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	want := remoteDoc(9, func(s *Settings) { s.K = 0.7; s.LazyExpand = false })
	data, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("왕복 후 값이 달라졌습니다: %+v != %+v", got, want)
	}
}

func TestDecodeRejectsInvalidStoredValues(t *testing.T) {
	data, _ := Encode(remoteDoc(1, func(s *Settings) { s.DailyLossLimitPct = 0.2 }))
	if _, err := Decode(data); err == nil {
		t.Fatal("범위 밖 저장값을 받아들였습니다")
	}
}

func TestFirestoreSourceFetch(t *testing.T) {
	const doc = `{
	  "name": "projects/p/databases/(default)/documents/settings/trader",
	  "fields": {
	    "version": {"integerValue": "4"},
	    "stop_loss_pct": {"doubleValue": 0.015},
	    "daily_loss_limit_pct": {"integerValue": "0"},
	    "k": {"doubleValue": 0.5},
	    "ma_window": {"integerValue": "5"},
	    "noise_window": {"integerValue": "20"},
	    "noise_min": {"doubleValue": 0.025},
	    "noise_max": {"doubleValue": 0.06},
	    "min_affordable": {"integerValue": "2"},
	    "rank_depth": {"integerValue": "30"},
	    "active_count": {"integerValue": "10"},
	    "active_keep_rank": {"integerValue": "30"},
	    "eval_per_min": {"integerValue": "3"},
	    "lazy_expand": {"booleanValue": true},
	    "rank_start_delay_minutes": {"integerValue": "5"},
	    "rank_refresh_seconds": {"integerValue": "60"},
	    "chase_limit_pct": {"doubleValue": 0.01},
	    "poll_interval_seconds": {"integerValue": "4"},
	    "heartbeat_interval_seconds": {"integerValue": "60"},
	    "eod_buffer_minutes": {"integerValue": "15"},
	    "updated_by": {"stringValue": "uid"},
	    "updated_at": {"timestampValue": "2026-10-08T00:00:00Z"}
	  }
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/token"):
			w.Header().Set("Metadata-Flavor", "Google")
			_, _ = w.Write([]byte(`{"access_token":"tok"}`))
		case strings.HasSuffix(r.URL.Path, "/documents/settings/trader"):
			if r.Header.Get("Authorization") != "Bearer tok" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(doc))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	src := FirestoreSource{ProjectID: "p", BaseURL: srv.URL + "/v1", MetadataURL: srv.URL + "/token"}
	got, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Version != 4 || got.StopLossPct != 0.015 || got.DailyLossLimitPct != 0 || !got.LazyExpand || got.ActiveCount != 10 {
		t.Fatalf("문서 해석 결과가 잘못되었습니다: %+v", got)
	}
	if err := got.Validate(); err == nil {
		t.Fatal("일일 한도 0은 범위 밖이라 검증에서 걸러져야 합니다")
	}
}

func TestFirestoreSourceErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			_, _ = w.Write([]byte(`{"access_token":"tok"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	src := FirestoreSource{ProjectID: "p", BaseURL: srv.URL + "/v1", MetadataURL: srv.URL + "/token"}
	if _, err := src.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "없습니다") {
		t.Fatalf("문서가 없을 때 에러를 돌려주지 않았습니다: %v", err)
	}
	if _, err := (FirestoreSource{}).Fetch(context.Background()); err == nil {
		t.Fatal("프로젝트 ID가 없을 때 에러가 나야 합니다")
	}
	if _, err := decodeFirestoreDocument([]byte(`{"fields":{"k":{"integerValue":"x"}}}`)); err == nil {
		t.Fatal("잘못된 정수 필드를 받아들였습니다")
	}
}
