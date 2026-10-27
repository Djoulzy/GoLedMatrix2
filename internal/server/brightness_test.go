package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrightnessAPI(t *testing.T) {
	api, target, cancel := testAPI(t)
	defer cancel()
	handler := api.Handler()
	for _, body := range []string{`{"brightness":1}`, `{"brightness":35}`, `{"brightness":100}`} {
		request := httptest.NewRequest(http.MethodPut, "/v1/brightness", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var state BrightnessState
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &state) != nil {
			t.Fatalf("brightness response = %d %s", response.Code, response.Body)
		}
		if target.Brightness() != state.Brightness || api.currentInfo().Brightness != state.Brightness {
			t.Fatalf("brightness not applied/reported: target=%d, info=%d, response=%d", target.Brightness(), api.currentInfo().Brightness, state.Brightness)
		}
		info := httptest.NewRecorder()
		handler.ServeHTTP(info, httptest.NewRequest(http.MethodGet, "/v1/info", nil))
		var got Info
		if json.Unmarshal(info.Body.Bytes(), &got) != nil || got.Brightness != state.Brightness {
			t.Fatalf("info lost brightness: %s", info.Body)
		}
	}
	if stats := api.renderer.Stats(); stats.Accepted != 0 || stats.Rendered != 0 || stats.Failed != 0 {
		t.Fatalf("brightness changed frame statistics: %+v", stats)
	}
}

func TestBrightnessRejectsInvalidRequestsWithoutChangingSetting(t *testing.T) {
	api, target, cancel := testAPI(t)
	defer cancel()
	handler := api.Handler()
	for _, tc := range []struct {
		body, contentType string
		status            int
	}{
		{`{"brightness":35}`, "text/plain", http.StatusUnsupportedMediaType},
		{`{}`, "application/json", http.StatusBadRequest},
		{`null`, "application/json", http.StatusBadRequest},
		{`{"brightness":null}`, "application/json", http.StatusBadRequest},
		{`{"brightness":0}`, "application/json", http.StatusBadRequest},
		{`{"brightness":101}`, "application/json", http.StatusBadRequest},
		{`{"brightness":-1}`, "application/json", http.StatusBadRequest},
		{`{"brightness":35.5}`, "application/json", http.StatusBadRequest},
		{`{"brightness":"35"}`, "application/json", http.StatusBadRequest},
		{`{"brightness":35,"unknown":true}`, "application/json", http.StatusBadRequest},
		{`{"brightness":35}{}`, "application/json", http.StatusBadRequest},
		{`{"brightness":35}` + strings.Repeat(" ", 1024), "application/json", http.StatusBadRequest},
	} {
		request := httptest.NewRequest(http.MethodPut, "/v1/brightness", strings.NewReader(tc.body))
		request.Header.Set("Content-Type", tc.contentType)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.status || response.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("invalid request %q: %d %s", tc.body, response.Code, response.Body)
		}
		if target.Brightness() != 100 || api.currentInfo().Brightness != 100 {
			t.Fatal("invalid request changed brightness")
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/brightness", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET brightness = %d", response.Code)
	}
}
