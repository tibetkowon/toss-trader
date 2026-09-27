package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Uploader는 지정한 객체를 덮어씁니다. 랜덤 슬러그를 포함한 객체 경로와
// 버킷의 목록 조회 권한 제한은 호출자 및 배포 설정에서 관리합니다.
type Uploader interface {
	Upload(ctx context.Context, object, contentType string, data []byte) error
}

// GCSUploader는 메타데이터 서버 인증과 GCS JSON API만 사용합니다.
// SDK나 서비스 계정 키 파일을 사용하지 않습니다.
type GCSUploader struct {
	http        *http.Client
	bucket      string
	metadataURL string
	gcsURL      string
}

var _ Uploader = (*GCSUploader)(nil)

func NewGCSUploader(client *http.Client, bucket string) *GCSUploader {
	c := http.Client{Timeout: 30 * time.Second}
	if client != nil {
		c = *client
		if c.Timeout == 0 {
			c.Timeout = 30 * time.Second
		}
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &GCSUploader{
		http:        &c,
		bucket:      bucket,
		metadataURL: "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token",
		gcsURL:      "https://storage.googleapis.com/upload/storage/v1",
	}
}

func (u *GCSUploader) Upload(ctx context.Context, object, contentType string, data []byte) error {
	if strings.TrimSpace(u.bucket) == "" || object == "" || strings.TrimSpace(contentType) == "" {
		return errors.New("버킷, 객체 이름, 콘텐츠 유형이 필요합니다")
	}
	token, err := u.metadataToken(ctx)
	if err != nil {
		return err
	}
	query := url.Values{"uploadType": {"media"}, "name": {object}}
	endpoint := u.gcsURL + "/b/" + url.PathEscape(u.bucket) + "/o?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return errors.New("GCS 업로드 요청 생성 실패")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	resp, err := u.http.Do(req)
	if err != nil {
		return requestError(ctx, "GCS 업로드 호출 실패")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GCS 업로드 HTTP 상태 %d", resp.StatusCode)
	}
	return nil
}

func (u *GCSUploader) metadataToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.metadataURL, nil)
	if err != nil {
		return "", errors.New("메타데이터 요청 생성 실패")
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := u.http.Do(req)
	if err != nil {
		return "", requestError(ctx, "메타데이터 호출 실패")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("메타데이터 HTTP 상태 %d", resp.StatusCode)
	}
	if resp.Header.Get("Metadata-Flavor") != "Google" {
		return "", errors.New("유효하지 않은 메타데이터 서버 응답")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<16)+1))
	if err != nil || len(body) > 1<<16 {
		return "", errors.New("메타데이터 응답 읽기 실패 또는 크기 초과")
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(body, &token) != nil || strings.TrimSpace(token.AccessToken) == "" {
		return "", errors.New("잘못된 메타데이터 토큰 응답")
	}
	return token.AccessToken, nil
}

// 요청 URL이나 응답 본문에 포함될 수 있는 토큰 및 비공개 경로를 노출하지 않습니다.
func requestError(ctx context.Context, message string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New(message)
}
