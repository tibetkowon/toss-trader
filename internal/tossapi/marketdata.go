package tossapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

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
