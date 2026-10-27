package client

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/server"
)

func TestScanNetworks(t *testing.T) {
	for _, tc := range []struct {
		name     string
		prefixes []string
		want     []string
	}{
		{
			name:     "actual masks, overlap, and numeric order",
			prefixes: []string{"192.168.1.90/23", "192.168.0.0/24", "10.0.0.10/32", "10.0.0.2/32", "192.168.1.90/23"},
			want:     []string{"10.0.0.2/32", "10.0.0.10/32", "192.168.0.0/23"},
		},
		{name: "maximum subnet", prefixes: []string{"10.0.42.7/16"}, want: []string{"10.0.0.0/16"}},
		{name: "host and point to point", prefixes: []string{"192.168.0.1/31", "192.168.1.7/32"}, want: []string{"192.168.0.0/31", "192.168.1.7/32"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var prefixes []netip.Prefix
			for _, prefix := range tc.prefixes {
				prefixes = append(prefixes, netip.MustParsePrefix(prefix))
			}
			got, err := scanNetworks(prefixes)
			if err != nil {
				t.Fatal(err)
			}
			var actual []string
			for _, prefix := range got {
				actual = append(actual, prefix.String())
			}
			if !reflect.DeepEqual(actual, tc.want) {
				t.Fatalf("networks = %v, want %v", actual, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name     string
		prefixes []netip.Prefix
	}{
		{name: "no networks"},
		{name: "invalid prefix", prefixes: []netip.Prefix{{}}},
		{name: "IPv6", prefixes: []netip.Prefix{netip.MustParsePrefix("2001:db8::/64")}},
		{name: "large network", prefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}},
		{name: "large total", prefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/16"), netip.MustParsePrefix("192.168.0.0/24")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := scanNetworks(tc.prefixes); err == nil {
				t.Fatal("invalid or oversized scan accepted")
			}
		})
	}
}

func TestScanSubnets(t *testing.T) {
	networks, err := scanNetworks([]netip.Prefix{
		netip.MustParsePrefix("192.168.0.0/28"),
		netip.MustParsePrefix("192.168.0.1/32"), // already covered
		netip.MustParsePrefix("192.168.1.0/31"),
		netip.MustParsePrefix("192.168.2.7/32"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	visits := make(map[string]int)
	httpClient := &http.Client{Transport: handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/info" || r.URL.Port() != "9090" {
			t.Errorf("unexpected scan request: %s %s", r.Method, r.URL)
		}
		ip := r.URL.Hostname()
		mu.Lock()
		visits[ip]++
		mu.Unlock()
		switch ip {
		case "192.168.0.2", "192.168.0.10":
			_ = json.NewEncoder(w).Encode(scanTestInfo())
		case "192.168.0.3":
			_, _ = w.Write([]byte(`{"service":"another HTTP service"}`))
		default:
			http.NotFound(w, r)
		}
	})}}
	results, err := scanSubnets(context.Background(), networks, 9090, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].URL != "http://192.168.0.2:9090" || results[1].URL != "http://192.168.0.10:9090" {
		t.Fatalf("results = %+v", results)
	}
	if results[0].Info.Width != 64 || results[0].Info.Height != 32 || results[0].Info.Backend != "memory" {
		t.Fatalf("server information lost: %+v", results[0])
	}
	if len(visits) != 17 || visits["192.168.0.0"] != 0 || visits["192.168.0.15"] != 0 {
		t.Fatalf("unexpected host enumeration: %v", visits)
	}
	for _, ip := range []string{"192.168.0.1", "192.168.1.0", "192.168.1.1", "192.168.2.7"} {
		if visits[ip] != 1 {
			t.Errorf("host %s visited %d times, want 1", ip, visits[ip])
		}
	}
	for ip, count := range visits {
		if count != 1 {
			t.Errorf("duplicate probe for %s: %d", ip, count)
		}
	}
}

func TestProbeServerRejectsOtherServices(t *testing.T) {
	valid, err := json.Marshal(scanTestInfo())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{name: "compatible", status: http.StatusOK, body: string(valid), want: true},
		{name: "unknown optional field", status: http.StatusOK, body: strings.TrimSuffix(string(valid), "}") + `,"future":true}`, want: true},
		{name: "HTTP error", status: http.StatusServiceUnavailable, body: string(valid)},
		{name: "redirect", status: http.StatusFound, body: string(valid)},
		{name: "HTML", status: http.StatusOK, body: "<html>hello</html>"},
		{name: "empty JSON", status: http.StatusOK, body: "{}"},
		{name: "other protocol", status: http.StatusOK, body: strings.Replace(string(valid), `"protocol_version":"1"`, `"protocol_version":"2"`, 1)},
		{name: "other format", status: http.StatusOK, body: strings.Replace(string(valid), "rgb24", "rgba32", 1)},
		{name: "invalid dimensions", status: http.StatusOK, body: strings.Replace(string(valid), `"width":64`, `"width":0`, 1)},
		{name: "wrong frame size", status: http.StatusOK, body: strings.Replace(string(valid), `"frame_bytes":6144`, `"frame_bytes":12`, 1)},
		{name: "missing backend", status: http.StatusOK, body: strings.Replace(string(valid), `"backend":"memory"`, `"backend":""`, 1)},
		{name: "trailing JSON", status: http.StatusOK, body: string(valid) + "{}"},
		{name: "too large", status: http.StatusOK, body: string(valid) + strings.Repeat(" ", maxScanInfoBytes)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			httpClient := &http.Client{Transport: handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})}}
			_, found := probeServer(context.Background(), httpClient, "http://matrix.test:8080")
			if found != tc.want {
				t.Fatalf("detected = %v, want %v", found, tc.want)
			}
		})
	}
}

func TestScanCancellationKeepsResultsAndBoundsConcurrency(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var active, blocked atomic.Int32
	allBlocked := make(chan struct{})
	httpClient := &http.Client{Transport: handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count := active.Add(1); count > scanWorkers {
			t.Errorf("too many concurrent probes: %d", count)
		}
		defer active.Add(-1)
		if r.URL.Hostname() == "192.168.0.1" {
			_ = json.NewEncoder(w).Encode(scanTestInfo())
			return
		}
		if blocked.Add(1) == scanWorkers {
			// All workers are blocked only after the first result has reached
			// the collector and its worker has started another request.
			close(allBlocked)
		}
		<-r.Context().Done()
	})}}
	type outcome struct {
		results []ScanResult
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		results, err := scanSubnets(ctx, []netip.Prefix{netip.MustParsePrefix("192.168.0.0/24")}, 8080, httpClient)
		done <- outcome{results, err}
	}()
	select {
	case <-allBlocked:
	case <-time.After(2 * time.Second):
		t.Fatal("workers did not start")
	}
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) || len(got.results) != 1 || got.results[0].URL != "http://192.168.0.1:8080" {
			t.Fatalf("canceled scan = %+v, %v", got.results, got.err)
		}
		if active.Load() != 0 {
			t.Fatal("scan returned with active requests")
		}
	case <-time.After(time.Second):
		t.Fatal("scan did not stop after cancellation")
	}
}

func TestScanUsesDirectHTTPWithoutRedirects(t *testing.T) {
	var proxyRequests, targetRequests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests.Add(1)
		http.Error(w, "proxy must not be used", http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests.Add(1)
		_ = json.NewEncoder(w).Encode(scanTestInfo())
	}))
	defer target.Close()
	var redirect atomic.Bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if redirect.Load() {
			http.Redirect(w, r, target.URL, http.StatusFound)
			return
		}
		_ = json.NewEncoder(w).Encode(scanTestInfo())
	}))
	defer api.Close()
	options := scanTestOptions(t, api)
	for _, want := range []int{1, 0} {
		results, err := Scan(context.Background(), options)
		if err != nil || len(results) != want {
			t.Fatalf("scan results = %+v, %v; want %d", results, err, want)
		}
		redirect.Store(true)
	}
	if proxyRequests.Load() != 0 || targetRequests.Load() != 0 {
		t.Fatal("scan used a proxy or followed a redirect")
	}
}

func TestScanProbeTimeoutAndValidation(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer api.Close()
	options := scanTestOptions(t, api)
	options.ProbeTimeout = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	results, err := Scan(ctx, options)
	if err != nil || len(results) != 0 {
		t.Fatalf("unresponsive server: %+v, %v", results, err)
	}
	for _, invalid := range []ScanOptions{
		{Port: -1}, {Port: 65536}, {ProbeTimeout: -time.Second},
		{Networks: []netip.Prefix{netip.MustParsePrefix("2001:db8::/64")}},
	} {
		if _, err := Scan(context.Background(), invalid); err == nil {
			t.Fatalf("invalid options accepted: %+v", invalid)
		}
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := Scan(canceled, options); !errors.Is(err, context.Canceled) {
		t.Fatalf("already canceled context: %v", err)
	}
}

func scanTestInfo() server.Info {
	return server.Info{ProtocolVersion: "1", Width: 64, Height: 32, PixelFormat: "rgb24", FrameBytes: 6144, Backend: "memory"}
}

func scanTestOptions(t *testing.T, api *httptest.Server) ScanOptions {
	t.Helper()
	host, port, err := net.SplitHostPort(api.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	address := netip.MustParseAddr(host)
	if !address.Is4() {
		t.Fatal("scan test requires an IPv4 listener")
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return ScanOptions{Networks: []netip.Prefix{netip.PrefixFrom(address, 32)}, Port: number}
}
