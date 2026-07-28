//go:build !rpi

package display

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"sync"
	"sync/atomic"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"golang.org/x/exp/shiny/driver"
	"golang.org/x/exp/shiny/screen"
	"golang.org/x/mobile/event/key"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"
)

const simulationWindowTitle = "GoLedMatrix2 — simulation"

// Simulator displays frames in a desktop window without accessing GPIO.
// Drawing is performed by RunSimulator's UI event loop, never by Present.
type Simulator struct {
	window screen.Window
	width  int
	height int

	mu     sync.RWMutex
	pixels []byte
	closed atomic.Bool
}

// RunSimulator must be called directly from main.main. In particular it must
// not run in a goroutine: macOS requires driver.Main on the initial OS thread.
// run executes in a background goroutine and should block for the lifetime of
// the HTTP server.
func RunSimulator(
	width, height, pixelPitch int,
	run func(Display, <-chan struct{}) error,
) (result error) {
	if _, err := frame.ByteLen(width, height); err != nil {
		return err
	}
	if pixelPitch < 2 {
		return fmt.Errorf("simulation pixel pitch must be at least 2")
	}
	if run == nil {
		return fmt.Errorf("simulation runner is required")
	}

	driver.Main(func(displayScreen screen.Screen) {
		windowSize := initialWindowSize(width, height, pixelPitch)
		window, err := displayScreen.NewWindow(&screen.NewWindowOptions{
			Title: simulationWindowTitle, Width: windowSize.X, Height: windowSize.Y,
		})
		if err != nil {
			result = fmt.Errorf("create simulation window: %w", err)
			return
		}
		defer window.Release()

		byteLen, _ := frame.ByteLen(width, height)
		simulator := &Simulator{
			window: window, width: width, height: height, pixels: make([]byte, byteLen),
		}
		windowClosed := make(chan struct{})
		var closeOnce sync.Once
		closeWindow := func() {
			closeOnce.Do(func() {
				simulator.closed.Store(true)
				close(windowClosed)
			})
		}
		defer closeWindow()

		runDone := make(chan error, 1)
		go func() {
			runDone <- run(simulator, windowClosed)
		}()
		go func() {
			window.Send(simulationStopped{err: <-runDone})
		}()

		currentSize := windowSize
		for {
			event := window.NextEvent()
			switch event := event.(type) {
			case lifecycle.Event:
				if event.To == lifecycle.StageDead {
					closeWindow()
				}
			case key.Event:
				if event.Code == key.CodeEscape && event.Direction == key.DirPress {
					closeWindow()
				}
			case size.Event:
				currentSize = event.Size()
				window.Send(paint.Event{})
			case paint.Event:
				if !simulator.closed.Load() {
					simulator.paint(currentSize)
				}
			case simulationStopped:
				result = event.err
				return
			}
		}
	})
	return result
}

func (s *Simulator) Geometry() (int, int) { return s.width, s.height }

func (s *Simulator) Present(_ context.Context, next frame.Frame) error {
	if next.Width != s.width || next.Height != s.height {
		return fmt.Errorf("frame geometry %dx%d does not match simulator %dx%d", next.Width, next.Height, s.width, s.height)
	}
	if s.closed.Load() {
		return fmt.Errorf("simulation window is closed")
	}
	s.mu.Lock()
	copy(s.pixels, next.Pixels)
	s.mu.Unlock()
	s.window.Send(paint.Event{})
	return nil
}

// Close asks the UI loop to stop through the server lifecycle. The window
// itself is released by RunSimulator on its owning UI thread.
func (s *Simulator) Close() error { return nil }

func (s *Simulator) paint(view image.Point) {
	pixels := make([]byte, len(s.pixels))
	s.mu.RLock()
	copy(pixels, s.pixels)
	s.mu.RUnlock()

	pitch, gutter, margin := simulationLayout(view, s.width, s.height)
	s.window.Fill(image.Rectangle{Max: view}, color.RGBA{R: 24, G: 24, B: 24, A: 0xff}, screen.Src)
	for y, offset := 0, 0; y < s.height; y++ {
		for x := 0; x < s.width; x, offset = x+1, offset+3 {
			left := margin + x*(pitch+gutter)
			top := margin + y*(pitch+gutter)
			s.window.Fill(
				image.Rect(left, top, left+pitch, top+pitch),
				color.RGBA{R: pixels[offset], G: pixels[offset+1], B: pixels[offset+2], A: 0xff},
				screen.Src,
			)
		}
	}
	s.window.Publish()
}

type simulationStopped struct{ err error }

func initialWindowSize(width, height, preferredPitch int) image.Point {
	pitch := preferredPitch
	for pitch > 1 {
		gutter := max(1, pitch/2)
		size := image.Pt(20+width*(pitch+gutter)-gutter, 20+height*(pitch+gutter)-gutter)
		if size.X <= 1600 && size.Y <= 1000 {
			return size
		}
		pitch--
	}
	return image.Pt(20+width*2-1, 20+height*2-1)
}

func simulationLayout(view image.Point, width, height int) (pitch, gutter, margin int) {
	margin = 10
	cellWidth := max(1, (view.X-2*margin)/width)
	cellHeight := max(1, (view.Y-2*margin)/height)
	cell := min(cellWidth, cellHeight)
	gutter = max(1, cell/3)
	pitch = max(1, cell-gutter)
	return pitch, gutter, margin
}
