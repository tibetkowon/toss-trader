package backtest

import (
	"fmt"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

type ohlc struct{ o, h, l, c float64 }

var replayDay = time.Date(2026, 10, 5, 0, 0, 0, 0, seoul)

// 어제까지 25개 일봉: 노이즈 4%(구간 안), 종가 상승(추세 OK), 전일 고저 100~104 → 목표가 = 시가 100 + 4×0.5 = 102.
func replayCache(t *testing.T, minutes map[int]ohlc, extraDaily ...tossapi.Candle) Cache {
	t.Helper()
	cache := Cache{Dir: t.TempDir()}
	if err := cache.SaveUniverse("KR", []tossapi.RankingItem{{Symbol: "AAA"}}); err != nil {
		t.Fatal(err)
	}
	var d SymbolData
	for i := 25; i >= 1; i-- {
		day := replayDay.AddDate(0, 0, -i)
		d.Daily = append(d.Daily, bar(day, 100, 104, 100, 100+float64(25-i)*0.5, 1000))
	}
	d.Daily = append(d.Daily, extraDaily...)
	for m := 0; m < 390; m++ { // 09:00~15:29
		b := ohlc{100, 100, 100, 100}
		if v, ok := minutes[m]; ok {
			b = v
		}
		d.Minutes = append(d.Minutes, bar(replayDay.Add(9*time.Hour+time.Duration(m)*time.Minute), b.o, b.h, b.l, b.c, 1000))
	}
	d.Stocks = []tossapi.Stock{{}}
	d.Stocks[0].Raw = []byte(`{}`)
	if err := cache.SaveSymbol("KR", "AAA", d); err != nil {
		t.Fatal(err)
	}
	return cache
}

func runReplayWith(t *testing.T, cache Cache, mod func(*ReplayConfig)) Report {
	t.Helper()
	src, err := NewHistSource(cache, "KR")
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultReplayConfig()
	cfg.CommissionRate = 0.00015
	if mod != nil {
		mod(&cfg)
	}
	r := Replay(cfg, src)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	return r
}

func TestReplayBreakoutThenStopLoss(t *testing.T) {
	// 09:11 봉이 목표가 102를 돌파하고, 다음 봉(09:12)에서 손절가 99.96 아래로 내려간다.
	r := runReplayWith(t, replayCache(t, map[int]ohlc{11: {100, 103, 100, 103}, 12: {102, 102, 99.9, 100}}), nil)
	if len(r.Trades) != 1 {
		t.Fatalf("거래 %d건: %+v", len(r.Trades), r.Trades)
	}
	tr := r.Trades[0]
	if tr.EntryPrice != 102 || tr.Reason != "StoppedOut" || tr.ExitPrice < 99.95 || tr.ExitPrice > 99.97 {
		t.Errorf("진입 %v / 청산 %v / %s (진입 목표가 102, 손절 99.96 기대)", tr.EntryPrice, tr.ExitPrice, tr.Reason)
	}
	if tr.PnL >= 0 {
		t.Errorf("손절 손익이 음수여야 함: %v", tr.PnL)
	}
}

func TestReplayHoldsUntilEndOfDayCutoff(t *testing.T) {
	// 진입 후 -2%에 닿지 않고 가격 105를 유지 → 마감 15분 전(15:15) 봉 시가로 청산.
	m := map[int]ohlc{11: {100, 103, 100, 103}}
	for i := 12; i < 390; i++ {
		m[i] = ohlc{105, 105, 105, 105}
	}
	r := runReplayWith(t, replayCache(t, m), nil)
	if len(r.Trades) != 1 || r.Trades[0].Reason != "ClosedEndOfDay" {
		t.Fatalf("마감 청산 1건 기대: %+v", r.Trades)
	}
	if got := r.Trades[0].ExitTime.In(seoul).Format("15:04"); got != "15:15" {
		t.Errorf("청산 시각 %s, want 15:15", got)
	}
	if r.Trades[0].PnL <= 0 || r.FinalEquity <= r.StartingCash {
		t.Errorf("수익 거래여야 함: %+v", r.Trades[0])
	}
}

func TestReplayIgnoresTodayAndFutureDailyBars(t *testing.T) {
	// 같은 분봉에서 일봉에 당일·미래 봉(극단값)을 섞어도 결과가 달라지면 안 된다.
	m := map[int]ohlc{11: {100, 103, 100, 103}, 12: {102, 102, 99.9, 100}}
	clean := runReplayWith(t, replayCache(t, m), nil)
	poison := []tossapi.Candle{
		bar(replayDay, 100, 500, 1, 400, 1000),
		bar(replayDay.AddDate(0, 0, 1), 100, 900, 1, 800, 1000),
	}
	dirty := runReplayWith(t, replayCache(t, m, poison...), nil)
	if len(clean.Trades) != len(dirty.Trades) || len(clean.Trades) == 0 {
		t.Fatalf("거래 수 불일치(누출 의심): clean=%d dirty=%d", len(clean.Trades), len(dirty.Trades))
	}
	for i := range clean.Trades {
		// time.Time은 타임존 포인터까지 ==로 비교되므로 출력 표현으로 비교한다.
		if fmt.Sprintf("%+v", clean.Trades[i]) != fmt.Sprintf("%+v", dirty.Trades[i]) {
			t.Errorf("거래 %d 불일치(누출 의심): %+v vs %+v", i, clean.Trades[i], dirty.Trades[i])
		}
	}
}

func TestReplayCostsReduceProfit(t *testing.T) {
	// 저가 101: 슬리피지로 진입가가 올라가도 같은 분에 손절선(진입가 -2%)을 건드리지 않게 한다.
	m := map[int]ohlc{11: {101, 103, 101, 103}}
	for i := 12; i < 390; i++ {
		m[i] = ohlc{105, 105, 105, 105}
	}
	cache := replayCache(t, m)
	free := runReplayWith(t, cache, nil)
	costly := runReplayWith(t, cache, func(c *ReplayConfig) { c.SlippageBps = 10; c.SellTaxRate = 0.002 })
	if len(free.Trades) != 1 || len(costly.Trades) != 1 {
		t.Fatalf("거래 수: %d, %d", len(free.Trades), len(costly.Trades))
	}
	// 편도 10bp×2 + 거래세 0.2% ≈ 0.4%p 이상 줄어야 한다(AAA는 ETF 목록에 없는 개별주).
	diff := free.Trades[0].Return() - costly.Trades[0].Return()
	if diff < 0.0035 || diff > 0.0050 {
		t.Errorf("비용 반영 차이 %.4f, want 약 0.004", diff)
	}
}

func TestReplayIntrabarOrderIsConservative(t *testing.T) {
	// 포지션이 없을 때 같은 봉의 고가 돌파(진입)와 저가 99(손절가 아래)가 함께 있으면
	// O,H,L,C 순서로 진입 직후 같은 분에 손절된다.
	r := runReplayWith(t, replayCache(t, map[int]ohlc{11: {100, 103, 99, 100}}), nil)
	if len(r.Trades) != 1 || r.Trades[0].Reason != "StoppedOut" || !r.Trades[0].EntryTime.Equal(r.Trades[0].ExitTime) {
		t.Fatalf("같은 분 진입→손절 1건 기대: %+v", r.Trades)
	}
	// 포지션이 있을 때는 O,L,H,C: 저가가 손절가 아래면 고가가 아무리 높아도 손절이 먼저다.
	r = runReplayWith(t, replayCache(t, map[int]ohlc{11: {100, 103, 100, 103}, 12: {102, 110, 99, 109}}), nil)
	if len(r.Trades) != 1 || r.Trades[0].Reason != "StoppedOut" {
		t.Fatalf("보유 중에는 저가 우선(손절) 기대: %+v", r.Trades)
	}
}

func TestSummarizeTrades(t *testing.T) {
	s := summarizeTrades([]Trade{{Cost: 100, PnL: 2}, {Cost: 100, PnL: -2}, {Cost: 100, PnL: -4}})
	if s.Wins != 1 || s.Losses != 2 || s.AvgWin != 0.02 || s.AvgLoss != -0.03 {
		t.Fatalf("%+v", s)
	}
	if got, want := s.BreakevenWinRate, 0.03/0.05; got < want-1e-9 || got > want+1e-9 {
		t.Errorf("손익분기 승률 %v, want %v", got, want)
	}
	if got := fmt.Sprintf("%.4f", s.Expectancy); got != "-0.0133" {
		t.Errorf("기대값 %s", got)
	}
	if summarizeTrades(nil).Trades != 0 {
		t.Error("빈 목록")
	}
}

func TestReplayEntryDelayPostponesEntry(t *testing.T) {
	// 09:11 돌파 후 가격이 목표가 위(103)에 머문다. 15분 진입 금지면 09:15 이후에야 진입한다.
	m := map[int]ohlc{11: {101, 103, 101, 103}}
	for i := 12; i < 390; i++ {
		m[i] = ohlc{103, 103, 103, 103}
	}
	r := runReplayWith(t, replayCache(t, m), func(c *ReplayConfig) { c.EntryDelayMin = 15 })
	if len(r.Trades) != 1 {
		t.Fatalf("거래 %d건: %+v", len(r.Trades), r.Trades)
	}
	if got := r.Trades[0].EntryTime.In(seoul).Format("15:04"); got != "09:15" {
		t.Errorf("진입 시각 %s, want 09:15", got)
	}
}

func TestReplayTrailingStopExitsAndBlocksReentry(t *testing.T) {
	m := map[int]ohlc{11: {101, 103, 101, 103}, 12: {103, 106, 103, 106}, 13: {106, 106, 104.5, 105}}
	for i := 14; i < 390; i++ {
		m[i] = ohlc{105, 105, 105, 105} // 목표가 위로 다시 올라와도 재진입하면 안 된다
	}
	r := runReplayWith(t, replayCache(t, m), func(c *ReplayConfig) { c.TrailActivate, c.TrailGap = 0.02, 0.01 })
	if len(r.Trades) != 1 || r.Trades[0].Reason != "TrailingStop" {
		t.Fatalf("트레일링 청산 1건 기대: %+v", r.Trades)
	}
	// 고점 106, 갭 1% → 104.94. 시가 106이 그 위이므로 레벨에서 체결된다.
	if px := r.Trades[0].ExitPrice; px < 104.9 || px > 105 {
		t.Errorf("트레일링 청산가 %v, want 약 104.94", px)
	}
}

func TestReplayNoiseScaledStopIsTighterThanFixed(t *testing.T) {
	// 진입가 102에서 저가 100.5: 고정 2%(99.96)에는 안 닿지만, 노이즈 4%×0.25=1%(100.98)에는 닿는다.
	m := map[int]ohlc{11: {101, 103, 101, 103}, 12: {102, 102, 100.5, 101}}
	base := runReplayWith(t, replayCache(t, m), nil)
	for _, tr := range base.Trades {
		if tr.Reason == "StoppedOut" {
			t.Fatalf("고정 2%%에서는 손절되면 안 됨: %+v", tr)
		}
	}
	r := runReplayWith(t, replayCache(t, m), func(c *ReplayConfig) { c.StopNoiseMult, c.StopMinPct, c.StopMaxPct = 0.25, 0.01, 0.02 })
	if len(r.Trades) != 1 || r.Trades[0].Reason != "StoppedOut" || r.Trades[0].ExitPrice < 100.9 || r.Trades[0].ExitPrice > 101.0 {
		t.Fatalf("노이즈 비례 손절 기대: %+v", r.Trades)
	}
}
