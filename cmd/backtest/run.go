package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/tibetkowon/toss-trader/internal/backtest"
)

// runReplay는 캐시만 읽어 스크리너 전략을 재생합니다(네트워크 없음). 비용 시나리오별로 같은 구간을 돌립니다.
func runReplay(args []string, out io.Writer) error {
	if len(args) < 1 || (args[0] != "KR" && args[0] != "US") {
		return fmt.Errorf("사용법: backtest run KR|US [from=YYYY-MM-DD] [to=YYYY-MM-DD]")
	}
	market := args[0]
	base := backtest.DefaultReplayConfig()
	for _, a := range args[1:] {
		k, v, ok := strings.Cut(a, "=")
		switch {
		case ok && k == "from":
			base.From = v
		case ok && k == "to":
			base.To = v
		default:
			return fmt.Errorf("알 수 없는 인자: %q", a)
		}
	}
	// 라이브 수수료: 국내 0.015%, 미국 0.1% (SPEC 6.2). 국내 개별주 매도세는 0.2% 가정이고 ETF는 0이다.
	base.CommissionRate = 0.00015
	if market == "US" {
		base.CommissionRate = 0.001
	} else {
		base.SellTaxRate = 0.002
	}
	src, err := backtest.NewHistSource(backtest.Cache{}, market)
	if err != nil {
		return fmt.Errorf("캐시를 읽을 수 없습니다(먼저 fetch %s): %w", market, err)
	}

	fmt.Fprintf(out, "=== %s 스크리너 백테스트 (캐시 기준, 한계는 docs/superpowers/specs/2026-10-11-screener-backtest-design.md) ===\n", market)
	fmt.Fprintf(out, "%-10s %12s %9s %8s %7s %7s %9s %9s %9s\n", "슬리피지", "최종자산", "수익률", "MDD", "거래", "승률", "평균수익", "평균손실", "기대값")
	for i, bps := range []float64{0, 5, 10} {
		cfg := base
		cfg.SlippageBps = bps
		r := backtest.Replay(cfg, src)
		if r.Err != nil {
			return fmt.Errorf("재생 실패(부분 결과는 사용 불가): %w", r.Err)
		}
		s := r.Stats
		fmt.Fprintf(out, "%-10s %12.2f %8.2f%% %7.2f%% %7d %6.1f%% %8.2f%% %8.2f%% %8.2f%%\n",
			fmt.Sprintf("%.0fbp", bps), r.FinalEquity, (r.FinalEquity/r.StartingCash-1)*100, r.MaxDrawdownPct*100,
			s.Trades, s.WinRate*100, s.AvgWin*100, s.AvgLoss*100, s.Expectancy*100)
		if i == 0 {
			fmt.Fprintf(out, "           구간 %s ~ %s (%d거래일), 손익분기 승률 %.1f%%", r.Days[0].Date, r.Days[len(r.Days)-1].Date, len(r.Days), s.BreakevenWinRate*100)
			if r.Benchmark != nil {
				fmt.Fprintf(out, ", 단순보유 %s %.2f%%", r.Benchmark.Symbol, r.Benchmark.Return*100)
			}
			fmt.Fprintln(out)
			for _, w := range r.Warnings {
				fmt.Fprintln(out, "           경고:", w)
			}
			defer printTrades(out, r)
		}
	}
	return nil
}

func printTrades(out io.Writer, r backtest.Report) {
	fmt.Fprintf(out, "\n--- 거래 목록 (슬리피지 0bp) ---\n")
	for _, t := range r.Trades {
		fmt.Fprintf(out, "%s %s→%s %-8s %12.4f→%-12.4f %-14s 손익 %+9.2f (%+.2f%%)\n",
			t.EntryTime.Format("2006-01-02"), t.EntryTime.Format("15:04"), t.ExitTime.Format("15:04"), t.Symbol,
			t.EntryPrice, t.ExitPrice, t.Reason, t.PnL, t.Return()*100)
	}
}
