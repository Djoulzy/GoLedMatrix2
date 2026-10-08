package server

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
)

func marqueeAPI(t *testing.T) (*API, *display.Memory) {
	t.Helper()
	target, _ := display.NewMemory(64, 32)
	renderer := render.New(target)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go renderer.Run(ctx)
	store, err := animation.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clockFrame := frame.Frame{Width: 64, Height: 32, Pixels: bytes.Repeat([]byte{7, 0, 0}, 64*32)}
	_ = renderer.SetDefault(clockFrame)
	api, err := New(64, 32, "memory", renderer,
		WithMarquee(marquee.NewPlayer(ctx, renderer)),
		WithAnimations(animation.NewPlayer(ctx, store, renderer, 64, 32), 1<<20),
		WithClockDisplay(func(ClockSelection) (ClockState, error) {
			return ClockState{Mode: "round"}, renderer.ActivateDefault()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return api, target
}

func marqueeRequest(api *API, content string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/v1/marquee", bytes.NewBufferString(content))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.Handler().ServeHTTP(response, request)
	return response
}

func TestMarqueeDefaultsAndInvalidRequests(t *testing.T) {
	api, _ := marqueeAPI(t)
	response := marqueeRequest(api, `{"text":"Bonjour été"}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("marquee = %d %s", response.Code, response.Body)
	}
	var state marquee.Options
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Font != "regular" || state.Size != 16 || state.Color != "#FFFFFF" || state.Speed != 30 || state.CycleSeconds != 6 {
		t.Fatalf("defaults = %+v", state)
	}
	for _, body := range []string{
		`{`, `{}`, `null`, `{"text":"test","size":257}`, `{"text":"test","font":"bad"}`,
		`{"text":"test","color_cycle":["#ff0000"]}`, `{"text":"test","unknown":1}`,
		`{"text":"test"} {}`, `{"text":"test"} trailing`,
	} {
		if response := marqueeRequest(api, body); response.Code != http.StatusBadRequest {
			t.Fatalf("body %s = %d %s", body, response.Code, response.Body)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/marquee", bytes.NewBufferString(`{"text":"test"}`))
	response = httptest.NewRecorder()
	api.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("missing Content-Type = %d", response.Code)
	}
}

func TestMarqueeReplacedByOtherModes(t *testing.T) {
	for _, mode := range []string{"frame", "clock", "animation"} {
		t.Run(mode, func(t *testing.T) {
			api, target := marqueeAPI(t)
			response := marqueeRequest(api, `{"text":"Bonjour","speed":500}`)
			if response.Code != http.StatusAccepted {
				t.Fatal(response.Body)
			}
			var request *http.Request
			want := bytes.Repeat([]byte{7, 0, 0}, 64*32)
			switch mode {
			case "frame":
				want = bytes.Repeat([]byte{10, 20, 30}, 64*32)
				request = httptest.NewRequest(http.MethodPut, "/v1/frame", bytes.NewReader(want))
				request.Header.Set("Content-Type", frame.MediaType)
			case "clock":
				request = httptest.NewRequest(http.MethodPost, "/v1/clock?mode=round", nil)
			case "animation":
				want = bytes.Repeat([]byte{0, 0, 99}, 64*32)
				bundle := animation.Bundle{Width: 64, Height: 32, Loops: 0, Frames: []animation.TimedFrame{
					{Frame: frame.Frame{Width: 64, Height: 32, Pixels: want}, Duration: 20 * time.Millisecond},
				}}
				var encoded bytes.Buffer
				if err := animation.Encode(&encoded, bundle); err != nil {
					t.Fatal(err)
				}
				if _, err := api.animations.Upload("demo", &encoded); err != nil {
					t.Fatal(err)
				}
				request = httptest.NewRequest(http.MethodPost, "/v1/animations/demo/play", nil)
			}
			response = httptest.NewRecorder()
			api.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusAccepted {
				t.Fatalf("replace = %d %s", response.Code, response.Body)
			}
			deadline := time.Now().Add(time.Second)
			for !bytes.Equal(target.Latest().Pixels, want) && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			for i := 0; i < 10; i++ {
				if !bytes.Equal(target.Latest().Pixels, want) {
					t.Fatal("marquee replaced the new display mode")
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
