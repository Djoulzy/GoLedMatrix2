package client

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/animation"
	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/marquee"
	"github.com/Djoulzy/GoLedMatrix2/internal/render"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
	"github.com/Djoulzy/GoLedMatrix2/internal/thermal"
)

func TestInfoAndSend(t *testing.T) {
	memory, _ := display.NewMemory(2, 1)
	renderer := render.New(memory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderer.Run(ctx)
	sampledAt := time.Now().UTC()
	api, _ := server.New(2, 1, "memory", renderer, server.WithTemperatures(func() thermal.Stats {
		return thermal.Stats{
			Temperatures: []thermal.Temperature{{Sensor: "thermal_zone3", Name: "cpu-thermal", Celsius: 52.5}},
			SampledAt:    &sampledAt,
		}
	}))

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
	if len(info.System.Temperatures) != 1 || info.System.Temperatures[0].Celsius != 52.5 || info.System.SampledAt == nil || !info.System.SampledAt.Equal(sampledAt) {
		t.Fatalf("client lost temperatures: %+v", info.System)
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

func TestInfoFromOlderServerWithoutTemperatures(t *testing.T) {
	api, err := New("http://matrix.test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	api.http.Transport = handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"protocol_version":"1","width":2,"height":1,"pixel_format":"rgb24"}`))
	})}
	info, err := api.Info(context.Background())
	if err != nil || info.Width != 2 || len(info.System.Temperatures) != 0 {
		t.Fatalf("older server info = %+v, %v", info, err)
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
	if err := client.DisplayClock(context.Background(), "round", "#112233", "#AABBCC"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/v1/clock?color1=%23112233&color2=%23AABBCC&mode=round" {
		t.Fatalf("request = %s %s", method, path)
	}
}

func TestDisplayMarquee(t *testing.T) {
	api, err := New("http://matrix.test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	options := marquee.Options{Text: "Été & hiver ?", Font: "mono", Size: 18, Color: "#123456", Speed: 40, Bounce: true,
		VerticalBounce: true, VerticalBounceSeconds: 2.5,
		ColorCycle: []string{"#FF0000", "#00FF00"}, CycleSeconds: 3}
	api.http.Transport = handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/marquee" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		var got marquee.Options
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.Text != options.Text || got.Font != options.Font || got.Size != options.Size || got.Color != options.Color || got.Speed != options.Speed || !got.Bounce || len(got.ColorCycle) != 2 || got.CycleSeconds != 3 {
			t.Fatalf("sent settings = %+v", got)
		}
		if !got.VerticalBounce || got.VerticalBounceSeconds != options.VerticalBounceSeconds {
			t.Fatalf("lost vertical bounce settings: %+v", got)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(got)
	})}
	state, err := api.DisplayMarquee(context.Background(), options)
	if err != nil || state.Text != options.Text || state.Font != options.Font || !state.Bounce || !state.VerticalBounce || state.VerticalBounceSeconds != options.VerticalBounceSeconds {
		t.Fatalf("marquee state = %+v, %v", state, err)
	}
	api.http.Transport = handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid text", http.StatusBadRequest)
	})}
	if _, err := api.DisplayMarquee(context.Background(), options); err == nil {
		t.Fatal("server error ignored")
	}
}

func TestUploadAndPlayAnimation(t *testing.T) {
	client, err := New("http://matrix.test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := frame.New(1, 1, []byte{1, 2, 3})
	bundle := animation.Bundle{
		Width: 1, Height: 1, Loops: 1,
		Frames: []animation.TimedFrame{{Frame: next, Duration: 20 * time.Millisecond}},
	}
	var uploaded bool
	client.http.Transport = handlerTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPut:
			if request.URL.Path != "/v1/animations/demo" ||
				request.Header.Get("Content-Type") != animation.MediaType {
				t.Fatalf("upload request = %s %s", request.Method, request.URL.Path)
			}
			var body bytes.Buffer
			_, _ = body.ReadFrom(request.Body)
			if _, err := animation.Decode(&body); err != nil {
				t.Fatal(err)
			}
			uploaded = true
			w.WriteHeader(http.StatusCreated)
		case request.Method == http.MethodPost:
			if request.URL.Path != "/v1/animations/demo/play" {
				t.Fatalf("play path = %s", request.URL.Path)
			}
			w.WriteHeader(http.StatusAccepted)
		}
		_, _ = w.Write([]byte(`{"name":"demo","width":1,"height":1,"frame_count":1}`))
	})}
	if _, err := client.UploadAnimation(context.Background(), "demo", bundle, true); err != nil {
		t.Fatal(err)
	}
	if !uploaded {
		t.Fatal("animation was not uploaded")
	}
	if _, err := client.PlayAnimation(context.Background(), "demo"); err != nil {
		t.Fatal(err)
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
