// Command trader is the entrypoint for the auto-trading service.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/tibetkowon/toss-trader/internal/lifecycle"
	"github.com/tibetkowon/toss-trader/internal/session"
	"github.com/tibetkowon/toss-trader/internal/simulator"
	"github.com/tibetkowon/toss-trader/internal/snapshot"
	"github.com/tibetkowon/toss-trader/internal/strategy"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
	"github.com/tibetkowon/toss-trader/internal/tradingloop"
)

const dateLayout = "2006-01-02"

type calendarAdapter struct{ client *tossapi.Client }

func (a calendarAdapter) IsMarketOpen(ctx context.Context, market string) (bool, error) {
	calendar, err := a.client.MarketCalendar(ctx, market)
	if err != nil {
		return false, err
	}
	return calendar.IsOpenToday(), nil
}

func main() {
	market := os.Getenv("MARKET")
	if market != "KR" && market != "US" {
		log.Print("필수 환경 변수 MARKET은 정확히 KR 또는 US여야 합니다")
		os.Exit(1)
	}

	ctx := context.Background()
	client, err := newTossClient(ctx)
	if err != nil {
		log.Printf("토스 API 클라이언트 초기화에 실패했습니다: %v", err)
		os.Exit(1)
	}

	stopper := lifecycle.NewComputeStopper(nil)

	open, err := lifecycle.SelfStopIfClosed(ctx, calendarAdapter{client}, stopper, market)
	if err != nil {
		log.Printf("%s 시장 개장 여부 확인 또는 인스턴스 자체 정지에 실패했습니다: %v", market, err)
		os.Exit(1)
	}
	if !open {
		log.Printf("%s 시장은 오늘 휴장입니다. 인스턴스 자체 정지 요청이 수락되었습니다", market)
		return
	}
	log.Printf("%s 시장은 오늘 개장합니다", market)

	if err := runTradingSession(ctx, client, market); err != nil {
		log.Printf("거래 세션 실행 중 오류: %v", err)
		os.Exit(1)
	}

	log.Print("장 마감 처리를 완료했습니다. 인스턴스를 자체 정지합니다")
	if err := stopper.Stop(ctx); err != nil {
		log.Printf("자체 정지 요청 실패(백스톱 스케줄러가 대신 정지합니다): %v", err)
	}
}

// runTradingSession runs the paper-trading polling loop for one session
// (SPEC.md 5.1, 6.1) until the market closes, persisting state on every
// change and publishing the dashboard snapshot (SPEC.md 9).
func runTradingSession(ctx context.Context, client *tossapi.Client, market string) error {
	accounts, err := client.Accounts(ctx)
	if err != nil || len(accounts) == 0 {
		return err
	}
	accountSeq := accounts[0].AccountSeq.String()

	commissionRate, err := commissionRateFor(ctx, client, accountSeq, market)
	if err != nil {
		return err
	}

	store, err := session.Open(sessionDBPath())
	if err != nil {
		return err
	}
	defer store.Close()

	calendar, err := client.MarketCalendar(ctx, market)
	if err != nil {
		return err
	}
	sessionStart, err := time.Parse(time.RFC3339, calendar.Today.Integrated.RegularMarket.StartTime)
	if err != nil {
		return err
	}
	sessionEnd, err := time.Parse(time.RFC3339, calendar.Today.Integrated.RegularMarket.EndTime)
	if err != nil {
		return err
	}
	eodCutoff := sessionEnd.Add(-eodBuffer())

	// 오늘 시가는 정규장이 실제로 시작된 뒤에만 의미가 있습니다(장 시작 전 Price()는
	// 전일 마지막 체결가를 반환) — Cloud Scheduler가 버퍼를 두고 미리 기동하므로
	// (SPEC.md 2.1) 여기서 세션 시작까지 대기합니다.
	if wait := time.Until(sessionStart) + 5*time.Second; wait > 0 {
		log.Printf("정규장 시작까지 %s 대기합니다", wait.Round(time.Second))
		time.Sleep(wait)
	}

	today := time.Now().Format(dateLayout)
	cfg := simulator.Config{StopLossPct: 0.02, DailyLossLimitPct: 0.05, CommissionRate: commissionRate}

	sim, err := loadOrStartSimulator(ctx, store, cfg, today, market)
	if err != nil {
		return err
	}

	setups := todaySetups(ctx, client)

	uploader := snapshot.NewGCSUploader(nil, os.Getenv("SNAPSHOT_BUCKET"))
	publish := func(halted bool, reason string) {
		publishSnapshot(ctx, uploader, sim, client, halted, reason)
	}
	publish(false, "")

	pollInterval := pollIntervalDuration()
	const staleness = 30 * time.Second
	heartbeatInterval := heartbeatIntervalDuration()
	lastHeartbeat := time.Now()

	for time.Now().Before(eodCutoff) {
		observations := pollWatchlist(ctx, client)
		actions := tradingloop.ProcessTick(sim, setups, observations, time.Now(), staleness)
		if len(actions) > 0 {
			if err := store.Save(ctx, today, market, sim.State()); err != nil {
				log.Printf("세션 상태 저장 실패: %v", err)
			}
			publish(false, "")
			lastHeartbeat = time.Now()
		} else if time.Since(lastHeartbeat) >= heartbeatInterval {
			// SPEC.md 9: 상태 변화가 없어도 주기적으로 하트비트를 올려서
			// "서버가 살아있고 마지막 갱신이 오래되지 않았음"을 보여줍니다.
			publish(false, "")
			lastHeartbeat = time.Now()
		}
		time.Sleep(pollInterval)
	}

	if pos, ok := sim.Position(); ok {
		price, err := client.Price(ctx, pos.Symbol)
		if err != nil {
			return err
		}
		last, err := strconv.ParseFloat(price.LastPrice, 64)
		if err != nil {
			return err
		}
		sim.OnTick(setups[pos.Symbol], last, true)
	}

	if err := store.Save(ctx, today, market, sim.State()); err != nil {
		log.Printf("세션 상태 저장 실패: %v", err)
	}
	publish(false, "")
	return nil
}

func loadOrStartSimulator(ctx context.Context, store *session.Store, cfg simulator.Config, today, market string) (*simulator.Simulator, error) {
	if state, ok, err := store.Load(ctx, today, market); err != nil {
		return nil, err
	} else if ok {
		log.Printf("기존 세션 상태를 복구했습니다: cash=%.0f", state.Cash)
		return simulator.Restore(cfg, state), nil
	}

	startingCash := paperSeed()
	if cash, ok, err := store.LatestCash(ctx, market); err != nil {
		return nil, err
	} else if ok {
		startingCash = cash
	}
	log.Printf("새 세션을 시작합니다: seed=%.0f", startingCash)
	return simulator.New(cfg, startingCash), nil
}

// todaySetups computes each watchlist symbol's Setup for today. A symbol
// whose data can't be fetched or is incomplete is logged and skipped rather
// than aborting the whole session — a transient failure on one of eight
// symbols shouldn't take the other seven off watch for the day.
func todaySetups(ctx context.Context, client *tossapi.Client) map[string]simulator.Setup {
	const k = 0.5
	const maWindow = 5
	setups := make(map[string]simulator.Setup, len(strategy.Watchlist))
	for _, entry := range strategy.Watchlist {
		candles, _, err := client.Candles(ctx, entry.Symbol, "1d", maWindow+1, "")
		if err != nil {
			log.Printf("%s 일봉 조회 실패, 오늘 감시 대상에서 제외합니다: %v", entry.Symbol, err)
			continue
		}
		if len(candles) < maWindow+1 {
			log.Printf("%s 일봉 데이터 부족, 오늘 감시 대상에서 제외합니다", entry.Symbol)
			continue
		}
		// 최신순으로 오므로 candles[0]=어제, candles[1..maWindow]=그 이전 5일.
		yesterday := candles[0]
		prevHigh, _ := strconv.ParseFloat(yesterday.HighPrice, 64)
		prevLow, _ := strconv.ParseFloat(yesterday.LowPrice, 64)
		prevClose, _ := strconv.ParseFloat(yesterday.ClosePrice, 64)
		closes := make([]float64, maWindow)
		for i := 0; i < maWindow; i++ {
			c, _ := strconv.ParseFloat(candles[i].ClosePrice, 64)
			closes[i] = c
		}
		todayPrice, err := client.Price(ctx, entry.Symbol)
		if err != nil {
			log.Printf("%s 시가 조회 실패, 오늘 감시 대상에서 제외합니다: %v", entry.Symbol, err)
			continue
		}
		todayOpen, _ := strconv.ParseFloat(todayPrice.LastPrice, 64) // 장 시작 직후 첫 조회를 시가 근사치로 사용
		bar := strategy.DailyBar{Close: prevClose, High: prevHigh, Low: prevLow}
		target, trendOK, err := strategy.ComputeDaySetup(todayOpen, bar, closes, k)
		if err != nil {
			log.Printf("%s 설정 계산 실패, 오늘 감시 대상에서 제외합니다: %v", entry.Symbol, err)
			continue
		}
		setups[entry.Symbol] = simulator.Setup{Symbol: entry.Symbol, TargetPrice: target, TrendOK: trendOK}
	}
	return setups
}

func pollWatchlist(ctx context.Context, client *tossapi.Client) []tradingloop.PriceObservation {
	observations := make([]tradingloop.PriceObservation, 0, len(strategy.Watchlist))
	for _, entry := range strategy.Watchlist {
		price, err := client.Price(ctx, entry.Symbol)
		if err != nil {
			observations = append(observations, tradingloop.PriceObservation{Symbol: entry.Symbol, Err: err})
			continue
		}
		last, err := strconv.ParseFloat(price.LastPrice, 64)
		if err != nil {
			observations = append(observations, tradingloop.PriceObservation{Symbol: entry.Symbol, Err: err})
			continue
		}
		ts, err := time.Parse(time.RFC3339, price.Timestamp)
		if err != nil {
			observations = append(observations, tradingloop.PriceObservation{Symbol: entry.Symbol, Err: err})
			continue
		}
		observations = append(observations, tradingloop.PriceObservation{Symbol: entry.Symbol, Price: last, Timestamp: ts})
	}
	return observations
}

func publishSnapshot(ctx context.Context, uploader *snapshot.GCSUploader, sim *simulator.Simulator, client *tossapi.Client, halted bool, reason string) {
	s := snapshot.Snapshot{
		DailyLossLimitPct: 0.05,
		KillSwitch:        snapshot.KillSwitchStatus{Halted: halted, Reason: reason},
		UpdatedAt:         time.Now(),
	}
	s.Seed = sim.State().Seed // 당일 시작 시드 — DailyLossProgress의 분모(SPEC.md 4.3)
	if pos, ok := sim.Position(); ok {
		current := pos.EntryPrice
		if price, err := client.Price(ctx, pos.Symbol); err == nil {
			if p, err := strconv.ParseFloat(price.LastPrice, 64); err == nil {
				current = p
			}
		}
		s.Positions = []snapshot.Position{{
			Symbol:        pos.Symbol,
			Quantity:      float64(pos.Shares),
			UnrealizedPnL: (current - pos.EntryPrice) * float64(pos.Shares),
		}}
		s.DailyPnL = sim.DailyPnL(current)
	} else {
		s.DailyPnL = sim.RealizedPnLToday()
	}

	data, err := s.RenderJSON()
	if err != nil {
		log.Printf("스냅샷 JSON 생성 실패: %v", err)
		return
	}
	if err := uploader.Upload(ctx, "status.json", "application/json", data); err != nil {
		log.Printf("스냅샷 JSON 업로드 실패: %v", err)
	}
	html, err := s.RenderHTML()
	if err != nil {
		log.Printf("스냅샷 HTML 생성 실패: %v", err)
		return
	}
	if err := uploader.Upload(ctx, "status.html", "text/html; charset=utf-8", html); err != nil {
		log.Printf("스냅샷 HTML 업로드 실패: %v", err)
	}
}

// newTossClient prefers plain TOSS_CLIENT_ID/TOSS_CLIENT_SECRET for local
// development (no GCE metadata server available); the deployed environment
// sets TOSS_CLIENT_ID_SECRET/TOSS_CLIENT_SECRET_SECRET (Secret Manager
// resource names) instead — mirrors cmd/backtest's same convenience path.
func newTossClient(ctx context.Context) (*tossapi.Client, error) {
	var secrets tossapi.SecretProvider
	clientIDName := os.Getenv("TOSS_CLIENT_ID_SECRET")
	clientSecretName := os.Getenv("TOSS_CLIENT_SECRET_SECRET")
	if id, secret := os.Getenv("TOSS_CLIENT_ID"), os.Getenv("TOSS_CLIENT_SECRET"); id != "" && secret != "" {
		secrets = tossapi.NewMemorySecretProvider(map[string]string{"id": id, "secret": secret})
		clientIDName, clientSecretName = "id", "secret"
	} else if clientIDName != "" && clientSecretName != "" {
		secrets = tossapi.NewSecretManagerProvider(nil)
	} else {
		return nil, errors.New("TOSS_CLIENT_ID/TOSS_CLIENT_SECRET 또는 TOSS_CLIENT_ID_SECRET/TOSS_CLIENT_SECRET_SECRET 환경변수가 필요합니다")
	}
	return tossapi.New(ctx, tossapi.Config{Secrets: secrets, ClientIDSecret: clientIDName, ClientSecretSecret: clientSecretName})
}

func commissionRateFor(ctx context.Context, client *tossapi.Client, accountSeq, market string) (float64, error) {
	commissions, err := client.Commissions(ctx, accountSeq)
	if err != nil {
		return 0, err
	}
	for _, c := range commissions {
		if c.MarketCountry == market {
			return strconv.ParseFloat(c.CommissionRate, 64)
		}
	}
	return 0, nil
}

func sessionDBPath() string {
	if v := os.Getenv("SESSION_DB_PATH"); v != "" {
		return v
	}
	return "session.db"
}

func paperSeed() float64 {
	if v := os.Getenv("PAPER_SEED"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
			return n
		}
	}
	return 100000 // SPEC.md 3.2/6.2의 초기 시드 가정 — 실계좌 입금 전까지 사용
}

func pollIntervalDuration() time.Duration {
	if v := os.Getenv("POLL_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 4 * time.Second // SPEC.md 5.1의 3~5초 폴링 주기
}

func heartbeatIntervalDuration() time.Duration {
	if v := os.Getenv("HEARTBEAT_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 60 * time.Second // SPEC.md 9의 주기적 하트비트 간격 — 실측 조정 예정(11절)
}

func eodBuffer() time.Duration {
	if v := os.Getenv("EOD_BUFFER_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Minute
		}
	}
	return 15 * time.Minute // SPEC.md 3.1의 "장 마감 전(예: 15:10경)" 예시 근사
}
