package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/client"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
)

func TestDefaultGUIStartsAndDiscoversServers(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(server.Info{
			ProtocolVersion: "1", Width: 64, Height: 32, PixelFormat: "rgb24", FrameBytes: 6144, Backend: "memory", Brightness: 25,
		})
	}))
	defer api.Close()
	host, port, _ := net.SplitHostPort(api.Listener.Addr().String())
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic discovery", true: "explicit server"}[explicit], func(t *testing.T) {
			args := []string{"-scan-port", port, "-scan-network", host + "/32"}
			if explicit {
				// This subnet would fail a scan; an explicit target must skip it.
				args = []string{"-server", api.URL, "-scan-network", "10.0.0.0/8"}
			}
			url := startGUIProcess(t, args)
			httpClient := &http.Client{Timeout: time.Second}
			index, err := httpClient.Get(url + "/")
			if err != nil {
				t.Fatal(err)
			}
			var html bytes.Buffer
			_, _ = html.ReadFrom(index.Body)
			index.Body.Close()
			if index.StatusCode != http.StatusOK || !strings.Contains(html.String(), `id="server-select"`) {
				t.Fatalf("GUI unavailable: %d %s", index.StatusCode, html.String())
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				response, err := httpClient.Get(url + "/api/servers")
				if err != nil {
					t.Fatal(err)
				}
				var inventory struct {
					Servers []struct {
						URL string `json:"url"`
					} `json:"servers"`
					Scanning bool   `json:"scanning"`
					Error    string `json:"error"`
				}
				err = json.NewDecoder(response.Body).Decode(&inventory)
				response.Body.Close()
				if err != nil || inventory.Error != "" {
					t.Fatalf("discovery inventory = %+v, %v", inventory, err)
				}
				if !inventory.Scanning {
					if len(inventory.Servers) != 1 || inventory.Servers[0].URL != api.URL {
						t.Fatalf("discovery lost server: %+v", inventory)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("discovery did not finish")
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}
}

func TestHeadlessDiscoversUniqueServer(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/info":
			_ = json.NewEncoder(w).Encode(server.Info{ProtocolVersion: "1", Width: 2, Height: 1, PixelFormat: "rgb24", FrameBytes: 6, Backend: "memory"})
		case "/v1/brightness":
			var state server.BrightnessState
			if r.Method != http.MethodPut || json.NewDecoder(r.Body).Decode(&state) != nil || state.Brightness != 35 {
				t.Errorf("invalid brightness request: %s %+v", r.Method, state)
			}
			_ = json.NewEncoder(w).Encode(state)
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	host, port, _ := net.SplitHostPort(api.Listener.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestClientCLIHelper$", "--", "-no-gui", "-brightness", "35", "-scan-port", port, "-scan-network", host+"/32")
	command.Env = append(os.Environ(), "GOLEDMATRIX_CLIENT_CLI_TEST=1")
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "brightness set to 35%") {
		t.Fatalf("headless discovery = %s, %v", output, err)
	}
}

func TestGUIListenAddressUsesKernelRoute(t *testing.T) {
	for _, tc := range []struct {
		name, address, ipv4, ipv6, want string
		calls                           int
	}{
		{"default IPv4 route", defaultGUIListenAddress, "192.168.0.113", "2001:db8::2", "192.168.0.113:8080", 1},
		{"VPN default route", "detect:9090", "10.8.0.2", "", "10.8.0.2:9090", 1},
		{"IPv6-only route", "detect:8080", "", "2001:db8::2", "[2001:db8::2]:8080", 2},
		{"offline fallback", "detect:8080", "", "", "127.0.0.1:8080", 2},
		{"ignore routed loopback", "detect:8080", "127.0.0.1", "2001:db8::2", "[2001:db8::2]:8080", 2},
		{"ignore unspecified IP", "detect:8080", "0.0.0.0", "::", "127.0.0.1:8080", 2},
		{"ignore link-local IP", "detect:8080", "169.254.1.2", "fe80::1", "127.0.0.1:8080", 2},
		{"ignore multicast IP", "detect:8080", "224.0.0.1", "ff02::1", "127.0.0.1:8080", 2},
		{"case-insensitive detection", "DETECT:0", "192.168.0.113", "", "192.168.0.113:0", 1},
		{"explicit loopback", "127.0.0.1:8080", "192.168.0.113", "", "127.0.0.1:8080", 0},
		{"explicit wildcard", "0.0.0.0:8080", "192.168.0.113", "", "0.0.0.0:8080", 0},
		{"explicit all interfaces", ":8080", "192.168.0.113", "", ":8080", 0},
		{"explicit IPv6", "[::1]:9090", "192.168.0.113", "", "[::1]:9090", 0},
		{"explicit hostname", "localhost:8080", "192.168.0.113", "", "localhost:8080", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var probes []string
			got, err := guiListenAddress(tc.address, func(network, destination string) net.IP {
				probes = append(probes, network+" "+destination)
				if network == "udp4" {
					return net.ParseIP(tc.ipv4)
				}
				return net.ParseIP(tc.ipv6)
			})
			if err != nil || got != tc.want {
				t.Fatalf("GUI listen address = %q, %v; want %q", got, err, tc.want)
			}
			wantProbes := []string{"udp4 192.0.2.1:9", "udp6 [2001:db8::1]:9"}[:tc.calls]
			if len(probes) != tc.calls || (tc.calls > 0 && !reflect.DeepEqual(probes, wantProbes)) {
				t.Fatalf("route probes = %v, want %v", probes, wantProbes)
			}
		})
	}
}

func TestGUIListenAddressRejectsMissingPort(t *testing.T) {
	for _, address := range []string{"detect", "127.0.0.1", "[::1]"} {
		if _, err := guiListenAddress(address, func(string, string) net.IP {
			t.Fatal("invalid listen address triggered route detection")
			return nil
		}); err == nil {
			t.Fatalf("invalid listen address accepted: %q", address)
		}
	}
}

func TestGUIStartsOnDetectedInterface(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(server.Info{ProtocolVersion: "1", Width: 64, Height: 32, Backend: "memory"})
	}))
	defer api.Close()
	address, err := guiListenAddress("detect:0", guiRouteSourceIP)
	if err != nil {
		t.Fatal(err)
	}
	wantHost, _, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	rawURL := startGUIProcess(t, []string{"-gui-listen", "detect:0", "-server", api.URL})
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() != wantHost || parsed.Port() == "0" {
		t.Fatalf("GUI startup URL = %s, %v; want host %s", rawURL, err, wantHost)
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: time.Second}
	response, err := httpClient.Get(rawURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GUI unavailable on detected address: %s", response.Status)
	}
}

func TestGUIAndHeadlessFlagRules(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		want    string
		wantErr bool
	}{
		{[]string{"-color", "#ffffff"}, "CLI actions require -no-gui", true},
		{[]string{"-no-gui"}, "provide exactly one", true},
		{[]string{"-gui"}, "flag provided but not defined: -gui", true},
		{[]string{"-h"}, `detect:8080`, false},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		command := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestClientCLIHelper$", "--"}, tc.args...)...)
		command.Env = append(os.Environ(), "GOLEDMATRIX_CLIENT_CLI_TEST=1")
		output, err := command.CombinedOutput()
		cancel()
		if (err != nil) != tc.wantErr || !strings.Contains(string(output), tc.want) {
			t.Fatalf("flag rules = %s, %v; want %q", output, err, tc.want)
		}
	}
}

func TestHeadlessDiscoveryRejectsAmbiguousOrIncompleteResults(t *testing.T) {
	results := []client.ScanResult{{URL: "http://matrix-a.test:7272"}, {URL: "http://matrix-b.test:7272"}}
	if url, err := serverFromScan(results[:1], nil); err != nil || url != results[0].URL {
		t.Fatalf("unique result = %q, %v", url, err)
	}
	for _, tc := range []struct {
		results []client.ScanResult
		err     error
		want    string
	}{
		{nil, nil, "no GoLedMatrix2 servers found"},
		{results, nil, "multiple server addresses found"},
		{results[:1], context.DeadlineExceeded, "discovery incomplete"},
	} {
		if url, err := serverFromScan(tc.results, tc.err); err == nil || url != "" || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("unsafe automatic selection: %q, %v", url, err)
		}
	}
}

func startGUIProcess(t *testing.T, args []string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	arguments := append([]string{"-test.run=^TestClientCLIHelper$", "--", "-gui-listen", "127.0.0.1:0"}, args...)
	command := exec.CommandContext(ctx, os.Args[0], arguments...)
	command.Env = append(os.Environ(), "GOLEDMATRIX_CLIENT_CLI_TEST=1")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
		cancel()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("GUI did not start: %v", err)
	}
	const prefix = "graphical client available at "
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("unexpected GUI output: %s", line)
	}
	return strings.TrimSpace(strings.TrimPrefix(line, prefix))
}
