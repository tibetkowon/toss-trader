package tossapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(context.Background(), Config{
		BaseURL: server.URL, HTTPClient: server.Client(),
		Secrets:        NewMemorySecretProvider(map[string]string{"id": "test-id", "secret": "test-secret"}),
		ClientIDSecret: "id", ClientSecretSecret: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func writeToken(w http.ResponseWriter) {
	fmt.Fprint(w, `{"access_token":"cached-token","token_type":"Bearer","expires_in":100}`)
}

func TestTokenCachingAndExpiry(t *testing.T) {
	var calls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/oauth2/token" ||
			r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("토큰 요청 메서드, 경로 또는 Content-Type 불일치")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "test-id" || r.Form.Get("client_secret") != "test-secret" {
			t.Error("Client Credentials 폼 불일치")
		}
		writeToken(w)
	})
	now := time.Unix(1_800_000_000, 0)
	client.now = func() time.Time { return now }
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			token, err := client.Token(context.Background())
			if err != nil || token != "cached-token" {
				t.Errorf("Token: %q, %v", token, err)
			}
		}()
	}
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("동시 요청의 발급 횟수: %d", calls.Load())
	}
	now = now.Add(101 * time.Second)
	if _, err := client.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("만료 후 발급 횟수: %d", calls.Load())
	}
}

func TestAccountsThenHoldings(t *testing.T) {
	var tokenCalls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			tokenCalls.Add(1)
			writeToken(w)
			return
		}
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer cached-token" {
			t.Error("조회 요청의 인증 또는 메서드 불일치")
		}
		switch r.URL.Path {
		case "/api/v1/accounts":
			if r.Header.Get("X-Tossinvest-Account") != "" {
				t.Error("최초 계좌 조회에 계좌 헤더가 포함되었습니다")
			}
			fmt.Fprint(w, `{"result":[{"accountSeq":1,"accountNo":"111","accountType":"BROKERAGE"},{"accountSeq":2,"accountNo":"222","accountType":"BROKERAGE"}]}`)
		case "/api/v1/holdings":
			if r.Header.Get("X-Tossinvest-Account") != "2" {
				t.Error("선택한 accountSeq가 전달되지 않았습니다")
			}
			fmt.Fprint(w, `{"result":{"dailyProfitLoss":{"amount":{"krw":"-123.45","usd":null},"rate":"-1.2"}}}`)
		default:
			t.Errorf("예상하지 않은 경로: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	accounts, err := client.Accounts(context.Background())
	if err != nil || len(accounts) != 2 {
		t.Fatalf("Accounts: %v, %v", accounts, err)
	}
	holdings, err := client.Holdings(context.Background(), accounts[1].AccountSeq.String())
	if err != nil {
		t.Fatal(err)
	}
	if holdings.DailyProfitLoss.Amount.KRW != "-123.45" || holdings.DailyProfitLoss.Rate != "-1.2" || len(holdings.Raw) == 0 {
		t.Fatalf("일간손익 또는 원문 불일치: %+v", holdings)
	}
	if tokenCalls.Load() != 1 {
		t.Fatal("계좌 흐름에서 토큰을 다시 발급했습니다")
	}
	if _, err := client.Holdings(context.Background(), ""); err == nil {
		t.Fatal("빈 accountSeq를 허용했습니다")
	}
}

func TestRateLimitRetryAfter(t *testing.T) {
	for _, path := range []string{"/oauth2/token", "/api/v1/accounts"} {
		for _, dateHeader := range []bool{false, true} {
			t.Run(path+strconv.FormatBool(dateHeader), func(t *testing.T) {
				now := time.Unix(1_800_000_000, 0)
				var calls atomic.Int32
				client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == path && calls.Add(1) == 1 {
						w.Header().Set("X-RateLimit-Limit", "7")
						w.Header().Set("X-RateLimit-Remaining", "0")
						w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(now.Add(time.Second).Unix(), 10))
						retry := "3"
						if dateHeader {
							retry = now.Add(3 * time.Second).UTC().Format(http.TimeFormat)
						}
						w.Header().Set("Retry-After", retry)
						w.WriteHeader(http.StatusTooManyRequests)
						return
					}
					if r.URL.Path == "/oauth2/token" {
						writeToken(w)
					} else {
						fmt.Fprint(w, `[{"accountSeq":1}]`)
					}
				})
				client.now = func() time.Time { return now }
				var waits []time.Duration
				client.sleep = func(ctx context.Context, d time.Duration) error {
					group := "ACCOUNT"
					if path == "/oauth2/token" {
						group = "AUTH"
					}
					state := client.RateLimit(group)
					if state.Limit != 7 || state.Remaining != 0 || state.Reset.IsZero() {
						t.Errorf("응답 제한 헤더 누락: %+v", state)
					}
					waits = append(waits, d)
					now = now.Add(d)
					return nil
				}
				if _, err := client.Accounts(context.Background()); err != nil {
					t.Fatal(err)
				}
				if calls.Load() != 2 || len(waits) != 1 || waits[0] != 3*time.Second {
					t.Fatalf("백오프: calls=%d waits=%v", calls.Load(), waits)
				}
			})
		}
	}
}

func TestExhaustedQuotaAndIndependentGroups(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			writeToken(w)
		case "/api/v1/accounts":
			w.Header().Set("X-RateLimit-Limit", "2")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(now.Add(5*time.Second).Unix(), 10))
			fmt.Fprint(w, `[{"accountSeq":1}]`)
		case "/api/v1/holdings":
			fmt.Fprint(w, `{"dailyProfitLoss":{"amount":{"krw":"0"},"rate":"0"}}`)
		}
	})
	client.now = func() time.Time { return now }
	var waits []time.Duration
	client.sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		now = now.Add(d)
		return nil
	}
	if _, err := client.Accounts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Holdings(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 0 {
		t.Fatal("ACCOUNT 제한이 ASSET 요청을 지연했습니다")
	}
	if _, err := client.Accounts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 1 || waits[0] != 5*time.Second {
		t.Fatalf("할당량 소진 대기: %v", waits)
	}
}

func TestRetryBoundAndCancellation(t *testing.T) {
	var calls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	now := time.Unix(1_800_000_000, 0)
	client.now = func() time.Time { return now }
	client.sleep = func(ctx context.Context, d time.Duration) error {
		now = now.Add(d)
		return nil
	}
	_, err := client.Token(context.Background())
	var status *HTTPError
	if !errors.As(err, &status) || status.StatusCode != 429 || calls.Load() != 4 {
		t.Fatalf("재시도 한도: calls=%d err=%v", calls.Load(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Token(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("취소 결과: %v", err)
	}
	if err := wait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("백오프 취소 결과: %v", err)
	}
	if calls.Load() != 4 {
		t.Fatal("취소 후 추가 요청이 발생했습니다")
	}
}

func TestInvalidResponses(t *testing.T) {
	for _, body := range []string{`{}`, `{"dailyProfitLoss":{"amount":{"krw":"0"}}}`, `{"dailyProfitLoss":{"amount":{"krw":"0"},"rate":null}}`} {
		t.Run(body, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth2/token" {
					writeToken(w)
					return
				}
				fmt.Fprint(w, body)
			})
			if _, err := client.Holdings(context.Background(), "one"); err == nil {
				t.Fatal("불완전한 일간손익을 허용했습니다")
			}
		})
	}
}

func TestTokenExpiryTimingAfterAuthBackoff(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	var calls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/token" {
			t.Fatalf("예상하지 않은 경로: %s", r.URL.Path)
		}
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "10")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"access_token":"fresh-token","token_type":"Bearer","expires_in":5}`)
	})
	client.now = func() time.Time { return now }
	client.sleep = func(ctx context.Context, d time.Duration) error {
		now = now.Add(d)
		return nil
	}
	// 첫 시도는 429(Retry-After 10s)로 지연되고, 두 번째 시도에서 만료까지 5초짜리
	// 토큰을 받습니다. 만료 기준 시각을 재시도 이전 시점으로 고정하면 이 토큰은
	// 도착 즉시 "이미 만료"로 오판됩니다.
	token, err := client.Token(context.Background())
	if err != nil {
		t.Fatalf("백오프 이후 발급된 유효한 토큰을 거부했습니다: %v", err)
	}
	if token != "fresh-token" {
		t.Fatalf("토큰: %q", token)
	}
	if calls.Load() != 2 {
		t.Fatalf("호출 횟수: %d", calls.Load())
	}
}

func TestUnauthorizedRetriesOnceWithFreshToken(t *testing.T) {
	var tokenCalls, holdingsCalls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			n := tokenCalls.Add(1)
			fmt.Fprintf(w, `{"access_token":"token-%d","token_type":"Bearer","expires_in":100}`, n)
			return
		}
		if r.URL.Path == "/api/v1/holdings" {
			n := holdingsCalls.Add(1)
			if n == 1 {
				// 다른 goroutine이 방금 재발급받아 이 토큰을 무효화했다고 가정합니다.
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Authorization") != "Bearer token-2" {
				t.Errorf("재시도에 새 토큰이 쓰이지 않았습니다: %s", r.Header.Get("Authorization"))
			}
			fmt.Fprint(w, `{"dailyProfitLoss":{"amount":{"krw":"1"},"rate":"1"}}`)
			return
		}
		t.Fatalf("예상하지 않은 경로: %s", r.URL.Path)
	})
	holdings, err := client.Holdings(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("401 이후 자동 재시도가 실패했습니다: %v", err)
	}
	if holdings.DailyProfitLoss.Amount.KRW != "1" {
		t.Fatalf("holdings: %+v", holdings)
	}
	if tokenCalls.Load() != 2 || holdingsCalls.Load() != 2 {
		t.Fatalf("호출 횟수: token=%d holdings=%d", tokenCalls.Load(), holdingsCalls.Load())
	}
}

func TestUnauthorizedRetryIsBoundedToOnce(t *testing.T) {
	var holdingsCalls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			writeToken(w)
			return
		}
		if r.URL.Path == "/api/v1/holdings" {
			holdingsCalls.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		t.Fatalf("예상하지 않은 경로: %s", r.URL.Path)
	})
	_, err := client.Holdings(context.Background(), "acc-1")
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("401 오류: %v", err)
	}
	if holdingsCalls.Load() != 2 {
		t.Fatalf("재시도 횟수가 1회를 초과했습니다(원본+재시도 1회여야 함): %d", holdingsCalls.Load())
	}
}

func Test429WithoutTimingDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "invalid")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, err := client.Token(context.Background()); err == nil || calls.Load() != 1 {
		t.Fatalf("잘못된 제한 헤더: calls=%d err=%v", calls.Load(), err)
	}
}
