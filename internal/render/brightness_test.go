package render

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

func TestBrightnessKeepsTemporaryScreenAndPendingFrame(t *testing.T) {
	target, _ := display.NewMemory(1, 1)
	_ = target.SetBrightness(context.Background(), 80)
	renderer := New(target)
	if renderer.Brightness() != 80 {
		t.Fatal("renderer lost initial brightness")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderer.Run(ctx)
	renderer.Submit(onePixel(200))
	waitForPixel(t, target, 160)
	if err := renderer.ShowTemporary(onePixel(100), 250*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	waitForPixel(t, target, 80)
	renderer.Submit(onePixel(120))
	if err := renderer.SetBrightness(ctx, 50); err != nil {
		t.Fatal(err)
	}
	if renderer.Brightness() != 50 {
		t.Fatal("applied brightness not reported")
	}
	waitForPixel(t, target, 50)
	waitForPixel(t, target, 60)
	if err := renderer.SetBrightness(ctx, 100); err != nil {
		t.Fatal(err)
	}
	waitForPixel(t, target, 120)
	if stats := renderer.Stats(); stats.Accepted != 2 || stats.Rendered != 2 || stats.Failed != 0 {
		t.Fatalf("brightness changed frame statistics: %+v", stats)
	}
}

func TestBrightnessAndShutdownAreNotStarvedByFrames(t *testing.T) {
	memory, _ := display.NewMemory(1, 1)
	target := &streamingDisplay{Memory: memory, started: make(chan struct{}, 1)}
	renderer := New(target)
	next := onePixel(200)
	target.onPresent = func() { renderer.SubmitPlayback(next) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderer.Run(ctx)
	renderer.SubmitPlayback(next)
	select {
	case <-target.started:
	case <-time.After(time.Second):
		t.Fatal("frame stream did not start")
	}
	requestCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if err := renderer.SetBrightness(requestCtx, 35); err != nil {
		t.Fatalf("brightness starved by frame stream: %v", err)
	}
	if renderer.Brightness() != 35 || memory.Brightness() != 35 {
		t.Fatal("brightness not applied during playback")
	}
	cancel()
	select {
	case <-renderer.done:
	case <-time.After(time.Second):
		t.Fatal("frame stream prevented shutdown")
	}
	if err := renderer.SetBrightness(context.Background(), 50); err == nil {
		t.Fatal("stopped renderer accepted a brightness change")
	}
}

func TestBrightnessFailurePreservesSetting(t *testing.T) {
	memory, _ := display.NewMemory(1, 1)
	renderer := New(&failingBrightnessDisplay{Memory: memory})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderer.Run(ctx)
	if err := renderer.SetBrightness(ctx, 35); err == nil || renderer.Brightness() != 100 {
		t.Fatalf("failed setting was accepted: brightness=%d, err=%v", renderer.Brightness(), err)
	}
	if err := renderer.SetBrightness(ctx, 0); err == nil {
		t.Fatal("invalid setting accepted")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := renderer.SetBrightness(canceled, 35); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request = %v", err)
	}
}

type streamingDisplay struct {
	*display.Memory
	started   chan struct{}
	onPresent func()
}

func (d *streamingDisplay) Present(ctx context.Context, next frame.Frame) error {
	if err := d.Memory.Present(ctx, next); err != nil {
		return err
	}
	d.onPresent()
	select {
	case d.started <- struct{}{}:
	default:
	}
	return nil
}

type failingBrightnessDisplay struct{ *display.Memory }

func (d *failingBrightnessDisplay) SetBrightness(context.Context, int) error {
	return errors.New("driver unavailable")
}

func waitForPixel(t *testing.T, target *display.Memory, want byte) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if latest := target.Latest(); len(latest.Pixels) > 0 && latest.Pixels[0] == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pixel = %v, want %d", target.Latest().Pixels, want)
}
