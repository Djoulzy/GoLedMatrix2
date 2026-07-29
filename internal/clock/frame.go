// Package clock renders the server's clock display modes.
package clock

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/assets"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
)

type Mode string

const (
	Simple Mode = "simple"
	Fancy  Mode = "fancy"
	Round  Mode = "round"
)

type Palette struct {
	Color1 color.RGBA
	Color2 color.RGBA
}

type fontResource struct {
	name   string
	data   []byte
	once   sync.Once
	parsed *truetype.Font
	err    error
}

var (
	simpleTimeFont  = fontResource{name: "SimpleTime", data: assets.SimpleTimeFont}
	fancyClockFont  = fontResource{name: "FancyClock", data: assets.FancyClockFont}
	officeRoundFont = fontResource{name: "OfficeRound", data: assets.OfficeRoundFont}
)

func ParseMode(value string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(value)))
	switch mode {
	case Simple, Fancy, Round:
		return mode, nil
	default:
		return "", fmt.Errorf("unknown clock mode %q (want simple, fancy, or round)", value)
	}
}

func ResolvePalette(mode Mode, color1, color2 string) (Palette, error) {
	defaults := defaultPalette(mode)
	var err error
	if strings.TrimSpace(color1) != "" {
		defaults.Color1, err = parseColor(color1)
		if err != nil {
			return Palette{}, fmt.Errorf("color1: %w", err)
		}
	}
	if strings.TrimSpace(color2) != "" {
		defaults.Color2, err = parseColor(color2)
		if err != nil {
			return Palette{}, fmt.Errorf("color2: %w", err)
		}
	}
	return defaults, nil
}

func FormatColor(value color.RGBA) string {
	return fmt.Sprintf("#%02X%02X%02X", value.R, value.G, value.B)
}

// Render creates an antialiased clock frame with the original TTF fonts and gg
// drawing primitives.
func Render(now time.Time, width, height int, mode Mode, palette Palette) (frame.Frame, error) {
	if _, err := frame.ByteLen(width, height); err != nil {
		return frame.Frame{}, err
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	var err error
	switch mode {
	case Simple:
		err = renderSimple(canvas, now, palette)
	case Fancy:
		err = renderFancy(canvas, now, palette)
	case Round:
		err = renderRound(canvas, now, palette)
	default:
		_, err = ParseMode(string(mode))
	}
	if err != nil {
		return frame.Frame{}, err
	}
	return frame.FromImage(canvas)
}

// renderSimple reproduces the former SimpleTime implementation with the
// Perform font and a moving HH:MM:SS value.
func renderSimple(canvas *image.RGBA, now time.Time, palette Palette) error {
	const fontSize = 12.0
	face, err := newFontFace(&simpleTimeFont, fontSize)
	if err != nil {
		return err
	}
	defer closeFace(face)

	ctx := gg.NewContextForRGBA(canvas)
	ctx.SetFontFace(face)
	ctx.SetHexColor("#000000")
	ctx.Clear()

	random := rand.New(rand.NewSource(now.Unix()))
	maxX := min(55, max(1, canvas.Bounds().Dx()-1))
	maxY := min(100, max(1, canvas.Bounds().Dy()-1))
	x := random.Intn(maxX) + 1
	y := random.Intn(maxY) + 1
	if palette.Color1 == palette.Color2 {
		ctx.SetColor(palette.Color1)
		ctx.DrawString(now.Format("15:04:05"), float64(x), float64(y))
		return nil
	}
	timeText := now.Format("15:04")
	ctx.SetColor(palette.Color1)
	ctx.DrawString(timeText, float64(x), float64(y))
	timeWidth, _ := ctx.MeasureString(timeText)
	ctx.SetColor(palette.Color2)
	ctx.DrawString(now.Format(":05"), float64(x)+timeWidth, float64(y))
	return nil
}

// renderFancy reproduces the former FancyClock implementation with the
// HappyBomb font, original colors and vertical positioning.
func renderFancy(canvas *image.RGBA, now time.Time, palette Palette) error {
	const fontSize = 55.0
	face, err := newFontFace(&fancyClockFont, fontSize)
	if err != nil {
		return err
	}
	defer closeFace(face)

	ctx := gg.NewContextForRGBA(canvas)
	ctx.SetFontFace(face)
	center := image.Point{X: canvas.Bounds().Dx() / 2, Y: canvas.Bounds().Dy() / 2}
	ctx.SetHexColor("#000000")
	ctx.Clear()

	timeHour := now.Format("15")
	timeMinute := now.Format("04")
	timeHourWidth, _ := ctx.MeasureString(timeHour)
	timeMinuteWidth, _ := ctx.MeasureString(timeMinute)
	timeMinuteHeight := fontSize * 72 / 96

	ctx.SetColor(palette.Color1)
	ctx.DrawString(timeHour, float64(center.X)-timeHourWidth/2, float64(center.Y))
	ctx.SetColor(palette.Color2)
	ctx.DrawString(
		timeMinute,
		float64(center.X)-timeMinuteWidth/2,
		float64(center.Y)+20+timeMinuteHeight,
	)
	return nil
}

// renderRound reproduces the former OfficeRound implementation with the same
// font, point size, colors, radii and gg drawing primitives.
func renderRound(canvas *image.RGBA, now time.Time, palette Palette) error {
	width, height := canvas.Bounds().Dx(), canvas.Bounds().Dy()
	face, err := newFontFace(&officeRoundFont, 38)
	if err != nil {
		return err
	}
	defer func() {
		if closer, ok := face.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	ctx := gg.NewContextForRGBA(canvas)
	ctx.SetFontFace(face)
	center := image.Point{X: width / 2, Y: height / 2}
	twoPi := 2 * math.Pi
	div12 := twoPi / 12
	div60 := twoPi / 60
	rotate := 90 * math.Pi / 180
	r1 := float64(center.Y) - 8
	r2 := float64(center.Y) - 2

	dotSize := 0.7
	if width > 64 {
		dotSize = 1
	}

	ctx.SetHexColor("#000000")
	ctx.Clear()

	ctx.SetColor(palette.Color2)
	for angle := 0.0; angle <= twoPi; angle += div12 {
		x := float64(center.X) + r1*math.Cos(angle)
		y := float64(center.Y) + r1*math.Sin(angle)
		ctx.DrawPoint(x, y, dotSize)
	}
	ctx.Stroke()

	ctx.SetColor(palette.Color1)
	timeString := now.Format("15:04")
	second := 0
	for angle := 0.0; angle <= twoPi; angle += div60 {
		x := float64(center.X) + r2*math.Cos(angle-rotate)
		y := float64(center.Y) + r2*math.Sin(angle-rotate)
		ctx.DrawPoint(x, y, dotSize)
		second++
		if second > now.Second() {
			break
		}
	}
	ctx.Stroke()

	timeWidth, _ := ctx.MeasureString(timeString)
	hourText := timeString[:2]
	minuteText := timeString[3:]
	hourWidth, _ := ctx.MeasureString(hourText)
	prefixWidth, _ := ctx.MeasureString(timeString[:3])
	const fontSize = 38.0
	timeHeight := fontSize * 72 / 96
	timeX := float64(center.X) - timeWidth/2
	timeY := float64(center.Y) + timeHeight/2
	ctx.DrawString(hourText, timeX, timeY)
	if now.Second()%2 == 0 {
		ctx.DrawString(":", timeX+hourWidth, timeY)
	}
	ctx.DrawString(minuteText, timeX+prefixWidth, timeY)
	return nil
}

func newFontFace(resource *fontResource, size float64) (font.Face, error) {
	resource.once.Do(func() {
		resource.parsed, resource.err = truetype.Parse(resource.data)
	})
	if resource.err != nil {
		return nil, fmt.Errorf("parse %s font: %w", resource.name, resource.err)
	}
	return truetype.NewFace(resource.parsed, &truetype.Options{Size: size}), nil
}

func closeFace(face font.Face) {
	if closer, ok := face.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

func defaultPalette(mode Mode) Palette {
	switch mode {
	case Fancy:
		return Palette{
			Color1: color.RGBA{R: 255, G: 131, B: 55, A: 255},
			Color2: color.RGBA{R: 123, G: 224, B: 222, A: 255},
		}
	case Round:
		return Palette{
			Color1: color.RGBA{R: 255, A: 255},
			Color2: color.RGBA{R: 255, G: 255, B: 255, A: 255},
		}
	default:
		return Palette{
			Color1: color.RGBA{R: 255, G: 255, B: 255, A: 255},
			Color2: color.RGBA{R: 255, G: 255, B: 255, A: 255},
		}
	}
}

func parseColor(value string) (color.RGBA, error) {
	raw := strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(raw) != 6 {
		return color.RGBA{}, fmt.Errorf("%q must use #RRGGBB", value)
	}
	packed, err := strconv.ParseUint(raw, 16, 24)
	if err != nil {
		return color.RGBA{}, fmt.Errorf("%q must use #RRGGBB", value)
	}
	return color.RGBA{
		R: byte(packed >> 16),
		G: byte(packed >> 8),
		B: byte(packed),
		A: 255,
	}, nil
}
