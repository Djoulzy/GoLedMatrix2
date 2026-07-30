package client

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"io"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/animation"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	xdraw "golang.org/x/image/draw"
)

func PrepareGIF(reader io.Reader, width, height int) (animation.Bundle, error) {
	decoded, err := gif.DecodeAll(reader)
	if err != nil {
		return animation.Bundle{}, fmt.Errorf("decode GIF: %w", err)
	}
	if len(decoded.Image) == 0 {
		return animation.Bundle{}, fmt.Errorf("GIF contains no frames")
	}
	if len(decoded.Image) > animation.MaxFrames {
		return animation.Bundle{}, fmt.Errorf("GIF contains %d frames; maximum is %d", len(decoded.Image), animation.MaxFrames)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, decoded.Config.Width, decoded.Config.Height))
	background := gifBackground(decoded)
	frames := make([]animation.TimedFrame, 0, len(decoded.Image))
	for index, source := range decoded.Image {
		var previous *image.RGBA
		disposal := byte(gif.DisposalNone)
		if index < len(decoded.Disposal) {
			disposal = decoded.Disposal[index]
		}
		if disposal == gif.DisposalPrevious {
			previous = cloneRGBA(canvas)
		}
		draw.Draw(canvas, source.Bounds(), source, source.Bounds().Min, draw.Over)
		next, err := resizeContain(canvas, width, height)
		if err != nil {
			return animation.Bundle{}, fmt.Errorf("prepare GIF frame %d: %w", index, err)
		}
		delay := 100 * time.Millisecond
		if index < len(decoded.Delay) && decoded.Delay[index] > 0 {
			delay = time.Duration(decoded.Delay[index]) * 10 * time.Millisecond
			if delay < 20*time.Millisecond {
				delay = 20 * time.Millisecond
			}
		}
		frames = append(frames, animation.TimedFrame{Frame: next, Duration: delay})
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, source.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			if previous != nil {
				draw.Draw(canvas, canvas.Bounds(), previous, image.Point{}, draw.Src)
			}
		}
	}
	loops := uint32(1)
	switch {
	case decoded.LoopCount == 0:
		loops = 0
	case decoded.LoopCount > 0:
		loops = uint32(decoded.LoopCount + 1)
	}
	bundle := animation.Bundle{Width: width, Height: height, Loops: loops, Frames: frames}
	if err := bundle.Validate(); err != nil {
		return animation.Bundle{}, err
	}
	return bundle, nil
}

func gifBackground(decoded *gif.GIF) color.Color {
	if palette, ok := decoded.Config.ColorModel.(color.Palette); ok {
		index := int(decoded.BackgroundIndex)
		if index >= 0 && index < len(palette) {
			return palette[index]
		}
	}
	return color.RGBA{A: 255}
}

func cloneRGBA(source *image.RGBA) *image.RGBA {
	result := image.NewRGBA(source.Bounds())
	draw.Draw(result, result.Bounds(), source, source.Bounds().Min, draw.Src)
	return result
}

func resizeContain(source image.Image, width, height int) (frame.Frame, error) {
	if _, err := frame.ByteLen(width, height); err != nil {
		return frame.Frame{}, err
	}
	bounds := source.Bounds()
	sourceWidth, sourceHeight := bounds.Dx(), bounds.Dy()
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return frame.Frame{}, fmt.Errorf("source image is empty")
	}
	scale := min(float64(width)/float64(sourceWidth), float64(height)/float64(sourceHeight))
	targetWidth := max(1, int(float64(sourceWidth)*scale+0.5))
	targetHeight := max(1, int(float64(sourceHeight)*scale+0.5))
	left := (width - targetWidth) / 2
	top := (height - targetHeight) / 2
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(
		canvas,
		image.Rect(left, top, left+targetWidth, top+targetHeight),
		source,
		bounds,
		draw.Over,
		nil,
	)
	return frame.FromImage(canvas)
}
