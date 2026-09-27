package strategy

import "testing"

func TestWatchlistSizeAndNoDuplicates(t *testing.T) {
	if n := len(Watchlist); n < 5 || n > 10 {
		t.Fatalf("SPEC.md 3.3은 5~10종목 고정 워치리스트를 요구합니다: %d개", n)
	}
	seen := make(map[string]bool, len(Watchlist))
	for _, entry := range Watchlist {
		if entry.Symbol == "" {
			t.Fatal("빈 symbol이 포함되었습니다")
		}
		if seen[entry.Symbol] {
			t.Fatalf("중복 symbol: %s", entry.Symbol)
		}
		seen[entry.Symbol] = true
	}
}
