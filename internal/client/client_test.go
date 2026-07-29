package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/render"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
)

func TestInfoAndSend(t *testing.T) {
	memory, _ := display.NewMemory(2, 1)
	renderer := render.New(memory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderer.Run(ctx)
	api, _ := server.New(2, 1, "memory", renderer)

	client, err := New("http://matrix.test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = handlerTransport{handler: api.Handler()}
	info, err := client.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 2 || info.Height != 1 {
		t.Fatalf("geometry = %dx%d", info.Width, info.Height)
	}
	next, _ := frame.New(2, 1, []byte{1, 2, 3, 4, 5, 6})
	sequence, err := client.Send(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	if sequence != 1 {
		t.Fatalf("sequence = %d, want 1", sequence)
	}
}

func TestDisplayInfo(t *testing.T) {
	var method, path string
	client, err := New("http://matrix.test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		method, path = request.Method, request.URL.RequestURI()
		w.WriteHeader(http.StatusAccepted)
	})}
	if err := client.DisplayInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/v1/display-info" {
		t.Fatalf("request = %s %s", method, path)
	}
}

func TestDisplayClock(t *testing.T) {
	var method, path string
	client, err := New("http://matrix.test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		method, path = request.Method, request.URL.RequestURI()
		w.WriteHeader(http.StatusAccepted)
	})}
	if err := client.DisplayClock(context.Background(), "round"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/v1/clock?mode=round" {
		t.Fatalf("request = %s %s", method, path)
	}
}

type handlerTransport struct {
	handler http.Handler
}

func (t handlerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, request)
	return recorder.Result(), nil
}
