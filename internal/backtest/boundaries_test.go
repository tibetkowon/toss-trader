package backtest

import (
	"context"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/strategy"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

func TestClockMovesBothDirections(t *testing.T) {
	h := testSource(t)
	for _, tc := range []struct{ day, daily, minutes int }{{6, 1, 1}, {7, 2, 4}, {8, 3, 6}, {6, 1, 1}} {
		h.SetClock(time.Date(2026, 10, tc.day, 9, 1, 0, 0, seoul))
		for interval, want := range map[string]int{"1d": tc.daily, "1m": tc.minutes} {
			got, _, err := h.Candles(context.Background(), "AAA", interval, 200, "")
			if err != nil || len(got) != want {
				t.Fatalf("day %d %s: %d want %d (%v)", tc.day, interval, len(got), want, err)
			}
		}
	}
}
func TestRegularSessionDSTAndComplete(t *testing.T) {
	for _, tc := range []struct{ market, open string }{
		{"KR", "2026-10-02T09:00:00+09:00"},
		{"US", "2026-07-01T13:30:00Z"},
		{"US", "2026-12-01T14:30:00Z"},
	} {
		t.Run(tc.open, func(t *testing.T) {
			open := fetchTime(tc.open)
			d := SymbolData{}
			for i := -1; i <= 391; i++ {
				d.Minutes = append(d.Minutes, fetchCandle(open.Add(time.Duration(i)*time.Minute).Format(time.RFC3339), "10", "1"))
			}
			c := Cache{Dir: t.TempDir()}
			if err := c.SaveUniverse(tc.market, []tossapi.RankingItem{{Symbol: "A"}}); err != nil {
				t.Fatal(err)
			}
			if err := c.SaveSymbol(tc.market, "A", d); err != nil {
				t.Fatal(err)
			}
			h, err := NewHistSource(c, tc.market)
			if err != nil {
				t.Fatal(err)
			}
			h.SetClock(open.Add(8 * time.Hour))
			ctx := context.Background()
			first, next, err := h.Candles(ctx, "A", "1m", 200, "")
			if err != nil || len(first) != 200 || next == "" {
				t.Fatalf("first page: %d %q %v", len(first), next, err)
			}
			second, end, err := h.Candles(ctx, "A", "1m", 200, next)
			if err != nil || len(second) != 190 || end != "" {
				t.Fatalf("second page: %d %q %v", len(second), end, err)
			}
			if !fetchTime(second[0].Timestamp).Before(fetchTime(next)) || second[189].Timestamp != open.Format(time.RFC3339) {
				t.Fatal("pagination/session boundary")
			}
			ev := screener.NewEvaluator(h, screener.DefaultConfig(tc.market), open)
			ev.Seed([]screener.Entry{{Symbol: "A", Passed: true, Prev: strategy.DailyBar{High: 11, Low: 9, Close: 10}, Closes: []float64{10, 10, 10, 10, 10}}})
			entry, err := ev.Complete(ctx, "A")
			if err != nil || !entry.HasOpen || entry.Open != 10 {
				t.Fatalf("Complete: %+v %v", entry, err)
			}
		})
	}
}
