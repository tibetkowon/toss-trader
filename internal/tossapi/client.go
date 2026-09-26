package tossapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config에는 인증정보 자체가 아니라 Secret Manager 버전 리소스 이름을 지정합니다.
type Config struct {
	BaseURL            string
	HTTPClient         *http.Client
	Secrets            SecretProvider
	ClientIDSecret     string
	ClientSecretSecret string
}

type Client struct {
	baseURL      string
	http         *http.Client
	clientID     string
	clientSecret string
	tokenGate    chan struct{}
	token        string
	expires      time.Time
	mu           sync.Mutex
	limits       map[string]RateLimit
	now          func() time.Time
	sleep        func(context.Context, time.Duration) error
}

type Account struct {
	AccountSeq string `json:"accountSeq"`
}

type ProfitLoss struct {
	Amount json.Number `json:"amount"`
	Rate   json.Number `json:"rate"`
}

// HoldingsResponse는 스펙에 정의된 일간손익을 노출합니다.
// 전체 원문도 보존하여 아직 스키마가 명시되지 않은 자산 정보를 잃지 않습니다.
type HoldingsResponse struct {
	DailyProfitLoss ProfitLoss      `json:"dailyProfitLoss"`
	Raw            json.RawMessage `json:"-"`
}

type RateLimit struct {
	Limit        int64
	Remaining    int64
	Reset        time.Time
	BlockedUntil time.Time
}

// HTTPError에는 인증정보가 포함될 수 있는 응답 본문을 저장하지 않습니다.
type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("API HTTP 상태 %d", e.StatusCode)
}

// New는 시작 시 시크릿을 읽어 메모리에만 보관합니다.
// 한 인증정보 쌍에는 하나의 Client를 공유해야 토큰 재발급 충돌을 피할 수 있습니다.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Secrets == nil || cfg.ClientIDSecret == "" || cfg.ClientSecretSecret == "" {
		return nil, errors.New("시크릿 공급자와 두 시크릿 이름이 필요합니다")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = "https://openapi.tossinvest.com"
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("잘못된 API 기본 URL")
	}
	id, err := cfg.Secrets.GetSecret(ctx, cfg.ClientIDSecret)
	if err != nil {
		return nil, fmt.Errorf("client_id 조회: %w", err)
	}
	secret, err := cfg.Secrets.GetSecret(ctx, cfg.ClientSecretSecret)
	if err != nil {
		return nil, fmt.Errorf("client_secret 조회: %w", err)
	}
	if id == "" || secret == "" {
		return nil, errors.New("빈 API 인증정보")
	}
	return &Client{
		baseURL: base, http: safeHTTPClient(cfg.HTTPClient),
		clientID: id, clientSecret: secret, tokenGate: make(chan struct{}, 1),
		limits: make(map[string]RateLimit), now: time.Now, sleep: wait,
	}, nil
}

func safeHTTPClient(src *http.Client) *http.Client {
	c := http.Client{Timeout: 30 * time.Second}
	if src != nil {
		c = *src
		if c.Timeout == 0 {
			c.Timeout = 30 * time.Second
		}
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Token은 만료 전까지 캐시를 공유하고 동시 토큰 발급을 직렬화합니다.
func (c *Client) Token(ctx context.Context) (string, error) {
	select {
	case c.tokenGate <- struct{}{}:
		defer func() { <-c.tokenGate }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if c.token != "" && c.now().Before(c.expires) {
		return c.token, nil
	}
	form := url.Values{
		"grant_type": {"client_credentials"},
		"client_id": {c.clientID}, "client_secret": {c.clientSecret},
	}
	// 요청 시작 시점 기준으로 만료를 계산해 전송 지연을 보수적으로 반영합니다.
	started := c.now()
	body, err := c.request(ctx, "AUTH", func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/oauth2/token", strings.NewReader(form.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		return req, err
	})
	if err != nil {
		return "", err
	}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return "", errors.New("잘못된 토큰 응답")
	}
	if token.AccessToken == "" || !strings.EqualFold(token.TokenType, "Bearer") || token.ExpiresIn <= 0 || token.ExpiresIn > int64((1<<63-1)/time.Second) {
		return "", errors.New("유효하지 않은 토큰 또는 만료 정보")
	}
	ttl := time.Duration(token.ExpiresIn) * time.Second
	margin := ttl / 10
	if margin > 30*time.Second {
		margin = 30 * time.Second
	}
	c.token, c.expires = token.AccessToken, started.Add(ttl-margin)
	if !c.now().Before(c.expires) {
		c.token = ""
		return "", errors.New("수신한 토큰이 이미 만료되었습니다")
	}
	return c.token, nil
}

// Accounts는 계좌 헤더 없이 계좌 목록을 조회합니다.
// 호출자가 사용할 AccountSeq를 선택한 뒤 Holdings에 전달합니다.
func (c *Client) Accounts(ctx context.Context) ([]Account, error) {
	body, err := c.get(ctx, "ACCOUNT", "/api/v1/accounts", "")
	if err != nil {
		return nil, err
	}
	body = unwrapData(body)
	var accounts []Account
	if err := json.Unmarshal(body, &accounts); err != nil {
		return nil, errors.New("잘못된 계좌 목록 응답")
	}
	for _, account := range accounts {
		if strings.TrimSpace(account.AccountSeq) == "" {
			return nil, errors.New("계좌 응답에 accountSeq가 없습니다")
		}
	}
	return accounts, nil
}

func (c *Client) Holdings(ctx context.Context, accountSeq string) (*HoldingsResponse, error) {
	if strings.TrimSpace(accountSeq) == "" {
		return nil, errors.New("accountSeq가 필요합니다")
	}
	body, err := c.get(ctx, "ASSET", "/api/v1/holdings", accountSeq)
	if err != nil {
		return nil, err
	}
	body = unwrapData(body)
	var result HoldingsResponse
	if err := json.Unmarshal(body, &result); err != nil || result.DailyProfitLoss.Amount == "" || result.DailyProfitLoss.Rate == "" {
		return nil, errors.New("보유자산 응답에 유효한 dailyProfitLoss.amount/rate가 없습니다")
	}
	result.Raw = append(json.RawMessage(nil), body...)
	return &result, nil
}

// 스펙에 응답 봉투가 명시되지 않아 직접 응답과 data 봉투를 모두 수용합니다.
func unwrapData(body []byte) []byte {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) == nil {
		if data, ok := envelope["data"]; ok {
			return data
		}
	}
	return body
}

func (c *Client) get(ctx context.Context, group, path, account string) ([]byte, error) {
	return c.request(ctx, group, func() (*http.Request, error) {
		token, err := c.Token(ctx)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+token)
			if account != "" {
				req.Header.Set("X-Tossinvest-Account", account)
			}
		}
		return req, err
	})
}

// RateLimit은 그룹별 마지막 응답 헤더와 현재 대기 기한의 복사본입니다.
// Limit/Remaining의 -1은 해당 응답에 유효한 헤더가 없음을 뜻합니다.
func (c *Client) RateLimit(group string) RateLimit {
	c.mu.Lock()
	defer c.mu.Unlock()
	if limit, ok := c.limits[group]; ok {
		return limit
	}
	return RateLimit{Limit: -1, Remaining: -1}
}

func (c *Client) request(ctx context.Context, group string, makeRequest func() (*http.Request, error)) ([]byte, error) {
	// 재시도 횟수만 제한하며 서버의 요청 할당량은 하드코딩하지 않습니다.
	for attempt := 0; attempt < 4; attempt++ {
		for {
			delay := c.RateLimit(group).BlockedUntil.Sub(c.now())
			if delay <= 0 {
				break
			}
			if err := c.sleep(ctx, delay); err != nil {
				return nil, err
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := makeRequest()
		if err != nil {
			return nil, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			// net/http 오류에 토큰 요청 URL 등이 노출되지 않도록 감쌉니다.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("API HTTP 요청 실패")
		}
		retry := c.observe(group, resp)
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests && retry && attempt < 3 {
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, &HTTPError{StatusCode: resp.StatusCode}
		}
		if readErr != nil || len(body) > 4<<20 {
			return nil, errors.New("API 응답 읽기 실패 또는 크기 초과")
		}
		if !json.Valid(bytes.TrimSpace(body)) {
			return nil, errors.New("API 응답이 JSON이 아닙니다")
		}
		return body, nil
	}
	return nil, errors.New("API 재시도 한도 초과")
}

func nonnegative(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return -1
	}
	return n
}

func (c *Client) observe(group string, resp *http.Response) bool {
	now := c.now()
	retryAfterValid := false
	limit := RateLimit{
		Limit: nonnegative(resp.Header.Get("X-RateLimit-Limit")),
		Remaining: nonnegative(resp.Header.Get("X-RateLimit-Remaining")),
	}
	// X-RateLimit-Reset은 Unix epoch 초로 해석합니다.
	if reset := nonnegative(resp.Header.Get("X-RateLimit-Reset")); reset >= 0 {
		limit.Reset = time.Unix(reset, 0)
	}
	if limit.Remaining == 0 && limit.Reset.After(now) {
		limit.BlockedUntil = limit.Reset
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		if limit.Reset.After(now) {
			limit.BlockedUntil = limit.Reset
		}
		value := resp.Header.Get("Retry-After")
		var until time.Time
		if seconds := nonnegative(value); seconds >= 0 && seconds <= int64((1<<63-1)/time.Second) {
			retryAfterValid = true
			until = now.Add(time.Duration(seconds) * time.Second)
		} else if date, err := http.ParseTime(value); err == nil {
			retryAfterValid = true
			until = date
		}
		if until.After(limit.BlockedUntil) {
			limit.BlockedUntil = until
		}
	}
	c.mu.Lock()
	if old := c.limits[group]; old.BlockedUntil.After(limit.BlockedUntil) {
		limit.BlockedUntil = old.BlockedUntil
	}
	c.limits[group] = limit
	c.mu.Unlock()
	// 유효한 제한 정보가 없으면 추측한 할당량으로 반복 요청하지 않습니다.
	return retryAfterValid || limit.BlockedUntil.After(now)
}
