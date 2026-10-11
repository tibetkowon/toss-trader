package backtest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

var seoul = time.FixedZone("KST", 9*3600)

func bar(t time.Time, o, h, l, c float64, vol int) tossapi.Candle {
	return tossapi.Candle{
		Timestamp: t.Format("2006-01-02T15:04:05.000-07:00"),
		OpenPrice: fmt.Sprint(o), HighPrice: fmt.Sprint(h), LowPrice: fmt.Sprint(l), ClosePrice: fmt.Sprint(c),
		Volume: fmt.Sprint(vol),
	}
}

// 두 거래일(10/6, 10/7) 동안 AAA는 거래대금이 크고 BBB는 작다. 10/6은 BBB가 더 크다.
func testSource(t *testing.T) *HistSource {
	t.Helper()
	cache := Cache{Dir: t.TempDir()}
	days := []time.Time{time.Date(2026, 10, 5, 0, 0, 0, 0, seoul), time.Date(2026, 10, 6, 0, 0, 0, 0, seoul), time.Date(2026, 10, 7, 0, 0, 0, 0, seoul)}
	mk := func(vol1, vol2 int) SymbolData {
		var d SymbolData
		for i, day := range days {
			d.Daily = append(d.Daily, bar(day, 100, 110, 90, 100, 1000*(i+1)))
		}
		for _, day := range days[1:] {
			for m := 0; m < 3; m++ { // 09:00~09:02 정규장 봉
				d.Minutes = append(d.Minutes, bar(day.Add(9*time.Hour+time.Duration(m)*time.Minute), 100, 101, 99, 100+float64(m), vol1))
			}
			d.Minutes = append(d.Minutes, bar(day.Add(8*time.Hour+59*time.Minute), 1, 1, 1, 1, 999999)) // 장전 봉: 무시되어야 함
		}
		if vol2 > 0 {
			d.Minutes[0].Volume = fmt.Sprint(vol2)
		}
		return d
	}
	uni := []tossapi.RankingItem{{Symbol: "AAA"}, {Symbol: "BBB"}}
	if err := cache.SaveUniverse("KR", uni); err != nil {
		t.Fatal(err)
	}
	for sym, vol := range map[string]int{"AAA": 100, "BBB": 10} {
		d := mk(vol, 0)
		d.Stocks = []tossapi.Stock{{}}
		d.Stocks[0].Raw = []byte(`{}`)
		if err := cache.SaveSymbol("KR", sym, d); err != nil {
			t.Fatal(err)
		}
	}
	h, err := NewHistSource(cache, "KR")
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestCandlesNeverLookPastClock(t *testing.T) {
	h := testSource(t)
	ctx := context.Background()

	h.SetClock(time.Date(2026, 10, 7, 9, 1, 0, 0, seoul)) // 09:01: 09:00 봉만 완성
	m, _, err := h.Candles(ctx, "AAA", "1m", 200, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range m {
		ts, _ := time.Parse(time.RFC3339, c.Timestamp)
		if !ts.Before(h.clock) {
			t.Errorf("clock 이후 분봉 반환: %s", c.Timestamp)
		}
		if ts.In(seoul).Hour() == 8 {
			t.Errorf("장전 봉이 섞임: %s", c.Timestamp)
		}
	}
	if len(m) != 4 || m[0].Timestamp[:16] != "2026-10-07T09:00" { // 10/6 3개 + 10/7 1개, 최신순
		t.Errorf("분봉 %d개, 첫 봉 %s", len(m), m[0].Timestamp)
	}

	d, _, _ := h.Candles(ctx, "AAA", "1d", 200, "")
	if len(d) != 2 || d[0].Timestamp[:10] != "2026-10-06" {
		t.Errorf("당일(10/7) 일봉이 섞이거나 최신순이 아님: %d개 %v", len(d), d[0].Timestamp)
	}
}

func TestCandlesPagination(t *testing.T) {
	h := testSource(t)
	h.SetClock(time.Date(2026, 10, 7, 15, 0, 0, 0, seoul))
	ctx := context.Background()
	var all []string
	before := ""
	for i := 0; i < 10; i++ {
		page, next, err := h.Candles(ctx, "AAA", "1m", 2, before)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range page {
			all = append(all, c.Timestamp)
		}
		if next == "" {
			break
		}
		before = next
	}
	if len(all) != 6 {
		t.Fatalf("페이지를 이어 읽은 봉 %d개, want 6: %v", len(all), all)
	}
	for i := 1; i < len(all); i++ {
		if all[i-1] <= all[i] {
			t.Errorf("최신순/중복 위반: %s, %s", all[i-1], all[i])
		}
	}
}

func TestRankingsPreOpenUsesPreviousDailyOnly(t *testing.T) {
	h := testSource(t)
	// 10/7 장 시작 전: 전일(10/6) 일봉 거래량 2000 — 동률이면 순서가 유니버스 순서(안정 정렬)여야 한다.
	h.SetClock(time.Date(2026, 10, 7, 8, 30, 0, 0, seoul))
	r, err := h.Rankings(context.Background(), "MARKET_TRADING_AMOUNT", "1d", "KR")
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 || r[0].Rank != 1 || r[1].Rank != 2 {
		t.Fatalf("랭킹 %+v", r)
	}
	if r[0].TradingVolume != "2000" {
		t.Errorf("전일 거래량 2000이어야 함: %s (당일/미래 일봉 누출?)", r[0].TradingVolume)
	}
}

func TestRankingsIntradayAccumulatesFromOpenToClock(t *testing.T) {
	h := testSource(t)
	ctx := context.Background()
	// 09:02: 09:00, 09:01 두 봉만 누적. AAA = 100*100 + 100*101 = 20100, BBB = 10*100 + 10*101 = 2010
	h.SetClock(time.Date(2026, 10, 7, 9, 2, 0, 0, seoul))
	r, _ := h.Rankings(ctx, "MARKET_TRADING_AMOUNT", "1d", "KR")
	if r[0].Symbol != "AAA" || r[0].TradingAmount != "20100" || r[1].TradingAmount != "2010" {
		t.Errorf("장중 누적 거래대금: %+v", r)
	}
	if r[0].Price.LastPrice != "101" {
		t.Errorf("마지막 가격은 clock 이전 마지막 분봉 종가여야 함: %s", r[0].Price.LastPrice)
	}
	// 장 시작 직후(09:00 정각)에는 아직 완성된 봉이 없으므로 누적 0이어야 한다(전날 값이 새면 안 됨).
	h.SetClock(time.Date(2026, 10, 7, 9, 0, 0, 0, seoul))
	r, _ = h.Rankings(ctx, "MARKET_TRADING_AMOUNT", "1d", "KR")
	if r[0].TradingAmount != "0" {
		t.Errorf("개장 직후 누적은 0이어야 함: %s", r[0].TradingAmount)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	cache := Cache{Dir: t.TempDir()}
	in := SymbolData{Daily: []tossapi.Candle{{Timestamp: "2026-10-06T00:00:00.000+09:00", ClosePrice: "5"}}, Minutes: []tossapi.Candle{}}
	in.Stocks = []tossapi.Stock{{}}
	in.Stocks[0].Raw = []byte(`{"x":1}`)
	if err := cache.SaveSymbol("KR", "AAA", in); err != nil {
		t.Fatal(err)
	}
	out, err := cache.LoadSymbol("KR", "AAA")
	if err != nil || len(out.Daily) != 1 || out.Daily[0].ClosePrice != "5" || string(out.Stocks[0].Raw) != `{"x":1}` {
		t.Errorf("왕복 실패: %+v, %v", out, err)
	}
	if _, err := cache.LoadSymbol("KR", "../x"); err == nil {
		t.Error("경로 이탈 심볼은 거부되어야 함")
	}
}
