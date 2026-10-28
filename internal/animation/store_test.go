package animation

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

func TestListStoredAnimations(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	names, err := store.List()
	if err != nil || names == nil || len(names) != 0 {
		t.Fatalf("empty catalogue = %v, %v", names, err)
	}
	next, _ := frame.New(1, 1, []byte{1, 2, 3})
	bundle := Bundle{Width: 1, Height: 1, Loops: 1,
		Frames: []TimedFrame{{Frame: next, Duration: time.Second}}}
	var encoded bytes.Buffer
	if err := Encode(&encoded, bundle); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"zebra", "alpha", "zebra"} {
		if _, err := store.Save(name, bytes.NewReader(encoded.Bytes()), nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{".alpha-upload.tmp", ".hidden.glma", "source.gif", "invalid name.glma"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("ignored"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(directory, "directory.glma"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(directory, "alpha.glma"), filepath.Join(directory, "link.glma")); err != nil {
		t.Fatal(err)
	}
	// The catalogue must also include imports from a previous server process.
	reopened, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	names, err = reopened.List()
	if err != nil || !reflect.DeepEqual(names, []string{"alpha", "zebra"}) {
		t.Fatalf("stored catalogue = %v, %v", names, err)
	}
}
