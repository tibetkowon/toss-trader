package screener

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

var kst = time.FixedZone("KST", 9*3600)

func testSessionStart() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, kst) }

func testConfig() Config {
	return Config{
		Market: "KR", NoiseMin: 0.025, NoiseMax: 0.06, NoiseWindow: 20, MAWindow: 5, K: 0.5,
		RankDepth: 5, ActiveCount: 2, MinAffordable: 0, EvalPerMin: 0,
		StartDelay: 5 * time.Minute, RefreshEvery: time.Minute, LazyExpand: true,
		Seed: 1_000_000, CommissionRate: 0,
	}
}

type fakeSource struct {
	rankings   []tossapi.RankingItem
	rankingErr error
	stocks     map[string]tossapi.Stock
	warnings   map[string][]json.RawMessage
	daily      map[string][]tossapi.Candle
	minute     map[string][]tossapi.Candle
	errOn      map[string]error // 키: "Stocks:SYM", "Warnings:SYM", "Daily:SYM", "Minute:SYM"
	calls      map[string]int
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		stocks: map[string]tossapi.Stock{}, warnings: map[string][]json.RawMessage{},
		daily: map[string][]tossapi.Candle{}, minute: map[string][]tossapi.Candle{},
		errOn: map[string]error{}, calls: map[string]int{},
	}
}

func (f *fakeSource) Rankings(ctx context.Context, rankingType, duration, market string) ([]tossapi.RankingItem, error) {
	f.calls["Rankings"]++
	return f.rankings, f.rankingErr
}

func (f *fakeSource) Stocks(ctx context.Context, symbols ...string) ([]tossapi.Stock, error) {
	f.calls["Stocks"]++
	if err := f.errOn["Stocks:"+symbols[0]]; err != nil {
		return nil, err
	}
	if s, ok := f.stocks[symbols[0]]; ok {
		return []tossapi.Stock{s}, nil
	}
	return nil, nil
}

func (f *fakeSource) StockWarnings(ctx context.Context, symbol string) ([]json.RawMessage, error) {
	f.calls["Warnings"]++
	if err := f.errOn["Warnings:"+symbol]; err != nil {
		return nil, err
	}
	return f.warnings[symbol], nil
}

func (f *fakeSource) Candles(ctx context.Context, symbol, interval string, count int, before string) ([]tossapi.Candle, string, error) {
	if interval == "1d" {
		f.calls["Daily"]++
		if err := f.errOn["Daily:"+symbol]; err != nil {
			return nil, "", err
		}
		return f.daily[symbol], "", nil
	}
	f.calls["Minute"]++
	if err := f.errOn["Minute:"+symbol]; err != nil {
		return nil, "", err
	}
	return f.minute[symbol], "", nil
}

// addStock은 평범한(레버리지 아님) 종목 정보를 등록합니다.
func (f *fakeSource) addStock(symbol string) {
	f.stocks[symbol] = tossapi.Stock{
		Symbol: symbol,
		Raw:    json.RawMessage(fmt.Sprintf(`{"symbol":%q,"leverageFactor":"1"}`, symbol)),
	}
}

// dailyCandles는 최신순 일봉 n개를 만듭니다. 직전 봉은 시 100 고 104 저 100 종 103
// (노이즈 4%, 추세 통과), 그 이전 봉은 종가 100입니다. withToday면 형성 중인 오늘 봉을 맨 앞에 넣습니다.
func dailyCandles(sessionDate string, n int, withToday bool) []tossapi.Candle {
	day, _ := time.Parse("2006-01-02", sessionDate)
	var out []tossapi.Candle
	if withToday {
		out = append(out, tossapi.Candle{
			Timestamp: sessionDate + "T00:00:00.000+09:00",
			OpenPrice: "110", HighPrice: "130", LowPrice: "109", ClosePrice: "125",
		})
	}
	for i := 1; i <= n; i++ {
		closePrice := "100"
		if i == 1 {
			closePrice = "103"
		}
		out = append(out, tossapi.Candle{
			Timestamp: day.AddDate(0, 0, -i).Format("2006-01-02") + "T00:00:00.000+09:00",
			OpenPrice: "100", HighPrice: "104", LowPrice: "100", ClosePrice: closePrice,
		})
	}
	return out
}

func minuteCandle(ts time.Time, open string) tossapi.Candle {
	return tossapi.Candle{Timestamp: ts.Format(time.RFC3339), OpenPrice: open, HighPrice: open, LowPrice: open, ClosePrice: open}
}

func rankingItem(rank int, symbol, price string) tossapi.RankingItem {
	return tossapi.RankingItem{Rank: rank, Symbol: symbol, Price: tossapi.RankingPrice{LastPrice: price}}
}
