// Package server implements version 1 of the matrix HTTP protocol.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"

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
}

type Info struct {
	ProtocolVersion string       `json:"protocol_version"`
	Width           int          `json:"width"`
	Height          int          `json:"height"`
	PixelFormat     string       `json:"pixel_format"`
	FrameBytes      int          `json:"frame_bytes"`
	Backend         string       `json:"backend"`
	Stats           render.Stats `json:"stats"`
}

func New(width, height int, backend string, renderer *render.Renderer) (*API, error) {
	frameSize, err := frame.ByteLen(width, height)
	if err != nil {
		return nil, err
	}
	if renderer == nil {
		return nil, errors.New("renderer is required")
	}
	return &API{
		width: width, height: height, frameSize: frameSize,
		backend: backend, renderer: renderer,
	}, nil
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /v1/info", a.info)
	mux.HandleFunc("PUT /v1/frame", a.putFrame)
	return mux
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"status":"ok"}`+"\n")
}

func (a *API) info(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Info{
		ProtocolVersion: ProtocolVersion,
		Width:           a.width,
		Height:          a.height,
		PixelFormat:     frame.PixelFormat,
		FrameBytes:      a.frameSize,
		Backend:         a.backend,
		Stats:           a.renderer.Stats(),
	})
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
	sequence := a.renderer.Submit(next)
	writeJSON(w, http.StatusAccepted, struct {
		Sequence uint64 `json:"sequence"`
	}{Sequence: sequence})
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
