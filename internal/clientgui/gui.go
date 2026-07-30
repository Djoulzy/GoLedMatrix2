// Package clientgui provides the browser-based control panel for the matrix
// client. The browser only talks to this local HTTP handler; all matrix
// protocol details remain implemented by the Go client.
package clientgui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Djoulzy/GoLedMatrix2/internal/animation"
	"github.com/Djoulzy/GoLedMatrix2/internal/client"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
)

const maxBrowserUpload = 256 << 20

//go:embed static/*
var staticFiles embed.FS

type matrixClient interface {
	Info(context.Context) (server.Info, error)
	Send(context.Context, frame.Frame) (uint64, error)
	DisplayInfo(context.Context) error
	DisplayClock(context.Context, string, string, string) error
	UploadAnimation(context.Context, string, animation.Bundle, bool) (animation.Metadata, error)
	PlayAnimation(context.Context, string) (animation.Metadata, error)
}

type GUI struct {
	client matrixClient
}

func New(api *client.Client) (http.Handler, error) {
	if api == nil {
		return nil, errors.New("matrix client is required")
	}
	return newHandler(api), nil
}

func newHandler(api matrixClient) http.Handler {
	gui := &GUI{client: api}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", serveIndex)
	mux.Handle("GET /static/", http.FileServer(http.FS(staticFiles)))
	mux.HandleFunc("GET /api/info", gui.info)
	mux.HandleFunc("POST /api/color", gui.color)
	mux.HandleFunc("POST /api/image", gui.image)
	mux.HandleFunc("POST /api/clock", gui.clock)
	mux.HandleFunc("POST /api/display-info", gui.displayInfo)
	mux.HandleFunc("POST /api/animations", gui.uploadAnimation)
	mux.HandleFunc("POST /api/animations/play", gui.playAnimation)
	return securityHeaders(mux)
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	content, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "interface unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(content)
}

func (g *GUI) info(w http.ResponseWriter, r *http.Request) {
	info, err := g.client.Info(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "Impossible de joindre le serveur LED", err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (g *GUI) color(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Color string `json:"color"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "Couleur invalide", err)
		return
	}
	info, err := g.client.Info(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "Impossible de joindre le serveur LED", err)
		return
	}
	next, err := solidFrame(info.Width, info.Height, request.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Couleur invalide", err)
		return
	}
	sequence, err := g.client.Send(r.Context(), next)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Envoi de la couleur impossible", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"sequence": sequence})
}

func (g *GUI) image(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBrowserUpload)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeMultipartError(w, "Image absente ou trop volumineuse", err)
		return
	}
	defer file.Close()
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	info, err := g.client.Info(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "Impossible de joindre le serveur LED", err)
		return
	}
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Image PNG ou JPEG invalide", err)
		return
	}
	if config.Width != info.Width || config.Height != info.Height {
		writeError(w, http.StatusBadRequest, "Dimensions incorrectes",
			fmt.Errorf("image %dx%d, dimensions attendues %dx%d", config.Width, config.Height, info.Width, info.Height))
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "Lecture de l’image impossible", err)
		return
	}
	source, _, err := image.Decode(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Image PNG ou JPEG invalide", err)
		return
	}
	next, err := frame.FromImage(source)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Image invalide", err)
		return
	}
	sequence, err := g.client.Send(r.Context(), next)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Envoi de l’image impossible", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"sequence": sequence})
}

func (g *GUI) clock(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Mode   string `json:"mode"`
		Color1 string `json:"color1"`
		Color2 string `json:"color2"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "Paramètres d’horloge invalides", err)
		return
	}
	if err := g.client.DisplayClock(r.Context(), request.Mode, request.Color1, request.Color2); err != nil {
		writeError(w, http.StatusBadGateway, "Activation de l’horloge impossible", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"mode": request.Mode})
}

func (g *GUI) displayInfo(w http.ResponseWriter, r *http.Request) {
	if err := g.client.DisplayInfo(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, "Affichage des informations impossible", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (g *GUI) uploadAnimation(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBrowserUpload)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeMultipartError(w, "GIF absent ou trop volumineux", err)
		return
	}
	defer file.Close()
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = strings.TrimSuffix(header.Filename, extension(header.Filename))
	}
	loops, err := strconv.ParseInt(r.FormValue("loops"), 10, 64)
	if err != nil || loops < -1 || loops > int64(^uint32(0)) {
		writeError(w, http.StatusBadRequest, "Nombre de répétitions invalide",
			errors.New("utiliser -1 pour la valeur du GIF, 0 pour l’infini, ou un entier positif"))
		return
	}
	info, err := g.client.Info(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "Impossible de joindre le serveur LED", err)
		return
	}
	bundle, err := client.PrepareGIF(file, info.Width, info.Height)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Prétraitement du GIF impossible", err)
		return
	}
	if loops >= 0 {
		bundle.Loops = uint32(loops)
	}
	metadata, err := g.client.UploadAnimation(r.Context(), name, bundle, true)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Stockage de l’animation impossible", err)
		return
	}
	writeJSON(w, http.StatusCreated, metadata)
}

func (g *GUI) playAnimation(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "Nom d’animation invalide", err)
		return
	}
	metadata, err := g.client.PlayAnimation(r.Context(), request.Name)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Lecture de l’animation impossible", err)
		return
	}
	writeJSON(w, http.StatusAccepted, metadata)
}

func decodeJSON(r *http.Request, target any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("Content-Type doit être application/json")
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func solidFrame(width, height int, value string) (frame.Frame, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 {
		return frame.Frame{}, errors.New("la couleur doit utiliser le format #RRGGBB")
	}
	packed, err := strconv.ParseUint(value, 16, 24)
	if err != nil {
		return frame.Frame{}, errors.New("la couleur doit utiliser le format #RRGGBB")
	}
	size, err := frame.ByteLen(width, height)
	if err != nil {
		return frame.Frame{}, err
	}
	pixels := make([]byte, size)
	red, green, blue := byte(packed>>16), byte(packed>>8), byte(packed)
	for offset := 0; offset < size; offset += 3 {
		pixels[offset], pixels[offset+1], pixels[offset+2] = red, green, blue
	}
	return frame.New(width, height, pixels)
}

func extension(name string) string {
	index := strings.LastIndexByte(name, '.')
	if index <= 0 {
		return ""
	}
	return name[index:]
}

func writeMultipartError(w http.ResponseWriter, message string, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, message, err)
		return
	}
	writeError(w, http.StatusBadRequest, message, err)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string, err error) {
	writeJSON(w, status, map[string]string{"error": message, "detail": err.Error()})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' blob: data:; connect-src 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
