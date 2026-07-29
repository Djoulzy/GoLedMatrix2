// Package clock renders the server's clock display modes.
package clock

import (
	"fmt"
	"image"
	"math"
	"math/rand"
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

// Render creates an antialiased clock frame with the original TTF fonts and gg
// drawing primitives.
func Render(now time.Time, width, height int, mode Mode) (frame.Frame, error) {
	if _, err := frame.ByteLen(width, height); err != nil {
		return frame.Frame{}, err
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	var err error
	switch mode {
	case Simple:
		err = renderSimple(canvas, now)
	case Fancy:
		err = renderFancy(canvas, now)
	case Round:
		err = renderRound(canvas, now)
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
func renderSimple(canvas *image.RGBA, now time.Time) error {
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
	ctx.SetHexColor("#FFFFFF")

	random := rand.New(rand.NewSource(now.Unix()))
	maxX := min(55, max(1, canvas.Bounds().Dx()-1))
	maxY := min(100, max(1, canvas.Bounds().Dy()-1))
	x := random.Intn(maxX) + 1
	y := random.Intn(maxY) + 1
	ctx.DrawString(now.Format("15:04:05"), float64(x), float64(y))
	return nil
}

// renderFancy reproduces the former FancyClock implementation with the
// HappyBomb font, original colors and vertical positioning.
func renderFancy(canvas *image.RGBA, now time.Time) error {
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

	ctx.SetHexColor("#FF8337")
	ctx.DrawString(timeHour, float64(center.X)-timeHourWidth/2, float64(center.Y))
	ctx.SetHexColor("#7be0de")
	ctx.DrawString(
		timeMinute,
		float64(center.X)-timeMinuteWidth/2,
		float64(center.Y)+20+timeMinuteHeight,
	)
	return nil
}

// renderRound reproduces the former OfficeRound implementation with the same
// font, point size, colors, radii and gg drawing primitives.
func renderRound(canvas *image.RGBA, now time.Time) error {
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

	ctx.SetHexColor("#FFFFFF")
	for angle := 0.0; angle <= twoPi; angle += div12 {
		x := float64(center.X) + r1*math.Cos(angle)
		y := float64(center.Y) + r1*math.Sin(angle)
		ctx.DrawPoint(x, y, dotSize)
	}
	ctx.Stroke()

	ctx.SetHexColor("#FF0000")
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
	const fontSize = 38.0
	timeHeight := fontSize * 72 / 96
	ctx.DrawString(
		timeString,
		float64(center.X)-timeWidth/2,
		float64(center.Y)+timeHeight/2,
	)
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
