package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/tibetkowon/toss-trader/internal/screener"
	"github.com/tibetkowon/toss-trader/internal/tossapi"
)

// HistSource는 로드 이후 네트워크를 사용하지 않습니다. 동시 사용은 지원하지 않습니다.
type HistSource struct {
	market   string
	loc      *time.Location
	clock    time.Time
	universe []tossapi.RankingItem
	data     map[string]SymbolData
	idx      map[string]*symIndex
}

// symIndex는 종목별로 한 번 계산해 두는 조회용 색인입니다. 분봉을 매번 처음부터 훑으면
// 수개월 × 분 단위 × 종목 100개 재생이 끝나지 않습니다.
type symIndex struct {
	dayT, minT []time.Time // data.Daily/Minutes와 같은 순서(최신순)
	ascT       []time.Time // 정규장 분봉만, 시간 오름차순
	cumAmt     []float64   // ascT 시점까지(그 봉 포함) 같은 날 정규장 누적 거래대금
	cumVol     []float64
}

var _ screener.Source = (*HistSource)(nil)

func NewHistSource(cache Cache, market string) (*HistSource, error) {
	zone := "Asia/Seoul"
	if market == "US" {
		zone = "America/New_York"
	} else if market != "KR" {
		return nil, fmt.Errorf("잘못된 시장: %s", market)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, err
	}
	u, err := cache.LoadUniverse(market)
	if err != nil {
		return nil, err
	}
	h := &HistSource{market: market, loc: loc, universe: u, data: map[string]SymbolData{}, idx: map[string]*symIndex{}}
	for _, item := range u {
		d, err := cache.LoadSymbol(market, item.Symbol)
		if err != nil {
			return nil, err
		}
		d.Daily, err = ordered(d.Daily)
		if err != nil {
			return nil, err
		}
		d.Minutes, err = ordered(d.Minutes)
		if err != nil {
			return nil, err
		}
		h.data[item.Symbol] = d
		h.idx[item.Symbol] = h.index(d)
	}
	return h, nil
}
func (h *HistSource) index(d SymbolData) *symIndex {
	x := &symIndex{}
	parse := func(bars []tossapi.Candle) []time.Time {
		out := make([]time.Time, len(bars))
		for i, b := range bars {
			out[i], _ = time.Parse(time.RFC3339, b.Timestamp)
		}
		return out
	}
	x.dayT, x.minT = parse(d.Daily), parse(d.Minutes)
	number := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
	var amt, vol float64
	var day string
	for i := len(d.Minutes) - 1; i >= 0; i-- { // 오래된 것부터
		t := x.minT[i]
		if !h.regular(t) {
			continue
		}
		if k := t.In(h.loc).Format("2006-01-02"); k != day {
			day, amt, vol = k, 0, 0
		}
		v := number(d.Minutes[i].Volume)
		vol += v
		amt += v * number(d.Minutes[i].ClosePrice)
		x.ascT = append(x.ascT, t)
		x.cumAmt = append(x.cumAmt, amt)
		x.cumVol = append(x.cumVol, vol)
	}
	return x
}

func ordered(c []tossapi.Candle) ([]tossapi.Candle, error) {
	times := map[string]time.Time{}
	unique := map[time.Time]tossapi.Candle{}
	for _, b := range c {
		t, err := time.Parse(time.RFC3339, b.Timestamp)
		if err != nil {
			return nil, err
		}
		times[b.Timestamp] = t
		unique[t.UTC()] = b
	}
	out := make([]tossapi.Candle, 0, len(unique))
	for _, b := range unique {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return times[out[i].Timestamp].After(times[out[j].Timestamp]) })
	return out, nil
}
func (h *HistSource) SetClock(t time.Time) { h.clock = t }
func (h *HistSource) session(t time.Time) (time.Time, time.Time) {
	t = t.In(h.loc)
	hour, minute, end := 9, 0, 15
	if h.market == "US" {
		minute, end = 30, 16
	}
	open := time.Date(t.Year(), t.Month(), t.Day(), hour, minute, 0, 0, h.loc)
	closeMinute := 30
	if h.market == "US" {
		closeMinute = 0
	}
	return open, time.Date(t.Year(), t.Month(), t.Day(), end, closeMinute, 0, 0, h.loc)
}
func (h *HistSource) regular(t time.Time) bool {
	open, close := h.session(t)
	return !t.Before(open) && t.Before(close)
}
func (h *HistSource) Candles(ctx context.Context, symbol, interval string, count int, before string) ([]tossapi.Candle, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if count < 1 || count > 200 {
		return nil, "", fmt.Errorf("count는 1~200이어야 합니다")
	}
	d, ok := h.data[symbol]
	if !ok {
		return nil, "", fmt.Errorf("캐시 종목 없음: %s", symbol)
	}
	bars := d.Minutes
	if interval == "1d" {
		bars = d.Daily
	} else if interval != "1m" {
		return nil, "", fmt.Errorf("지원하지 않는 간격: %s", interval)
	}
	limit := h.clock
	if before != "" {
		t, err := time.Parse(time.RFC3339, before)
		if err != nil {
			return nil, "", err
		}
		if t.Before(limit) {
			limit = t
		}
	}
	times := h.idx[symbol].minT
	if interval == "1d" {
		times = h.idx[symbol].dayT
	}
	today := h.clock.In(h.loc).Format("2006-01-02")
	out := make([]tossapi.Candle, 0, count)
	// bars는 최신순이므로 limit보다 오래된 첫 위치를 이진 탐색으로 찾는다.
	for i := sort.Search(len(times), func(i int) bool { return times[i].Before(limit) }); i < len(bars); i++ {
		b, t := bars[i], times[i]
		if interval == "1d" {
			// 일봉은 API가 표시한 거래일을 유지합니다(BarsFromCandles와 동일).
			if b.Timestamp[:10] >= today {
				continue
			}
		} else if !h.regular(t) {
			continue
		}
		if len(out) == count {
			return out, out[len(out)-1].Timestamp, nil
		}
		out = append(out, b)
	}
	return out, "", nil
}
func (h *HistSource) Stocks(ctx context.Context, symbols ...string) ([]tossapi.Stock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []tossapi.Stock
	for _, s := range symbols {
		d, ok := h.data[s]
		if !ok {
			return nil, fmt.Errorf("캐시 종목 없음: %s", s)
		}
		out = append(out, d.Stocks...)
	}
	return out, nil
}
func (h *HistSource) StockWarnings(ctx context.Context, symbol string) ([]json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d, ok := h.data[symbol]
	if !ok {
		return nil, fmt.Errorf("캐시 종목 없음: %s", symbol)
	}
	return d.Warnings, nil
}
func (h *HistSource) Rankings(ctx context.Context, kind, duration, market string) ([]tossapi.RankingItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if kind != "MARKET_TRADING_AMOUNT" || duration != "1d" || market != h.market {
		return nil, fmt.Errorf("지원하지 않는 랭킹 요청")
	}
	open, _ := h.session(h.clock)
	type ranked struct {
		item   tossapi.RankingItem
		amount float64
	}
	var rows []ranked
	number := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
	for _, u := range h.universe {
		minutes, _, err := h.Candles(ctx, u.Symbol, "1m", 1, "")
		if err != nil {
			return nil, err
		}
		item := tossapi.RankingItem{Symbol: u.Symbol, Currency: u.Currency}
		if len(minutes) > 0 {
			item.Price.LastPrice = minutes[0].ClosePrice
		}
		amount, volume := 0.0, 0.0
		if h.clock.Before(open) {
			daily, _, err := h.Candles(ctx, u.Symbol, "1d", 1, "")
			if err != nil {
				return nil, err
			}
			if len(daily) == 0 {
				continue
			}
			b := daily[0]
			volume = number(b.Volume)
			amount = volume * (number(b.OpenPrice) + number(b.HighPrice) + number(b.LowPrice) + number(b.ClosePrice)) / 4
		} else {
			x := h.idx[u.Symbol]
			// clock 이전 마지막 정규장 분봉의 당일 누적값. 오늘 봉이 없으면 0.
			if j := sort.Search(len(x.ascT), func(i int) bool { return !x.ascT[i].Before(h.clock) }); j > 0 && !x.ascT[j-1].Before(open) {
				amount, volume = x.cumAmt[j-1], x.cumVol[j-1]
			}
		}
		item.TradingAmount = strconv.FormatFloat(amount, 'f', -1, 64)
		item.TradingVolume = strconv.FormatFloat(volume, 'f', -1, 64)
		rows = append(rows, ranked{item, amount})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].amount > rows[j].amount })
	out := make([]tossapi.RankingItem, len(rows))
	for i, r := range rows {
		out[i] = r.item
		out[i].Rank = i + 1
	}
	return out, nil
}
