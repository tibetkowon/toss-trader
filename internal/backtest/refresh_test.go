package backtest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

type refreshSrc struct {
	daily []tossapi.Candle
	pages [][]tossapi.Candle
	n     int
}

func (r *refreshSrc) Rankings(context.Context, string, string, string) ([]tossapi.RankingItem, error) {
	return nil, nil
}
func (r *refreshSrc) Stocks(context.Context, ...string) ([]tossapi.Stock, error) { return nil, nil }
func (r *refreshSrc) StockWarnings(context.Context, string) ([]json.RawMessage, error) {
	return nil, nil
}
func (r *refreshSrc) Candles(_ context.Context, _, interval string, _ int, _ string) ([]tossapi.Candle, string, error) {
	if interval == "1d" {
		return r.daily, "", nil
	}
	if r.n >= len(r.pages) {
		return nil, "", nil
	}
	p := r.pages[r.n]
	r.n++
	return p, "next", nil
}

func TestRefreshAppendsNewBarsAndDetectsGap(t *testing.T) {
	dir := t.TempDir()
	cache := Cache{Dir: dir}
	bar := func(ts string) tossapi.Candle { return tossapi.Candle{Timestamp: ts, ClosePrice: "1"} }
	if err := cache.SaveUniverse("KR", []tossapi.RankingItem{{Symbol: "A"}}); err != nil {
		t.Fatal(err)
	}
	if err := cache.save("KR", "A", "1d", []tossapi.Candle{bar("2026-10-08T00:00:00+09:00")}); err != nil {
		t.Fatal(err)
	}
	if err := cache.save("KR", "A", "1m", []tossapi.Candle{bar("2026-10-08T09:01:00+09:00"), bar("2026-10-08T09:00:00+09:00")}); err != nil {
		t.Fatal(err)
	}
	src := &refreshSrc{daily: []tossapi.Candle{bar("2026-10-12T00:00:00+09:00")}, pages: [][]tossapi.Candle{
		{bar("2026-10-12T09:01:00+09:00"), bar("2026-10-12T09:00:00+09:00")},
		{bar("2026-10-08T09:01:00+09:00")},
	}}
	if err := Refresh(context.Background(), src, cache, "KR", 5, nil); err != nil {
		t.Fatal(err)
	}
	var m, d []tossapi.Candle
	cache.load("KR", "A", "1m", &m)
	cache.load("KR", "A", "1d", &d)
	if len(m) != 4 || m[0].Timestamp != "2026-10-12T09:01:00+09:00" || len(d) != 2 {
		t.Fatalf("minutes=%v daily=%v", m, d)
	}
	src.n = 0
	if err := Refresh(context.Background(), &refreshSrc{pages: [][]tossapi.Candle{{bar("2026-10-12T09:01:00+09:00")}}}, Cache{Dir: dir}, "KR", 1, nil); err != nil {
		t.Fatalf("이미 최신이면 성공해야 함: %v", err)
	}
}
