package clientgui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/client"
)

// ServerHeader binds an entire browser request to one known server, even while
// another tab selects a different matrix or a scan updates the server list.
const ServerHeader = "X-GoLedMatrix-Server"

type DiscoveryOptions struct {
	ServerURL   string
	Timeout     time.Duration
	ScanOptions client.ScanOptions
}

type discoveredServer struct {
	URL     string `json:"url"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	Backend string `json:"backend,omitempty"`
}

type discoveryState struct {
	Servers       []discoveredServer `json:"servers"`
	InitialServer string             `json:"initial_server"`
	Scanning      bool               `json:"scanning"`
	Error         string             `json:"error,omitempty"`
}

var errNoServer = errors.New("aucun serveur sélectionné ; lancez une recherche ou choisissez un serveur")

type serverRegistry struct {
	ctx      context.Context
	options  DiscoveryOptions
	scan     func(context.Context, client.ScanOptions) ([]client.ScanResult, error)
	mu       sync.RWMutex
	known    map[string]*client.Client
	pinned   string
	servers  []discoveredServer
	scanning bool
	scanErr  string
}

// NewWithDiscovery starts discovery in the background when ServerURL is empty.
// The interface remains available while scanning, including with no results.
func NewWithDiscovery(ctx context.Context, options DiscoveryOptions) (http.Handler, error) {
	registry, err := newServerRegistry(ctx, options, client.Scan)
	if err != nil {
		return nil, err
	}
	gui := &GUI{registry: registry}
	if registry.pinned == "" {
		registry.startScan()
	}
	return gui.handler(), nil
}

func newServerRegistry(ctx context.Context, options DiscoveryOptions, scan func(context.Context, client.ScanOptions) ([]client.ScanResult, error)) (*serverRegistry, error) {
	if options.Timeout <= 0 {
		return nil, errors.New("timeout must be positive")
	}
	registry := &serverRegistry{ctx: ctx, options: options, scan: scan, known: make(map[string]*client.Client)}
	if rawURL := strings.TrimSpace(options.ServerURL); rawURL != "" {
		api, err := client.New(rawURL, options.Timeout)
		if err != nil {
			return nil, err
		}
		registry.pinned = api.ServerURL()
		registry.known[registry.pinned] = api
		registry.servers = []discoveredServer{{URL: registry.pinned}}
	}
	return registry, nil
}

func (r *serverRegistry) snapshot() discoveryState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := discoveryState{
		Servers: append([]discoveredServer{}, r.servers...), InitialServer: r.pinned,
		Scanning: r.scanning, Error: r.scanErr,
	}
	if state.InitialServer == "" && len(state.Servers) > 0 {
		state.InitialServer = state.Servers[0].URL
	}
	return state
}

func (r *serverRegistry) clientFor(rawURL string) (*client.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if rawURL == "" {
		rawURL = r.pinned
		if rawURL == "" && len(r.servers) > 0 {
			rawURL = r.servers[0].URL
		}
	}
	if rawURL == "" {
		return nil, errNoServer
	}
	api, found := r.known[rawURL]
	if !found {
		return nil, fmt.Errorf("serveur inconnu : %s ; relancez la recherche", rawURL)
	}
	return api, nil
}

func (r *serverRegistry) startScan() {
	r.mu.Lock()
	if r.scanning || r.ctx.Err() != nil {
		r.mu.Unlock()
		return
	}
	r.scanning, r.scanErr = true, ""
	r.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(r.ctx, r.options.Timeout)
		defer cancel()
		results, scanErr := r.scan(ctx, r.options.ScanOptions)
		r.mu.Lock()
		defer r.mu.Unlock()
		var servers []discoveredServer
		seen := make(map[string]bool)
		for _, result := range results {
			api, err := client.New(result.URL, r.options.Timeout)
			if err != nil {
				if scanErr == nil {
					scanErr = err
				}
				continue
			}
			url := api.ServerURL()
			if seen[url] {
				continue
			}
			seen[url] = true
			if _, exists := r.known[url]; !exists {
				r.known[url] = api
			}
			servers = append(servers, discoveredServer{URL: url, Width: result.Info.Width, Height: result.Info.Height, Backend: result.Info.Backend})
		}
		if r.pinned != "" && !seen[r.pinned] {
			servers = append([]discoveredServer{{URL: r.pinned}}, servers...)
		}
		// Keep the last inventory if the scan failed before finding anything.
		// Known clients also remain valid for requests already using an address.
		if scanErr == nil || len(results) > 0 {
			r.servers = servers
		}
		r.scanning = false
		if scanErr != nil {
			r.scanErr = scanErr.Error()
		}
	}()
}

func (g *GUI) servers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, g.registry.snapshot())
}

func (g *GUI) scanServers(w http.ResponseWriter, _ *http.Request) {
	g.registry.startScan()
	writeJSON(w, http.StatusAccepted, g.registry.snapshot())
}

func (g *GUI) matrixHandler(next func(*GUI, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		api := g.client
		if g.registry != nil {
			var err error
			api, err = g.registry.clientFor(r.Header.Get(ServerHeader))
			if err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, errNoServer) {
					status = http.StatusServiceUnavailable
				}
				writeError(w, status, "Serveur LED indisponible", err)
				return
			}
		}
		// Capture the client once: image preparation may take time between Info
		// and Send, and those calls must always target the same matrix.
		next(&GUI{client: api}, w, r)
	}
}
