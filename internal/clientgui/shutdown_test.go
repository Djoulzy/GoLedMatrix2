package clientgui

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

func TestShutdownProxy(t *testing.T) {
	for _, body := range []string{`{"power_off":false}`, `{"power_off":true}`} {
		matrix := &fakeMatrixClient{info: testInfo()}
		handler := newHandler(matrix)
		request := httptest.NewRequest(http.MethodPost, "/api/shutdown", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusAccepted || matrix.shutdownCalls != 1 || matrix.powerOff != strings.Contains(body, "true") {
			t.Fatalf("shutdown proxy = %d %s, calls=%d powerOff=%v", response.Code, response.Body, matrix.shutdownCalls, matrix.powerOff)
		}
		if !strings.Contains(response.Body.String(), `"status":"shutting_down"`) {
			t.Fatalf("shutdown acknowledgement = %s", response.Body)
		}
	}
}

func TestShutdownProxyRejectsInvalidRequests(t *testing.T) {
	for _, body := range []string{"", "null", "[]", `{"power_off":"true"}`, `{"command":"poweroff"}`, `{} {}`, `{}` + strings.Repeat(" ", 1025)} {
		matrix := &fakeMatrixClient{info: testInfo()}
		request := httptest.NewRequest(http.MethodPost, "/api/shutdown", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		newHandler(matrix).ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || matrix.shutdownCalls != 0 {
			t.Fatalf("invalid request %q = %d %s, calls=%d", body, response.Code, response.Body, matrix.shutdownCalls)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		matrix := &fakeMatrixClient{info: testInfo()}
		request := httptest.NewRequest(method, "/api/shutdown", strings.NewReader(`{"power_off":true}`))
		request.Header.Set("Content-Type", "text/plain")
		response := httptest.NewRecorder()
		newHandler(matrix).ServeHTTP(response, request)
		if response.Code < 400 || matrix.shutdownCalls != 0 {
			t.Fatalf("unsafe request = %d %s, calls=%d", response.Code, response.Body, matrix.shutdownCalls)
		}
	}
}

func TestShutdownProxyReportsServerFailure(t *testing.T) {
	matrix := &fakeMatrixClient{info: testInfo(), shutdownErr: errors.New("power-off permission denied")}
	request := httptest.NewRequest(http.MethodPost, "/api/shutdown", strings.NewReader(`{"power_off":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newHandler(matrix).ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "power-off permission denied") {
		t.Fatalf("failed shutdown proxy = %d %s", response.Code, response.Body)
	}
}

func TestShutdownButtonMarkup(t *testing.T) {
	response := httptest.NewRecorder()
	newHandler(&fakeMatrixClient{info: testInfo()}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, text := range []string{`id="shutdown"`, `class="danger"`, `aria-describedby="shutdown-hint"`, `id="shutdown-hint"`} {
		if !bytes.Contains(response.Body.Bytes(), []byte(text)) {
			t.Fatalf("shutdown UI missing %s", text)
		}
	}
}

func TestBrowserShutdown(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("optional browser script tests require Node.js")
	}
	if output, err := exec.Command(node, "testdata/shutdown.mjs").CombinedOutput(); err != nil {
		t.Fatalf("browser shutdown tests failed: %v\n%s", err, output)
	}
}
