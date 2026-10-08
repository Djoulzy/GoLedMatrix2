package marquee

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/Djoulzy/GoLedMatrix2/assets"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

type Font struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Category string `json:"category"`
}

// Fonts returns a deterministic catalogue shared by the CLI and GUI. Asset
// names are paths relative to assets/ttf, so duplicate basenames stay distinct.
func Fonts() ([]Font, error) {
	fonts := []Font{
		{Name: "regular", Label: "Go Regular", Category: "Go"},
		{Name: "bold", Label: "Go Bold", Category: "Go"},
		{Name: "mono", Label: "Go Mono", Category: "Go"},
	}
	err := fs.WalkDir(assets.FontFiles, "ttf", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		ext := strings.ToLower(path.Ext(name))
		if entry.IsDir() || (ext != ".ttf" && ext != ".otf") {
			return nil
		}
		name = strings.TrimPrefix(name, "ttf/")
		fonts = append(fonts, Font{
			Name: name, Label: strings.TrimSuffix(path.Base(name), path.Ext(name)), Category: path.Dir(name),
		})
		return nil
	})
	return fonts, err
}

func resolveFont(value string) (string, []byte, error) {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "regular":
		return "regular", goregular.TTF, nil
	case "bold":
		return "bold", gobold.TTF, nil
	case "mono":
		return "mono", gomono.TTF, nil
	}
	fonts, err := Fonts()
	if err != nil {
		return "", nil, fmt.Errorf("list embedded fonts: %w", err)
	}
	value = strings.TrimPrefix(strings.TrimPrefix(value, "assets/ttf/"), "ttf/")
	// Prefer a full path, then accept an unambiguous filename or bare name.
	selected := ""
	for _, candidate := range fonts {
		if candidate.Category != "Go" && strings.EqualFold(value, candidate.Name) {
			selected = candidate.Name
			break
		}
	}
	if selected == "" {
		for _, candidate := range fonts {
			if candidate.Category != "Go" && (strings.EqualFold(value, path.Base(candidate.Name)) || strings.EqualFold(value, candidate.Label)) {
				if selected != "" {
					return "", nil, fmt.Errorf("ambiguous font %q: use its path relative to assets/ttf", value)
				}
				selected = candidate.Name
			}
		}
	}
	if selected == "" {
		return "", nil, fmt.Errorf("unknown font %q (want a font from assets/ttf, or regular, bold, mono)", value)
	}
	data, err := assets.FontFiles.ReadFile("ttf/" + selected)
	if err != nil {
		return "", nil, fmt.Errorf("read embedded font: %w", err)
	}
	return selected, data, nil
}
