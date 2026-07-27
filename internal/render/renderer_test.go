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

func onePixel(value byte) frame.Frame {
	next, _ := frame.New(1, 1, []byte{value, 0, 0})
	return next
}

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
