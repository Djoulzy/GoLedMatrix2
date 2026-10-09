package main

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/marquee"
)

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
			args := []string{"-test.run=^TestClientCLIHelper$", "--", "-server", server.URL, "-marquee", "Bonjour"}
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
