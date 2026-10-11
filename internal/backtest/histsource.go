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
	ascT       []time.Time // 정규장 분봉 시각만, 시간 오름차순(재생할 거래일 목록용)
	// 랭킹용: 실제 Rankings(1d) 거래대금은 장전·정규장·장후(NXT 등) 봉을 모두 합한 당일 누적이다
	// (2026-10-08 실측: 대형주 real/전체봉합 = 1.00, 정규장만이면 1.1~1.5). 현지 날짜별로 누적한다.
	allT   []time.Time // 모든 분봉, 시간 오름차순
	allAmt []float64   // 같은 현지 날짜 누적 거래대금(거래량 × 종가)
	allVol []float64
	allPx  []float64 // 봉 종가(현재가)
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
	for i := len(d.Minutes) - 1; i >= 0; i-- { // 오래된 것부터
		if h.regular(x.minT[i]) {
			x.ascT = append(x.ascT, x.minT[i])
		}
	}
	var amt, vol float64
	var day string
	for i := len(d.Minutes) - 1; i >= 0; i-- {
		t := x.minT[i]
		if k := t.In(h.loc).Format("2006-01-02"); k != day {
			day, amt, vol = k, 0, 0
		}
		v, px := number(d.Minutes[i].Volume), number(d.Minutes[i].ClosePrice)
		vol += v
		amt += v * px
		x.allT = append(x.allT, t)
		x.allAmt = append(x.allAmt, amt)
		x.allVol = append(x.allVol, vol)
		x.allPx = append(x.allPx, px)
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
	// 랭킹 하루의 시작: 국내 NXT 장전 08:00, 미국 확장시간 04:00(현지). 이전이면 직전 거래일 전체 합을 보인다.
	dayStart := time.Date(h.clock.In(h.loc).Year(), h.clock.In(h.loc).Month(), h.clock.In(h.loc).Day(), 8, 0, 0, 0, h.loc)
	if h.market == "US" {
		dayStart = dayStart.Add(-4 * time.Hour)
	}
	type ranked struct {
		item   tossapi.RankingItem
		amount float64
	}
	var rows []ranked
	for _, u := range h.universe {
		x := h.idx[u.Symbol]
		// clock 이전 마지막 봉(장전·장후 포함).
		j := sort.Search(len(x.allT), func(i int) bool { return !x.allT[i].Before(h.clock) })
		if j == 0 {
			continue
		}
		k := j - 1
		if !h.clock.Before(dayStart) && x.allT[k].Before(dayStart) {
			continue // 오늘 거래가 아직 없음(실제 랭킹은 거래대금 0 종목을 싣지 않는다)
		}
		amount, volume := x.allAmt[k], x.allVol[k]
		if amount <= 0 {
			continue
		}
		item := tossapi.RankingItem{Symbol: u.Symbol, Currency: u.Currency}
		item.Price.LastPrice = strconv.FormatFloat(x.allPx[k], 'f', -1, 64)
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

func parseNum(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
