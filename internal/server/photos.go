package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/Djoulzy/GoLedMatrix2/internal/photo"
)

func WithPhotos(store *photo.Store) Option {
	return func(api *API) error {
		if store == nil {
			return errors.New("photo store is required")
		}
		width, height := store.Geometry()
		if width != api.width || height != api.height {
			return fmt.Errorf("photo store geometry %dx%d does not match display %dx%d", width, height, api.width, api.height)
		}
		api.photos = store
		return nil
	}
}

func (a *API) listPhotos(w http.ResponseWriter, _ *http.Request) {
	if a.photos == nil {
		writeProblem(w, http.StatusServiceUnavailable, "photos unavailable", "photo storage is disabled")
		return
	}
	names, err := a.photos.List()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "unable to list photos", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, names)
}

func (a *API) putPhoto(w http.ResponseWriter, r *http.Request) {
	if a.photos == nil {
		writeProblem(w, http.StatusServiceUnavailable, "photos unavailable", "photo storage is disabled")
		return
	}
	name := r.PathValue("name")
	if err := photo.ValidateName(name); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid photo name", err.Error())
		return
	}
	next, ok := a.readFrame(w, r)
	if !ok {
		return
	}
	metadata, err := a.photos.Save(name, next)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "unable to store photo", err.Error())
		return
	}
	if r.URL.Query().Get("display") != "false" {
		a.submitFrame(next)
	}
	writeJSON(w, http.StatusCreated, metadata)
}

func (a *API) displayPhoto(w http.ResponseWriter, r *http.Request) {
	if a.photos == nil {
		writeProblem(w, http.StatusServiceUnavailable, "photos unavailable", "photo storage is disabled")
		return
	}
	name := r.PathValue("name")
	if err := photo.ValidateName(name); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid photo name", err.Error())
		return
	}
	next, err := a.photos.Load(name)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		writeProblem(w, status, "photo unavailable", err.Error())
		return
	}
	a.submitFrame(next)
	writeJSON(w, http.StatusAccepted, photo.Metadata{Name: name, Width: next.Width, Height: next.Height})
}
