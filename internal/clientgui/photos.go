package clientgui

import (
	"net/http"
	"strings"

	"github.com/Djoulzy/GoLedMatrix2/internal/photo"
)

func (g *GUI) listPhotos(w http.ResponseWriter, r *http.Request) {
	names, err := g.client.ListPhotos(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "Liste des photos indisponible", err)
		return
	}
	writeJSON(w, http.StatusOK, names)
}

func (g *GUI) displayPhoto(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "Nom de photo invalide", err)
		return
	}
	if err := photo.ValidateName(request.Name); err != nil {
		writeError(w, http.StatusBadRequest, "Nom de photo invalide", err)
		return
	}
	metadata, err := g.client.DisplayPhoto(r.Context(), request.Name)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Affichage de la photo impossible", err)
		return
	}
	writeJSON(w, http.StatusAccepted, metadata)
}

func defaultPhotoName(filename string) string {
	base := strings.TrimSuffix(filename, extension(filename))
	name := strings.Map(func(char rune) rune {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' {
			return char
		}
		return '-'
	}, base)
	name = strings.Trim(name, "-_")
	if len(name) > 64 {
		name = name[:64]
	}
	if name == "" {
		return "photo"
	}
	return name
}
