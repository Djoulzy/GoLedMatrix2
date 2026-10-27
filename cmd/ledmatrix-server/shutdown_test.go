package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/client"
	appconfig "github.com/Djoulzy/GoLedMatrix2/internal/config"
	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

type shutdownDisplay struct {
	brightness atomic.Int32
	closed     atomic.Bool
	closes     atomic.Int32
	active     atomic.Int32
	badClose   atomic.Bool
	started    chan struct{}
	startOnce  sync.Once
	closeErr   error
}

func (d *shutdownDisplay) Geometry() (int, int) { return 64, 32 }

func (d *shutdownDisplay) Brightness() int { return int(d.brightness.Load()) }

func (d *shutdownDisplay) SetBrightness(ctx context.Context, value int) error {
	if err := display.ValidateBrightness(value); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	d.brightness.Store(int32(value))
	return nil
}

func (d *shutdownDisplay) Present(ctx context.Context, _ frame.Frame) error {
	d.active.Add(1)
	defer d.active.Add(-1)
	d.startOnce.Do(func() { close(d.started) })
	// Simulate a render still in flight when shutdown is requested.
	<-ctx.Done()
	return ctx.Err()
}

func (d *shutdownDisplay) Close() error {
	if d.active.Load() != 0 {
		d.badClose.Store(true)
	}
	d.closed.Store(true)
	d.closes.Add(1)
	return d.closeErr
}

func startShutdownServer(t *testing.T, target *shutdownDisplay, action *powerOffAction, reason string) (*client.Client, <-chan error, func()) {
	t.Helper()
	// Reserve an ephemeral port instead of depending on the user's :8080.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	settings := appconfig.Default()
	settings.HTTP.InfoDisplaySeconds = 0
	settings.Animation.Directory = t.TempDir()
	cfg := options{backend: "memory", listen: address, config: settings}
	externalDone := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(externalDone) }) }
	done := make(chan error, 1)
	go func() {
		done <- serveWithPowerOff(cfg, target, "memory", externalDone, action, reason)
		close(done)
	}()
	t.Cleanup(func() {
		stop()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("test server did not stop")
		}
	})
	api, err := client.New("http://"+address, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, err := api.Info(ctx); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("server stopped before becoming ready: %v", err)
		case <-ctx.Done():
			t.Fatal("server did not become ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case <-target.started:
	case <-ctx.Done():
		t.Fatal("renderer did not start")
	}
	return api, done, stop
}

func TestGracefulShutdownJoinsRendererBeforePowerOff(t *testing.T) {
	for _, test := range []struct {
		name     string
		powerOff bool
		runErr   error
		closeErr error
	}{
		{name: "server only"},
		{name: "power off", powerOff: true},
		{name: "power off failure", powerOff: true, runErr: errors.New("systemctl failed")},
		{name: "driver close failure", powerOff: true, closeErr: errors.New("driver failed to close")},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := &shutdownDisplay{started: make(chan struct{}), closeErr: test.closeErr}
			var checked, poweredOff atomic.Int32
			action := &powerOffAction{
				check: func(context.Context) error { checked.Add(1); return nil },
				run: func(context.Context) error {
					poweredOff.Add(1)
					if !target.closed.Load() || target.active.Load() != 0 {
						return errors.New("power-off ran before display shutdown")
					}
					return test.runErr
				},
			}
			api, done, _ := startShutdownServer(t, target, action, "")
			info, err := api.Info(context.Background())
			if err != nil || info.Shutdown == nil || !info.Shutdown.PowerOff {
				t.Fatalf("shutdown capabilities = %+v, %v", info.Shutdown, err)
			}
			// The production HTTP response must arrive before the process exits.
			if err := api.Shutdown(context.Background(), test.powerOff); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if want := errors.Join(test.runErr, test.closeErr); fmt.Sprint(err) != fmt.Sprint(want) {
					t.Fatalf("shutdown error = %v, want %v", err, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("shutdown did not finish")
			}
			wantCheck, wantPowerOff := int32(0), int32(0)
			if test.powerOff {
				wantCheck = 1
				if test.closeErr == nil {
					wantPowerOff = 1
				}
			}
			if target.badClose.Load() || target.closes.Load() != 1 || checked.Load() != wantCheck || poweredOff.Load() != wantPowerOff {
				t.Fatalf("shutdown ordering: badClose=%v closes=%d checks=%d powerOff=%d", target.badClose.Load(), target.closes.Load(), checked.Load(), poweredOff.Load())
			}
		})
	}
}

func TestShutdownPermissionFailureLeavesServerRunning(t *testing.T) {
	target := &shutdownDisplay{started: make(chan struct{})}
	action := &powerOffAction{
		check: func(context.Context) error { return errors.New("permission denied") },
		run:   func(context.Context) error { t.Error("poweroff executed after failed permission check"); return nil },
	}
	api, done, _ := startShutdownServer(t, target, action, "")
	if err := api.Shutdown(context.Background(), true); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("poweroff permission error = %v", err)
	}
	if _, err := api.Info(context.Background()); err != nil || target.closed.Load() {
		t.Fatalf("server stopped after rejecting poweroff: %v", err)
	}
	if err := api.Shutdown(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestUnavailablePowerOffAndExternalShutdown(t *testing.T) {
	target := &shutdownDisplay{started: make(chan struct{})}
	api, done, stop := startShutdownServer(t, target, nil, "permission missing")
	info, err := api.Info(context.Background())
	if err != nil || info.Shutdown == nil || info.Shutdown.PowerOff || info.Shutdown.PowerOffUnavailableReason != "permission missing" {
		t.Fatalf("unavailable poweroff info = %+v, %v", info.Shutdown, err)
	}
	if err := api.Shutdown(context.Background(), true); err == nil || target.closed.Load() {
		t.Fatalf("unavailable poweroff stopped server: %v", err)
	}
	stop()
	if err := <-done; err != nil || target.badClose.Load() || target.closes.Load() != 1 {
		t.Fatalf("external shutdown did not join renderer: %v", err)
	}
}

func TestHTTPShutdownDrainsActiveRequestsAndRejectsLateOnes(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	tracker := &requestTracker{next: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusAccepted)
	})}
	httpServer := httptest.NewServer(tracker)
	defer httpServer.Close()
	responseDone := make(chan error, 1)
	go func() {
		response, err := http.Get(httpServer.URL)
		if err == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusAccepted {
				err = fmt.Errorf("active request = %s", response.Status)
			}
		}
		responseDone <- err
	}()
	<-started
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- shutdownHTTP(httpServer.Config, tracker) }()
	deadline := time.Now().Add(time.Second)
	for {
		tracker.mu.Lock()
		stopping := tracker.stopping
		tracker.mu.Unlock()
		if stopping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("HTTP drain did not start")
		}
		time.Sleep(time.Millisecond)
	}
	late := httptest.NewRecorder()
	tracker.ServeHTTP(late, httptest.NewRequest(http.MethodGet, "/", nil))
	if late.Code != http.StatusServiceUnavailable {
		t.Fatalf("late request = %d", late.Code)
	}
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown did not wait for active request: %v", err)
	default:
	}
	unblock()
	if err := <-responseDone; err != nil {
		t.Fatal(err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatal(err)
	}
}

func TestPowerOffCommandIsRestrictedToRPiAndFixedArguments(t *testing.T) {
	for _, test := range []struct {
		goos, backend string
		uid           int
		path          string
		args          []string
	}{
		{"linux", "rpi", 0, systemctlPath, []string{"--no-ask-password", "poweroff"}},
		{"linux", "rpi", 1, sudoPath, []string{"-n", "--", systemctlPath, "--no-ask-password", "poweroff"}},
		{"linux", "memory", 0, "", nil},
		{"linux", "simulation", 0, "", nil},
		{"darwin", "rpi", 0, "", nil},
		{"windows", "rpi", 0, "", nil},
	} {
		path, args, err := powerOffCommand(test.goos, test.backend, test.uid)
		if path != test.path || !reflect.DeepEqual(args, test.args) || (err != nil) != (test.path == "") {
			t.Fatalf("poweroff command (%s, %s, %d) = %s %v, %v", test.goos, test.backend, test.uid, path, args, err)
		}
	}
	for _, backend := range []string{"memory", "simulation"} {
		if action, err := newPowerOffAction(backend); action != nil || err != nil {
			t.Fatalf("non-RPi backend exposed system poweroff: %v, %v", action, err)
		}
	}
	// Keep the deployed permission synchronized with the fixed executable and
	// arguments. There must be no wildcard or general administrator command.
	rule, err := os.ReadFile("../../deploy/goledmatrix2-poweroff.sudoers")
	if err != nil {
		t.Fatal(err)
	}
	var rules []string
	for _, line := range strings.Split(string(rule), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			rules = append(rules, line)
		}
	}
	want := []string{"daemon ALL=(root) NOPASSWD: " + systemctlPath + " --no-ask-password poweroff"}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("sudoers permissions = %v, want %v", rules, want)
	}
}
