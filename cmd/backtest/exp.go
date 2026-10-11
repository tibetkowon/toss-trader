package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/tibetkowon/toss-trader/internal/backtest"
	"github.com/tibetkowon/toss-trader/internal/screener"
)

type experiment struct {
	name string
	note string
	mod  func(*backtest.ReplayConfig)
}

// experiments는 SPEC v17의 실험 A~E입니다. 각 항목은 기준선에서 변수 하나만 바꿉니다.
var experiments = []experiment{
	{"기준선", "라이브와 동일", func(c *backtest.ReplayConfig) {}},
	{"C 진입금지 10분", "개장 후 10분", func(c *backtest.ReplayConfig) { c.EntryDelayMin = 10 }},
	{"C 진입금지 20분", "개장 후 20분", func(c *backtest.ReplayConfig) { c.EntryDelayMin = 20 }},
	{"C 진입금지 30분", "개장 후 30분", func(c *backtest.ReplayConfig) { c.EntryDelayMin = 30 }},
	{"A 손절=0.5×노이즈[1,2]%", "SPEC 4.3 범위 안(더 타이트)", func(c *backtest.ReplayConfig) {
		c.StopNoiseMult, c.StopMinPct, c.StopMaxPct = 0.5, 0.01, 0.02
	}},
	{"A 손절=0.5×노이즈[1.5,3]%", "범위 밖: SPEC 4.3 개정 필요", func(c *backtest.ReplayConfig) {
		c.StopNoiseMult, c.StopMinPct, c.StopMaxPct = 0.5, 0.015, 0.03
	}},
	{"A 손절 3% 고정", "범위 밖: SPEC 4.3 개정 필요", func(c *backtest.ReplayConfig) { c.StopLossPct = 0.03 }},
	{"B 트레일 +1%/-0.7%", "수익 1% 후 고점 -0.7%", func(c *backtest.ReplayConfig) { c.TrailActivate, c.TrailGap = 0.01, 0.007 }},
	{"B 트레일 +1.5%/-1%", "수익 1.5% 후 고점 -1%", func(c *backtest.ReplayConfig) { c.TrailActivate, c.TrailGap = 0.015, 0.01 }},
	{"B 트레일 +2%/-1.5%", "수익 2% 후 고점 -1.5%", func(c *backtest.ReplayConfig) { c.TrailActivate, c.TrailGap = 0.02, 0.015 }},
	{"D 살수있는종목우선 끔", "MinAffordable=0", func(c *backtest.ReplayConfig) {
		c.Screener = func(s *screener.Config) { s.MinAffordable = 0 }
	}},
	{"E 노이즈 상한 4%", "NoiseMax=4%", func(c *backtest.ReplayConfig) { c.Screener = func(s *screener.Config) { s.NoiseMax = 0.04 } }},
	{"E 노이즈 상한 5%", "NoiseMax=5%", func(c *backtest.ReplayConfig) { c.Screener = func(s *screener.Config) { s.NoiseMax = 0.05 } }},
	// 아래는 1차 결과를 본 뒤 추가한 사후 탐색입니다(다중 비교 주의, 최종 검증은 다른 시장·새 데이터에서).
	{"E 노이즈 상한 3.5%", "NoiseMax=3.5% (사후)", func(c *backtest.ReplayConfig) { c.Screener = func(s *screener.Config) { s.NoiseMax = 0.035 } }},
	{"E 노이즈 상한 3%", "NoiseMax=3% (사후)", func(c *backtest.ReplayConfig) { c.Screener = func(s *screener.Config) { s.NoiseMax = 0.03 } }},
	{"E4%+C20분", "NoiseMax=4% + 진입금지 20분 (사후)", func(c *backtest.ReplayConfig) {
		c.EntryDelayMin = 20
		c.Screener = func(s *screener.Config) { s.NoiseMax = 0.04 }
	}},
	{"E4%+C30분", "NoiseMax=4% + 진입금지 30분 (사후)", func(c *backtest.ReplayConfig) {
		c.EntryDelayMin = 30
		c.Screener = func(s *screener.Config) { s.NoiseMax = 0.04 }
	}},
}

// runExperiments는 구간의 앞 60%(조정용)와 뒤 40%(검증용), 전체를 한 표로 비교합니다.
// 설계 원칙: 앞에서 고르고 뒤는 한 번만 봅니다. 결과 해석은 기대값(거래당 수익률)을 보고, 슬리피지 5bp 기준입니다.
func runExperiments(args []string, out io.Writer) error {
	if len(args) < 1 || (args[0] != "KR" && args[0] != "US") {
		return fmt.Errorf("사용법: backtest exp KR|US [from=YYYY-MM-DD] [to=YYYY-MM-DD] [bps=5]")
	}
	market := args[0]
	base := backtest.DefaultReplayConfig()
	base.SlippageBps = 5
	only := ""
	for _, a := range args[1:] {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			return fmt.Errorf("알 수 없는 인자: %q", a)
		}
		switch k {
		case "from":
			base.From = v
		case "to":
			base.To = v
		case "only":
			only = v
		case "bps":
			if _, err := fmt.Sscan(v, &base.SlippageBps); err != nil {
				return err
			}
		default:
			return fmt.Errorf("알 수 없는 인자: %q", a)
		}
	}
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
	days := src.TradingDays(base.From, base.To)
	if len(days) < 10 {
		return fmt.Errorf("거래일이 너무 적습니다(%d)", len(days))
	}
	cut := len(days) * 6 / 10
	periods := []struct{ name, from, to string }{
		{"앞60%", days[0], days[cut-1]},
		{"뒤40%", days[cut], days[len(days)-1]},
		{"전체", days[0], days[len(days)-1]},
	}
	fmt.Fprintf(out, "=== %s 실험 (슬리피지 %.0fbp) 앞60%% %s~%s / 뒤40%% %s~%s ===\n", market, base.SlippageBps, periods[0].from, periods[0].to, periods[1].from, periods[1].to)
	fmt.Fprintf(out, "%-26s %-6s %9s %7s %5s %6s %8s %8s %9s\n", "실험", "구간", "수익률", "MDD", "거래", "승률", "평균수익", "평균손실", "기대값")
	for _, e := range experiments {
		if only != "" && !strings.Contains(e.name, only) {
			continue
		}
		for _, p := range periods {
			cfg := base
			cfg.From, cfg.To = p.from, p.to
			e.mod(&cfg)
			r := backtest.Replay(cfg, src)
			if r.Err != nil {
				return fmt.Errorf("%s %s: %w", e.name, p.name, r.Err)
			}
			s := r.Stats
			fmt.Fprintf(out, "%-26s %-6s %8.2f%% %6.2f%% %5d %5.1f%% %7.2f%% %7.2f%% %8.2f%%\n", e.name, p.name,
				(r.FinalEquity/r.StartingCash-1)*100, r.MaxDrawdownPct*100, s.Trades, s.WinRate*100, s.AvgWin*100, s.AvgLoss*100, s.Expectancy*100)
		}
		fmt.Fprintln(out)
	}
	return nil
}
