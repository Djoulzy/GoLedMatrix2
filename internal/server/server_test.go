package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/render"
)

func testAPI(t *testing.T) (*API, *display.Memory, context.CancelFunc) {
	t.Helper()
	memory, err := display.NewMemory(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	renderer := render.New(memory)
	ctx, cancel := context.WithCancel(context.Background())
	go renderer.Run(ctx)
	api, err := New(2, 1, "memory", renderer)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return api, memory, cancel
}

func TestInfo(t *testing.T) {
	api, _, cancel := testAPI(t)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/v1/info", nil)
	response := httptest.NewRecorder()

	api.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var got Info
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Width != 2 || got.Height != 1 || got.FrameBytes != 6 || got.PixelFormat != frame.PixelFormat {
		t.Fatalf("unexpected info: %+v", got)
	}
}

func TestPutFrame(t *testing.T) {
	api, memory, cancel := testAPI(t)
	defer cancel()
	payload := []byte{1, 2, 3, 4, 5, 6}
	request := httptest.NewRequest(http.MethodPut, "/v1/frame", bytes.NewReader(payload))
	request.Header.Set("Content-Type", frame.MediaType)
	response := httptest.NewRecorder()

	api.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	deadline := time.Now().Add(time.Second)
	for len(memory.Latest().Pixels) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	got := memory.Latest()
	if !bytes.Equal(got.Pixels, payload) {
		t.Fatalf("pixels = %v, want %v", got.Pixels, payload)
	}
}

func TestPutFrameRejectsWrongSizeAndType(t *testing.T) {
	api, _, cancel := testAPI(t)
	defer cancel()
	tests := []struct {
		name        string
		contentType string
		payload     []byte
		wantStatus  int
	}{
		{"media type", "application/octet-stream", make([]byte, 6), http.StatusUnsupportedMediaType},
		{"short frame", frame.MediaType, make([]byte, 5), http.StatusBadRequest},
		{"long frame", frame.MediaType, make([]byte, 7), http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/v1/frame", bytes.NewReader(test.payload))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			api.Handler().ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestDisplayInfo(t *testing.T) {
	memory, err := display.NewMemory(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	renderer := render.New(memory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderer.Run(ctx)
	technicalFrame, _ := frame.New(2, 1, []byte{9, 0, 0, 9, 0, 0})
	api, err := New(2, 1, "memory", renderer, WithTechnicalDisplay(
		[]string{"http://matrix.local:8080"},
		20*time.Millisecond,
		func(Info) (frame.Frame, error) { return technicalFrame, nil },
	))
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/display-info", nil)
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}

	deadline := time.Now().Add(time.Second)
	for len(memory.Latest().Pixels) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := memory.Latest().Pixels; len(got) == 0 || got[0] != 9 {
		t.Fatalf("technical frame pixels = %v", got)
	}
}

func TestDisplayClock(t *testing.T) {
	memory, err := display.NewMemory(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	renderer := render.New(memory)
	clockFrame, _ := frame.New(2, 1, []byte{7, 0, 0, 7, 0, 0})
	if err := renderer.SetDefault(clockFrame); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderer.Run(ctx)
	renderer.Submit(frame.Frame{Width: 2, Height: 1, Pixels: []byte{1, 0, 0, 1, 0, 0}})

	api, err := New(2, 1, "memory", renderer, WithClockDisplay(
		func(mode string) (string, error) {
			if err := renderer.ActivateDefault(); err != nil {
				return "", err
			}
			return mode, nil
		},
	))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/clock?mode=round", nil)
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got := memory.Latest()
		if len(got.Pixels) > 0 && got.Pixels[0] == 7 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	got := memory.Latest()
	if len(got.Pixels) == 0 || got.Pixels[0] != 7 {
		t.Fatalf("clock pixels = %v, want first channel 7", got.Pixels)
	}
}
