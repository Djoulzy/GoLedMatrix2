// Package clientgui provides the browser-based control panel for the matrix
// client. The browser only talks to this local HTTP handler; all matrix
// protocol details remain implemented by the Go client.
package clientgui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/animation"
	"github.com/Djoulzy/GoLedMatrix2/internal/client"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/marquee"
	"github.com/Djoulzy/GoLedMatrix2/internal/photo"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
)

const maxBrowserUpload = 256 << 20

//go:embed static/*
var staticFiles embed.FS

type matrixClient interface {
	Info(context.Context) (server.Info, error)
	SetBrightness(context.Context, int) (server.BrightnessState, error)
	Send(context.Context, frame.Frame) (uint64, error)
	UploadPhoto(context.Context, string, frame.Frame, bool) (photo.Metadata, error)
	ListPhotos(context.Context) ([]string, error)
	DisplayPhoto(context.Context, string) (photo.Metadata, error)
	DisplayInfo(context.Context) error
	Shutdown(context.Context, bool) error
	DisplayClock(context.Context, string, string, string) error
	DisplayMarquee(context.Context, marquee.Options) (marquee.Options, error)
	UploadAnimation(context.Context, string, animation.Bundle, bool) (animation.Metadata, error)
	ListAnimations(context.Context) ([]string, error)
	PlayAnimation(context.Context, string) (animation.Metadata, error)
}

type GUI struct {
	client   matrixClient
	registry *serverRegistry
}

func New(api *client.Client) (http.Handler, error) {
	if api == nil {
		return nil, errors.New("matrix client is required")
	}
	registry, err := newServerRegistry(context.Background(), DiscoveryOptions{ServerURL: api.ServerURL(), Timeout: 2 * time.Minute}, client.Scan)
	if err != nil {
		return nil, err
	}
	registry.known[registry.pinned] = api
	return (&GUI{registry: registry}).handler(), nil
}

func newHandler(api matrixClient) http.Handler {
	gui := &GUI{client: api}
	return gui.handler()
}

func (gui *GUI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", serveIndex)
	mux.Handle("GET /static/", http.FileServer(http.FS(staticFiles)))
	if gui.registry != nil {
		mux.HandleFunc("GET /api/servers", gui.servers)
		mux.HandleFunc("POST /api/servers/scan", gui.scanServers)
	}
	mux.HandleFunc("GET /api/info", gui.matrixHandler((*GUI).info))
	mux.HandleFunc("POST /api/brightness", gui.matrixHandler((*GUI).brightness))
	mux.HandleFunc("POST /api/color", gui.matrixHandler((*GUI).color))
	mux.HandleFunc("POST /api/image", gui.matrixHandler((*GUI).image))
	mux.HandleFunc("GET /api/photos", gui.matrixHandler((*GUI).listPhotos))
	mux.HandleFunc("POST /api/photos/display", gui.matrixHandler((*GUI).displayPhoto))
	mux.HandleFunc("POST /api/clock", gui.matrixHandler((*GUI).clock))
	mux.HandleFunc("POST /api/marquee", gui.matrixHandler((*GUI).marquee))
	mux.HandleFunc("GET /api/marquee/fonts", marqueeFonts)
	mux.HandleFunc("POST /api/display-info", gui.matrixHandler((*GUI).displayInfo))
	mux.HandleFunc("POST /api/shutdown", gui.matrixHandler((*GUI).shutdown))
	mux.HandleFunc("GET /api/animations", gui.matrixHandler((*GUI).listAnimations))
	mux.HandleFunc("POST /api/animations", gui.matrixHandler((*GUI).uploadAnimation))
	mux.HandleFunc("POST /api/animations/play", gui.matrixHandler((*GUI).playAnimation))
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

func (g *GUI) brightness(w http.ResponseWriter, r *http.Request) {
	var request server.BrightnessState
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "Luminosité invalide", err)
		return
	}
	if request.Brightness < 1 || request.Brightness > 100 {
		writeError(w, http.StatusBadRequest, "Luminosité invalide", errors.New("utiliser un entier de 1 à 100"))
		return
	}
	state, err := g.client.SetBrightness(r.Context(), request.Brightness)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Réglage de la luminosité impossible", err)
		return
	}
	writeJSON(w, http.StatusOK, state)
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
	file, header, err := r.FormFile("file")
	if err != nil {
		writeMultipartError(w, "Image absente ou trop volumineuse", err)
		return
	}
	defer file.Close()
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = defaultPhotoName(header.Filename)
	}
	if err := photo.ValidateName(name); err != nil {
		writeError(w, http.StatusBadRequest, "Nom de photo invalide", err)
		return
	}
	info, err := g.client.Info(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "Impossible de joindre le serveur LED", err)
		return
	}
	next, err := client.PrepareImage(r.Context(), file, info.Width, info.Height)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, client.ErrHEICUnavailable) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, "Préparation de l’image PNG, JPEG ou HEIC impossible", err)
		return
	}
	metadata, err := g.client.UploadPhoto(r.Context(), name, next, true)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Stockage de la photo impossible", err)
		return
	}
	writeJSON(w, http.StatusCreated, metadata)
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

func (g *GUI) shutdown(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var request *server.ShutdownRequest
	if err := decodeJSON(r, &request); err != nil || request == nil {
		if err == nil {
			err = errors.New("un objet JSON est requis")
		}
		writeError(w, http.StatusBadRequest, "Demande d’arrêt invalide", err)
		return
	}
	if err := g.client.Shutdown(r.Context(), request.PowerOff); err != nil {
		writeError(w, http.StatusBadGateway, "Arrêt du serveur impossible", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "shutting_down", "power_off": request.PowerOff})
}

func (g *GUI) marquee(w http.ResponseWriter, r *http.Request) {
	var options marquee.Options
	if err := decodeJSON(r, &options); err != nil {
		writeError(w, http.StatusBadRequest, "Paramètres de texte défilant invalides", err)
		return
	}
	// Validate before forwarding so form errors are reported as 400, not 502.
	text, err := marquee.New(1, 1, options)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Paramètres de texte défilant invalides", err)
		return
	}
	state, err := g.client.DisplayMarquee(r.Context(), text.Options())
	if err != nil {
		writeError(w, http.StatusBadGateway, "Activation du texte défilant impossible", err)
		return
	}
	writeJSON(w, http.StatusAccepted, state)
}

func marqueeFonts(w http.ResponseWriter, _ *http.Request) {
	fonts, err := marquee.Fonts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Catalogue de polices indisponible", err)
		return
	}
	writeJSON(w, http.StatusOK, fonts)
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

func (g *GUI) listAnimations(w http.ResponseWriter, r *http.Request) {
	names, err := g.client.ListAnimations(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "Liste des animations indisponible", err)
		return
	}
	writeJSON(w, http.StatusOK, names)
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
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("Content-Type doit être application/json")
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("le corps doit contenir un seul objet JSON")
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
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' blob: data:; connect-src 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
