// Command dailyreport reads internal/session's persisted daily state and
// prints each market's equity curve and max drawdown so far — the tool
// SPEC.md 11 flags as missing for the 4-week paper-trading verification.
// Unlike cmd/backtest (historical candle replay), this reads the real
// paper-trading account's actual saved state.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/tibetkowon/toss-trader/internal/report"
	"github.com/tibetkowon/toss-trader/internal/session"
)

func main() {
	path := "session.db"
	if v := os.Getenv("SESSION_DB_PATH"); v != "" {
		path = v
	}

	store, err := session.Open(path)
	if err != nil {
		log.Fatalf("세션 DB(%s) 열기 실패: %v", path, err)
	}
	defer store.Close()

	records, err := store.All(context.Background())
	if err != nil {
		log.Fatalf("세션 이력 조회 실패: %v", err)
	}
	if len(records) == 0 {
		fmt.Println("저장된 거래일이 아직 없습니다.")
		return
	}

	byMarket := make(map[string][]session.DailyRecord)
	var marketOrder []string
	for _, r := range records {
		if _, seen := byMarket[r.Market]; !seen {
			marketOrder = append(marketOrder, r.Market)
		}
		byMarket[r.Market] = append(byMarket[r.Market], r)
	}

	for _, market := range marketOrder {
		curve := report.BuildEquityCurve(byMarket[market])
		fmt.Printf("\n=== %s ===\n", market)
		for _, p := range curve {
			fmt.Printf("  %s  %12.2f\n", p.Date, p.Equity)
		}
		fmt.Printf("최대 낙폭(MDD): %.2f%%\n", report.MaxDrawdown(curve)*100)
	}
}
