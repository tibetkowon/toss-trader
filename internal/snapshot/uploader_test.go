package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGCSUploader(t *testing.T) {
	for _, tc := range []struct {
		name       string
		token      string
		flavor     string
		metaStatus int
		gcsStatus  int
		wantErr    string
		wantCalls  int32
	}{
		{"성공", `{"access_token":"tok-1"}`, "Google", 200, 200, "", 1},
		{"생성 성공", `{"access_token":"tok-1"}`, "Google", 200, 201, "", 1},
		{"인증 실패", `{}`, "Google", 403, 200, "메타데이터 HTTP 상태 403", 0},
		{"응답 검증 실패", `{"access_token":"tok-1"}`, "", 200, 200, "유효하지 않은", 0},
		{"토큰 JSON 오류", `{`, "Google", 200, 200, "잘못된", 0},
		{"빈 토큰", `{"access_token":""}`, "Google", 200, 200, "잘못된", 0},
		{"누락된 토큰", `{}`, "Google", 200, 200, "잘못된", 0},
		{"공백 토큰", `{"access_token":" "}`, "Google", 200, 200, "잘못된", 0},
		{"응답 크기 초과", strings.Repeat("x", (1<<16)+1), "Google", 200, 200, "크기 초과", 0},
		{"업로드 권한 오류", `{"access_token":"tok-1"}`, "Google", 200, 403, "GCS 업로드 HTTP 상태 403", 1},
		{"업로드 서버 오류", `{"access_token":"tok-1"}`, "Google", 200, 500, "GCS 업로드 HTTP 상태 500", 1},
		{"리디렉션 거부", `{"access_token":"tok-1"}`, "Google", 200, 302, "GCS 업로드 HTTP 상태 302", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			metadata := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/instance/service-accounts/default/token" {
					t.Errorf("메타데이터 요청: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Metadata-Flavor") != "Google" || r.Header.Get("Authorization") != "" {
					t.Error("메타데이터 요청 헤더 오류")
				}
				w.Header().Set("Metadata-Flavor", tc.flavor)
				w.WriteHeader(tc.metaStatus)
				fmt.Fprint(w, tc.token)
			}))
			defer metadata.Close()
			object := "random-slug/상태 +?#&.html"
			payload := []byte("<html>거래 상태</html>")
			gcs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/b/test-bucket/o" {
					t.Errorf("업로드 요청: %s %s", r.Method, r.URL.Path)
				}
				q := r.URL.Query()
				if len(q) != 2 || q.Get("uploadType") != "media" || q.Get("name") != object {
					t.Errorf("업로드 쿼리: %v", q)
				}
				if r.Header.Get("Authorization") != "Bearer tok-1" || r.Header.Get("Content-Type") != "text/html; charset=utf-8" {
					t.Error("업로드 요청 헤더 오류")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != string(payload) {
					t.Errorf("업로드 본문: %q, 오류: %v", body, err)
				}
				w.Header().Set("Location", metadata.URL+"/redirect")
				w.WriteHeader(tc.gcsStatus)
				fmt.Fprint(w, "비공개 응답 본문")
			}))
			defer gcs.Close()
			u := NewGCSUploader(nil, "test-bucket")
			u.metadataURL = metadata.URL + "/instance/service-accounts/default/token"
			u.gcsURL = gcs.URL
			err := u.Upload(context.Background(), object, "text/html; charset=utf-8", payload)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("오류: %v, 기대: %s", err, tc.wantErr)
			}
			if err != nil && (strings.Contains(err.Error(), "tok-1") || strings.Contains(err.Error(), "비공개")) {
				t.Error("오류에 비공개 정보가 포함되었습니다")
			}
			if got := calls.Load(); got != tc.wantCalls {
				t.Errorf("업로드 호출 수: %d, 기대: %d", got, tc.wantCalls)
			}
		})
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("비공개 전송 오류")
}

func TestGCSUploaderRequestErrors(t *testing.T) {
	u := NewGCSUploader(&http.Client{Transport: failingTransport{}}, "bucket")
	if err := u.Upload(context.Background(), "status.json", "application/json", nil); err == nil || strings.Contains(err.Error(), "비공개") {
		t.Fatalf("전송 오류: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := u.Upload(ctx, "status.json", "application/json", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("취소 오류: %v", err)
	}
	u.metadataURL = "://invalid"
	if err := u.Upload(context.Background(), "status.json", "application/json", nil); err == nil {
		t.Fatal("잘못된 메타데이터 URL 오류가 필요합니다")
	}
	for _, tc := range []struct{ bucket, object, contentType string }{
		{"", "status.json", "application/json"},
		{"bucket", "", "application/json"},
		{"bucket", "status.json", ""},
	} {
		u := NewGCSUploader(nil, tc.bucket)
		if err := u.Upload(context.Background(), tc.object, tc.contentType, nil); err == nil {
			t.Errorf("빈 업로드 인수 오류가 필요합니다: %+v", tc)
		}
	}
}

func TestGCSUploaderUploadTransportError(t *testing.T) {
	metadata := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Metadata-Flavor", "Google")
		fmt.Fprint(w, `{"access_token":"tok-1"}`)
	}))
	defer metadata.Close()
	gcs := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	gcs.Close()
	u := NewGCSUploader(nil, "bucket")
	u.metadataURL = metadata.URL
	u.gcsURL = gcs.URL
	if err := u.Upload(context.Background(), "status.json", "application/json", nil); err == nil || !strings.Contains(err.Error(), "GCS 업로드 호출 실패") {
		t.Fatalf("업로드 전송 오류: %v", err)
	}
	u.gcsURL = "://invalid"
	if err := u.Upload(context.Background(), "status.json", "application/json", nil); err == nil || !strings.Contains(err.Error(), "요청 생성 실패") {
		t.Fatalf("업로드 URL 오류: %v", err)
	}
}
