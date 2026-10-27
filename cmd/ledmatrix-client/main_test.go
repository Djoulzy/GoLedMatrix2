package main

import (
	"context"
	"encoding/json"
	"flag"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/marquee"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
)

func TestBrightnessFlags(t *testing.T) {
	var requests atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var state server.BrightnessState
		if r.Method != http.MethodPut || r.URL.Path != "/v1/brightness" || json.NewDecoder(r.Body).Decode(&state) != nil || state.Brightness != 35 {
			t.Errorf("invalid brightness request: %s %s, %+v", r.Method, r.URL, state)
		}
		_ = json.NewEncoder(w).Encode(state)
	}))
	defer api.Close()
	for _, tc := range []struct {
		args    []string
		want    string
		wantErr bool
	}{
		{[]string{"-brightness", "35"}, "brightness set to 35%", false},
		{[]string{"-brightness", "0"}, "brightness must be between 1 and 100", true},
		{[]string{"-brightness", "101"}, "brightness must be between 1 and 100", true},
		{[]string{"-brightness", "35", "-clock", "round"}, "provide exactly one", true},
		{[]string{"-brightness", "35", "-scan"}, "provide exactly one", true},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		args := append([]string{"-test.run=^TestClientCLIHelper$", "--", "-no-gui", "-server", api.URL}, tc.args...)
		command := exec.CommandContext(ctx, os.Args[0], args...)
		command.Env = append(os.Environ(), "GOLEDMATRIX_CLIENT_CLI_TEST=1")
		output, err := command.CombinedOutput()
		cancel()
		if (err != nil) != tc.wantErr || !strings.Contains(string(output), tc.want) {
			t.Fatalf("brightness CLI = %s, %v; want %q, error = %v", output, err, tc.want, tc.wantErr)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("invalid arguments reached server: %d requests", requests.Load())
	}
}

func TestScanFlags(t *testing.T) {
	var mode atomic.Int32
	var requests atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/v1/info" {
			t.Errorf("unexpected scan request: %s %s", r.Method, r.URL)
		}
		switch mode.Load() {
		case 1:
			http.NotFound(w, r)
		case 2:
			<-r.Context().Done()
		default:
			_, _ = w.Write([]byte(`{"protocol_version":"1","width":64,"height":32,"frame_bytes":6144,"pixel_format":"rgb24","backend":"memory"}`))
		}
	}))
	defer api.Close()
	host, port, err := net.SplitHostPort(api.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		args    []string
		mode    int32
		want    string
		wantErr bool
		probes  int32
	}{
		{name: "list server and ignore server URL", args: []string{"-scan", "-scan-port", port, "-scan-network", host + "/32", "-server", ":invalid"}, want: api.URL, probes: 1},
		{name: "no servers", args: []string{"-scan", "-scan-port", port, "-scan-network", host + "/32"}, mode: 1, want: "no GoLedMatrix2 servers found", probes: 1},
		{name: "total deadline", args: []string{"-scan", "-scan-port", port, "-scan-network", host + "/32", "-scan-timeout", "1s", "-timeout", "50ms"}, mode: 2, want: "scan incomplete: context deadline exceeded", wantErr: true, probes: 1},
		{name: "exclusive action", args: []string{"-scan", "-color", "#ffffff"}, want: "provide exactly one", wantErr: true},
		{name: "modifier requires scan", args: []string{"-scan-port", port, "-server", "http://unused.test:7272", "-color", "#ffffff"}, want: "-scan-port requires -scan", wantErr: true},
		{name: "invalid port", args: []string{"-scan", "-scan-port", "65536"}, want: "-scan-port must be between 1 and 65535", wantErr: true},
		{name: "zero port", args: []string{"-scan", "-scan-port", "0"}, want: "-scan-port must be between 1 and 65535", wantErr: true},
		{name: "invalid prefix", args: []string{"-scan", "-scan-network", "192.168.0.1"}, want: "-scan-network must be an IPv4 CIDR prefix", wantErr: true},
		{name: "IPv6 prefix", args: []string{"-scan", "-scan-network", "2001:db8::/64"}, want: "-scan-network must be an IPv4 CIDR prefix", wantErr: true},
		{name: "zero probe timeout", args: []string{"-scan", "-scan-timeout", "0"}, want: "must be positive when scanning", wantErr: true},
		{name: "zero total timeout", args: []string{"-scan", "-timeout", "0"}, want: "must be positive when scanning", wantErr: true},
		{name: "oversized subnet", args: []string{"-scan", "-scan-network", "10.0.0.0/8"}, want: "select a smaller subnet with -scan-network", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode.Store(tc.mode)
			requests.Store(0)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			args := append([]string{"-test.run=^TestClientCLIHelper$", "--", "-no-gui"}, tc.args...)
			command := exec.CommandContext(ctx, os.Args[0], args...)
			command.Env = append(os.Environ(), "GOLEDMATRIX_CLIENT_CLI_TEST=1")
			output, err := command.CombinedOutput()
			if (err != nil) != tc.wantErr || !strings.Contains(string(output), tc.want) {
				t.Fatalf("client output = %s, error = %v; want %q, error = %v", output, err, tc.want, tc.wantErr)
			}
			if tc.name == "list server and ignore server URL" && (!strings.Contains(string(output), "64x32") || !strings.Contains(string(output), "memory")) {
				t.Fatalf("server details missing: %s", output)
			}
			if requests.Load() != tc.probes {
				t.Fatalf("got %d probes, want %d", requests.Load(), tc.probes)
			}
		})
	}
}

func TestMarqueeMotionFlags(t *testing.T) {
	requests := make(chan marquee.Options, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/marquee" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		var options marquee.Options
		if err := json.NewDecoder(r.Body).Decode(&options); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests <- options
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(options)
	}))
	defer server.Close()
	for _, tc := range []struct {
		name     string
		flags    []string
		vertical bool
		period   float64
		bounce   bool
		message  string
	}{
		{name: "default", message: "scrolling"},
		{name: "vertical default", flags: []string{"-marquee-vertical-bounce"}, vertical: true, period: 4, message: "scrolling with vertical sine wave (4.0 s)"},
		{name: "vertical custom", flags: []string{"-marquee-vertical-bounce", "-marquee-vertical-bounce-seconds", "2.5"}, vertical: true, period: 2.5, message: "scrolling with vertical sine wave (2.5 s)"},
		{name: "horizontal", flags: []string{"-marquee-bounce"}, bounce: true, message: "bouncing"},
		{name: "combined", flags: []string{"-marquee-bounce", "-marquee-vertical-bounce"}, vertical: true, period: 4, bounce: true, message: "bouncing with vertical sine wave (4.0 s)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			args := []string{"-test.run=^TestClientCLIHelper$", "--", "-no-gui", "-server", server.URL, "-marquee", "Bonjour"}
			command := exec.CommandContext(ctx, os.Args[0], append(args, tc.flags...)...)
			command.Env = append(os.Environ(), "GOLEDMATRIX_CLIENT_CLI_TEST=1")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("client command failed: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), tc.message) {
				t.Fatalf("client output = %s, want %q", output, tc.message)
			}
			select {
			case got := <-requests:
				if got.Text != "Bonjour" || got.Bounce != tc.bounce || got.VerticalBounce != tc.vertical || got.VerticalBounceSeconds != tc.period {
					t.Fatalf("client lost motion settings: %+v", got)
				}
			default:
				t.Fatal("client did not send a marquee request")
			}
		})
	}
}

// Run main in an isolated process because it owns the flag set and may exit.
func TestClientCLIHelper(t *testing.T) {
	if os.Getenv("GOLEDMATRIX_CLIENT_CLI_TEST") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
			break
		}
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()
}
