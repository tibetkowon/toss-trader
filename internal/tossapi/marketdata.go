package tossapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
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
