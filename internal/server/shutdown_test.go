package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/render"
)

func shutdownAPI(t *testing.T, info ShutdownInfo, request func(context.Context, ShutdownRequest) error) *API {
	t.Helper()
	target, err := display.NewMemory(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(2, 1, "memory", render.New(target), WithShutdown(info, request))
	if err != nil {
		t.Fatal(err)
	}
	return api
}

func shutdownResponse(handler http.Handler, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/v1/shutdown", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestShutdownCapabilities(t *testing.T) {
	api := shutdownAPI(t, ShutdownInfo{PowerOff: true}, func(context.Context, ShutdownRequest) error { return nil })
	info := api.currentInfo()
	if info.Shutdown == nil || !info.Shutdown.PowerOff {
		t.Fatalf("shutdown capabilities = %+v", info.Shutdown)
	}
	info.Shutdown.PowerOff = false
	if !api.currentInfo().Shutdown.PowerOff {
		t.Fatal("info leaked mutable shutdown capabilities")
	}
	if err := WithShutdown(ShutdownInfo{}, nil)(api); err == nil {
		t.Fatal("nil shutdown controller accepted")
	}
	api.shutdown, api.shutdownInfo = nil, nil
	if api.currentInfo().Shutdown != nil {
		t.Fatal("disabled shutdown was advertised")
	}
	response := shutdownResponse(api.Handler(), `{"power_off":false}`)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled shutdown = %d %s", response.Code, response.Body)
	}
}

func TestShutdownRequestAndIdempotence(t *testing.T) {
	for _, powerOff := range []bool{false, true} {
		t.Run(map[bool]string{false: "server", true: "poweroff"}[powerOff], func(t *testing.T) {
			var calls atomic.Int32
			api := shutdownAPI(t, ShutdownInfo{PowerOff: true}, func(ctx context.Context, request ShutdownRequest) error {
				if request.PowerOff != powerOff || ctx == nil {
					t.Errorf("shutdown request = %+v", request)
				}
				calls.Add(1)
				return nil
			})
			body, _ := json.Marshal(ShutdownRequest{PowerOff: powerOff})
			handler := api.Handler()
			var workers sync.WaitGroup
			for i := 0; i < 20; i++ {
				workers.Add(1)
				go func() {
					defer workers.Done()
					response := shutdownResponse(handler, string(body))
					var got struct {
						Status   string `json:"status"`
						PowerOff bool   `json:"power_off"`
					}
					if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &got) != nil || got.Status != "shutting_down" || got.PowerOff != powerOff {
						t.Errorf("shutdown response = %d %s", response.Code, response.Body)
					}
				}()
			}
			workers.Wait()
			if calls.Load() != 1 {
				t.Fatalf("controller called %d times, want once", calls.Load())
			}
			body, _ = json.Marshal(ShutdownRequest{PowerOff: !powerOff})
			if response := shutdownResponse(handler, string(body)); response.Code != http.StatusConflict {
				t.Fatalf("changed shutdown mode = %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestShutdownRejectsUnsafeRequests(t *testing.T) {
	for _, test := range []struct {
		name, method, contentType, body string
		status                          int
	}{
		{"get", http.MethodGet, "application/json", `{}`, http.StatusMethodNotAllowed},
		{"form", http.MethodPost, "application/x-www-form-urlencoded", `power_off=true`, http.StatusUnsupportedMediaType},
		{"plain", http.MethodPost, "text/plain", `{"power_off":true}`, http.StatusUnsupportedMediaType},
		{"invalid media type", http.MethodPost, "application/json-invalid", `{}`, http.StatusUnsupportedMediaType},
		{"empty", http.MethodPost, "application/json", ``, http.StatusBadRequest},
		{"null", http.MethodPost, "application/json", `null`, http.StatusBadRequest},
		{"array", http.MethodPost, "application/json", `[]`, http.StatusBadRequest},
		{"unknown field", http.MethodPost, "application/json", `{"command":"reboot"}`, http.StatusBadRequest},
		{"wrong type", http.MethodPost, "application/json", `{"power_off":"true"}`, http.StatusBadRequest},
		{"two objects", http.MethodPost, "application/json", `{} {}`, http.StatusBadRequest},
		{"too large", http.MethodPost, "application/json", `{}` + strings.Repeat(" ", 1025), http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			api := shutdownAPI(t, ShutdownInfo{PowerOff: true}, func(context.Context, ShutdownRequest) error { calls++; return nil })
			request := httptest.NewRequest(test.method, "/v1/shutdown", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			api.Handler().ServeHTTP(response, request)
			if response.Code != test.status || calls != 0 {
				t.Fatalf("response = %d %s, calls = %d", response.Code, response.Body, calls)
			}
		})
	}
}

func TestShutdownUnsupportedPowerOffDoesNotStopServer(t *testing.T) {
	calls := 0
	api := shutdownAPI(t, ShutdownInfo{PowerOffUnavailableReason: "permission missing"}, func(context.Context, ShutdownRequest) error { calls++; return nil })
	response := shutdownResponse(api.Handler(), `{"power_off":true}`)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "permission missing") || calls != 0 {
		t.Fatalf("unsupported poweroff = %d %s, calls = %d", response.Code, response.Body, calls)
	}
	response = shutdownResponse(api.Handler(), `{"power_off":false}`)
	if response.Code != http.StatusAccepted || calls != 1 {
		t.Fatalf("server-only shutdown = %d %s, calls = %d", response.Code, response.Body, calls)
	}
}

func TestShutdownFailureCanBeRetried(t *testing.T) {
	calls := 0
	api := shutdownAPI(t, ShutdownInfo{PowerOff: true}, func(context.Context, ShutdownRequest) error {
		calls++
		if calls == 1 {
			return errors.New("permission denied")
		}
		return nil
	})
	handler := api.Handler()
	response := shutdownResponse(handler, `{"power_off":true}`)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "permission denied") {
		t.Fatalf("failed poweroff = %d %s", response.Code, response.Body)
	}
	if response := shutdownResponse(handler, `{"power_off":true}`); response.Code != http.StatusAccepted || calls != 2 {
		t.Fatalf("retry = %d %s, calls = %d", response.Code, response.Body, calls)
	}
}
