package tossapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Price는 현재가와 응답 원문을 보존합니다.
type Price struct {
	Price json.Number     `json:"price"`
	Raw   json.RawMessage `json:"-"`
}

// Price는 계좌 헤더 없이 종목의 현재가를 조회합니다.
func (c *Client) Price(ctx context.Context, symbol string) (*Price, error) {
	if strings.TrimSpace(symbol) == "" {
		return nil, errors.New("symbol이 필요합니다")
	}
	body, err := c.get(ctx, "MARKET_DATA", "/api/v1/prices?symbol="+url.QueryEscape(symbol), "")
	if err != nil {
		return nil, err
	}
	body = unwrapData(body)
	var result Price
	if err := json.Unmarshal(body, &result); err != nil || result.Price == "" {
		return nil, errors.New("시세 응답에 유효한 price가 없습니다")
	}
	result.Raw = append(json.RawMessage(nil), body...)
	return &result, nil
}

// Candle은 캔들의 시가, 고가, 저가, 종가와 응답 원문을 보존합니다.
type Candle struct {
	Open  json.Number     `json:"open"`
	High  json.Number     `json:"high"`
	Low   json.Number     `json:"low"`
	Close json.Number     `json:"close"`
	Raw   json.RawMessage `json:"-"`
}

// Candles는 계좌 헤더 없이 종목의 캔들을 조회합니다.
func (c *Client) Candles(ctx context.Context, symbol, interval string, count int, before string) ([]Candle, error) {
	if strings.TrimSpace(symbol) == "" {
		return nil, errors.New("symbol이 필요합니다")
	}
	if strings.TrimSpace(interval) == "" {
		return nil, errors.New("interval이 필요합니다")
	}
	if count < 1 || count > 200 {
		return nil, errors.New("count는 1 이상 200 이하여야 합니다")
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
		return nil, err
	}
	body = unwrapData(body)
	var candles []Candle
	if err := json.Unmarshal(body, &candles); err != nil {
		return nil, errors.New("잘못된 캔들 응답")
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, errors.New("잘못된 캔들 응답")
	}
	for i := range candles {
		candles[i].Raw = raw[i]
	}
	return candles, nil
}

// MarketCalendar는 시장의 개장 여부와 응답 원문을 보존합니다.
// isOpen 필드의 실제 제공 여부와 의미는 실제 API 응답으로 확인해야 합니다.
type MarketCalendar struct {
	IsOpen bool            `json:"isOpen"`
	Raw    json.RawMessage `json:"-"`
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
