package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
)

const (
	DefaultScanPort    = 7272
	DefaultScanTimeout = 500 * time.Millisecond
	scanWorkers        = 64
	maxScanAddresses   = 65536
	maxScanInfoBytes   = 64 * 1024
)

type ScanOptions struct {
	// Networks defaults to the IPv4 subnets of active, non-loopback,
	// non-point-to-point interfaces. Explicit prefixes can narrow the scan.
	Networks     []netip.Prefix
	Port         int
	ProbeTimeout time.Duration
}

type ScanResult struct {
	URL  string
	Info server.Info
}

// Scan finds compatible HTTP servers on a single port. It never changes the
// display. Results are sorted by IP address; cancellation returns partial results
// along with the context error. At most 64 requests run concurrently.
func Scan(ctx context.Context, options ScanOptions) ([]ScanResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Port == 0 {
		options.Port = DefaultScanPort
	}
	if options.ProbeTimeout == 0 {
		options.ProbeTimeout = DefaultScanTimeout
	}
	if options.Port < 1 || options.Port > 65535 {
		return nil, fmt.Errorf("scan port must be between 1 and 65535")
	}
	if options.ProbeTimeout < 0 {
		return nil, fmt.Errorf("scan timeout must be positive")
	}
	networks := options.Networks
	if len(networks) == 0 {
		var err error
		networks, err = localIPv4Networks()
		if err != nil {
			return nil, err
		}
	}
	networks, err := scanNetworks(networks)
	if err != nil {
		return nil, err
	}

	// Discovery must contact each address directly, even when HTTP_PROXY is
	// configured, and must not follow redirects to an unrelated server.
	transport := &http.Transport{
		DialContext:        (&net.Dialer{Timeout: options.ProbeTimeout}).DialContext,
		DisableKeepAlives:  true,
		DisableCompression: true,
	}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   options.ProbeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return scanSubnets(ctx, networks, options.Port, httpClient)
}

func scanSubnets(ctx context.Context, networks []netip.Prefix, port int, httpClient *http.Client) ([]ScanResult, error) {
	jobs := make(chan netip.Addr)
	matches := make(chan ScanResult)
	var workers sync.WaitGroup
	workers.Add(scanWorkers + 1)
	go func() {
		defer workers.Done()
		defer close(jobs)
		for _, network := range networks {
			first := network.Addr()
			for ip := first; ip.IsValid() && network.Contains(ip); ip = ip.Next() {
				// /31 point-to-point and /32 host prefixes have no reserved
				// network/broadcast addresses. Include the client's own IP.
				if network.Bits() < 31 && (ip == first || !network.Contains(ip.Next())) {
					continue
				}
				select {
				case jobs <- ip:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	for i := 0; i < scanWorkers; i++ {
		go func() {
			defer workers.Done()
			for ip := range jobs {
				if ctx.Err() != nil {
					return
				}
				baseURL := "http://" + net.JoinHostPort(ip.String(), strconv.Itoa(port))
				info, ok := probeServer(ctx, httpClient, baseURL)
				if !ok {
					continue
				}
				select {
				case matches <- ScanResult{URL: baseURL, Info: info}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		workers.Wait()
		close(matches)
	}()
	var results []ScanResult
	for result := range matches {
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool {
		// All URLs share the same scheme and port. Compare the parsed hosts
		// numerically so .2 precedes .10.
		left, _ := netip.ParseAddrPort(results[i].URL[len("http://"):])
		right, _ := netip.ParseAddrPort(results[j].URL[len("http://"):])
		return left.Addr().Less(right.Addr())
	})
	return results, ctx.Err()
}

func localIPv4Networks() ([]netip.Prefix, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	var networks []netip.Prefix
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("list addresses for %s: %w", iface.Name, err)
		}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err == nil && prefix.Addr().Is4() && !prefix.Addr().IsLoopback() && !prefix.Addr().IsUnspecified() {
				networks = append(networks, prefix)
			}
		}
	}
	return networks, nil
}

// scanNetworks normalizes and merges overlapping subnets without allocating one
// entry per host. Refuse very large scans instead of silently scanning
// only part of a network or keeping the client busy indefinitely.
func scanNetworks(prefixes []netip.Prefix) ([]netip.Prefix, error) {
	if len(prefixes) == 0 {
		return nil, fmt.Errorf("no active IPv4 network found")
	}
	networks := make([]netip.Prefix, len(prefixes))
	for i, prefix := range prefixes {
		if !prefix.IsValid() || !prefix.Addr().Is4() {
			return nil, fmt.Errorf("scan network must be an IPv4 CIDR prefix")
		}
		networks[i] = prefix.Masked()
	}
	sort.Slice(networks, func(i, j int) bool {
		if networks[i].Bits() != networks[j].Bits() {
			return networks[i].Bits() < networks[j].Bits()
		}
		return networks[i].Addr().Less(networks[j].Addr())
	})
	var merged []netip.Prefix
	var count uint64
	for _, prefix := range networks {
		contained := false
		for _, previous := range merged {
			if previous.Contains(prefix.Addr()) {
				contained = true
				break
			}
		}
		if contained {
			continue
		}
		count += uint64(1) << (32 - prefix.Bits())
		if count > maxScanAddresses {
			return nil, fmt.Errorf("scan exceeds %d IPv4 addresses; select a smaller subnet with -scan-network", maxScanAddresses)
		}
		merged = append(merged, prefix)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Addr().Less(merged[j].Addr()) })
	return merged, nil
}

func probeServer(ctx context.Context, httpClient *http.Client, baseURL string) (server.Info, bool) {
	var info server.Info
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/info", nil)
	if err != nil {
		return info, false
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return info, false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return info, false
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxScanInfoBytes+1))
	if err != nil || len(body) > maxScanInfoBytes || json.Unmarshal(body, &info) != nil {
		return info, false
	}
	size, err := frame.ByteLen(info.Width, info.Height)
	return info, err == nil && info.ProtocolVersion == server.ProtocolVersion &&
		info.PixelFormat == frame.PixelFormat && info.FrameBytes == size && info.Backend != ""
}
