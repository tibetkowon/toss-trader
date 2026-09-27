package tossapi

import (
	"context"
	"encoding/json"
	"errors"
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
	query := url.Values{"symbols": {symbol}}
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
	result := prices[0]
	result.Raw = raws[0]
	return &result, nil
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

// MarketDay는 특정 날짜가 거래일인지와(Integrated가 nil이면 휴장) 세션 시간을 담습니다.
type MarketDay struct {
	Date       string          `json:"date"`
	Integrated *MarketSessions `json:"integrated"`
}

// MarketCalendar는 2026-09-27 실API로 확인한 실제 스키마를 반영합니다.
// isOpen 같은 단순 불리언 필드는 없으며, "오늘이 거래일인가"는
// Today.Integrated가 nil인지로 판단합니다(IsOpenToday 참고).
type MarketCalendar struct {
	Today               MarketDay       `json:"today"`
	PreviousBusinessDay MarketDay       `json:"previousBusinessDay"`
	NextBusinessDay     MarketDay       `json:"nextBusinessDay"`
	Raw                 json.RawMessage `json:"-"`
}

// IsOpenToday는 오늘 세션 정보가 존재하는지(=오늘이 거래일인지)를 보고합니다.
// 휴장일(주말/공휴일)에는 Today.Integrated가 null로 내려옵니다.
func (m *MarketCalendar) IsOpenToday() bool {
	return m.Today.Integrated != nil
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
