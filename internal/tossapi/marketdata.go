package tossapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// RankingPrice는 랭킹 항목의 가격 정보를 담습니다.
type RankingPrice struct {
	LastPrice  string `json:"lastPrice"`
	BasePrice  string `json:"basePrice"`
	ChangeRate string `json:"changeRate"`
}

// RankingItem은 거래대금 등 랭킹 한 항목과 응답 원문을 보존합니다.
// 2026-09-27 실API로 확인한 실제 스키마를 반영합니다.
type RankingItem struct {
	Rank          int             `json:"rank"`
	Symbol        string          `json:"symbol"`
	Currency      string          `json:"currency"`
	Price         RankingPrice    `json:"price"`
	TradingVolume string          `json:"tradingVolume"`
	TradingAmount string          `json:"tradingAmount"`
	Raw           json.RawMessage `json:"-"`
}

// Rankings는 계좌 헤더 없이 유동성(거래대금 등) 랭킹을 조회합니다.
// rankingType 예: "MARKET_TRADING_AMOUNT". duration 예: "1mo". marketCountry는 "KR" 또는 "US"만 허용됩니다.
func (c *Client) Rankings(ctx context.Context, rankingType, duration, marketCountry string) ([]RankingItem, error) {
	if strings.TrimSpace(rankingType) == "" {
		return nil, errors.New("rankingType이 필요합니다")
	}
	if strings.TrimSpace(duration) == "" {
		return nil, errors.New("duration이 필요합니다")
	}
	if marketCountry != "KR" && marketCountry != "US" {
		return nil, errors.New("marketCountry는 KR 또는 US여야 합니다")
	}
	query := url.Values{
		"type":          {rankingType},
		"duration":      {duration},
		"marketCountry": {marketCountry},
	}
	body, err := c.get(ctx, "RANKING", "/api/v1/rankings?"+query.Encode(), "")
	if err != nil {
		return nil, err
	}
	body = unwrapData(body)
	var payload struct {
		Rankings []RankingItem `json:"rankings"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("잘못된 랭킹 응답")
	}
	var rawPayload struct {
		Rankings []json.RawMessage `json:"rankings"`
	}
	if err := json.Unmarshal(body, &rawPayload); err != nil || len(rawPayload.Rankings) != len(payload.Rankings) {
		return nil, errors.New("랭킹 응답 원문 파싱 실패")
	}
	for i := range payload.Rankings {
		payload.Rankings[i].Raw = rawPayload.Rankings[i]
	}
	return payload.Rankings, nil
}

// StockWarnings는 계좌 헤더 없이 종목의 유의사항(정리매매/단기과열/투자경고·위험/VI 등)을 조회합니다.
// 개별 유의사항 항목의 필드 스키마는 실제로 유의사항이 있는 종목으로 아직 확인하지 못해
// 원문 그대로 보존합니다 — 호출자는 통상 len(warnings) > 0 여부만으로 제외 판단을 하면 됩니다.
func (c *Client) StockWarnings(ctx context.Context, symbol string) ([]json.RawMessage, error) {
	if strings.TrimSpace(symbol) == "" {
		return nil, errors.New("symbol이 필요합니다")
	}
	body, err := c.get(ctx, "STOCK", "/api/v1/stocks/"+url.PathEscape(symbol)+"/warnings", "")
	if err != nil {
		return nil, err
	}
	body = unwrapData(body)
	var warnings []json.RawMessage
	if err := json.Unmarshal(body, &warnings); err != nil {
		return nil, errors.New("잘못된 유의사항 응답")
	}
	return warnings, nil
}

// KoreanMarketDetail은 국내 종목 전용 시장 상태 플래그를 담습니다.
// liquidationTrading이 true면 정리매매 종목입니다(SPEC.md 3.3의 운영 리스크 제외 대상).
type KoreanMarketDetail struct {
	LiquidationTrading  bool `json:"liquidationTrading"`
	NxtSupported        bool `json:"nxtSupported"`
	KrxTradingSuspended bool `json:"krxTradingSuspended"`
	NxtTradingSuspended bool `json:"nxtTradingSuspended"`
}

// Stock은 종목 기본정보와 응답 원문을 보존합니다.
// 시가총액은 별도 필드가 없어 SharesOutstanding × 현재가(Price)로 계산해야 합니다.
type Stock struct {
	Symbol             string              `json:"symbol"`
	Name               string              `json:"name"`
	EnglishName        string              `json:"englishName"`
	Market             string              `json:"market"`
	SecurityType       string              `json:"securityType"`
	Status             string              `json:"status"`
	Currency           string              `json:"currency"`
	SharesOutstanding  string              `json:"sharesOutstanding"`
	KoreanMarketDetail *KoreanMarketDetail `json:"koreanMarketDetail"`
	Raw                json.RawMessage     `json:"-"`
}

// IsLeveraged는 레버리지/인버스 상품 여부를 원문의 leverageFactor로 판단합니다.
// 값이 문자열이든 숫자든 받아들이고, 해석할 수 없는 값은 보수적으로 true입니다.
func (s Stock) IsLeveraged() bool {
	var probe struct {
		LeverageFactor json.RawMessage `json:"leverageFactor"`
	}
	if err := json.Unmarshal(s.Raw, &probe); err != nil {
		return false
	}
	v := strings.Trim(strings.TrimSpace(string(probe.LeverageFactor)), `"`)
	if v == "" || v == "null" {
		return false
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return true
	}
	return n != 1
}

// Commission은 시장별 수수료율과 적용 기간을 담습니다.
// 2026-09-27 실API로 확인: startDate/endDate가 있는 시장별 요율 이력이며,
// 한국 증권거래세 등 commissionRate에 포함되지 않는 별도 비용이 있을 수 있어
// 시뮬레이션은 이 값을 "브로커 수수료"로만 취급합니다(SPEC.md 6.1/8).
type Commission struct {
	MarketCountry  string `json:"marketCountry"`
	CommissionRate string `json:"commissionRate"`
	StartDate      string `json:"startDate"`
	EndDate        string `json:"endDate"`
}

// Commissions는 계좌 헤더가 필요한(계좌별로 다를 수 있는) 수수료율 목록을 조회합니다.
func (c *Client) Commissions(ctx context.Context, accountSeq string) ([]Commission, error) {
	if strings.TrimSpace(accountSeq) == "" {
		return nil, errors.New("accountSeq가 필요합니다")
	}
	body, err := c.get(ctx, "ASSET", "/api/v1/commissions", accountSeq)
	if err != nil {
		return nil, err
	}
	body = unwrapData(body)
	var commissions []Commission
	if err := json.Unmarshal(body, &commissions); err != nil {
		return nil, errors.New("잘못된 수수료 응답")
	}
	return commissions, nil
}

// Stocks는 계좌 헤더 없이 하나 이상의 종목 기본정보를 조회합니다.
func (c *Client) Stocks(ctx context.Context, symbols ...string) ([]Stock, error) {
	if len(symbols) == 0 {
		return nil, errors.New("symbols가 최소 1개 필요합니다")
	}
	query := url.Values{"symbols": {strings.Join(symbols, ",")}}
	body, err := c.get(ctx, "STOCK", "/api/v1/stocks?"+query.Encode(), "")
	if err != nil {
		return nil, err
	}
	body = unwrapData(body)
	var stocks []Stock
	if err := json.Unmarshal(body, &stocks); err != nil {
		return nil, errors.New("잘못된 종목 정보 응답")
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(body, &raws); err != nil || len(raws) != len(stocks) {
		return nil, errors.New("종목 정보 응답 원문 파싱 실패")
	}
	for i := range stocks {
		stocks[i].Raw = raws[i]
	}
	return stocks, nil
}

// Price는 종목의 현재가와 응답 원문을 보존합니다.
// 2026-09-27 실API로 확인한 실제 스키마를 반영합니다: 조회는 symbols(복수) 쿼리
// 파라미터를 쓰고, 응답은 배열입니다.
type Price struct {
	Symbol    string          `json:"symbol"`
	Timestamp string          `json:"timestamp"`
	LastPrice string          `json:"lastPrice"`
	Currency  string          `json:"currency"`
	Raw       json.RawMessage `json:"-"`
}

// Price는 계좌 헤더 없이 단일 종목의 현재가를 조회합니다.
func (c *Client) Price(ctx context.Context, symbol string) (*Price, error) {
	if strings.TrimSpace(symbol) == "" {
		return nil, errors.New("symbol이 필요합니다")
	}
	prices, err := c.fetchPrices(ctx, []string{symbol})
	if err != nil {
		return nil, err
	}
	return &prices[0], nil
}

// MaxPricesPerCall은 /prices 한 번에 보내는 심볼 수 상한입니다. 실API로 11개까지 확인했습니다.
const MaxPricesPerCall = 10

// Prices는 여러 종목의 현재가를 한 번의 호출로 가져와 symbol별로 돌려줍니다.
// 응답에 없는 종목은 결과 맵에서 빠집니다. 호출당 상한(MaxPricesPerCall) 이내로만 보내세요.
func (c *Client) Prices(ctx context.Context, symbols ...string) (map[string]Price, error) {
	if len(symbols) == 0 || len(symbols) > MaxPricesPerCall {
		return nil, fmt.Errorf("symbols는 1~%d개여야 합니다", MaxPricesPerCall)
	}
	for _, s := range symbols {
		if strings.TrimSpace(s) == "" || strings.Contains(s, ",") {
			return nil, errors.New("잘못된 symbol")
		}
	}
	prices, err := c.fetchPrices(ctx, symbols)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Price, len(prices))
	for _, p := range prices {
		out[p.Symbol] = p
	}
	return out, nil
}

func (c *Client) fetchPrices(ctx context.Context, symbols []string) ([]Price, error) {
	query := url.Values{"symbols": {strings.Join(symbols, ",")}}
	body, err := c.get(ctx, "MARKET_DATA", "/api/v1/prices?"+query.Encode(), "")
	if err != nil {
		return nil, err
	}
	body = unwrapData(body)
	var prices []Price
	if err := json.Unmarshal(body, &prices); err != nil || len(prices) == 0 {
		return nil, errors.New("시세 응답에 유효한 항목이 없습니다")
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(body, &raws); err != nil || len(raws) != len(prices) {
		return nil, errors.New("시세 응답 원문 파싱 실패")
	}
	for i := range prices {
		prices[i].Raw = raws[i]
	}
	return prices, nil
}

// Candle은 캔들 한 개의 OHLCV와 응답 원문을 보존합니다.
// 2026-09-27 실API로 확인한 실제 스키마를 반영합니다: 가격 필드명은
// openPrice/highPrice/lowPrice/closePrice이며 전부 JSON 문자열입니다.
type Candle struct {
	Timestamp  string          `json:"timestamp"`
	OpenPrice  string          `json:"openPrice"`
	HighPrice  string          `json:"highPrice"`
	LowPrice   string          `json:"lowPrice"`
	ClosePrice string          `json:"closePrice"`
	Volume     string          `json:"volume"`
	Currency   string          `json:"currency"`
	Raw        json.RawMessage `json:"-"`
}

// Candles는 계좌 헤더 없이 종목의 캔들을 조회합니다.
// nextBefore는 더 과거 데이터를 페이지네이션할 때 다음 호출의 before 값으로
// 그대로 넘기면 되는 커서이며, 더 이전 데이터가 없으면 빈 문자열일 수 있습니다.
func (c *Client) Candles(ctx context.Context, symbol, interval string, count int, before string) (candles []Candle, nextBefore string, err error) {
	if strings.TrimSpace(symbol) == "" {
		return nil, "", errors.New("symbol이 필요합니다")
	}
	if strings.TrimSpace(interval) == "" {
		return nil, "", errors.New("interval이 필요합니다")
	}
	if count < 1 || count > 200 {
		return nil, "", errors.New("count는 1 이상 200 이하여야 합니다")
	}
	query := url.Values{
		"symbol":   {symbol},
		"interval": {interval},
		"count":    {strconv.Itoa(count)},
	}
	if before != "" {
		query.Set("before", before)
	}
	body, err := c.get(ctx, "MARKET_DATA_CHART", "/api/v1/candles?"+query.Encode(), "")
	if err != nil {
		return nil, "", err
	}
	body = unwrapData(body)
	var payload struct {
		Candles    []Candle `json:"candles"`
		NextBefore string   `json:"nextBefore"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", errors.New("잘못된 캔들 응답")
	}
	var rawPayload struct {
		Candles []json.RawMessage `json:"candles"`
	}
	if err := json.Unmarshal(body, &rawPayload); err != nil || len(rawPayload.Candles) != len(payload.Candles) {
		return nil, "", errors.New("캔들 응답 원문 파싱 실패")
	}
	for i := range payload.Candles {
		payload.Candles[i].Raw = rawPayload.Candles[i]
	}
	return payload.Candles, payload.NextBefore, nil
}

// MarketSessionWindow는 한 세션의 시작/종료 시각을 담습니다(ISO8601, KST 오프셋 포함).
type MarketSessionWindow struct {
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

// MarketSessions는 하루치 프리마켓/정규장/애프터마켓 시간을 담습니다.
type MarketSessions struct {
	PreMarket     *MarketSessionWindow `json:"preMarket"`
	RegularMarket *MarketSessionWindow `json:"regularMarket"`
	AfterMarket   *MarketSessionWindow `json:"afterMarket"`
}

// MarketDay는 특정 날짜의 세션 시간을 담습니다. **KR과 US의 실제 응답 구조가
// 서로 다릅니다** (2026-09-28 실API로 확인):
//   - KR: `{"date", "integrated": {preMarket, regularMarket, afterMarket} | null}`
//     — 휴장일에는 integrated 전체가 null.
//   - US: `{"date", "dayMarket", "preMarket", "regularMarket", "afterMarket"}`
//     — integrated 래퍼가 아예 없고 세션들이 date와 같은 레벨에 바로 있습니다.
//     휴장일에 각 세션이 어떻게 표시되는지는 아직 실제 휴장일로 확인 못 했습니다
//     (11절) — null이 될 것으로 가정하고 구현했습니다.
//
// RegularSession()으로 두 구조를 모두 흡수해서 통일된 방식으로 조회하세요 —
// Integrated/RegularMarket 필드를 직접 읽지 마세요.
type MarketDay struct {
	Date          string               `json:"date"`
	Integrated    *MarketSessions      `json:"integrated"`    // KR
	DayMarket     *MarketSessionWindow `json:"dayMarket"`     // US에만 존재, 용도 미확인
	PreMarket     *MarketSessionWindow `json:"preMarket"`     // US에서는 이 레벨에 직접 존재
	RegularMarket *MarketSessionWindow `json:"regularMarket"` // US에서는 이 레벨에 직접 존재
	AfterMarket   *MarketSessionWindow `json:"afterMarket"`   // US에서는 이 레벨에 직접 존재
}

// RegularSession returns the day's regular-market window regardless of
// whether the response nested it under "integrated" (KR) or put it directly
// on the day (US). Returns nil when there is no session (휴장일).
func (d MarketDay) RegularSession() *MarketSessionWindow {
	if d.Integrated != nil {
		return d.Integrated.RegularMarket
	}
	return d.RegularMarket
}

// MarketCalendar는 KR/US 실API로 확인한 실제 스키마를 반영합니다(위 MarketDay
// 참고). isOpen 같은 단순 불리언 필드는 없으며, "오늘이 거래일인가"는
// RegularSession()이 nil인지로 판단합니다(IsOpenToday 참고).
type MarketCalendar struct {
	Today               MarketDay       `json:"today"`
	PreviousBusinessDay MarketDay       `json:"previousBusinessDay"`
	NextBusinessDay     MarketDay       `json:"nextBusinessDay"`
	Raw                 json.RawMessage `json:"-"`
}

// IsOpenToday는 오늘 정규장 세션 정보가 존재하는지(=오늘이 거래일인지)를
// 보고합니다. 휴장일(주말/공휴일)에는 세션 정보가 없습니다.
func (m *MarketCalendar) IsOpenToday() bool {
	return m.Today.RegularSession() != nil
}

// MarketCalendar는 계좌 헤더 없이 국내(KR) 또는 미국(US) 시장의 캘린더를 조회합니다.
func (c *Client) MarketCalendar(ctx context.Context, market string) (*MarketCalendar, error) {
	if market != "KR" && market != "US" {
		return nil, errors.New("market은 KR 또는 US여야 합니다")
	}
	body, err := c.get(ctx, "MARKET_INFO", "/api/v1/market-calendar/"+market, "")
	if err != nil {
		return nil, err
	}
	body = unwrapData(body)
	var result MarketCalendar
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, errors.New("잘못된 시장 캘린더 응답")
	}
	result.Raw = append(json.RawMessage(nil), body...)
	return &result, nil
}

// ExchangeRate는 /exchange-rate 응답입니다. 금액 필드는 다른 API처럼 JSON 문자열입니다.
type ExchangeRate struct {
	Rate       string `json:"rate"`
	MidRate    string `json:"midRate"`
	ValidFrom  string `json:"validFrom"`
	ValidUntil string `json:"validUntil"`
}

// USDKRWRate는 USD 1달러당 원화 환율을 돌려줍니다. 매매 기준율이 아니라 중간환율(midRate)을
// 우선 쓰고, 없으면 rate로 대체합니다.
func (c *Client) USDKRWRate(ctx context.Context) (float64, error) {
	query := url.Values{"baseCurrency": {"USD"}, "quoteCurrency": {"KRW"}}
	body, err := c.get(ctx, "EXCHANGE_RATE", "/api/v1/exchange-rate?"+query.Encode(), "")
	if err != nil {
		return 0, err
	}
	var r ExchangeRate
	if err := json.Unmarshal(unwrapData(body), &r); err != nil {
		return 0, errors.New("잘못된 환율 응답")
	}
	for _, v := range []string{r.MidRate, r.Rate} {
		if rate, err := strconv.ParseFloat(v, 64); err == nil && rate > 0 && !math.IsInf(rate, 0) {
			return rate, nil
		}
	}
	return 0, errors.New("환율 응답에 유효한 값이 없습니다")
}
