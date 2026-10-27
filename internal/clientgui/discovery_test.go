package clientgui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/client"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
)

func TestDiscoveryInventoryWhileScanningAndAfterFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	results := []client.ScanResult{{URL: "http://matrix-a.test:7272", Info: testInfo()}, {URL: "http://matrix-b.test:7272", Info: testInfo()}}
	registry, err := newServerRegistry(ctx, DiscoveryOptions{Timeout: time.Second}, func(ctx context.Context, _ client.ScanOptions) ([]client.ScanResult, error) {
		switch calls.Add(1) {
		case 1:
			close(started)
			select {
			case <-release:
				return results, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case 2:
			return nil, errors.New("network unavailable")
		default:
			return nil, nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := (&GUI{registry: registry}).handler()
	registry.startScan()
	<-started
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/servers", nil))
	var state discoveryState
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &state) != nil || !state.Scanning || len(state.Servers) != 0 {
		t.Fatalf("inventory unavailable during discovery: %d %s", response.Code, response.Body)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/info", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("command without a server = %d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/servers/scan", nil))
	if response.Code != http.StatusAccepted || calls.Load() != 1 {
		t.Fatal("overlapping discovery was started")
	}
	close(release)
	state = waitForDiscovery(t, registry)
	if len(state.Servers) != 2 || state.InitialServer != results[0].URL || state.Servers[0].Width != 2 {
		t.Fatalf("discovery results = %+v", state)
	}
	registry.startScan()
	state = waitForDiscovery(t, registry)
	if state.Error != "network unavailable" || len(state.Servers) != 2 {
		t.Fatalf("failed rescan lost inventory: %+v", state)
	}
	registry.startScan()
	state = waitForDiscovery(t, registry)
	if state.Error != "" || len(state.Servers) != 0 {
		t.Fatalf("successful empty rescan = %+v", state)
	}
	if _, err := registry.clientFor(results[0].URL); err != nil {
		t.Fatalf("a previously selected address was invalidated: %v", err)
	}
}

func TestExplicitServerStartsWithoutScan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handler, err := NewWithDiscovery(ctx, DiscoveryOptions{
		ServerURL: "http://matrix.test:9090/", Timeout: time.Second,
		ScanOptions: client.ScanOptions{Networks: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/servers", nil))
	var state discoveryState
	if json.Unmarshal(response.Body.Bytes(), &state) != nil || state.Scanning || state.Error != "" || len(state.Servers) != 1 || state.InitialServer != "http://matrix.test:9090" {
		t.Fatalf("explicit target triggered discovery: %s", response.Body)
	}
}

func TestServerSelectionIsBoundToEachRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var gate atomic.Bool
	gate.Store(true)
	framesA, framesB := make(chan []byte, 1), make(chan []byte, 1)
	makeMatrix := func(width int, frames chan<- []byte, block bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1/info":
				if block && gate.CompareAndSwap(true, false) {
					close(started)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				_ = json.NewEncoder(w).Encode(server.Info{Width: width, Height: 1, Brightness: 35})
			case "/v1/frame":
				pixels, _ := io.ReadAll(r.Body)
				frames <- pixels
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, `{"sequence":1}`)
			default:
				http.NotFound(w, r)
			}
		}))
	}
	a, b := makeMatrix(2, framesA, true), makeMatrix(3, framesB, false)
	defer a.Close()
	defer b.Close()
	registry, err := newServerRegistry(ctx, DiscoveryOptions{Timeout: 3 * time.Second}, func(context.Context, client.ScanOptions) ([]client.ScanResult, error) {
		return []client.ScanResult{{URL: a.URL}, {URL: b.URL}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	registry.startScan()
	waitForDiscovery(t, registry)
	handler := (&GUI{registry: registry}).handler()
	command := func(url string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/color", bytes.NewBufferString(`{"color":"#112233"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(ServerHeader, url)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- command(a.URL) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first target did not receive Info")
	}
	if response := command(b.URL); response.Code != http.StatusAccepted {
		t.Fatalf("second target = %d %s", response.Code, response.Body)
	}
	close(release)
	select {
	case response := <-done:
		if response.Code != http.StatusAccepted {
			t.Fatalf("first target = %d %s", response.Code, response.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("first command did not finish")
	}
	if got := <-framesA; !bytes.Equal(got, bytes.Repeat([]byte{0x11, 0x22, 0x33}, 2)) {
		t.Fatalf("first target lost its geometry: %v", got)
	}
	if got := <-framesB; !bytes.Equal(got, bytes.Repeat([]byte{0x11, 0x22, 0x33}, 3)) {
		t.Fatalf("second target lost its geometry: %v", got)
	}
	if response := command("http://unknown.test:7272"); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown target accepted: %d %s", response.Code, response.Body)
	}
}

func waitForDiscovery(t *testing.T, registry *serverRegistry) discoveryState {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if state := registry.snapshot(); !state.Scanning {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("discovery did not finish")
	return discoveryState{}
}
