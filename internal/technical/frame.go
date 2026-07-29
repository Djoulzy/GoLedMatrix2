// Package technical renders the temporary server information screen.
package technical

import (
	"fmt"
	"image"
	"image/color"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/hajimehoshi/bitmapfont"
	"golang.org/x/image/font"
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

func Render(state State) (frame.Frame, error) {
	if _, err := frame.ByteLen(state.Width, state.Height); err != nil {
		return frame.Frame{}, err
	}

	host, port := endpointParts(state.BaseURL)
	lines := technicalLines(state, host, port)

	face, err := fittingFace(lines, state.Width, state.Height)
	if err != nil {
		return frame.Frame{}, err
	}

	canvas := image.NewRGBA(image.Rect(0, 0, state.Width, state.Height))
	drawer := &font.Drawer{Dst: canvas, Face: face}
	metrics := face.Metrics()
	lineHeight := metrics.Height.Ceil()
	totalHeight := lineHeight * len(lines)
	baseline := max(metrics.Ascent.Ceil(), (state.Height-totalHeight)/2+metrics.Ascent.Ceil())

	for _, line := range lines {
		line.text = fitText(face, line.text, state.Width-2)
		bounds, _ := font.BoundString(face, line.text)
		width := bounds.Max.X.Ceil() - bounds.Min.X.Floor()
		left := max(1, (state.Width-width)/2)
		drawer.Src = image.NewUniform(line.color)
		drawer.Dot = fixed.Point26_6{
			X: fixed.I(left) - bounds.Min.X,
			Y: fixed.I(baseline),
		}
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
		if state.Height < 36 {
			return []textLine{
				{text: fmt.Sprintf("%s %dX%d V%s", compactLabel(backend), state.Width, state.Height, state.Protocol), color: green},
				{text: host, color: blue},
			}
		}
		return []textLine{
			{text: "GoLED " + backend, color: green},
			{text: fmt.Sprintf("%dx%d API V%s", state.Width, state.Height, state.Protocol), color: yellow},
			{text: endpoint, color: blue},
		}
	}

	if state.Height < 64 {
		return []textLine{
			{text: "GoLED Ready", color: green},
			{text: fmt.Sprintf("%s %dx%d", backend, state.Width, state.Height), color: yellow},
			{text: host, color: blue},
		}
	}

	lines := []textLine{
		{text: "GoLED Ready", color: green},
		{text: fmt.Sprintf("%s %dx%d", backend, state.Width, state.Height), color: yellow},
	}
	if host != "" {
		lines = append(lines, textLine{text: host, color: blue})
	}
	if port != "" {
		lines = append(lines, textLine{text: "HTTP " + port, color: blue})
	}
	return append(lines, textLine{
		text:  fmt.Sprintf("API v%s UP %s", state.Protocol, shortDuration(state.Uptime)),
		color: grey,
	})
}

func fittingFace(lines []textLine, width, height int) (font.Face, error) {
	faces := []font.Face{bitmapfont.Gothic12r, bitmapfont.Gothic10r}
	var fallback font.Face
	for _, face := range faces {
		lineHeight := face.Metrics().Height.Ceil()
		if lineHeight*len(lines) > height-2 {
			continue
		}
		fallback = face
		allLinesFit := true
		for _, line := range lines {
			if font.MeasureString(face, line.text).Ceil() > width-2 {
				allLinesFit = false
				break
			}
		}
		if allLinesFit {
			return face, nil
		}
	}
	if fallback != nil {
		return fallback, nil
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

func compactLabel(value string) string {
	runes := []rune(value)
	if len(runes) > 3 {
		runes = runes[:3]
	}
	return string(runes)
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
