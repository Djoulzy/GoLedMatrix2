package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const defaultGUIListenAddress = "detect:8080"

// Resolve only the automatic host. Explicit loopback, wildcard, hostname and
// IPv6 bindings keep their existing meaning and do not inspect the network.
func guiListenAddress(address string, sourceIP func(string, string) net.IP) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("parse GUI listen address: %w", err)
	}
	if !strings.EqualFold(host, "detect") {
		return address, nil
	}
	for _, route := range []struct{ network, destination string }{
		{"udp4", "192.0.2.1:9"},
		{"udp6", "[2001:db8::1]:9"},
	} {
		ip := sourceIP(route.network, route.destination)
		if ip.IsGlobalUnicast() && !ip.IsLoopback() {
			return net.JoinHostPort(ip.String(), port), nil
		}
	}
	// Keep the control panel usable locally while offline, without exposing it
	// on arbitrary interfaces when the routing table offers no usable source IP.
	return net.JoinHostPort("127.0.0.1", port), nil
}

func guiRouteSourceIP(network, destination string) net.IP {
	// A UDP connect asks the kernel for the route's local source address. No
	// packet is sent, and these documentation-only destinations need no DNS.
	connection, err := net.DialTimeout(network, destination, 100*time.Millisecond)
	if err != nil {
		return nil
	}
	defer connection.Close()
	if local, ok := connection.LocalAddr().(*net.UDPAddr); ok {
		return local.IP
	}
	return nil
}

func serveGUI(ctx context.Context, address string, handler http.Handler, output io.Writer) error {
	address, err := guiListenAddress(address, guiRouteSourceIP)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	fmt.Fprintf(output, "graphical client available at http://%s\n", listener.Addr())
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}
