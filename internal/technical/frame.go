// Package technical renders the temporary server information screen.
package technical

import (
	"fmt"
	"image"
	"image/color"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/assets"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type State struct {
	Backend  string
	BaseURL  string
	Width    int
	Height   int
	Uptime   time.Duration
	Protocol string
}

type textLine struct {
	text  string
	color color.RGBA
}

var (
	parsedFont     *opentype.Font
	parsedFontErr  error
	parsedFontOnce sync.Once
)

func Render(state State) (frame.Frame, error) {
	if _, err := frame.ByteLen(state.Width, state.Height); err != nil {
		return frame.Frame{}, err
	}
	parsedFontOnce.Do(func() {
		parsedFont, parsedFontErr = opentype.Parse(assets.TechnicalInfoFont)
	})
	if parsedFontErr != nil {
		return frame.Frame{}, fmt.Errorf("parse technical information font: %w", parsedFontErr)
	}

	host, port := endpointParts(state.BaseURL)
	lines := technicalLines(state, host, port)

	face, err := fittingFace(lines, state.Width, state.Height)
	if err != nil {
		return frame.Frame{}, err
	}
	defer closeFace(face)

	canvas := image.NewRGBA(image.Rect(0, 0, state.Width, state.Height))
	drawer := &font.Drawer{Dst: canvas, Face: face}
	metrics := face.Metrics()
	lineHeight := metrics.Height.Ceil()
	totalHeight := lineHeight * len(lines)
	baseline := max(metrics.Ascent.Ceil(), (state.Height-totalHeight)/2+metrics.Ascent.Ceil())

	for _, line := range lines {
		line.text = fitText(face, line.text, state.Width-2)
		width := drawer.MeasureString(line.text).Ceil()
		drawer.Src = image.NewUniform(line.color)
		drawer.Dot = fixed.P(max(1, (state.Width-width)/2), baseline)
		drawer.DrawString(line.text)
		baseline += lineHeight
	}
	return frame.FromImage(canvas)
}

func technicalLines(state State, host, port string) []textLine {
	green := color.RGBA{R: 64, G: 255, B: 96, A: 255}
	yellow := color.RGBA{R: 255, G: 220, B: 64, A: 255}
	blue := color.RGBA{R: 80, G: 200, B: 255, A: 255}
	grey := color.RGBA{R: 220, G: 220, B: 220, A: 255}
	backend := strings.ToUpper(state.Backend)

	if state.Height < 48 {
		endpoint := host
		if port != "" {
			endpoint = net.JoinHostPort(host, port)
		}
		return []textLine{
			{text: "GOLED " + backend, color: green},
			{text: fmt.Sprintf("%dX%d API V%s", state.Width, state.Height, state.Protocol), color: yellow},
			{text: endpoint, color: blue},
		}
	}

	lines := []textLine{
		{text: "GOLED READY", color: green},
		{text: fmt.Sprintf("%s %dX%d", backend, state.Width, state.Height), color: yellow},
	}
	if host != "" {
		lines = append(lines, textLine{text: host, color: blue})
	}
	if port != "" {
		lines = append(lines, textLine{text: "HTTP " + port, color: blue})
	}
	return append(lines, textLine{
		text:  fmt.Sprintf("API V%s UP %s", state.Protocol, shortDuration(state.Uptime)),
		color: grey,
	})
}

func fittingFace(lines []textLine, width, height int) (font.Face, error) {
	maxSize := min(16, max(5, height/len(lines)))
	for size := maxSize; size >= 5; size-- {
		face, err := opentype.NewFace(parsedFont, &opentype.FaceOptions{
			Size: float64(size), DPI: 72, Hinting: font.HintingFull,
		})
		if err != nil {
			return nil, fmt.Errorf("create technical information font: %w", err)
		}
		lineHeight := face.Metrics().Height.Ceil()
		if lineHeight*len(lines) <= height-2 {
			return face, nil
		}
		closeFace(face)
	}
	return nil, fmt.Errorf("matrix height %d is too small for technical information", height)
}

func fitText(face font.Face, value string, availableWidth int) string {
	if font.MeasureString(face, value).Ceil() <= availableWidth {
		return value
	}
	runes := []rune(value)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "."
		if font.MeasureString(face, candidate).Ceil() <= availableWidth {
			return candidate
		}
	}
	return "."
}

func endpointParts(rawURL string) (string, string) {
	endpoint, err := url.Parse(rawURL)
	if err != nil {
		return "", ""
	}
	host := endpoint.Hostname()
	port := endpoint.Port()
	if parsed := net.ParseIP(host); parsed != nil && parsed.IsLoopback() {
		host = "LOCALHOST"
	}
	return strings.ToUpper(host), port
}

func shortDuration(value time.Duration) string {
	if value < time.Minute {
		return fmt.Sprintf("%02dS", max(0, int(value.Seconds())))
	}
	if value < time.Hour {
		return fmt.Sprintf("%02dM", int(value.Minutes()))
	}
	if value < 24*time.Hour {
		return fmt.Sprintf("%02dH", int(value.Hours()))
	}
	return fmt.Sprintf("%02dD", int(value.Hours()/24))
}

func closeFace(face font.Face) {
	if closer, ok := face.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}
