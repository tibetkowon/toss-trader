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
		Secrets: NewMemorySecretProvider(map[string]string{"id": "test-id", "secret": "test-secret"}),
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
			fmt.Fprint(w, `{"data":[{"accountSeq":"account-1"},{"accountSeq":"account-2"}]}`)
		case "/api/v1/holdings":
			if r.Header.Get("X-Tossinvest-Account") != "account-2" {
				t.Error("선택한 accountSeq가 전달되지 않았습니다")
			}
			fmt.Fprint(w, `{"data":{"dailyProfitLoss":{"amount":-123.45,"rate":-1.2},"assets":[]}}`)
		default:
			t.Errorf("예상하지 않은 경로: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	accounts, err := client.Accounts(context.Background())
	if err != nil || len(accounts) != 2 {
		t.Fatalf("Accounts: %v, %v", accounts, err)
	}
	holdings, err := client.Holdings(context.Background(), accounts[1].AccountSeq)
	if err != nil {
		t.Fatal(err)
	}
	if holdings.DailyProfitLoss.Amount.String() != "-123.45" || holdings.DailyProfitLoss.Rate.String() != "-1.2" || len(holdings.Raw) == 0 {
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
						fmt.Fprint(w, `[{"accountSeq":"one"}]`)
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
			fmt.Fprint(w, `[{"accountSeq":"one"}]`)
		case "/api/v1/holdings":
			fmt.Fprint(w, `{"dailyProfitLoss":{"amount":0,"rate":0}}`)
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
	for _, body := range []string{`{}`, `{"dailyProfitLoss":{"amount":0}}`, `{"dailyProfitLoss":{"amount":null,"rate":0}}`} {
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
