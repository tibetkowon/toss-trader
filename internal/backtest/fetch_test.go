package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

type fetchFake struct {
	calls []string
	fail  bool
}

func (f *fetchFake) Rankings(_ context.Context, kind, duration, market string) ([]tossapi.RankingItem, error) {
	f.calls = append(f.calls, "rank:"+duration)
	return []tossapi.RankingItem{{Symbol: "A", Rank: 1}}, nil
}
func (f *fetchFake) Stocks(_ context.Context, symbols ...string) ([]tossapi.Stock, error) {
	f.calls = append(f.calls, "stocks")
	return []tossapi.Stock{{Symbol: "A", Raw: json.RawMessage(`{"leverageFactor":2}`)}}, nil
}
func (f *fetchFake) StockWarnings(_ context.Context, symbol string) ([]json.RawMessage, error) {
	f.calls = append(f.calls, "warnings")
	return []json.RawMessage{}, nil
}
func (f *fetchFake) Candles(_ context.Context, symbol, interval string, count int, before string) ([]tossapi.Candle, string, error) {
	f.calls = append(f.calls, interval+":"+before)
	if interval == "1d" {
		return []tossapi.Candle{fetchCandle("2026-10-01T00:00:00+09:00", "10", "1")}, "", nil
	}
	if before == "" {
		return []tossapi.Candle{fetchCandle("2026-10-02T09:02:00+09:00", "12", "1")}, "2026-10-02T09:02:00+09:00", nil
	}
	if f.fail {
		return nil, "", errors.New("429 after client retries")
	}
	return []tossapi.Candle{fetchCandle("2026-10-02T09:01:00+09:00", "11", "1")}, "", nil
}
func TestFetchResumeAndOfflineCache(t *testing.T) {
	ctx := context.Background()
	cache := Cache{Dir: t.TempDir()}
	f := &fetchFake{fail: true}
	if err := Fetch(ctx, f, cache, "KR", 400, nil); err == nil {
		t.Fatal("expected fetch failure")
	}
	partial, err := cache.LoadSymbol("KR", "A")
	if err != nil || len(partial.Minutes) != 1 {
		t.Fatalf("partial: %+v %v", partial, err)
	}
	f.fail = false
	f.calls = nil
	if err := Fetch(ctx, f, cache, "KR", 400, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, []string{"1m:2026-10-02T09:02:00+09:00"}) {
		t.Fatalf("resume calls: %v", f.calls)
	}
	f.calls = nil
	if err := Fetch(ctx, f, cache, "KR", 400, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("completed cache called source: %v", f.calls)
	}
	h, err := NewHistSource(cache, "KR")
	if err != nil {
		t.Fatal(err)
	}
	h.SetClock(fetchTime("2026-10-02T09:03:00+09:00"))
	bars, _, err := h.Candles(ctx, "A", "1m", 200, "")
	if err != nil || len(bars) != 2 {
		t.Fatalf("offline: %v %v", bars, err)
	}
	stocks, err := h.Stocks(ctx, "A")
	if err != nil || !stocks[0].IsLeveraged() {
		t.Fatal("stock raw lost")
	}
	warnings, err := h.StockWarnings(ctx, "A")
	if err != nil || len(warnings) != 0 {
		t.Fatal("warnings lost")
	}
}
func TestFetchPageLimit(t *testing.T) {
	cache := Cache{Dir: t.TempDir()}
	f := &fetchFake{}
	if err := Fetch(context.Background(), f, cache, "KR", 1, nil); err != nil {
		t.Fatal(err)
	}
	d, err := cache.LoadSymbol("KR", "A")
	if err != nil || len(d.Minutes) != 1 {
		t.Fatalf("page cap: %v %v", d, err)
	}
	f.calls = nil
	if err := Fetch(context.Background(), f, cache, "KR", 1, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, []string{"1m:2026-10-02T09:02:00+09:00"}) {
		t.Fatalf("resume: %v", f.calls)
	}
}

func fetchTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
func fetchCandle(s, price, volume string) tossapi.Candle {
	return tossapi.Candle{Timestamp: s, OpenPrice: price, HighPrice: price, LowPrice: price, ClosePrice: price, Volume: volume}
}
