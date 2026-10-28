package clientgui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/animation"
	"github.com/Djoulzy/GoLedMatrix2/internal/client"
	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/render"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
)

func TestAnimationCatalogueUsesSelectedServer(t *testing.T) {
	ctx := context.Background()
	makeMatrix := func() *httptest.Server {
		store, err := animation.NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		memory, err := display.NewMemory(2, 1)
		if err != nil {
			t.Fatal(err)
		}
		renderer := render.New(memory)
		player := animation.NewPlayer(ctx, store, renderer, 2, 1)
		api, err := server.New(2, 1, "memory", renderer, server.WithAnimations(player, 1024*1024))
		if err != nil {
			t.Fatal(err)
		}
		matrix := httptest.NewServer(api.Handler())
		t.Cleanup(matrix.Close)
		return matrix
	}
	a, b := makeMatrix(), makeMatrix()
	registry, err := newServerRegistry(ctx, DiscoveryOptions{ServerURL: a.URL, Timeout: time.Second}, client.Scan)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.New(b.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	registry.known[b.URL] = second
	handler := (&GUI{registry: registry}).handler()
	checkCatalogue := func(selected string, expected []string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/animations", nil)
		request.Header.Set(ServerHeader, selected)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var names []string
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &names) != nil || !reflect.DeepEqual(names, expected) {
			t.Fatalf("catalogue on %s = %d %s; want %v", selected, response.Code, response.Body, expected)
		}
	}
	checkCatalogue(a.URL, []string{})
	checkCatalogue(b.URL, []string{})
	next, _ := frame.New(2, 1, []byte{1, 2, 3, 4, 5, 6})
	bundle := animation.Bundle{Width: 2, Height: 1, Loops: 1,
		Frames: []animation.TimedFrame{{Frame: next, Duration: time.Second}}}
	for _, name := range []string{"zebra", "alpha"} {
		if _, err := registry.known[a.URL].UploadAnimation(ctx, name, bundle, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := second.UploadAnimation(ctx, "demo", bundle, false); err != nil {
		t.Fatal(err)
	}
	checkCatalogue(a.URL, []string{"alpha", "zebra"})
	checkCatalogue(b.URL, []string{"demo"})
	request := httptest.NewRequest(http.MethodGet, "/api/animations", nil)
	request.Header.Set(ServerHeader, "http://unknown.test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown server catalogue = %d %s", response.Code, response.Body)
	}
}

func TestAnimationCatalogueUnavailable(t *testing.T) {
	memory, _ := display.NewMemory(2, 1)
	api, err := server.New(2, 1, "memory", render.New(memory))
	if err != nil {
		t.Fatal(err)
	}
	matrix := httptest.NewServer(api.Handler())
	defer matrix.Close()
	proxy, err := NewWithDiscovery(context.Background(), DiscoveryOptions{ServerURL: matrix.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/animations", nil))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("disabled catalogue = %d %s", response.Code, response.Body)
	}
}
