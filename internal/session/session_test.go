package session

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tibetkowon/toss-trader/internal/simulator"
)

func testConfig() simulator.Config {
	return simulator.Config{StopLossPct: 0.02, DailyLossLimitPct: 0.05, CommissionRate: 0.001}
}

func sampleState() simulator.State {
	sim := simulator.New(testConfig(), 100000)
	sim.OnTick(simulator.Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}, 1000, false)
	sim.OnTick(simulator.Setup{Symbol: "A", TargetPrice: 1000, TrendOK: true}, 980, false) // stop-loss
	sim.OnTick(simulator.Setup{Symbol: "B", TargetPrice: 500, TrendOK: true}, 500, false)  // open position
	return sim.State()
}

func TestSaveAndLoad(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	want := sampleState()
	ctx := context.Background()
	if err := store.Save(ctx, "2026-09-27", "KR", want); err != nil {
		t.Fatal(err)
	}

	got, ok, err := store.Load(ctx, "2026-09-27", "KR")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("저장한 상태를 찾지 못했습니다")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("불러온 상태 불일치:\ngot  %+v\nwant %+v", got, want)
	}
	if got.Position == nil || got.Position.Symbol != "B" {
		t.Fatalf("포지션 복구 불일치: %+v", got.Position)
	}
	if len(got.StoppedOutSymbols) != 1 || got.StoppedOutSymbols[0] != "A" {
		t.Fatalf("재진입 금지 목록 복구 불일치: %+v", got.StoppedOutSymbols)
	}
}

func TestLoadNothingSaved(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_, ok, err := store.Load(context.Background(), "2026-09-27", "KR")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("아무것도 저장하지 않았는데 ok=true가 반환되었습니다")
	}
}

func TestSaveOverwrites(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	first := simulator.State{Cash: 1000}
	second := simulator.State{Cash: 2000}
	if err := store.Save(ctx, "2026-09-27", "KR", first); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "2026-09-27", "KR", second); err != nil {
		t.Fatal(err)
	}

	got, ok, err := store.Load(ctx, "2026-09-27", "KR")
	if err != nil || !ok {
		t.Fatalf("불러오기 실패: ok=%v err=%v", ok, err)
	}
	if got.Cash != 2000 {
		t.Fatalf("두 번째 저장으로 덮어써지지 않았습니다: %+v", got)
	}
}

func TestDifferentKeysDoNotCollide(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	kr := simulator.State{Cash: 1111}
	us := simulator.State{Cash: 2222}
	if err := store.Save(ctx, "2026-09-27", "KR", kr); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "2026-09-27", "US", us); err != nil {
		t.Fatal(err)
	}

	gotKR, _, err := store.Load(ctx, "2026-09-27", "KR")
	if err != nil || gotKR.Cash != 1111 {
		t.Fatalf("KR 상태 불일치: %+v err=%v", gotKR, err)
	}
	gotUS, _, err := store.Load(ctx, "2026-09-27", "US")
	if err != nil || gotUS.Cash != 2222 {
		t.Fatalf("US 상태 불일치: %+v err=%v", gotUS, err)
	}
}

func TestReopenSamePathPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	ctx := context.Background()

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	want := simulator.State{Cash: 5555}
	if err := store.Save(ctx, "2026-09-27", "KR", want); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	got, ok, err := reopened.Load(ctx, "2026-09-27", "KR")
	if err != nil || !ok {
		t.Fatalf("재오픈 후 불러오기 실패: ok=%v err=%v", ok, err)
	}
	if got.Cash != 5555 {
		t.Fatalf("재오픈 후 상태가 유지되지 않았습니다: %+v", got)
	}
}
