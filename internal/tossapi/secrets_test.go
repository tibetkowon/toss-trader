package tossapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMemorySecretProvider(t *testing.T) {
	values := map[string]string{"name": "value"}
	provider := NewMemorySecretProvider(values)
	values["name"] = "changed"
	value, err := provider.GetSecret(context.Background(), "name")
	if err != nil || value != "value" {
		t.Fatalf("복사된 시크릿: %q, %v", value, err)
	}
	if _, err := provider.GetSecret(context.Background(), "missing"); err == nil {
		t.Fatal("없는 시크릿을 허용했습니다")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.GetSecret(ctx, "name"); !errors.Is(err, context.Canceled) {
		t.Fatalf("취소 결과: %v", err)
	}
}

func TestSecretManagerProvider(t *testing.T) {
	const name = "projects/project-id/secrets/client-id/versions/latest"
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprint(denied), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("시크릿 요청 메서드 불일치")
				}
				switch r.URL.Path {
				case "/metadata":
					if r.Header.Get("Metadata-Flavor") != "Google" {
						t.Error("메타데이터 요청 헤더 누락")
					}
					w.Header().Set("Metadata-Flavor", "Google")
					fmt.Fprint(w, `{"access_token":"gce-token"}`)
				case "/v1/" + name + ":access":
					if r.Header.Get("Authorization") != "Bearer gce-token" {
						t.Error("Secret Manager 인증 헤더 불일치")
					}
					if denied {
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, "sensitive-error-body")
						return
					}
					fmt.Fprintf(w, `{"payload":{"data":%q}}`, base64.StdEncoding.EncodeToString([]byte("client-value")))
				default:
					t.Errorf("예상하지 않은 경로: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			provider := NewSecretManagerProvider(server.Client())
			provider.endpoint = server.URL + "/v1/"
			provider.metadataURL = server.URL + "/metadata"
			value, err := provider.GetSecret(context.Background(), name)
			if denied {
				if err == nil || err.Error() != "시크릿 HTTP 상태 403" {
					t.Fatalf("권한 오류: %v", err)
				}
			} else if err != nil || value != "client-value" {
				t.Fatalf("시크릿 결과: %q, %v", value, err)
			}
			if _, err := provider.GetSecret(context.Background(), "../invalid"); err == nil {
				t.Fatal("잘못된 시크릿 리소스 이름을 허용했습니다")
			}
		})
	}
}
