// Package photo persists matrix-ready RGB24 images without decoding or resizing
// source photographs on the server.
package photo

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

const (
	magic      = "GLMPHOT1"
	headerSize = len(magic) + 16
)

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

type Metadata struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type Store struct {
	directory string
	width     int
	height    int
	frameSize int
}

func NewStore(directory string, width, height int) (*Store, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, fmt.Errorf("photo directory must not be empty")
	}
	size, err := frame.ByteLen(width, height)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("create photo directory: %w", err)
	}
	return &Store{directory: directory, width: width, height: height, frameSize: size}, nil
}

func ValidateName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("photo name must match %s", validName.String())
	}
	return nil
}

func (s *Store) Geometry() (int, int) { return s.width, s.height }

// Save atomically replaces a stored photo only after its geometry and payload
// have been validated and the entire file has been written successfully.
func (s *Store) Save(name string, next frame.Frame) (Metadata, error) {
	if err := ValidateName(name); err != nil {
		return Metadata{}, err
	}
	if next.Width != s.width || next.Height != s.height || len(next.Pixels) != s.frameSize {
		return Metadata{}, fmt.Errorf("photo must contain a %dx%d RGB24 frame", s.width, s.height)
	}
	file, err := os.CreateTemp(s.directory, "."+name+"-*.tmp")
	if err != nil {
		return Metadata{}, fmt.Errorf("create temporary photo: %w", err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	var header [headerSize]byte
	copy(header[:], magic)
	binary.BigEndian.PutUint64(header[len(magic):], uint64(next.Width))
	binary.BigEndian.PutUint64(header[len(magic)+8:], uint64(next.Height))
	if _, err := file.Write(header[:]); err != nil {
		return Metadata{}, fmt.Errorf("write photo header: %w", err)
	}
	if _, err := file.Write(next.Pixels); err != nil {
		return Metadata{}, fmt.Errorf("write photo pixels: %w", err)
	}
	if err := file.Sync(); err != nil {
		return Metadata{}, fmt.Errorf("sync photo: %w", err)
	}
	if err := file.Close(); err != nil {
		return Metadata{}, fmt.Errorf("close photo: %w", err)
	}
	if err := os.Rename(file.Name(), s.path(name)); err != nil {
		return Metadata{}, fmt.Errorf("install photo: %w", err)
	}
	return Metadata{Name: name, Width: s.width, Height: s.height}, nil
}

func (s *Store) Load(name string) (frame.Frame, error) {
	if err := ValidateName(name); err != nil {
		return frame.Frame{}, err
	}
	file, err := os.Open(s.path(name))
	if err != nil {
		return frame.Frame{}, fmt.Errorf("open photo: %w", err)
	}
	defer file.Close()
	var header [headerSize]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return frame.Frame{}, fmt.Errorf("read photo header: %w", err)
	}
	if string(header[:len(magic)]) != magic {
		return frame.Frame{}, fmt.Errorf("invalid photo signature")
	}
	width := binary.BigEndian.Uint64(header[len(magic):])
	height := binary.BigEndian.Uint64(header[len(magic)+8:])
	if width != uint64(s.width) || height != uint64(s.height) {
		return frame.Frame{}, fmt.Errorf("photo geometry %dx%d does not match display %dx%d", width, height, s.width, s.height)
	}
	pixels, err := io.ReadAll(io.LimitReader(file, int64(s.frameSize)+1))
	if err != nil {
		return frame.Frame{}, fmt.Errorf("read photo pixels: %w", err)
	}
	return frame.New(s.width, s.height, pixels)
}

// List reads only filenames, so catalogue refreshes do not load image pixels.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		return nil, fmt.Errorf("list photos: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".glmp") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".glmp")
		if ValidateName(name) == nil {
			names = append(names, name)
		}
	}
	return names, nil
}

func (s *Store) path(name string) string {
	return filepath.Join(s.directory, name+".glmp")
}
