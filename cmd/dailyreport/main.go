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

	// KR/US는 하나의 원화 계좌를 공유하므로, 검증 기준(SPEC.md 6.2)은 시장별이 아닌 합산 곡선으로 봅니다.
	account := report.BuildAccountCurve(records)
	status := report.Assess(account)
	fmt.Printf("\n=== 계좌 합산 (KR+US, 계좌 공유 이후 US만) ===\n")
	for _, p := range account {
		fmt.Printf("  %-14s %12.2f\n", p.Date, p.Equity)
	}
	fmt.Printf("현재 자산 %.2f원 (누적 %+.2f%%) / 고점 %.2f원 / 현재 낙폭 %.2f%% / 최대 낙폭 %.2f%% [%s]\n",
		status.Equity, status.CumulativeReturn*100, status.Peak, status.CurrentDrawdown*100, status.MaxDrawdown*100, status.Level)
	if status.Level != report.DrawdownOK {
		fmt.Printf("⚠ 최대 낙폭이 검증 기준 10%%에 가까워졌습니다 (%.2f%%p 남음)\n", (0.10-status.MaxDrawdown)*100)
	}
}
