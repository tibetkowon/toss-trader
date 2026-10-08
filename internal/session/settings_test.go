package session

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDaySettingsFirstSaveWins(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, ok, err := store.LoadDaySettings(ctx, "2026-10-08", "KR"); err != nil || ok {
		t.Fatalf("저장 전 조회: ok=%v err=%v", ok, err)
	}
	if err := store.SaveDaySettings(ctx, "2026-10-08", "KR", []byte(`{"version":1}`)); err != nil {
		t.Fatal(err)
	}
	// 같은 날 재기동 시 나중에 바뀐 설정으로 덮어쓰면 안 됩니다.
	if err := store.SaveDaySettings(ctx, "2026-10-08", "KR", []byte(`{"version":2}`)); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.LoadDaySettings(ctx, "2026-10-08", "KR")
	if err != nil || !ok || string(got) != `{"version":1}` {
		t.Fatalf("처음 저장한 설정이 유지되지 않았습니다: %q ok=%v err=%v", got, ok, err)
	}
	// 시장과 날짜가 다르면 별개의 기록입니다.
	if _, ok, _ := store.LoadDaySettings(ctx, "2026-10-08", "US"); ok {
		t.Fatal("다른 시장의 기록이 섞였습니다")
	}
	if _, ok, _ := store.LoadDaySettings(ctx, "2026-10-09", "KR"); ok {
		t.Fatal("다른 날짜의 기록이 섞였습니다")
	}
}
