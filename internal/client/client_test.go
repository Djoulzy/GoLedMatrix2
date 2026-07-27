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

type handlerTransport struct {
	handler http.Handler
}

func (t handlerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, request)
	return recorder.Result(), nil
}
