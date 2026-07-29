// Package clock renders the server's clock display modes.
package clock

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/hajimehoshi/bitmapfont"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type Mode string

const (
	Simple Mode = "simple"
	Fancy  Mode = "fancy"
	Round  Mode = "round"
)

var (
	hourColor      = color.RGBA{R: 255, G: 131, B: 55, A: 255}
	minuteColor    = color.RGBA{R: 123, G: 224, B: 222, A: 255}
	separatorColor = color.RGBA{R: 220, G: 220, B: 220, A: 255}
	pendingColor   = color.RGBA{R: 20, G: 48, B: 48, A: 255}
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

// Render creates a pixel-perfect clock frame. Glyphs are enlarged only by an
// integer factor and every drawing primitive stays aligned to logical pixels.
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

// renderSimple replaces the former SimpleTime mode with a stable centered
// HH:MM display, a blinking separator and a seconds progress bar.
func renderSimple(canvas *image.RGBA, now time.Time) error {
	text := now.Format("15:04")
	source := bitmapText(text, func(index int) color.Color {
		switch {
		case index < 2:
			return hourColor
		case index == 2 && now.Second()%2 == 0:
			return separatorColor
		case index == 2:
			return color.RGBA{A: 255}
		default:
			return minuteColor
		}
	})

	const horizontalMargin = 2
	const progressHeight = 3
	scale := min(
		(canvas.Bounds().Dx()-horizontalMargin)/source.Bounds().Dx(),
		(canvas.Bounds().Dy()-progressHeight)/source.Bounds().Dy(),
	)
	if scale < 1 {
		return matrixTooSmall(canvas)
	}
	scaledWidth, scaledHeight := source.Bounds().Dx()*scale, source.Bounds().Dy()*scale
	offsetX := (canvas.Bounds().Dx() - scaledWidth) / 2
	offsetY := (canvas.Bounds().Dy() - progressHeight - scaledHeight) / 2
	drawScaled(canvas, source, offsetX, offsetY, scale)
	drawSecondsBar(canvas, now.Second())
	return nil
}

// renderFancy follows the former FancyClock layout with the hour and minute on
// two independently colored rows.
func renderFancy(canvas *image.RGBA, now time.Time) error {
	hour := bitmapText(now.Format("15"), func(int) color.Color { return hourColor })
	minute := bitmapText(now.Format("04"), func(int) color.Color { return minuteColor })
	sourceWidth := max(hour.Bounds().Dx(), minute.Bounds().Dx())
	sourceHeight := hour.Bounds().Dy() + 1 + minute.Bounds().Dy()
	scale := min(
		(canvas.Bounds().Dx()-2)/sourceWidth,
		(canvas.Bounds().Dy()-2)/sourceHeight,
	)
	if scale < 1 {
		return matrixTooSmall(canvas)
	}

	totalHeight := sourceHeight * scale
	offsetY := (canvas.Bounds().Dy() - totalHeight) / 2
	drawScaled(canvas, hour, (canvas.Bounds().Dx()-hour.Bounds().Dx()*scale)/2, offsetY, scale)
	separatorY := offsetY + hour.Bounds().Dy()*scale
	separatorSize := max(1, scale)
	separatorX := (canvas.Bounds().Dx() - separatorSize) / 2
	fillRect(canvas, separatorX, separatorY, separatorSize, separatorSize, separatorColor)
	drawScaled(
		canvas,
		minute,
		(canvas.Bounds().Dx()-minute.Bounds().Dx()*scale)/2,
		separatorY+scale,
		scale,
	)
	return nil
}

// renderRound follows the former OfficeRound mode: twelve dial markers, a
// sixty-step seconds ring and a centered HH:MM value.
func renderRound(canvas *image.RGBA, now time.Time) error {
	width, height := canvas.Bounds().Dx(), canvas.Bounds().Dy()
	radius := float64(min(width, height)-1)/2 - 1
	if radius < 3 {
		return matrixTooSmall(canvas)
	}
	centerX := float64(width-1) / 2
	centerY := float64(height-1) / 2
	markerSize := 1
	if min(width, height) >= 96 {
		markerSize = 2
	}

	secondsRadius := max(1.0, radius-3)
	for second := 0; second < 60; second++ {
		angle := float64(second)*2*math.Pi/60 - math.Pi/2
		x := int(math.Round(centerX + secondsRadius*math.Cos(angle)))
		y := int(math.Round(centerY + secondsRadius*math.Sin(angle)))
		value := pendingColor
		if second < now.Second() {
			value = hourColor
		}
		drawPoint(canvas, x, y, 1, value)
	}
	for hour := 0; hour < 12; hour++ {
		angle := float64(hour)*2*math.Pi/12 - math.Pi/2
		x := int(math.Round(centerX + radius*math.Cos(angle)))
		y := int(math.Round(centerY + radius*math.Sin(angle)))
		drawPoint(canvas, x, y, markerSize, separatorColor)
	}

	text := bitmapText(now.Format("15:04"), func(index int) color.Color {
		if index < 2 {
			return hourColor
		}
		if index == 2 {
			return separatorColor
		}
		return minuteColor
	})
	maxTextWidth := min(width-2, max(text.Bounds().Dx(), int(radius*1.6)))
	maxTextHeight := max(text.Bounds().Dy(), int(radius*.8))
	scale := max(1, min(maxTextWidth/text.Bounds().Dx(), maxTextHeight/text.Bounds().Dy()))
	drawScaled(
		canvas,
		text,
		(width-text.Bounds().Dx()*scale)/2,
		(height-text.Bounds().Dy()*scale)/2,
		scale,
	)
	return nil
}

func bitmapText(value string, colorAt func(int) color.Color) *image.RGBA {
	face := bitmapfont.Gothic10r
	bounds, _ := font.BoundString(face, value)
	width := bounds.Max.X.Ceil() - bounds.Min.X.Floor()
	height := bounds.Max.Y.Ceil() - bounds.Min.Y.Floor()
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	drawer := &font.Drawer{
		Dst:  result,
		Face: face,
		Dot: fixed.Point26_6{
			X: -bounds.Min.X,
			Y: -bounds.Min.Y,
		},
	}
	for index, character := range value {
		drawer.Src = image.NewUniform(colorAt(index))
		drawer.DrawString(string(character))
	}
	return result
}

func drawScaled(dst *image.RGBA, src *image.RGBA, offsetX, offsetY, scale int) {
	for y := 0; y < src.Bounds().Dy(); y++ {
		for x := 0; x < src.Bounds().Dx(); x++ {
			value := src.RGBAAt(x, y)
			if value.A == 0 {
				continue
			}
			fillRect(dst, offsetX+x*scale, offsetY+y*scale, scale, scale, value)
		}
	}
}

func drawSecondsBar(canvas *image.RGBA, second int) {
	width := canvas.Bounds().Dx()
	if width <= 2 {
		return
	}
	y := canvas.Bounds().Dy() - 2
	for x := 1; x < width-1; x++ {
		value := pendingColor
		if (x-1)*60 < second*(width-2) {
			value = hourColor
		}
		canvas.SetRGBA(x, y, value)
	}
}

func drawPoint(canvas *image.RGBA, centerX, centerY, size int, value color.RGBA) {
	fillRect(canvas, centerX-size/2, centerY-size/2, size, size, value)
}

func fillRect(canvas *image.RGBA, x, y, width, height int, value color.RGBA) {
	for offsetY := 0; offsetY < height; offsetY++ {
		for offsetX := 0; offsetX < width; offsetX++ {
			canvas.SetRGBA(x+offsetX, y+offsetY, value)
		}
	}
}

func matrixTooSmall(canvas *image.RGBA) error {
	return fmt.Errorf("matrix %dx%d is too small for clock", canvas.Bounds().Dx(), canvas.Bounds().Dy())
}
