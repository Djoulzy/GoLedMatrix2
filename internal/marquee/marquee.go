// Package marquee renders autonomous right-to-left scrolling text.
package marquee

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Options is shared by the HTTP API, CLI and GUI. Zero values select defaults.
// CycleSeconds is the duration of one complete, smoothly interpolated palette.
type Options struct {
	Text         string   `json:"text"`
	Font         string   `json:"font,omitempty"`
	Size         int      `json:"size,omitempty"`
	Color        string   `json:"color,omitempty"`
	Speed        float64  `json:"speed,omitempty"`
	ColorCycle   []string `json:"color_cycle,omitempty"`
	CycleSeconds float64  `json:"cycle_seconds,omitempty"`
}

var rainbow = []string{"#FF0000", "#FFFF00", "#00FF00", "#00FFFF", "#0000FF", "#FF00FF"}

// ParseCycle accepts "rainbow" or a comma-separated palette in #RRGGBB form.
func ParseCycle(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if strings.EqualFold(value, "rainbow") {
		return append([]string(nil), rainbow...), nil
	}
	colors := strings.Split(value, ",")
	if len(colors) < 2 || len(colors) > 16 {
		return nil, fmt.Errorf("color cycle must contain between 2 and 16 colors")
	}
	for i, value := range colors {
		parsed, err := parseColor(value)
		if err != nil {
			return nil, err
		}
		colors[i] = formatColor(parsed)
	}
	return colors, nil
}

type Marquee struct {
	options                  Options
	width, height, frameSize int
	mask                     *image.Alpha
	color                    color.RGBA
	palette                  []color.RGBA
}

// New validates the settings and rasterizes the text once. No fonts or images
// are read from disk; every font is embedded in the executable.
func New(width, height int, options Options) (*Marquee, error) {
	size, err := frame.ByteLen(width, height)
	if err != nil {
		return nil, err
	}
	if !utf8.ValidString(options.Text) || len(options.Text) > 4096 || strings.TrimSpace(options.Text) == "" {
		return nil, fmt.Errorf("text must be valid UTF-8, nonempty, and at most 4096 bytes")
	}
	for _, r := range options.Text {
		if unicode.IsControl(r) {
			return nil, fmt.Errorf("text must be a single line without control characters")
		}
	}
	name, data, err := resolveFont(options.Font)
	if err != nil {
		return nil, err
	}
	options.Font = name
	if options.Size == 0 {
		options.Size = 16
	}
	if options.Size < 2 || options.Size > 256 {
		return nil, fmt.Errorf("font size must be between 2 and 256 pixels")
	}
	if options.Speed == 0 {
		options.Speed = 30
	}
	if math.IsNaN(options.Speed) || math.IsInf(options.Speed, 0) || options.Speed < 1 || options.Speed > 500 {
		return nil, fmt.Errorf("speed must be between 1 and 500 pixels per second")
	}
	if options.CycleSeconds == 0 {
		options.CycleSeconds = 6
	}
	if math.IsNaN(options.CycleSeconds) || math.IsInf(options.CycleSeconds, 0) || options.CycleSeconds < 0.1 || options.CycleSeconds > 3600 {
		return nil, fmt.Errorf("color cycle duration must be between 0.1 and 3600 seconds")
	}
	if options.Color == "" {
		options.Color = "#FFFFFF"
	}
	ink, err := parseColor(options.Color)
	if err != nil {
		return nil, err
	}
	options.Color = formatColor(ink)
	if len(options.ColorCycle) != 0 && (len(options.ColorCycle) < 2 || len(options.ColorCycle) > 16) {
		return nil, fmt.Errorf("color cycle must contain between 2 and 16 colors")
	}
	palette := make([]color.RGBA, len(options.ColorCycle))
	options.ColorCycle = append([]string(nil), options.ColorCycle...)
	for i, value := range options.ColorCycle {
		palette[i], err = parseColor(value)
		if err != nil {
			return nil, fmt.Errorf("color cycle: %w", err)
		}
		options.ColorCycle[i] = formatColor(palette[i])
	}
	parsed, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse embedded font: %w", err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: float64(options.Size), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	defer face.Close()
	bounds, advance := font.BoundString(face, options.Text)
	left, top := bounds.Min.X.Floor(), bounds.Min.Y.Floor()
	right := max(bounds.Max.X.Ceil(), advance.Ceil())
	maskWidth, maskHeight := right-left, bounds.Max.Y.Ceil()-top
	if maskWidth <= 0 || maskHeight <= 0 || maskWidth > 65536 || maskWidth*maskHeight > 16<<20 {
		return nil, fmt.Errorf("rendered text is empty or too large (maximum width 65536 pixels, bitmap 16 MiB)")
	}
	mask := image.NewAlpha(image.Rect(0, 0, maskWidth, maskHeight))
	drawer := font.Drawer{Dst: mask, Src: image.White, Face: face, Dot: fixed.P(-left, -top)}
	drawer.DrawString(options.Text)
	return &Marquee{options: options, width: width, height: height, frameSize: size, mask: mask, color: ink, palette: palette}, nil
}

func (m *Marquee) Options() Options {
	options := m.options
	options.ColorCycle = append([]string(nil), options.ColorCycle...)
	return options
}

// Render begins just outside the right edge, crosses the display, then repeats
// once the entire text has left the left edge. Text is vertically centered.
func (m *Marquee) Render(elapsed time.Duration) frame.Frame {
	seconds := math.Max(0, elapsed.Seconds())
	span := m.width + m.mask.Rect.Dx()
	x := m.width - int(math.Floor(math.Mod(seconds*m.options.Speed, float64(span))))
	y := (m.height - m.mask.Rect.Dy()) / 2
	ink := m.colorAt(seconds)
	pixels := make([]byte, m.frameSize)
	for row := max(0, y); row < min(m.height, y+m.mask.Rect.Dy()); row++ {
		for col := max(0, x); col < min(m.width, x+m.mask.Rect.Dx()); col++ {
			alpha := uint16(m.mask.AlphaAt(col-x, row-y).A)
			offset := (row*m.width + col) * 3
			pixels[offset] = byte(uint16(ink.R) * alpha / 255)
			pixels[offset+1] = byte(uint16(ink.G) * alpha / 255)
			pixels[offset+2] = byte(uint16(ink.B) * alpha / 255)
		}
	}
	return frame.Frame{Width: m.width, Height: m.height, Pixels: pixels}
}

func (m *Marquee) colorAt(seconds float64) color.RGBA {
	if len(m.palette) == 0 {
		return m.color
	}
	position := math.Mod(seconds/m.options.CycleSeconds, 1) * float64(len(m.palette))
	index := int(position)
	a, b := m.palette[index], m.palette[(index+1)%len(m.palette)]
	fraction := position - float64(index)
	interpolate := func(a, b uint8) uint8 { return uint8(math.Round(float64(a)*(1-fraction) + float64(b)*fraction)) }
	return color.RGBA{R: interpolate(a.R, b.R), G: interpolate(a.G, b.G), B: interpolate(a.B, b.B), A: 255}
}

func parseColor(value string) (color.RGBA, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 {
		return color.RGBA{}, fmt.Errorf("color must use #RRGGBB form")
	}
	packed, err := strconv.ParseUint(value, 16, 24)
	if err != nil {
		return color.RGBA{}, fmt.Errorf("color must use #RRGGBB form")
	}
	return color.RGBA{R: byte(packed >> 16), G: byte(packed >> 8), B: byte(packed), A: 255}, nil
}

func formatColor(value color.RGBA) string {
	return fmt.Sprintf("#%02X%02X%02X", value.R, value.G, value.B)
}
