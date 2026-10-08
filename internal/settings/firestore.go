package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	firestoreBaseURL = "https://firestore.googleapis.com/v1"
	metadataTokenURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"
	settingsDocPath  = "settings/trader"
)

// FirestoreSource는 Firestore REST API로 settings/trader 문서를 읽습니다. 인증은 인스턴스
// 서비스 계정의 메타데이터 토큰을 쓰므로 SDK가 필요 없습니다(GCS 업로더와 같은 방식).
// 서비스 계정에 이 데이터베이스의 읽기 권한이 있어야 합니다.
type FirestoreSource struct {
	ProjectID   string
	HTTP        *http.Client // nil이면 시간 제한이 있는 기본 클라이언트를 씁니다
	BaseURL     string       // 비우면 Firestore REST 기본 주소
	MetadataURL string       // 비우면 인스턴스 서비스 계정 토큰 주소
}

// Fetch는 원격 설정 문서를 읽어 Document로 바꿉니다. 검증은 Loader가 합니다.
func (f FirestoreSource) Fetch(ctx context.Context) (Document, error) {
	if f.ProjectID == "" {
		return Document{}, errors.New("설정 프로젝트 ID가 없습니다")
	}
	client := f.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	token, err := f.token(ctx, client)
	if err != nil {
		return Document{}, err
	}
	base := f.BaseURL
	if base == "" {
		base = firestoreBaseURL
	}
	endpoint := base + "/projects/" + url.PathEscape(f.ProjectID) + "/databases/(default)/documents/" + settingsDocPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Document{}, errors.New("Firestore 요청 생성 실패")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return Document{}, errors.New("Firestore 호출 실패")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Document{}, errors.New("설정 문서가 없습니다")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Document{}, fmt.Errorf("Firestore HTTP 상태 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(body) > 1<<20 {
		return Document{}, errors.New("Firestore 응답 읽기 실패 또는 크기 초과")
	}
	return decodeFirestoreDocument(body)
}

func (f FirestoreSource) token(ctx context.Context, client *http.Client) (string, error) {
	endpoint := f.MetadataURL
	if endpoint == "" {
		endpoint = metadataTokenURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", errors.New("메타데이터 요청 생성 실패")
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("메타데이터 호출 실패")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("메타데이터 HTTP 상태 %d", resp.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&token); err != nil || strings.TrimSpace(token.AccessToken) == "" {
		return "", errors.New("잘못된 메타데이터 토큰 응답")
	}
	return token.AccessToken, nil
}

// firestoreDocument는 Firestore REST 응답의 필드 부분입니다. 설정에 쓰는 타입만 읽습니다.
type firestoreDocument struct {
	Fields map[string]firestoreValue `json:"fields"`
}

type firestoreValue struct {
	DoubleValue  *float64 `json:"doubleValue"`
	IntegerValue *string  `json:"integerValue"` // Firestore REST는 정수를 문자열로 보냅니다
	BooleanValue *bool    `json:"booleanValue"`
	StringValue  *string  `json:"stringValue"`
}

// decodeFirestoreDocument는 타입이 붙은 Firestore 필드를 평범한 값으로 바꾼 뒤 Document로 읽습니다.
// 설정과 무관한 필드(updated_at 등)는 무시합니다.
func decodeFirestoreDocument(body []byte) (Document, error) {
	var raw firestoreDocument
	if err := json.Unmarshal(body, &raw); err != nil {
		return Document{}, errors.New("잘못된 Firestore 문서 응답")
	}
	plain := make(map[string]any, len(raw.Fields))
	for name, v := range raw.Fields {
		switch {
		case v.DoubleValue != nil:
			plain[name] = *v.DoubleValue
		case v.IntegerValue != nil:
			n, err := strconv.ParseInt(*v.IntegerValue, 10, 64)
			if err != nil {
				return Document{}, fmt.Errorf("정수 필드 %s 해석 실패", name)
			}
			plain[name] = n
		case v.BooleanValue != nil:
			plain[name] = *v.BooleanValue
		case v.StringValue != nil:
			plain[name] = *v.StringValue
		}
	}
	data, err := json.Marshal(plain)
	if err != nil {
		return Document{}, err
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return Document{}, fmt.Errorf("설정 필드 형식 오류: %w", err)
	}
	return doc, nil
}
