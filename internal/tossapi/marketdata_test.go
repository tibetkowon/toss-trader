package tossapi

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestPrice(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/prices" {
			t.Errorf("시세 요청 메서드 또는 경로 불일치: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("symbol") != "AAPL" {
			t.Errorf("symbol 불일치: %q", r.URL.Query().Get("symbol"))
		}
		if r.Header.Get("X-Tossinvest-Account") != "" {
			t.Error("시세 조회에 계좌 헤더가 포함되었습니다")
		}
		w.Header().Set("X-RateLimit-Limit", "7")
		fmt.Fprint(w, `{"price": 123.45}`)
	})
	price, err := client.Price(context.Background(), "AAPL")
	if err != nil {
		t.Fatal(err)
	}
	if price == nil || price.Price.String() != "123.45" || len(price.Raw) == 0 {
		t.Fatalf("현재가 또는 원문 불일치: %+v", price)
	}
	if limit := client.RateLimit("MARKET_DATA"); limit.Limit != 7 {
		t.Fatalf("시세 제한 그룹 불일치: %+v", limit)
	}
}

func TestPriceEmptySymbol(t *testing.T) {
	var calls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "예상하지 않은 HTTP 요청", http.StatusBadRequest)
	})
	if _, err := client.Price(context.Background(), ""); err == nil {
		t.Fatal("빈 symbol을 허용했습니다")
	}
	if calls.Load() != 0 {
		t.Fatalf("빈 symbol로 HTTP 요청이 발생했습니다: %d", calls.Load())
	}
}
