package screener

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

func newTestEvaluator(f *fakeSource) *Evaluator {
	return NewEvaluator(f, testConfig(), testSessionStart())
}

func passingSource(symbols ...string) *fakeSource {
	f := newFakeSource()
	for _, s := range symbols {
		f.addStock(s)
		f.daily[s] = dailyCandles("2026-10-01", 22, true)
	}
	return f
}

func TestStaticPassesAndUsesOnlyPriorBars(t *testing.T) {
	f := passingSource("A")
	ev := newTestEvaluator(f)
	e, ok, err := ev.Static(context.Background(), "A", "prewarm", time.Now(), false)
	if err != nil || !ok {
		t.Fatalf("Static = %v %v", ok, err)
	}
	if !e.Passed || e.Origin != "prewarm" {
		t.Fatalf("entry = %+v", e)
	}
	if e.NoiseRatio < 0.0399 || e.NoiseRatio > 0.0401 {
		t.Errorf("노이즈 = %v, 기대 0.04 (오늘 형성 중인 봉이 섞이면 달라집니다)", e.NoiseRatio)
	}
	if e.Prev.Date != "2026-09-30" || e.Prev.Close != 103 || len(e.Closes) != 5 || e.Closes[0] != 103 {
		t.Errorf("전일 봉/종가: %+v %v", e.Prev, e.Closes)
	}
	if e.HasOpen {
		t.Error("정적 평가 단계에서 시가가 확정되었습니다")
	}
}

func TestStaticRejections(t *testing.T) {
	f := passingSource("OK")

	f.addStock("LEV")
	f.stocks["LEV"] = tossapi.Stock{Symbol: "LEV", Raw: json.RawMessage(`{"leverageFactor":"3"}`)}
	f.daily["LEV"] = dailyCandles("2026-10-01", 22, true)

	f.stocks["LIQ"] = tossapi.Stock{Symbol: "LIQ", Raw: json.RawMessage(`{}`), KoreanMarketDetail: &tossapi.KoreanMarketDetail{LiquidationTrading: true}}
	f.stocks["SUS"] = tossapi.Stock{Symbol: "SUS", Raw: json.RawMessage(`{}`), KoreanMarketDetail: &tossapi.KoreanMarketDetail{KrxTradingSuspended: true}}

	f.addStock("SHORT")
	f.daily["SHORT"] = dailyCandles("2026-10-01", 10, true)

	f.addStock("QUIET")
	quiet := dailyCandles("2026-10-01", 22, true)
	for i := range quiet[1:] {
		quiet[i+1].HighPrice = "101" // 노이즈 1%
	}
	f.daily["QUIET"] = quiet

	f.addStock("WILD")
	wild := dailyCandles("2026-10-01", 22, true)
	for i := range wild[1:] {
		wild[i+1].HighPrice = "110" // 노이즈 10%
	}
	f.daily["WILD"] = wild

	f.addStock("WARN")
	f.daily["WARN"] = dailyCandles("2026-10-01", 22, true)
	f.warnings["WARN"] = []json.RawMessage{json.RawMessage(`{"type":"투자경고"}`)}

	cases := map[string]string{
		"LEV": "레버리지", "LIQ": "정리매매", "SUS": "거래정지", "SHORT": "일봉 부족",
		"QUIET": "노이즈", "WILD": "노이즈", "WARN": "유의사항", "NOSTOCK": "종목 정보 없음",
	}
	ev := newTestEvaluator(f)
	for symbol, wantReason := range cases {
		e, ok, err := ev.Static(context.Background(), symbol, "intraday", time.Now(), false)
		if err != nil || !ok {
			t.Fatalf("%s: Static = %v %v", symbol, ok, err)
		}
		if e.Passed || !strings.Contains(e.Reason, wantReason) {
			t.Errorf("%s: %+v, 기대 사유 %q", symbol, e, wantReason)
		}
	}
	if f.calls["Warnings"] != 1 {
		t.Errorf("유의사항 조회는 노이즈까지 통과한 종목에만: %d회", f.calls["Warnings"])
	}
}

func TestStaticCachesIncludingRejections(t *testing.T) {
	f := passingSource("A")
	f.stocks["B"] = tossapi.Stock{Symbol: "B", Raw: json.RawMessage(`{"leverageFactor":"3"}`)}
	ev := newTestEvaluator(f)
	for i := 0; i < 3; i++ {
		ev.Static(context.Background(), "A", "intraday", time.Now(), false)
		ev.Static(context.Background(), "B", "intraday", time.Now(), false)
	}
	if f.calls["Stocks"] != 2 {
		t.Errorf("Stocks 호출 %d회, 기대 2회(탈락도 캐시)", f.calls["Stocks"])
	}
}

func TestStaticAPIErrorIsNotCached(t *testing.T) {
	f := passingSource("A")
	f.errOn["Daily:A"] = errors.New("boom")
	ev := newTestEvaluator(f)
	if _, ok, err := ev.Static(context.Background(), "A", "intraday", time.Now(), false); ok || err == nil {
		t.Fatalf("오류가 전달되지 않았습니다: %v %v", ok, err)
	}
	if _, cached := ev.Cached("A"); cached {
		t.Fatal("API 오류가 캐시되었습니다")
	}
	delete(f.errOn, "Daily:A")
	if e, ok, err := ev.Static(context.Background(), "A", "intraday", time.Now(), false); err != nil || !ok || !e.Passed {
		t.Fatalf("재시도 실패: %+v %v %v", e, ok, err)
	}
}

func TestStaticPerMinuteBudget(t *testing.T) {
	f := passingSource("A", "B", "C")
	cfg := testConfig()
	cfg.EvalPerMin = 2
	ev := NewEvaluator(f, cfg, testSessionStart())
	now := time.Date(2026, 10, 1, 9, 5, 0, 0, kst)
	for _, s := range []string{"A", "B"} {
		if _, ok, _ := ev.Static(context.Background(), s, "intraday", now, true); !ok {
			t.Fatalf("%s: 예산 안인데 평가되지 않았습니다", s)
		}
	}
	if _, ok, err := ev.Static(context.Background(), "C", "intraday", now.Add(10*time.Second), true); ok || err != nil {
		t.Fatalf("예산 소진 후: ok=%v err=%v", ok, err)
	}
	if _, ok, _ := ev.Static(context.Background(), "A", "intraday", now.Add(10*time.Second), true); !ok {
		t.Error("캐시된 종목은 예산과 무관하게 반환되어야 합니다")
	}
	if _, ok, _ := ev.Static(context.Background(), "C", "intraday", now.Add(61*time.Second), true); !ok {
		t.Error("1분이 지나면 예산이 복구되어야 합니다")
	}
	if _, ok, _ := ev.Static(context.Background(), "C", "prewarm", now, false); !ok {
		t.Error("limited=false는 예산을 쓰지 않습니다")
	}
}

func TestOnEvaluatedAndSeed(t *testing.T) {
	f := passingSource("A")
	ev := newTestEvaluator(f)
	var seen []string
	ev.OnEvaluated = func(e Entry) { seen = append(seen, e.Symbol) }
	ev.Seed([]Entry{{Symbol: "S", Passed: true}})
	ev.Static(context.Background(), "A", "intraday", time.Now(), false)
	ev.Static(context.Background(), "A", "intraday", time.Now(), false)
	ev.Static(context.Background(), "S", "intraday", time.Now(), false)
	if len(seen) != 1 || seen[0] != "A" {
		t.Errorf("콜백 호출: %v (Seed와 캐시 적중은 호출하지 않아야 합니다)", seen)
	}
	got := ev.Entries()
	if len(got) != 2 || got[0].Symbol != "A" || got[1].Symbol != "S" {
		t.Errorf("Entries = %+v", got)
	}
}
