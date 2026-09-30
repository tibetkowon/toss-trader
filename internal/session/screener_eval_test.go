package session

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveAndLoadEvals(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	got, err := store.LoadEvals(ctx, "2026-10-01", "KR")
	if err != nil || len(got) != 0 {
		t.Fatalf("빈 상태: %v %v", got, err)
	}

	for _, e := range []struct{ date, market, symbol, data string }{
		{"2026-10-01", "KR", "B", `{"n":2}`},
		{"2026-10-01", "KR", "A", `{"n":1}`},
		{"2026-10-01", "US", "A", `{"n":9}`},
		{"2026-10-02", "KR", "A", `{"n":8}`},
	} {
		if err := store.SaveEval(ctx, e.date, e.market, e.symbol, []byte(e.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveEval(ctx, "2026-10-01", "KR", "A", []byte(`{"n":10}`)); err != nil { // 덮어쓰기
		t.Fatal(err)
	}

	got, err = store.LoadEvals(ctx, "2026-10-01", "KR")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{[]byte(`{"n":10}`), []byte(`{"n":2}`)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadEvals = %q, want %q", got, want)
	}
}

func TestEvalsSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	ctx := context.Background()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveEval(ctx, "2026-10-01", "KR", "A", []byte(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	store.Close()

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.LoadEvals(ctx, "2026-10-01", "KR")
	if err != nil || len(got) != 1 || string(got[0]) != `{"n":1}` {
		t.Fatalf("재시작 후: %q %v", got, err)
	}
}
