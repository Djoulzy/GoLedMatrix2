package main

import (
	"net"
	"reflect"
	"testing"
)

func TestAdvertisedBaseURLsPrioritizesKernelSourceAddress(t *testing.T) {
	addresses := []net.IP{
		net.ParseIP("192.168.0.18"), // old deployment address still assigned
		net.ParseIP("10.0.0.1"),     // another interface sorts before the LAN
		net.ParseIP("192.168.0.42"), // current default-route source
		net.ParseIP("2001:db8::42"),
		net.ParseIP("127.0.0.1"),
		net.ParseIP("169.254.1.2"),
		net.ParseIP("192.168.0.42"), // duplicate
	}
	got := advertisedBaseURLsFrom(":8080", addresses, []net.IP{net.ParseIP("192.168.0.42"), net.ParseIP("2001:db8::42")})
	want := []string{
		"http://192.168.0.42:8080",
		"http://[2001:db8::42]:8080",
		"http://10.0.0.1:8080",
		"http://192.168.0.18:8080",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("advertised URLs = %v, want %v", got, want)
	}
	// DHCP/network changes must change the primary address on the next lookup.
	got = advertisedBaseURLsFrom(":8080", addresses, []net.IP{net.ParseIP("192.168.0.18")})
	if got[0] != "http://192.168.0.18:8080" {
		t.Fatalf("primary URL did not follow the new route: %v", got)
	}
}

func TestAdvertisedBaseURLsBindingAndFallbacks(t *testing.T) {
	addresses := []net.IP{net.ParseIP("192.168.0.42"), net.ParseIP("2001:db8::42")}
	for _, tc := range []struct {
		name, listen         string
		addresses, preferred []net.IP
		want                 []string
	}{
		{"explicit IPv4 binding", "192.168.0.18:8080", addresses, []net.IP{addresses[0]}, []string{"http://192.168.0.18:8080"}},
		{"loopback binding", "127.0.0.1:8090", addresses, nil, []string{"http://127.0.0.1:8090"}},
		{"hostname binding", "matrix.local:8080", addresses, nil, []string{"http://matrix.local:8080"}},
		{"IPv4 wildcard", "0.0.0.0:8080", addresses, nil, []string{"http://192.168.0.42:8080"}},
		{"no route", ":8080", addresses, nil, []string{"http://192.168.0.42:8080", "http://[2001:db8::42]:8080"}},
		{"IPv6 route", "[::]:8080", addresses, []net.IP{nil, addresses[1]}, []string{"http://[2001:db8::42]:8080", "http://192.168.0.42:8080"}},
		{"offline", ":8080", nil, nil, []string{"http://127.0.0.1:8080"}},
		{"offline IPv6", "[::]:8080", nil, nil, []string{"http://[::1]:8080"}},
		{"invalid binding", "invalid", addresses, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := advertisedBaseURLsFrom(tc.listen, tc.addresses, tc.preferred)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("advertised URLs = %v, want %v", got, tc.want)
			}
		})
	}
}
