package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/photo"
	"github.com/Djoulzy/GoLedMatrix2/internal/render"
)

func TestPhotoUploadRejectsInvalidReplacement(t *testing.T) {
	store, err := photo.NewStore(t.TempDir(), 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	memory, _ := display.NewMemory(2, 1)
	renderer := render.New(memory)
	api, err := New(2, 1, "memory", renderer, WithPhotos(store))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, contentType string
		pixels            []byte
		status            int
	}{
		{"/v1/photos/demo?display=false", "application/vnd.goledmatrix.rgb24", []byte{7, 0, 0, 7, 0, 0}, http.StatusCreated},
		{"/v1/photos/demo", "application/vnd.goledmatrix.rgb24", []byte{9}, http.StatusBadRequest},
		{"/v1/photos/demo", "image/png", []byte{9}, http.StatusUnsupportedMediaType},
		{"/v1/photos/bad%20name", "application/vnd.goledmatrix.rgb24", []byte{1, 2, 3, 4, 5, 6}, http.StatusBadRequest},
	} {
		request := httptest.NewRequest(http.MethodPut, tc.path, bytes.NewReader(tc.pixels))
		request.Header.Set("Content-Type", tc.contentType)
		response := httptest.NewRecorder()
		api.Handler().ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("photo upload %s = %d %s; want %d", tc.path, response.Code, response.Body, tc.status)
		}
	}
	stored, err := store.Load("demo")
	if err != nil || stored.Pixels[0] != 7 || renderer.Stats().Accepted != 0 {
		t.Fatalf("failed upload changed stored/displayed photo: %+v, %v, stats %+v", stored, err, renderer.Stats())
	}
}

func TestPhotoStorageUnavailable(t *testing.T) {
	api, _, cancel := testAPI(t)
	defer cancel()
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/v1/photos"},
		{http.MethodPut, "/v1/photos/demo"},
		{http.MethodPost, "/v1/photos/demo/display"},
	} {
		response := httptest.NewRecorder()
		api.Handler().ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("disabled photo API %s = %d", endpoint.path, response.Code)
		}
	}
}
