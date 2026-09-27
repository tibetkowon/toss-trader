package lifecycle

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestComputeStopperStop(t *testing.T) {
	for _, tc := range []struct {
		name         string
		tokenStatus  int
		wantErr      bool
		wantRequests int
	}{
		{name: "성공", tokenStatus: http.StatusOK, wantRequests: 1},
		{name: "토큰 서버 오류", tokenStatus: http.StatusInternalServerError, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Metadata-Flavor") != "Google" {
					http.NotFound(w, r)
					return
				}
				if r.Method != http.MethodGet {
					t.Errorf("메타데이터 요청 메서드: %s", r.Method)
				}
				w.Header().Set("Metadata-Flavor", "Google")
				switch r.URL.Path {
				case "/instance/service-accounts/default/token":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.tokenStatus)
					fmt.Fprint(w, `{"access_token":"tok-1"}`)
				case "/project/project-id":
					fmt.Fprint(w, "proj-1")
				case "/instance/zone":
					fmt.Fprint(w, "projects/123/zones/asia-northeast3-a")
				case "/instance/name":
					fmt.Fprint(w, "vm-1")
				default:
					http.NotFound(w, r)
				}
			}))
			defer metadata.Close()

			type request struct {
				method        string
				path          string
				authorization string
			}
			var mu sync.Mutex
			var requests []request
			compute := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, request{
					method:        r.Method,
					path:          r.URL.Path,
					authorization: r.Header.Get("Authorization"),
				})
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
			}))
			defer compute.Close()

			stopper := NewComputeStopper(nil)
			stopper.metadataURL = metadata.URL
			stopper.computeURL = compute.URL
			err := stopper.Stop(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("Stop 오류: %v, 오류 기대: %t", err, tc.wantErr)
			}

			mu.Lock()
			defer mu.Unlock()
			if len(requests) != tc.wantRequests {
				t.Fatalf("Compute 요청 수: %d, 기대: %d", len(requests), tc.wantRequests)
			}
			if tc.wantRequests == 0 {
				return
			}
			want := request{
				method:        http.MethodPost,
				path:          "/projects/proj-1/zones/asia-northeast3-a/instances/vm-1/stop",
				authorization: "Bearer tok-1",
			}
			if requests[0] != want {
				t.Errorf("Compute 요청: %+v, 기대: %+v", requests[0], want)
			}
		})
	}
}
