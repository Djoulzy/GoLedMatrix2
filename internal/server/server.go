// Package server implements version 1 of the matrix HTTP protocol.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/animation"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/render"
)

const ProtocolVersion = "1"

type API struct {
	width     int
	height    int
	frameSize int
	backend   string
	renderer  *render.Renderer
	startedAt time.Time
	baseURLs  []string

	technicalFrame    func(Info) (frame.Frame, error)
	technicalDuration time.Duration
	clockDisplay      func(ClockSelection) (ClockState, error)
	animations        *animation.Player
	maxAnimationBytes int64
}

type Info struct {
	ProtocolVersion string       `json:"protocol_version"`
	Width           int          `json:"width"`
	Height          int          `json:"height"`
	PixelFormat     string       `json:"pixel_format"`
	FrameBytes      int          `json:"frame_bytes"`
	Backend         string       `json:"backend"`
	BaseURLs        []string     `json:"base_urls"`
	StartedAt       time.Time    `json:"started_at"`
	UptimeSeconds   int64        `json:"uptime_seconds"`
	Stats           render.Stats `json:"stats"`
}

type Option func(*API) error

type ClockSelection struct {
	Mode   string
	Color1 string
	Color2 string
}

type ClockState struct {
	Mode   string `json:"mode"`
	Color1 string `json:"color1"`
	Color2 string `json:"color2"`
}

func WithTechnicalDisplay(
	baseURLs []string,
	duration time.Duration,
	builder func(Info) (frame.Frame, error),
) Option {
	return func(api *API) error {
		if duration <= 0 {
			return errors.New("technical information duration must be positive")
		}
		if builder == nil {
			return errors.New("technical information frame builder is required")
		}
		api.baseURLs = append([]string(nil), baseURLs...)
		api.technicalDuration = duration
		api.technicalFrame = builder
		return nil
	}
}

func WithClockDisplay(display func(ClockSelection) (ClockState, error)) Option {
	return func(api *API) error {
		if display == nil {
			return errors.New("clock display controller is required")
		}
		api.clockDisplay = display
		return nil
	}
}

func WithAnimations(player *animation.Player, maxUploadBytes int64) Option {
	return func(api *API) error {
		if player == nil {
			return errors.New("animation player is required")
		}
		if maxUploadBytes <= 0 {
			return errors.New("animation upload limit must be positive")
		}
		api.animations = player
		api.maxAnimationBytes = maxUploadBytes
		return nil
	}
}

func New(width, height int, backend string, renderer *render.Renderer, options ...Option) (*API, error) {
	frameSize, err := frame.ByteLen(width, height)
	if err != nil {
		return nil, err
	}
	if renderer == nil {
		return nil, errors.New("renderer is required")
	}
	api := &API{
		width: width, height: height, frameSize: frameSize,
		backend: backend, renderer: renderer, startedAt: time.Now(),
	}
	for _, option := range options {
		if err := option(api); err != nil {
			return nil, err
		}
	}
	return api, nil
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /v1/info", a.info)
	mux.HandleFunc("PUT /v1/frame", a.putFrame)
	mux.HandleFunc("POST /v1/display-info", a.displayInfo)
	mux.HandleFunc("POST /v1/clock", a.displayClock)
	mux.HandleFunc("PUT /v1/animations/{name}", a.putAnimation)
	mux.HandleFunc("POST /v1/animations/{name}/play", a.playAnimation)
	return mux
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"status":"ok"}`+"\n")
}

func (a *API) info(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.currentInfo())
}

func (a *API) currentInfo() Info {
	return Info{
		ProtocolVersion: ProtocolVersion,
		Width:           a.width,
		Height:          a.height,
		PixelFormat:     frame.PixelFormat,
		FrameBytes:      a.frameSize,
		Backend:         a.backend,
		BaseURLs:        append([]string(nil), a.baseURLs...),
		StartedAt:       a.startedAt,
		UptimeSeconds:   int64(time.Since(a.startedAt).Seconds()),
		Stats:           a.renderer.Stats(),
	}
}

// ShowTechnicalInfo queues the temporary information screen and restores the
// newest client frame after the configured duration.
func (a *API) ShowTechnicalInfo() error {
	if a.technicalFrame == nil {
		return errors.New("technical information display is disabled")
	}
	next, err := a.technicalFrame(a.currentInfo())
	if err != nil {
		return fmt.Errorf("render technical information: %w", err)
	}
	return a.renderer.ShowTemporary(next, a.technicalDuration)
}

func (a *API) displayInfo(w http.ResponseWriter, _ *http.Request) {
	if err := a.ShowTechnicalInfo(); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "technical information unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, struct {
		DurationSeconds int64 `json:"duration_seconds"`
	}{DurationSeconds: int64(a.technicalDuration.Seconds())})
}

func (a *API) displayClock(w http.ResponseWriter, r *http.Request) {
	if a.clockDisplay == nil {
		writeProblem(w, http.StatusServiceUnavailable, "clock unavailable", "clock display is disabled")
		return
	}
	state, err := a.clockDisplay(ClockSelection{
		Mode:   r.URL.Query().Get("mode"),
		Color1: r.URL.Query().Get("color1"),
		Color2: r.URL.Query().Get("color2"),
	})
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid clock parameters", err.Error())
		return
	}
	if state.Mode == "" {
		writeProblem(w, http.StatusServiceUnavailable, "clock unavailable", "clock controller returned no active mode")
		return
	}
	writeJSON(w, http.StatusAccepted, state)
}

func (a *API) putFrame(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != frame.MediaType {
		writeProblem(w, http.StatusUnsupportedMediaType, "unsupported media type",
			fmt.Sprintf("Content-Type must be %s", frame.MediaType))
		return
	}
	if r.ContentLength >= 0 && r.ContentLength != int64(a.frameSize) {
		writeProblem(w, http.StatusBadRequest, "invalid frame size",
			fmt.Sprintf("body has %d bytes; expected %d", r.ContentLength, a.frameSize))
		return
	}

	payload, err := io.ReadAll(io.LimitReader(r.Body, int64(a.frameSize)+1))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "unable to read frame", err.Error())
		return
	}
	next, err := frame.New(a.width, a.height, payload)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid frame", err.Error())
		return
	}
	if a.animations != nil {
		a.animations.Stop()
	}
	sequence := a.renderer.Submit(next)
	writeJSON(w, http.StatusAccepted, struct {
		Sequence uint64 `json:"sequence"`
	}{Sequence: sequence})
}

func (a *API) putAnimation(w http.ResponseWriter, r *http.Request) {
	if a.animations == nil {
		writeProblem(w, http.StatusServiceUnavailable, "animations unavailable", "animation storage is disabled")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != animation.MediaType {
		writeProblem(w, http.StatusUnsupportedMediaType, "unsupported media type",
			fmt.Sprintf("Content-Type must be %s", animation.MediaType))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.maxAnimationBytes)
	metadata, err := a.animations.Upload(r.PathValue("name"), r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeProblem(w, http.StatusRequestEntityTooLarge, "animation too large", err.Error())
			return
		}
		writeProblem(w, http.StatusBadRequest, "invalid animation", err.Error())
		return
	}
	if r.URL.Query().Get("play") != "false" {
		metadata, err = a.animations.Play(metadata.Name)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "unable to play animation", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusCreated, metadata)
}

func (a *API) playAnimation(w http.ResponseWriter, r *http.Request) {
	if a.animations == nil {
		writeProblem(w, http.StatusServiceUnavailable, "animations unavailable", "animation storage is disabled")
		return
	}
	metadata, err := a.animations.Play(r.PathValue("name"))
	if err != nil {
		writeProblem(w, http.StatusNotFound, "animation unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, metadata)
}

func writeProblem(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	writeJSON(w, status, struct {
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}{Title: title, Status: status, Detail: detail})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
