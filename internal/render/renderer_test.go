package render

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

func TestRendererKeepsNewestPendingFrame(t *testing.T) {
	target := &blockingDisplay{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	renderer := New(target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderer.Run(ctx)

	renderer.Submit(onePixel(1))
	select {
	case <-target.started:
	case <-time.After(time.Second):
		t.Fatal("first render did not start")
	}
	renderer.Submit(onePixel(2))
	renderer.Submit(onePixel(3))
	close(target.release)

	deadline := time.Now().Add(time.Second)
	for renderer.Stats().Rendered != 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	if len(target.values) != 2 {
		t.Fatalf("rendered %d frames, want 2", len(target.values))
	}
	if target.values[0] != 1 || target.values[1] != 3 {
		t.Fatalf("rendered values %v, want [1 3]", target.values)
	}
}

func TestTemporaryFrameRestoresNewestClientFrame(t *testing.T) {
	target := &recordingDisplay{presented: make(chan byte, 4)}
	renderer := New(target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderer.Run(ctx)

	renderer.Submit(onePixel(1))
	expectPresented(t, target.presented, 1)
	if err := renderer.ShowTemporary(onePixel(9), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	expectPresented(t, target.presented, 9)
	renderer.Submit(onePixel(2))
	expectPresented(t, target.presented, 2)

	stats := renderer.Stats()
	if stats.Accepted != 2 || stats.Rendered != 2 {
		t.Fatalf("unexpected stats after restoration: %+v", stats)
	}
}

func TestDefaultFrameStopsForClientAndCanBeReactivated(t *testing.T) {
	target := &recordingDisplay{presented: make(chan byte, 4)}
	renderer := New(target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderer.Run(ctx)

	if err := renderer.SetDefault(onePixel(7)); err != nil {
		t.Fatal(err)
	}
	expectPresented(t, target.presented, 7)

	renderer.Submit(onePixel(1))
	expectPresented(t, target.presented, 1)
	if err := renderer.SetDefault(onePixel(8)); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-target.presented:
		t.Fatalf("inactive default frame was presented: %d", got)
	case <-time.After(20 * time.Millisecond):
	}

	if err := renderer.ActivateDefault(); err != nil {
		t.Fatal(err)
	}
	expectPresented(t, target.presented, 8)
}

func TestPlaybackFrameDoesNotChangeClientStats(t *testing.T) {
	target := &recordingDisplay{presented: make(chan byte, 2)}
	renderer := New(target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderer.Run(ctx)

	renderer.SubmitPlayback(onePixel(6))
	expectPresented(t, target.presented, 6)
	if stats := renderer.Stats(); stats.Accepted != 0 || stats.Rendered != 0 {
		t.Fatalf("playback changed client stats: %+v", stats)
	}
}

func TestTemporaryFrameRestoresDefault(t *testing.T) {
	target := &recordingDisplay{presented: make(chan byte, 4)}
	renderer := New(target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderer.Run(ctx)

	if err := renderer.SetDefault(onePixel(7)); err != nil {
		t.Fatal(err)
	}
	expectPresented(t, target.presented, 7)
	if err := renderer.ShowTemporary(onePixel(9), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	expectPresented(t, target.presented, 9)
	expectPresented(t, target.presented, 7)
}

func expectPresented(t *testing.T, presented <-chan byte, want byte) {
	t.Helper()
	select {
	case got := <-presented:
		if got != want {
			t.Fatalf("presented value = %d, want %d", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("value %d was not presented", want)
	}
}

func onePixel(value byte) frame.Frame {
	next, _ := frame.New(1, 1, []byte{value, 0, 0})
	return next
}

type recordingDisplay struct {
	presented chan byte
}

func (d *recordingDisplay) Geometry() (int, int) { return 1, 1 }

func (d *recordingDisplay) Present(_ context.Context, next frame.Frame) error {
	d.presented <- next.Pixels[0]
	return nil
}

func (d *recordingDisplay) Close() error { return nil }

type blockingDisplay struct {
	started chan struct{}
	release chan struct{}

	mu     sync.Mutex
	values []byte
}

func (d *blockingDisplay) Geometry() (int, int) { return 1, 1 }

func (d *blockingDisplay) Present(_ context.Context, next frame.Frame) error {
	d.mu.Lock()
	d.values = append(d.values, next.Pixels[0])
	first := len(d.values) == 1
	d.mu.Unlock()
	select {
	case d.started <- struct{}{}:
	default:
	}
	if first {
		<-d.release
	}
	return nil
}

func (d *blockingDisplay) Close() error { return nil }
