package main

import (
	"net"
	"sort"
	"time"
)

func advertisedBaseURLs(listen string) []string {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return nil
	}
	// A listener explicitly bound to one host must advertise only that host.
	if host != "" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsUnspecified()) {
		return advertisedBaseURLsFrom(listen, nil, nil)
	}
	var addresses []net.IP
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		assigned, _ := iface.Addrs()
		for _, address := range assigned {
			if ip, _, err := net.ParseCIDR(address.String()); err == nil {
				addresses = append(addresses, ip)
			}
		}
	}
	preferred := []net.IP{
		routeSourceIP("udp4", "192.0.2.1:9"),
		routeSourceIP("udp6", "[2001:db8::1]:9"),
	}
	return advertisedBaseURLsFrom(listen, addresses, preferred)
}

// A UDP connect asks the kernel to select a route and its local source address.
// No data is written: this does not contact an Internet service or require DNS.
// The destinations are documentation-only addresses, used to select a route.
func routeSourceIP(network, destination string) net.IP {
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

func advertisedBaseURLsFrom(listen string, addresses, preferred []net.IP) []string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return nil
	}
	if host != "" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsUnspecified()) {
		return []string{"http://" + net.JoinHostPort(host, port)}
	}
	var candidates []net.IP
	seen := make(map[string]bool)
	for _, ip := range addresses {
		if !ip.IsGlobalUnicast() || ip.IsLoopback() || seen[ip.String()] {
			continue
		}
		// An IPv4-only wildcard cannot serve IPv6 addresses.
		if host == "0.0.0.0" && ip.To4() == nil {
			continue
		}
		seen[ip.String()] = true
		candidates = append(candidates, ip)
	}
	rank := func(ip net.IP) int {
		for index, source := range preferred {
			if source != nil && ip.Equal(source) {
				return index
			}
		}
		if ip.To4() != nil {
			return len(preferred)
		}
		return len(preferred) + 1
	}
	sort.Slice(candidates, func(i, j int) bool {
		iRank, jRank := rank(candidates[i]), rank(candidates[j])
		if iRank != jRank {
			return iRank < jRank
		}
		return candidates[i].String() < candidates[j].String()
	})
	result := make([]string, 0, len(candidates))
	for _, ip := range candidates {
		result = append(result, "http://"+net.JoinHostPort(ip.String(), port))
	}
	if len(result) == 0 {
		loopback := "127.0.0.1"
		if host == "::" {
			loopback = "::1"
		}
		result = append(result, "http://"+net.JoinHostPort(loopback, port))
	}
	return result
}
