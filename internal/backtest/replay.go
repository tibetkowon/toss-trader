package backtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/simulator"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
	"github.com/tibetkowon/toss-trader/internal/tradingloop"
)

// Seed는 원화이며 US 세션의 현금·거래·일별 자산은 FXRate로 환산한 달러입니다.
// Screener는 DefaultConfig에 적용하는 선택적 덮어쓰기입니다(0값 설정도 가능).
type ReplayConfig struct {
	From, To                                 string
	Seed, FXRate                             float64
	StopLossPct, DailyLossLimitPct           float64
	CommissionRate, SellTaxRate, SlippageBps float64
	ChaseLimitPct                            float64
	Screener                                 func(*screener.Config)
}

func DefaultReplayConfig() ReplayConfig {
	return ReplayConfig{Seed: 100000, FXRate: 1350, StopLossPct: .02, DailyLossLimitPct: .05, ChaseLimitPct: .01}
}

func (c ReplayConfig) validate() error {
	for _, d := range []string{c.From, c.To} {
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return fmt.Errorf("잘못된 날짜: %s", d)
			}
		}
	}
	if c.From != "" && c.To != "" && c.From > c.To {
		return fmt.Errorf("from은 to보다 늦을 수 없습니다")
	}
	for _, v := range []float64{c.Seed, c.FXRate, c.StopLossPct, c.DailyLossLimitPct, c.CommissionRate, c.SellTaxRate, c.SlippageBps, c.ChaseLimitPct} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return fmt.Errorf("설정은 유한한 음이 아닌 수여야 합니다")
		}
	}
	if c.Seed == 0 || c.FXRate == 0 || c.StopLossPct >= 1 || c.DailyLossLimitPct >= 1 || c.CommissionRate+c.SellTaxRate >= 1 || c.SlippageBps >= 10000 {
		return fmt.Errorf("시드·환율·위험·비용 설정 범위 오류")
	}
	return nil
}

// Replay는 캐시만 사용하며, 현재 분봉은 체결에만 쓰고 Gate에는 이전 분봉까지만 노출합니다.
// Err가 있으면 부분 결과이므로 성과 비교에 사용하면 안 됩니다.
func Replay(cfg ReplayConfig, src *HistSource) Report {
	var r Report
	if cfg.Seed == 0 {
		cfg.Seed = 100000
	}
	if cfg.FXRate == 0 {
		cfg.FXRate = 1350
	}
	if err := cfg.validate(); err != nil {
		r.Err = err
		return r
	}
	if src == nil {
		r.Err = fmt.Errorf("HistSource가 필요합니다")
		return r
	}
	savedClock := src.clock
	defer src.SetClock(savedClock)
	cash := cfg.Seed
	if src.market == "US" {
		cash /= cfg.FXRate
	}
	r.StartingCash, r.FinalEquity = cash, cash
	dates := map[string]bool{}
	for _, x := range src.idx {
		for _, t := range x.ascT {
			d := t.In(src.loc).Format("2006-01-02")
			if (cfg.From == "" || d >= cfg.From) && (cfg.To == "" || d <= cfg.To) {
				dates[d] = true
			}
		}
	}
	var days []string
	for d := range dates {
		days = append(days, d)
	}
	sort.Strings(days)
	if len(days) == 0 {
		r.Err = fmt.Errorf("요청 구간에 정규장 분봉이 없습니다")
		return r
	}
	ctx := context.Background()
	peak := cash
	mark := func(equity float64) {
		peak = math.Max(peak, equity)
		if peak > 0 {
			r.MaxDrawdownPct = math.Max(r.MaxDrawdownPct, (peak-equity)/peak)
		}
	}
	for _, date := range days {
		day, _ := time.ParseInLocation("2006-01-02", date, src.loc)
		start, end := src.session(day)
		cutoff := end.Add(-15 * time.Minute)
		now := start.Add(-time.Minute)
		src.SetClock(now)
		scfg := screener.DefaultConfig(src.market)
		// 라이브(cmd/trader/screener.go)와 같은 시장별 기본값: 국내는 살 수 있는 종목을 활성 집합에 우선 채우고 미국은 끈다.
		if src.market == "KR" {
			scfg.MinAffordable = scfg.ActiveCount
		} else {
			scfg.MinAffordable = 0
		}
		if cfg.Screener != nil {
			cfg.Screener(&scfg)
		}
		scfg.Market, scfg.Seed, scfg.CommissionRate = src.market, cash, cfg.CommissionRate
		ev := screener.NewEvaluator(src, scfg, start)
		gate := screener.NewGate(scfg, src, ev, start)
		gate.SetClock(func() time.Time { return now })
		pre := gate.PreWarm(ctx, start.Add(-30*time.Second))
		if pre.Err != nil {
			r.Err = pre.Err
			return r
		}
		if len(pre.SymbolErrs) > 0 {
			r.Err = pre.SymbolErrs[0]
			return r
		}
		simcfg := simulator.Config{StopLossPct: cfg.StopLossPct, DailyLossLimitPct: cfg.DailyLossLimitPct, CommissionRate: cfg.CommissionRate, SlippageBps: cfg.SlippageBps, Fractional: src.market == "US"}
		sim := simulator.New(simcfg, cash)
		chase := tradingloop.NewChaseGuard(cfg.ChaseLimitPct)
		dr := DayReport{Date: date, StartCash: cash}
		last := map[string]float64{}
		var entry Trade
		record := func(a simulator.Action) {
			if a.Type == simulator.NoAction {
				return
			}
			dr.Actions = append(dr.Actions, a)
			if a.Type == simulator.Bought {
				entry = Trade{Symbol: a.Symbol, EntryTime: now, EntryPrice: a.Price, Shares: a.Shares, Cost: -a.Proceeds}
			}
			if a.Type == simulator.StoppedOut || a.Type == simulator.ClosedEndOfDay {
				entry.ExitTime, entry.ExitPrice, entry.Reason, entry.PnL = now, a.Price, a.Type.String(), a.PnL
				r.Trades = append(r.Trades, entry)
			}
		}
		for now = start; !now.After(cutoff); now = now.Add(time.Minute) {
			src.SetClock(now)
			up := gate.Refresh(ctx, now)
			if len(up.Errs) > 0 {
				r.Err = up.Errs[0]
				return r
			}
			pos, holding := sim.Position()
			symbols := gate.Active()
			if holding && !containsSymbol(symbols, pos.Symbol) {
				symbols = append([]string{pos.Symbol}, symbols...)
			}
			setups := replaySetups(gate, symbols, pos.Symbol)
			bars := map[string][4]float64{}
			for _, s := range symbols {
				if b, ok := src.minute(s, now); ok {
					prices, err := minutePrices(b, holding)
					if err != nil {
						r.Err = fmt.Errorf("%s %s: %w", now.Format(time.RFC3339), s, err)
						return r
					}
					bars[s] = prices
				}
			}
			if now.Equal(cutoff) {
				if holding {
					price, ok := last[pos.Symbol]
					if prices, found := bars[pos.Symbol]; found {
						price, ok = prices[0], true
					} else {
						r.Warnings = append(r.Warnings, fmt.Sprintf("%s %s: 마감 청산 분봉 누락, 마지막 관측가 사용", date, pos.Symbol))
					}
					if !ok {
						r.Err = fmt.Errorf("%s: 청산 가격 없음", pos.Symbol)
						return r
					}
					record(sim.OnTick(setups[pos.Symbol], price, true))
				}
				mark(sim.Cash())
				break
			}
			// 한 분의 네 단계마다 전체 종목을 랭킹 순으로 처리합니다.
			for phase := 0; phase < 4; phase++ {
				for _, symbol := range symbols {
					prices, ok := bars[symbol]
					if !ok {
						continue
					}
					price := prices[phase]
					last[symbol] = price
					pos, holding = sim.Position()
					if !holding && src.market == "US" && !now.Before(end.Add(-time.Hour)) {
						continue
					}
					obs := []tradingloop.PriceObservation{{Symbol: symbol, Price: price, Timestamp: now}}
					obs, _ = chase.Filter(obs, setups, holding, now, 30*time.Second)
					if len(obs) == 0 {
						continue
					}
					setup, exists := setups[symbol]
					if !exists {
						continue
					}
					if !holding {
						// 종목별 세율을 적용하되 일중 위험 카운터와 현금을 그대로 유지합니다.
						simcfg.SellTaxRate = 0
						if src.market == "KR" && !src.isETF(symbol) {
							simcfg.SellTaxRate = cfg.SellTaxRate
						}
						sim = simulator.Restore(simcfg, sim.State())
					}
					obs[0].Price = fillObservation(setup, pos, holding, price, prices[0], cfg.StopLossPct)
					for _, a := range tradingloop.ProcessTick(sim, setups, obs, now, 30*time.Second) {
						record(a)
					}
				}
				equity := sim.Cash()
				if p, ok := sim.Position(); ok {
					equity += last[p.Symbol] * p.Shares
				}
				mark(equity)
			}
		}
		cash = sim.Cash()
		dr.EndCash = cash
		r.Days = append(r.Days, dr)
		r.FinalEquity = cash
	}
	r.Stats = summarizeTrades(r.Trades)
	r.Benchmark = replayBenchmark(src, days)
	return r
}

func containsSymbol(symbols []string, symbol string) bool {
	for _, s := range symbols {
		if s == symbol {
			return true
		}
	}
	return false
}
func replaySetups(gate *screener.Gate, symbols []string, held string) map[string]simulator.Setup {
	out := map[string]simulator.Setup{}
	for _, s := range symbols {
		if setup, ok := gate.Setup(s); ok {
			out[s] = setup
		}
	}
	if _, ok := out[held]; held != "" && !ok {
		out[held] = simulator.Setup{Symbol: held}
	}
	return out
}
func (h *HistSource) minute(symbol string, t time.Time) (tossapi.Candle, bool) {
	x := h.idx[symbol]
	if x == nil {
		return tossapi.Candle{}, false
	}
	i := sort.Search(len(x.minT), func(i int) bool { return !x.minT[i].After(t) })
	if i == len(x.minT) || !x.minT[i].Equal(t) {
		return tossapi.Candle{}, false
	}
	return h.data[symbol].Minutes[i], true
}
func minutePrices(b tossapi.Candle, holding bool) ([4]float64, error) {
	var p [4]float64
	for i, s := range []string{b.OpenPrice, b.HighPrice, b.LowPrice, b.ClosePrice} {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return p, fmt.Errorf("잘못된 OHLC 가격: %q", s)
		}
		p[i] = v
	}
	if p[1] < math.Max(p[0], p[3]) || p[2] > math.Min(p[0], p[3]) || p[1] < p[2] {
		return p, fmt.Errorf("OHLC 범위 오류")
	}
	if holding {
		p[1], p[2] = p[2], p[1]
	}
	return p, nil
}
func fillObservation(setup simulator.Setup, pos simulator.Position, holding bool, price, open, stopPct float64) float64 {
	if holding && pos.Symbol == setup.Symbol && price <= pos.EntryPrice*(1-stopPct) {
		return math.Min(pos.EntryPrice*(1-stopPct), open)
	}
	if !holding && setup.TrendOK && price >= setup.TargetPrice {
		return math.Max(setup.TargetPrice, open)
	}
	return price
}
func (h *HistSource) isETF(symbol string) bool {
	if symbol == "069500" {
		return true
	}
	for _, s := range h.data[symbol].Stocks {
		if strings.Contains(strings.ToUpper(s.SecurityType), "ETF") {
			return true
		}
		// shortcut: 종목 유형이 없으면 이름 접두사로 근사하므로 새 ETF 브랜드는 목록 보강이 필요합니다.
		name := strings.ToUpper(strings.TrimSpace(s.Name))
		for _, prefix := range []string{"KODEX", "TIGER", "KBSTAR", "RISE", "ACE", "ARIRANG", "PLUS", "HANARO", "KOSEF", "KIWOOM", "SOL", "TIMEFOLIO", "WOORI"} {
			if strings.HasPrefix(name, prefix+" ") {
				return true
			}
		}
	}
	return false
}
