package tossapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

// SecretProvider는 시크릿 버전 이름에 해당하는 값을 메모리로 반환합니다.
type SecretProvider interface {
	GetSecret(ctx context.Context, name string) (string, error)
}

// MemorySecretProvider는 생성 시 복사한 값을 사용하는 테스트용 공급자입니다.
type MemorySecretProvider struct {
	values map[string]string
}

func NewMemorySecretProvider(values map[string]string) *MemorySecretProvider {
	copy := make(map[string]string, len(values))
	for name, value := range values {
		copy[name] = value
	}
	return &MemorySecretProvider{values: copy}
}

func (p *MemorySecretProvider) GetSecret(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, ok := p.values[name]
	if !ok {
		return "", errors.New("시크릿을 찾을 수 없습니다")
	}
	return value, nil
}

// SecretManagerProvider는 GCE에 연결된 서비스 계정으로 REST API를 호출합니다.
// 인스턴스에는 cloud-platform OAuth 범위와 해당 시크릿 접근 IAM 권한이 필요합니다.
// 외부 SDK나 디스크의 서비스 계정 키 파일을 사용하지 않습니다.
type SecretManagerProvider struct {
	http        *http.Client
	endpoint    string
	metadataURL string
}

func NewSecretManagerProvider(client *http.Client) *SecretManagerProvider {
	return &SecretManagerProvider{
		http:        safeHTTPClient(client),
		endpoint:    "https://secretmanager.googleapis.com/v1/",
		metadataURL: "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token",
	}
}

var secretVersionName = regexp.MustCompile(`^projects/[a-zA-Z0-9_-]+/secrets/[a-zA-Z0-9_-]+/versions/([0-9]+|latest)$`)

func (p *SecretManagerProvider) GetSecret(ctx context.Context, name string) (string, error) {
	if !secretVersionName.MatchString(name) {
		return "", errors.New("시크릿 이름은 projects/.../secrets/.../versions/... 형식이어야 합니다")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.metadataURL, nil)
	if err != nil {
		return "", errors.New("메타데이터 요청 생성 실패")
	}
	req.Header.Set("Metadata-Flavor", "Google")
	body, err := p.read(req, true)
	if err != nil {
		return "", err
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(body, &token) != nil || token.AccessToken == "" {
		return "", errors.New("잘못된 서비스 계정 토큰 응답")
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+name+":access", nil)
	if err != nil {
		return "", errors.New("Secret Manager 요청 생성 실패")
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	body, err = p.read(req, false)
	if err != nil {
		return "", err
	}
	var response struct {
		Payload struct {
			Data string `json:"data"`
		} `json:"payload"`
	}
	if json.Unmarshal(body, &response) != nil {
		return "", errors.New("잘못된 Secret Manager 응답")
	}
	value, err := base64.StdEncoding.DecodeString(response.Payload.Data)
	if err != nil || len(value) == 0 {
		return "", errors.New("Secret Manager 페이로드가 비어 있거나 유효하지 않습니다")
	}
	return string(value), nil
}

func (p *SecretManagerProvider) read(req *http.Request, metadata bool) ([]byte, error) {
	resp, err := p.http.Do(req)
	if err != nil {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, errors.New("시크릿 HTTP 요청 실패")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("시크릿 HTTP 상태 %d", resp.StatusCode)
	}
	if metadata && resp.Header.Get("Metadata-Flavor") != "Google" {
		return nil, errors.New("유효하지 않은 메타데이터 서버 응답")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return nil, errors.New("시크릿 응답 읽기 실패 또는 크기 초과")
	}
	return body, nil
}
