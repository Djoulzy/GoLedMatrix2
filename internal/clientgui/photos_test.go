package clientgui

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/client"
	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/photo"
	"github.com/Djoulzy/GoLedMatrix2/internal/render"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
)

func TestPhotoUploadCatalogueAndDisplayAfterRestart(t *testing.T) {
	ctx := context.Background()
	makeMatrix := func(directory string) (*httptest.Server, *display.Memory, *render.Renderer) {
		store, err := photo.NewStore(directory, 2, 1)
		if err != nil {
			t.Fatal(err)
		}
		memory, err := display.NewMemory(2, 1)
		if err != nil {
			t.Fatal(err)
		}
		renderer := render.New(memory)
		renderCtx, cancel := context.WithCancel(ctx)
		t.Cleanup(cancel)
		go renderer.Run(renderCtx)
		api, err := server.New(2, 1, "memory", renderer, server.WithPhotos(store))
		if err != nil {
			t.Fatal(err)
		}
		matrix := httptest.NewServer(api.Handler())
		t.Cleanup(matrix.Close)
		return matrix, memory, renderer
	}
	directory := t.TempDir()
	a, _, _ := makeMatrix(directory)
	b, _, _ := makeMatrix(t.TempDir())
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
		request := httptest.NewRequest(http.MethodGet, "/api/photos", nil)
		request.Header.Set(ServerHeader, selected)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var names []string
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &names) != nil || !reflect.DeepEqual(names, expected) {
			t.Fatalf("photo catalogue on %s = %d %s; want %v", selected, response.Code, response.Body, expected)
		}
	}
	checkCatalogue(a.URL, []string{})
	checkCatalogue(b.URL, []string{})
	source := image.NewRGBA(image.Rect(0, 0, 2, 1))
	source.SetRGBA(0, 0, color.RGBA{R: 28, G: 40, B: 10, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	body, contentType := multipartBody(t, "original.png", encoded.Bytes(), map[string]string{"name": "album"})
	upload := httptest.NewRequest(http.MethodPost, "/api/image", body)
	upload.Header.Set("Content-Type", contentType)
	upload.Header.Set(ServerHeader, a.URL)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, upload)
	var metadata photo.Metadata
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &metadata) != nil || metadata.Name != "album" {
		t.Fatalf("photo import = %d %s", response.Code, response.Body)
	}
	checkCatalogue(a.URL, []string{"album"})
	checkCatalogue(b.URL, []string{})
	a.Close()
	restarted, memory, renderer := makeMatrix(directory)
	restored, err := client.New(restarted.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	registry.known[restarted.URL] = restored
	checkCatalogue(restarted.URL, []string{"album"})
	for _, name := range []string{"album", "missing"} {
		request := httptest.NewRequest(http.MethodPost, "/api/photos/display", strings.NewReader(`{"name":"`+name+`"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(ServerHeader, restarted.URL)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		expected := http.StatusAccepted
		if name == "missing" {
			expected = http.StatusBadGateway
		}
		if response.Code != expected {
			t.Fatalf("stored photo %q = %d %s", name, response.Code, response.Body)
		}
	}
	deadline := time.Now().Add(time.Second)
	for renderer.Stats().Rendered != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	shown := memory.Latest()
	if renderer.Stats().Accepted != 1 || len(shown.Pixels) != 6 || shown.Pixels[0] != 28 {
		t.Fatalf("stored photo was not restored after restart: %+v, stats %+v", shown, renderer.Stats())
	}
}

func TestDefaultPhotoName(t *testing.T) {
	for filename, expected := range map[string]string{
		"IMG_1234.HEIC":                  "IMG_1234",
		"photo de famille.jpg":           "photo-de-famille",
		"...png":                         "photo",
		strings.Repeat("x", 90) + ".png": strings.Repeat("x", 64),
	} {
		name := defaultPhotoName(filename)
		if name != expected || photo.ValidateName(name) != nil {
			t.Fatalf("name for %q = %q; want %q", filename, name, expected)
		}
	}
}
