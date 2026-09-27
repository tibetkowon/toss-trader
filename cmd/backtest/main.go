// Command backtest fetches real historical candles for the fixed watchlist
// (internal/strategy.Watchlist) and replays them through internal/backtest's
// simulator-backed engine, reporting max drawdown against SPEC.md 6.2's
// 10%-of-seed pass criterion.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/tibetkowon/toss-trader/internal/backtest"
	"github.com/tibetkowon/toss-trader/internal/simulator"
	"github.com/tibetkowon/toss-trader/internal/strategy"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

const dateLayout = "2006-01-02"

func main() {
	ctx := context.Background()

	client, accountSeq := mustClient(ctx)

	tradingDays := 65 // ~3 calendar months of KRX trading days (SPEC.md 6.2)
	if v := os.Getenv("BACKTEST_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			log.Fatalf("BACKTEST_DAYS는 양의 정수여야 합니다: %q", v)
		}
		tradingDays = n
	}

	startingCash := 100000.0 // SPEC.md 3.2/6.2's initial seed assumption; real balance is 0 pre-funding
	if v := os.Getenv("BACKTEST_SEED"); v != "" {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil || n <= 0 {
			log.Fatalf("BACKTEST_SEED은 양의 실수여야 합니다: %q", v)
		}
		startingCash = n
	}

	commissionRate := mustCommissionRate(ctx, client, accountSeq)
	log.Printf("KR 수수료율: %.5f", commissionRate)

	symbols := make([]string, len(strategy.Watchlist))
	for i, e := range strategy.Watchlist {
		symbols[i] = e.Symbol
	}

	dailyBySymbol := make(map[string][]dailyBar, len(symbols))
	for _, symbol := range symbols {
		bars := fetchDailyBars(ctx, client, symbol, tradingDays+10) // +MA warmup buffer
		dailyBySymbol[symbol] = bars
		log.Printf("%s: 일봉 %d개 확보 (%s ~ %s)", symbol, len(bars), bars[0].date, bars[len(bars)-1].date)
	}

	dates, setups := buildSetups(dailyBySymbol, tradingDays)
	if len(dates) == 0 {
		log.Fatal("백테스트 대상 거래일이 없습니다")
	}
	log.Printf("백테스트 구간: %s ~ %s (%d 거래일)", dates[0], dates[len(dates)-1], len(dates))

	intraday := make(map[string][]backtest.IntradayTick)
	for _, symbol := range symbols {
		ticksBySymbolDate := fetchMinuteTicks(ctx, client, symbol, dates[0])
		for date, ticks := range ticksBySymbolDate {
			intraday[date] = append(intraday[date], ticks...)
		}
	}
	for date := range intraday {
		sortAndMarkEndOfDay(intraday[date])
	}

	cfg := simulator.Config{StopLossPct: 0.02, DailyLossLimitPct: 0.05, CommissionRate: commissionRate}
	report := backtest.Run(cfg, startingCash, dates, setups, intraday)

	printReport(report, startingCash)
}

type dailyBar struct {
	date                   string
	open, high, low, close float64
}

func mustClient(ctx context.Context) (*tossapi.Client, string) {
	var secrets tossapi.SecretProvider
	clientIDName := os.Getenv("TOSS_CLIENT_ID_SECRET")
	clientSecretName := os.Getenv("TOSS_CLIENT_SECRET_SECRET")
	if id, secret := os.Getenv("TOSS_CLIENT_ID"), os.Getenv("TOSS_CLIENT_SECRET"); id != "" && secret != "" {
		// 로컬 개발 편의 경로 — 배포 환경(GCE)에서는 TOSS_CLIENT_ID_SECRET/
		// TOSS_CLIENT_SECRET_SECRET(Secret Manager 리소스 이름)만 사용합니다.
		secrets = tossapi.NewMemorySecretProvider(map[string]string{"id": id, "secret": secret})
		clientIDName, clientSecretName = "id", "secret"
	} else if clientIDName != "" && clientSecretName != "" {
		secrets = tossapi.NewSecretManagerProvider(nil)
	} else {
		log.Fatal("TOSS_CLIENT_ID/TOSS_CLIENT_SECRET 또는 TOSS_CLIENT_ID_SECRET/TOSS_CLIENT_SECRET_SECRET 환경변수가 필요합니다")
	}

	client, err := tossapi.New(ctx, tossapi.Config{
		Secrets: secrets, ClientIDSecret: clientIDName, ClientSecretSecret: clientSecretName,
	})
	if err != nil {
		log.Fatalf("토스 API 클라이언트 초기화 실패: %v", err)
	}
	accounts, err := client.Accounts(ctx)
	if err != nil || len(accounts) == 0 {
		log.Fatalf("계좌 조회 실패: %v", err)
	}
	return client, accounts[0].AccountSeq.String()
}

func mustCommissionRate(ctx context.Context, client *tossapi.Client, accountSeq string) float64 {
	commissions, err := client.Commissions(ctx, accountSeq)
	if err != nil {
		log.Fatalf("수수료 조회 실패: %v", err)
	}
	for _, c := range commissions {
		if c.MarketCountry == "KR" {
			rate, err := strconv.ParseFloat(c.CommissionRate, 64)
			if err != nil {
				log.Fatalf("잘못된 수수료율: %q", c.CommissionRate)
			}
			return rate
		}
	}
	log.Fatal("KR 수수료율을 찾을 수 없습니다")
	return 0
}

func fetchDailyBars(ctx context.Context, client *tossapi.Client, symbol string, count int) []dailyBar {
	if count > 200 {
		count = 200
	}
	candles, _, err := client.Candles(ctx, symbol, "1d", count, "")
	if err != nil {
		log.Fatalf("%s 일봉 조회 실패: %v", symbol, err)
	}
	bars := make([]dailyBar, len(candles))
	for i, c := range candles {
		ts, err := time.Parse(time.RFC3339, c.Timestamp)
		if err != nil {
			log.Fatalf("%s 일봉 타임스탬프 파싱 실패: %v", symbol, err)
		}
		open, _ := strconv.ParseFloat(c.OpenPrice, 64)
		high, _ := strconv.ParseFloat(c.HighPrice, 64)
		low, _ := strconv.ParseFloat(c.LowPrice, 64)
		closeP, _ := strconv.ParseFloat(c.ClosePrice, 64)
		bars[i] = dailyBar{date: ts.Format(dateLayout), open: open, high: high, low: low, close: closeP}
	}
	// API returns newest-first; backtest needs ascending chronological order.
	sort.Slice(bars, func(i, j int) bool { return bars[i].date < bars[j].date })
	return bars
}

// buildSetups computes each backtest day's per-symbol Setup from the PRIOR
// day's bars only (SPEC.md 3.1's look-ahead-safe target price and MA trend
// filter), for the most recent `tradingDays` days common to every symbol's
// data.
func buildSetups(dailyBySymbol map[string][]dailyBar, tradingDays int) ([]string, map[string]map[string]simulator.Setup) {
	const maWindow = 5
	setups := make(map[string]map[string]simulator.Setup)
	var dates []string

	for symbol, bars := range dailyBySymbol {
		if len(bars) <= maWindow {
			log.Fatalf("%s: MA(%d) 계산에 필요한 일봉이 부족합니다 (%d개)", symbol, maWindow, len(bars))
		}
		start := len(bars) - tradingDays
		if start < maWindow {
			start = maWindow
		}
		for i := start; i < len(bars); i++ {
			today, prev := bars[i], bars[i-1]
			closes := make([]float64, maWindow)
			for w := 0; w < maWindow; w++ {
				closes[w] = bars[i-1-w].close
			}
			ma, err := strategy.MovingAverage(closes)
			if err != nil {
				log.Fatalf("%s MA 계산 실패: %v", symbol, err)
			}
			setup := simulator.Setup{
				Symbol:      symbol,
				TargetPrice: strategy.TargetPrice(today.open, prev.high, prev.low, 0.5),
				TrendOK:     strategy.TrendFilterPasses(prev.close, ma),
			}
			if setups[today.date] == nil {
				setups[today.date] = make(map[string]simulator.Setup)
				dates = append(dates, today.date)
			}
			setups[today.date][symbol] = setup
		}
	}

	sort.Strings(dates)
	// Only the tail matching the requested window, across all symbols combined.
	uniqueDates := dedupeSorted(dates)
	if len(uniqueDates) > tradingDays {
		uniqueDates = uniqueDates[len(uniqueDates)-tradingDays:]
	}
	return uniqueDates, setups
}

func dedupeSorted(dates []string) []string {
	out := dates[:0:0]
	var last string
	for i, d := range dates {
		if i == 0 || d != last {
			out = append(out, d)
			last = d
		}
	}
	return out
}

// fetchMinuteTicks pages backward through 1-minute candles until it has
// covered every day from `since` (inclusive) to today, grouped by date.
func fetchMinuteTicks(ctx context.Context, client *tossapi.Client, symbol, since string) map[string][]backtest.IntradayTick {
	result := make(map[string][]backtest.IntradayTick)
	before := ""
	for page := 0; ; page++ {
		candles, nextBefore, err := client.Candles(ctx, symbol, "1m", 200, before)
		if err != nil {
			log.Fatalf("%s 분봉 조회 실패(page %d): %v", symbol, page, err)
		}
		if len(candles) == 0 {
			break
		}
		oldestDate := ""
		for _, c := range candles {
			ts, err := time.Parse(time.RFC3339, c.Timestamp)
			if err != nil {
				continue
			}
			date := ts.Format(dateLayout)
			if oldestDate == "" || date < oldestDate {
				oldestDate = date
			}
			price, err := strconv.ParseFloat(c.ClosePrice, 64)
			if err != nil {
				continue
			}
			result[date] = append(result[date], backtest.IntradayTick{Symbol: symbol, Time: ts, Price: price})
		}
		if oldestDate <= since || nextBefore == "" {
			break
		}
		before = nextBefore
	}
	return result
}

func sortAndMarkEndOfDay(ticks []backtest.IntradayTick) {
	sort.Slice(ticks, func(i, j int) bool { return ticks[i].Time.Before(ticks[j].Time) })
	lastIndexBySymbol := make(map[string]int)
	for i, t := range ticks {
		lastIndexBySymbol[t.Symbol] = i
	}
	for _, i := range lastIndexBySymbol {
		ticks[i].IsEndOfDay = true
	}
}

func printReport(report backtest.Report, startingCash float64) {
	fmt.Printf("\n=== 백테스트 결과 ===\n")
	fmt.Printf("기간: %d 거래일\n", len(report.Days))
	fmt.Printf("시작 자산: %.0f\n", startingCash)
	fmt.Printf("최종 자산: %.0f (%.2f%%)\n", report.FinalEquity, (report.FinalEquity/startingCash-1)*100)
	fmt.Printf("최대 낙폭(MDD): %.2f%%\n", report.MaxDrawdownPct*100)
	pass := "PASS"
	if report.MaxDrawdownPct > 0.10 {
		pass = "FAIL"
	}
	fmt.Printf("SPEC.md 6.2 통과 기준(최대손실 10%% 이내): %s\n\n", pass)

	tradeCount := 0
	for _, day := range report.Days {
		for _, a := range day.Actions {
			if a.Type == simulator.Bought || a.Type == simulator.StoppedOut || a.Type == simulator.ClosedEndOfDay {
				tradeCount++
				fmt.Printf("%s %-10v %s %d주 @ %.0f (PnL %.0f)\n", day.Date, a.Type, a.Symbol, a.Shares, a.Price, a.PnL)
			}
		}
	}
	fmt.Printf("\n총 체결 이벤트: %d건\n", tradeCount)
}
