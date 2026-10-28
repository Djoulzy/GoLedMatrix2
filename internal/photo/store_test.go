package photo

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

func TestStorePersistsPhotosAndRejectsInvalidReplacement(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	names, err := store.List()
	if err != nil || names == nil || len(names) != 0 {
		t.Fatalf("empty catalogue = %v, %v", names, err)
	}
	next, _ := frame.New(2, 1, []byte{7, 0, 0, 7, 0, 0})
	for _, name := range []string{"zebra", "alpha"} {
		metadata, err := store.Save(name, next)
		if err != nil || metadata.Name != name || metadata.Width != 2 || metadata.Height != 1 {
			t.Fatalf("saved photo = %+v, %v", metadata, err)
		}
	}
	if _, err := store.Save("alpha", frame.Frame{Width: 2, Height: 1, Pixels: []byte{9}}); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	for _, name := range []string{"../escape", ".hidden", "bad/name", "bad name", ""} {
		if _, err := store.Save(name, next); err == nil {
			t.Fatalf("invalid name %q accepted", name)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, ".alpha-upload.tmp"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(directory, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	names, err = reopened.List()
	if err != nil || !reflect.DeepEqual(names, []string{"alpha", "zebra"}) {
		t.Fatalf("persisted catalogue = %v, %v", names, err)
	}
	loaded, err := reopened.Load("alpha")
	if err != nil || loaded.Width != 2 || loaded.Height != 1 || len(loaded.Pixels) != 6 || loaded.Pixels[0] != 7 {
		t.Fatalf("original photo lost after invalid replacement: %+v, %v", loaded, err)
	}
	next.Pixels[0] = 9
	if _, err := reopened.Save("alpha", next); err != nil {
		t.Fatal(err)
	}
	loaded, err = reopened.Load("alpha")
	if err != nil || loaded.Pixels[0] != 9 {
		t.Fatalf("replacement not persisted: %+v, %v", loaded, err)
	}
}

func TestLoadRejectsIncompatibleAndIncompletePhotos(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := frame.New(2, 1, []byte{7, 0, 0, 7, 0, 0})
	if _, err := store.Save("landscape", next); err != nil {
		t.Fatal(err)
	}
	portrait, err := NewStore(directory, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := portrait.Load("landscape"); err == nil {
		t.Fatal("photo with different geometry but equal byte count was accepted")
	}
	if err := os.Truncate(filepath.Join(directory, "landscape.glmp"), int64(headerSize+1)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("landscape"); err == nil {
		t.Fatal("truncated photo accepted")
	}
}
