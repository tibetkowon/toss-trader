package tossapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
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

func TestCandles(t *testing.T) {
	const body = `[{"open":1,"high":2,"low":0.5,"close":1.5}]`
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/candles" {
			t.Errorf("캔들 요청 메서드 또는 경로 불일치: %s %s", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("symbol") != "AAPL" || query.Get("interval") != "1d" || query.Get("count") != "10" {
			t.Errorf("캔들 쿼리 불일치: %v", query)
		}
		if query.Has("before") {
			t.Error("빈 before가 쿼리에 포함되었습니다")
		}
		if r.Header.Get("X-Tossinvest-Account") != "" {
			t.Error("캔들 조회에 계좌 헤더가 포함되었습니다")
		}
		fmt.Fprint(w, body)
	})
	candles, err := client.Candles(context.Background(), "AAPL", "1d", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(candles) != 1 {
		t.Fatalf("캔들 개수 불일치: %d", len(candles))
	}
	candle := candles[0]
	if candle.Open.String() != "1" || candle.High.String() != "2" || candle.Low.String() != "0.5" || candle.Close.String() != "1.5" {
		t.Fatalf("OHLC 불일치: %+v", candle)
	}
	if string(candle.Raw) != body[1:len(body)-1] {
		t.Fatalf("캔들 원문 불일치: %s", candle.Raw)
	}
}

func TestCandlesWithBefore(t *testing.T) {
	const before = "2026-09-25T00:00:00+09:00"
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/candles" {
			t.Errorf("캔들 요청 메서드 또는 경로 불일치: %s %s", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("symbol") != "AAPL" || query.Get("interval") != "1d" || query.Get("count") != "10" || query.Get("before") != before {
			t.Errorf("캔들 쿼리 불일치: %v", query)
		}
		fmt.Fprint(w, `{"data":[{"open":1,"high":2,"low":0.5,"close":1.5}]}`)
	})
	candles, err := client.Candles(context.Background(), "AAPL", "1d", 10, before)
	if err != nil {
		t.Fatal(err)
	}
	if len(candles) != 1 || candles[0].Open.String() != "1" || candles[0].High.String() != "2" || candles[0].Low.String() != "0.5" || candles[0].Close.String() != "1.5" {
		t.Fatalf("캔들 응답 불일치: %+v", candles)
	}
	if string(candles[0].Raw) != `{"open":1,"high":2,"low":0.5,"close":1.5}` {
		t.Fatalf("캔들 원문 불일치: %s", candles[0].Raw)
	}
}

func TestCandlesCountOutOfRange(t *testing.T) {
	for _, count := range []int{0, 201} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("잘못된 count로 HTTP 요청이 발생했습니다")
			})
			if _, err := client.Candles(context.Background(), "AAPL", "1d", count, ""); err == nil {
				t.Fatal("범위를 벗어난 count를 허용했습니다")
			}
		})
	}
}

func TestCandlesRateLimitGroupIndependent(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			writeToken(w)
		case "/api/v1/candles":
			w.Header().Set("X-RateLimit-Limit", "2")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(now.Add(5*time.Second).Unix(), 10))
			fmt.Fprint(w, `[{"open":1,"high":2,"low":0.5,"close":1.5}]`)
		case "/api/v1/prices":
			fmt.Fprint(w, `{"price":1.5}`)
		default:
			t.Errorf("예상하지 않은 경로: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	client.now = func() time.Time { return now }
	var waits []time.Duration
	client.sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		now = now.Add(d)
		return nil
	}
	if _, err := client.Candles(context.Background(), "AAPL", "1d", 10, ""); err != nil {
		t.Fatal(err)
	}
	if limit := client.RateLimit("MARKET_DATA_CHART"); limit.Limit != 2 || limit.Remaining != 0 || !limit.BlockedUntil.Equal(now.Add(5*time.Second)) {
		t.Fatalf("캔들 제한 그룹 불일치: %+v", limit)
	}
	if _, err := client.Price(context.Background(), "AAPL"); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 0 {
		t.Fatal("MARKET_DATA_CHART 제한이 MARKET_DATA 요청을 지연했습니다")
	}
	if _, err := client.Candles(context.Background(), "AAPL", "1d", 10, ""); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 1 || waits[0] != 5*time.Second {
		t.Fatalf("할당량 소진 대기: %v", waits)
	}
}
