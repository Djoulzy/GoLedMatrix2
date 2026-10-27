package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	systemctlPath = "/usr/bin/systemctl"
	sudoPath      = "/usr/bin/sudo"
	shutdownGrace = 5 * time.Second
)

type powerOffAction struct {
	check func(context.Context) error
	run   func(context.Context) error
}

// Native initialization normally drops root privileges to daemon. Deployment
// grants that account only this exact command, not general administrator access.
func powerOffCommand(goos, backend string, uid int) (string, []string, error) {
	if goos != "linux" || backend != "rpi" {
		return "", nil, fmt.Errorf("system power-off is only supported by the Linux rpi backend")
	}
	args := []string{"--no-ask-password", "poweroff"}
	if uid == 0 {
		return systemctlPath, args, nil
	}
	return sudoPath, append([]string{"-n", "--", systemctlPath}, args...), nil
}

func newPowerOffAction(backend string) (*powerOffAction, error) {
	if backend != "rpi" {
		return nil, nil
	}
	command, args, err := powerOffCommand(runtime.GOOS, backend, os.Geteuid())
	if err != nil {
		return nil, err
	}
	action := &powerOffAction{
		check: func(ctx context.Context) error {
			if _, err := os.Stat("/run/systemd/system"); err != nil {
				return fmt.Errorf("systemd is required for system power-off: %w", err)
			}
			for _, path := range []string{systemctlPath, command} {
				info, err := os.Stat(path)
				if err != nil {
					return fmt.Errorf("power-off command unavailable: %w", err)
				}
				if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
					return fmt.Errorf("power-off command is not executable: %s", path)
				}
			}
			if command == sudoPath {
				// -l checks permission without executing the command.
				checkArgs := append([]string{"-n", "-l"}, args[1:]...)
				if err := runShutdownCommand(ctx, command, checkArgs); err != nil {
					return fmt.Errorf("power-off permission missing; redeploy to install goledmatrix2-poweroff sudoers rule: %w", err)
				}
			}
			return ctx.Err()
		},
		run: func(ctx context.Context) error {
			// No --force: systemd stops other services and unmounts filesystems
			// normally. poweroff queues the operation and returns asynchronously.
			return runShutdownCommand(ctx, command, args)
		},
	}
	if err := action.check(context.Background()); err != nil {
		return nil, err
	}
	return action, nil
}

func runShutdownCommand(parent context.Context, path string, args []string) error {
	ctx, cancel := context.WithTimeout(parent, shutdownGrace)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("power-off command: %w", ctx.Err())
	}
	if err != nil {
		return fmt.Errorf("power-off command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// Stop prevents late requests from starting while Wait joins handlers already
// in flight, including after a forced connection close on shutdown timeout.
type requestTracker struct {
	next     http.Handler
	mu       sync.Mutex
	stopping bool
	active   sync.WaitGroup
}

func (t *requestTracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t.mu.Lock()
	if t.stopping {
		t.mu.Unlock()
		http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
		return
	}
	t.active.Add(1)
	t.mu.Unlock()
	defer t.active.Done()
	t.next.ServeHTTP(w, r)
}

func shutdownHTTP(server *http.Server, requests *requestTracker) error {
	requests.mu.Lock()
	requests.stopping = true
	requests.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	err := server.Shutdown(ctx)
	if err != nil {
		_ = server.Close()
	}
	requests.active.Wait()
	return err
}
