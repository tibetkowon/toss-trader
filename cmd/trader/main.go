// Command trader is the entrypoint for the auto-trading service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/tibetkowon/toss-trader/internal/lifecycle"
	"github.com/tibetkowon/toss-trader/internal/report"
	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/session"
	"github.com/tibetkowon/toss-trader/internal/settings"
	"github.com/tibetkowon/toss-trader/internal/simulator"
	"github.com/tibetkowon/toss-trader/internal/snapshot"
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

// detectMarket infers today's session from the current KST time of day.
// SPEC.md 2.1's two boot windows (~08:50 and ~22:20 KST) never overlap, so
// a morning boot/restart is always for KR and an evening/night one is
// always for US — no external signal from Cloud Scheduler is needed.
func detectMarket(now time.Time) string {
	kst, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		kst = time.FixedZone("KST", 9*60*60)
	}
	hour := now.In(kst).Hour()
	if hour >= 6 && hour < 18 {
		return "KR"
	}
	return "US"
}

func main() {
	log.Printf("trader 버전: %s", buildVersion())
	market := os.Getenv("MARKET")
	if market != "KR" && market != "US" {
		market = detectMarket(time.Now())
		log.Printf("MARKET 환경변수가 없어 현재 시각(KST) 기준으로 %s로 자동 판단합니다", market)
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
	regular := calendar.Today.RegularSession()
	if regular == nil {
		return errors.New("오늘 정규장 세션 정보가 없습니다(휴장일이어야 하는데 여기까지 온 것은 버그입니다)")
	}
	sessionStart, err := time.Parse(time.RFC3339, regular.StartTime)
	if err != nil {
		return err
	}
	sessionEnd, err := time.Parse(time.RFC3339, regular.EndTime)
	if err != nil {
		return err
	}

	sessionDate := sessionStart.Format(dateLayout)
	applied, err := resolveSettings(ctx, store, settingsLoader(), sessionDate, market)
	if err != nil {
		return err
	}
	logSettings(applied)
	eodCutoff := sessionEnd.Add(-applied.EODBuffer())
	scfg := screenerConfigFrom(applied.Settings, market, commissionRate)
	ev := screener.NewEvaluator(client, scfg, sessionStart)
	if restored, skipped, err := restoreEvals(ctx, store, ev, sessionDate, market); err != nil {
		log.Printf("저장된 평가 복구 실패(처음부터 평가합니다): %v", err)
	} else if restored+skipped > 0 {
		log.Printf("저장된 평가 %d개를 복구했습니다(깨진 항목 %d개 건너뜀)", restored, skipped)
	}
	saveEval := evalSaver(ctx, store, sessionDate, market)
	ev.OnEvaluated = func(e screener.Entry) {
		log.Println(describeEntry(e))
		saveEval(e)
	}
	gate := screener.NewGate(scfg, client, ev, sessionStart)
	chase := tradingloop.NewChaseGuard(applied.ChaseLimitPct)
	log.Printf("스크리너 설정: %+v, 추격 상한 %.2f%%", scfg, applied.ChaseLimitPct*100)

	// 장 시작 전 대기 시간에 어제 랭킹 상위 종목을 미리 평가합니다(정규장 시작 30초 전까지).
	res := gate.PreWarm(ctx, sessionStart.Add(-30*time.Second))
	log.Printf("사전 평가: 랭킹 %d개(상위 %v) 중 %d개 평가, %d개 통과, 시간초과=%v, 랭킹 오류=%v, 종목 오류 %d건",
		res.Ranked, res.Top, res.Evaluated, res.Passed, res.TimedOut, res.Err, len(res.SymbolErrs))

	// 오늘 시가는 정규장이 실제로 시작된 뒤에만 의미가 있습니다(장 시작 전 Price()는
	// 전일 마지막 체결가를 반환) — Cloud Scheduler가 버퍼를 두고 미리 기동하므로
	// (SPEC.md 2.1) 여기서 세션 시작까지 대기합니다.
	if wait := time.Until(sessionStart) + 5*time.Second; wait > 0 {
		log.Printf("정규장 시작까지 %s 대기합니다", wait.Round(time.Second))
		time.Sleep(wait)
	}

	today := time.Now().Format(dateLayout)
	cfg := simulator.Config{StopLossPct: applied.StopLossPct, DailyLossLimitPct: applied.DailyLossLimitPct, CommissionRate: commissionRate, Fractional: market == "US"}
	// 미국 소수점 매수는 금액 지정 주문으로만 가능하고 정규장 종료 1시간 전까지만 접수됩니다(SPEC.md 7.4).
	amountOrderCutoff := sessionEnd.Add(-time.Hour)

	sim, err := loadOrStartSimulator(ctx, store, cfg, today, market, client.USDKRWRate)
	if err != nil {
		return err
	}
	gate.SetSeed(sim.State().Seed)

	// 계좌 낙폭 요약용 과거 기록. 오늘 세션 기록은 현재 상태로 대신하므로(복구된 세션이면 이미 저장돼 있다) 뺀다.
	history, err := store.All(ctx)
	if err != nil {
		log.Printf("과거 세션 기록 조회 실패(낙폭 요약 없이 진행합니다): %v", err)
	}
	history = slices.DeleteFunc(history, func(r session.DailyRecord) bool { return r.Date == today && r.Market == market })
	fills := 0
	withToday := func() []session.DailyRecord {
		return append(slices.Clone(history), session.DailyRecord{Date: today, Market: market, State: sim.State()})
	}
	currentSummary := func() *snapshot.DaySummary {
		sum := buildDaySummary(withToday(), fills, sim.RealizedPnLToday()*sim.State().KRWRate())
		return &sum
	}
	if line := drawdownAlertLine(report.Assess(report.BuildAccountCurve(history))); line != "" {
		log.Print(line)
	}

	uploader := snapshot.NewGCSUploader(nil, os.Getenv("SNAPSHOT_BUCKET"))
	var recentOrders []snapshot.Order
	screenerStatus := func() *snapshot.ScreenerStatus {
		return buildScreenerStatus(gate.Active(), gate.Rank, ev.Entries(), 30)
	}
	publish := func(halted bool, reason string) {
		publishSnapshot(ctx, uploader, sim, client, halted, reason, recentOrders, screenerStatus(), currentSummary(), applied.Document)
	}
	publish(false, "")

	pollInterval := applied.PollInterval()
	const staleness = 30 * time.Second
	heartbeatInterval := applied.HeartbeatInterval()
	lastHeartbeat := time.Now()
	skips := newSkipTracker()
	stale := newStaleTracker()

	for time.Now().Before(eodCutoff) {
		now := time.Now()
		held := ""
		if pos, ok := sim.Position(); ok {
			held = pos.Symbol
		}
		for _, line := range describeUpdate(gate.Refresh(ctx, now), gate.Rank) {
			log.Println(line)
		}
		symbols := pollSymbols(gate.Active(), held)
		setups := setupsFor(gate.Setup, symbols, held)
		if cfg.Fractional && held == "" && now.After(amountOrderCutoff) {
			setups = map[string]simulator.Setup{}
		}
		observations := pollPrices(ctx, client, symbols, held)
		for _, line := range describeSkippedObservations(observations) {
			log.Println(line)
		}
		for _, line := range stale.observe(observations, now, staleness, held) {
			log.Println(line)
		}
		observations, blocked := chase.Filter(observations, setups, held != "", now, staleness)
		for _, symbol := range blocked {
			log.Printf("추격 상한 초과로 오늘 진입에서 제외: %s (목표가 %.2f)", symbol, setups[symbol].TargetPrice)
		}
		actions := skips.filter(tradingloop.ProcessTick(sim, setups, observations, now, staleness), sim.Cash())
		if len(actions) > 0 {
			fills += len(actions)
			for _, action := range actions {
				log.Printf("체결: %s %s %s주 @ %.2f (손익 %.2f, 현금 잔고 %.2f)",
					action.Type, action.Symbol, formatShares(action.Shares), action.Price, action.PnL, sim.Cash())
				recentOrders = appendRecentOrder(recentOrders, toOrder(action, now), 10)
			}
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

	// 청산 시세를 못 받아도 마감 저장·스냅샷·히스토리 보관은 반드시 수행하고, 오류는 그 뒤에 돌려줍니다
	// (2026-09-30: 청산 시세 오류로 조기 반환해 그날 히스토리가 통째로 사라졌음).
	var eodErr error
	if pos, ok := sim.Position(); ok {
		obs := pollPrices(ctx, client, []string{pos.Symbol}, pos.Symbol)[0]
		if obs.Err != nil {
			eodErr = fmt.Errorf("장마감 청산 시세 조회 실패: %w", obs.Err)
			log.Print(eodErr)
		} else {
			eodSetup := setupsFor(gate.Setup, nil, pos.Symbol)[pos.Symbol]
			eodAction := sim.OnTick(eodSetup, obs.Price, true)
			log.Printf("장마감 강제청산: %s %s %s주 @ %.2f (손익 %.2f, 현금 잔고 %.2f)",
				eodAction.Type, eodAction.Symbol, formatShares(eodAction.Shares), eodAction.Price, eodAction.PnL, sim.Cash())
			recentOrders = appendRecentOrder(recentOrders, toOrder(eodAction, time.Now()), 10)
			if eodAction.Type != simulator.NoAction {
				fills++
			}
		}
	}

	for _, line := range skips.summary() {
		log.Println(line)
	}
	for _, line := range stale.summary(time.Now()) {
		log.Println(line)
	}
	eodSummary := currentSummary()
	log.Print(summaryLine(*eodSummary))
	if line := drawdownAlertLine(report.Assess(report.BuildAccountCurve(withToday()))); line != "" {
		log.Print(line)
	}
	if err := store.Save(ctx, today, market, sim.State()); err != nil {
		log.Printf("세션 상태 저장 실패: %v", err)
	}
	publish(false, "")

	if data, err := buildSnapshot(ctx, sim, client, false, "", recentOrders, screenerStatus(), eodSummary, applied.Document).RenderJSON(); err != nil {
		log.Printf("마감 히스토리 스냅샷 생성 실패: %v", err)
	} else if err := uploader.Upload(ctx, historyObjectKey(today, market), "application/json", data); err != nil {
		log.Printf("마감 히스토리 업로드 실패: %v", err)
	}
	return eodErr
}

// loadOrStartSimulator는 오늘 세션을 복구하거나 새로 시작합니다. KR/US는 하나의 원화 계좌를
// 공유하므로 새 세션의 시작 현금은 직전 세션(어느 시장이든)의 원화 잔고이며, US는 시작 시점
// 환율로 달러로 바꿔 세션 내내 같은 환율로 운용합니다(환차손익이 전략 성과에 섞이지 않게).
func loadOrStartSimulator(ctx context.Context, store *session.Store, cfg simulator.Config, today, market string, usdKRW func(context.Context) (float64, error)) (*simulator.Simulator, error) {
	if state, ok, err := store.Load(ctx, today, market); err != nil {
		return nil, err
	} else if ok {
		log.Printf("기존 세션 상태를 복구했습니다: cash=%.2f %s", state.Cash, currencyLabel(state))
		return simulator.Restore(cfg, state), nil
	}

	krw := paperSeed()
	if cash, ok, err := store.LatestKRWCash(ctx); err != nil {
		return nil, err
	} else if ok {
		krw = cash
	}
	currency, rate := "KRW", 1.0
	if market == "US" {
		fx, err := usdKRW(ctx)
		if err != nil {
			return nil, fmt.Errorf("환율 조회 실패(원화 시드를 달러 가격에 그대로 쓰지 않도록 세션을 시작하지 않습니다): %w", err)
		}
		currency, rate = "USD", fx
	}
	start := krw / rate
	log.Printf("새 세션을 시작합니다: 원화 잔고 %.0f원 → seed=%.2f %s (환율 %.2f)", krw, start, currency, rate)
	sim := simulator.New(cfg, start)
	sim.SetCurrency(currency, rate)
	return sim, nil
}

func currencyLabel(st simulator.State) string {
	if st.Currency == "" {
		return "KRW"
	}
	return st.Currency
}

// historyObjectKey는 그날 마감 스냅샷을 영구 보관할 GCS 오브젝트 이름이다.
// status.json은 매번 덮어써져서 하루만 지나도 전날 결과를 알 수 없었던
// 문제(2026-09-28 분석 중 발견)를 고치기 위해 도입.
func historyObjectKey(date, market string) string {
	return fmt.Sprintf("history/%s-%s.json", date, market)
}

// describeSkippedObservations는 시세 조회 실패만 틱마다 기록합니다. stale은 같은 내용이 틱마다
// 반복되므로 staleTracker가 에피소드 단위로 따로 기록합니다.
func describeSkippedObservations(observations []tradingloop.PriceObservation) []string {
	var lines []string
	for _, obs := range observations {
		if obs.Err != nil {
			lines = append(lines, fmt.Sprintf("시세 조회 실패로 이번 틱 스킵: %s: %v", obs.Symbol, obs.Err))
		}
	}
	return lines
}

// toOrder는 simulator.Action을 대시보드용 snapshot.Order로 변환한다.
func toOrder(a simulator.Action, at time.Time) snapshot.Order {
	side := "SELL"
	if a.Type == simulator.Bought {
		side = "BUY"
	}
	return snapshot.Order{
		Symbol:    a.Symbol,
		Side:      side,
		Quantity:  a.Shares,
		Price:     a.Price,
		Status:    a.Type.String(),
		CreatedAt: at,
	}
}

// appendRecentOrder는 최근 주문 목록에 o를 추가하고, max개를 넘으면 가장
// 오래된 것부터 버린다.
func appendRecentOrder(orders []snapshot.Order, o snapshot.Order, max int) []snapshot.Order {
	orders = append(orders, o)
	if len(orders) > max {
		orders = orders[len(orders)-max:]
	}
	return orders
}

// buildSnapshot assembles the current dashboard snapshot without uploading
// it — shared by publishSnapshot (live status.json/status.html) and the
// EOD history archive (historyObjectKey) so both reflect the exact same
// state.
func buildSnapshot(ctx context.Context, sim *simulator.Simulator, client *tossapi.Client, halted bool, reason string, recentOrders []snapshot.Order, status *snapshot.ScreenerStatus, summary *snapshot.DaySummary, applied settings.Document) snapshot.Snapshot {
	s := snapshot.Snapshot{
		DailyLossLimitPct: applied.DailyLossLimitPct,
		KillSwitch:        snapshot.KillSwitchStatus{Halted: halted, Reason: reason},
		RecentOrders:      recentOrders,
		UpdatedAt:         time.Now(),
		Screener:          status,
		Summary:           summary,
		Config:            &applied.Settings,
		ConfigVersion:     applied.Version,
	}
	st := sim.State()
	s.Seed = st.Seed // 당일 시작 시드 — DailyLossProgress의 분모(SPEC.md 4.3)
	s.Currency, s.FXRate = st.Currency, st.FXRate
	s.Version = buildVersion()
	if pos, ok := sim.Position(); ok {
		current := pos.EntryPrice
		if price, err := client.Price(ctx, pos.Symbol); err == nil {
			if p, err := strconv.ParseFloat(price.LastPrice, 64); err == nil {
				current = p
			}
		}
		s.Positions = []snapshot.Position{{
			Symbol:        pos.Symbol,
			Quantity:      pos.Shares,
			UnrealizedPnL: (current - pos.EntryPrice) * pos.Shares,
		}}
		s.DailyPnL = sim.DailyPnL(current)
	} else {
		s.DailyPnL = sim.RealizedPnLToday()
	}
	return s
}

func publishSnapshot(ctx context.Context, uploader *snapshot.GCSUploader, sim *simulator.Simulator, client *tossapi.Client, halted bool, reason string, recentOrders []snapshot.Order, status *snapshot.ScreenerStatus, summary *snapshot.DaySummary, applied settings.Document) {
	s := buildSnapshot(ctx, sim, client, halted, reason, recentOrders, status, summary, applied)
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
