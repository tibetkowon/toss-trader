package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Stopper stops the current compute instance.
type Stopper interface {
	Stop(ctx context.Context) error
}

// ComputeStopper stops the current GCE instance using only the metadata server
// and the Compute Engine REST API — no SDK, no service account key files.
// Project ID, zone, and instance name are discovered at call time from the
// metadata server; nothing project-specific is hardcoded (this repo is public).
type ComputeStopper struct {
	http        *http.Client
	metadataURL string // default "http://metadata.google.internal/computeMetadata/v1", overridable for tests
	computeURL  string // default "https://compute.googleapis.com/compute/v1", overridable for tests
}

func NewComputeStopper(client *http.Client) *ComputeStopper {
	if client == nil {
		client = &http.Client{}
	}
	return &ComputeStopper{
		http:        client,
		metadataURL: "http://metadata.google.internal/computeMetadata/v1",
		computeURL:  "https://compute.googleapis.com/compute/v1",
	}
}

func (s *ComputeStopper) Stop(ctx context.Context) error {
	token, err := s.metadataToken(ctx)
	if err != nil {
		return err
	}
	project, err := s.metadataText(ctx, "/project/project-id")
	if err != nil {
		return err
	}
	zonePath, err := s.metadataText(ctx, "/instance/zone")
	if err != nil {
		return err
	}
	zone := zonePath
	if i := strings.LastIndex(zonePath, "/"); i >= 0 {
		zone = zonePath[i+1:]
	}
	name, err := s.metadataText(ctx, "/instance/name")
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/projects/%s/zones/%s/instances/%s/stop", s.computeURL, project, zone, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return errors.New("정지 요청 생성 실패")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.http.Do(req)
	if err != nil {
		return errors.New("정지 API 호출 실패")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("정지 API HTTP 상태 %d", resp.StatusCode)
	}
	return nil
}

func (s *ComputeStopper) metadataToken(ctx context.Context) (string, error) {
	body, err := s.metadataRaw(ctx, "/instance/service-accounts/default/token")
	if err != nil {
		return "", err
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(body, &token) != nil || token.AccessToken == "" {
		return "", errors.New("잘못된 메타데이터 토큰 응답")
	}
	return token.AccessToken, nil
}

func (s *ComputeStopper) metadataText(ctx context.Context, path string) (string, error) {
	body, err := s.metadataRaw(ctx, path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func (s *ComputeStopper) metadataRaw(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.metadataURL+path, nil)
	if err != nil {
		return nil, errors.New("메타데이터 요청 생성 실패")
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, errors.New("메타데이터 HTTP 요청 실패")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Metadata-Flavor") != "Google" {
		return nil, fmt.Errorf("메타데이터 HTTP 상태 %d 또는 유효하지 않은 응답", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<16))
}
