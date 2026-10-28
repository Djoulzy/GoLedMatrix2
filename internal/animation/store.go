package animation

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

type Store struct {
	directory string
}

func NewStore(directory string) (*Store, error) {
	if directory == "" {
		return nil, fmt.Errorf("animation directory must not be empty")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("create animation directory: %w", err)
	}
	return &Store{directory: directory}, nil
}

func ValidateName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("animation name must match %s", validName.String())
	}
	return nil
}

func (s *Store) Save(
	name string,
	reader io.Reader,
	validate func(Bundle) error,
) (Metadata, error) {
	if err := ValidateName(name); err != nil {
		return Metadata{}, err
	}
	temporary, err := os.CreateTemp(s.directory, "."+name+"-*.tmp")
	if err != nil {
		return Metadata{}, fmt.Errorf("create temporary animation: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, reader); err != nil {
		temporary.Close()
		return Metadata{}, fmt.Errorf("write animation: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return Metadata{}, fmt.Errorf("sync animation: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return Metadata{}, fmt.Errorf("close animation: %w", err)
	}
	bundle, err := s.LoadPath(temporaryPath)
	if err != nil {
		return Metadata{}, err
	}
	if validate != nil {
		if err := validate(bundle); err != nil {
			return Metadata{}, err
		}
	}
	finalPath := s.path(name)
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return Metadata{}, fmt.Errorf("install animation: %w", err)
	}
	return bundle.Metadata(name), nil
}

func (s *Store) Load(name string) (Bundle, error) {
	if err := ValidateName(name); err != nil {
		return Bundle{}, err
	}
	return s.LoadPath(s.path(name))
}

// List returns stored animation names in alphabetical order. Reading only the
// directory keeps catalogue refreshes independent of animation size and avoids
// loading RGB24 frames while another animation is playing.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		return nil, fmt.Errorf("list animations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".glma") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".glma")
		if ValidateName(name) == nil {
			names = append(names, name)
		}
	}
	return names, nil
}

func (s *Store) LoadPath(path string) (Bundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return Bundle{}, fmt.Errorf("open animation: %w", err)
	}
	defer file.Close()
	bundle, err := Decode(file)
	if err != nil {
		return Bundle{}, fmt.Errorf("decode animation: %w", err)
	}
	return bundle, nil
}

func (s *Store) path(name string) string {
	return filepath.Join(s.directory, name+".glma")
}
