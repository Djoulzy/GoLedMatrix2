// Package render serializes access to the display and keeps only the newest
// submitted frame when producers are faster than the hardware.
package render

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

type submission struct {
	sequence uint64
	frame    frame.Frame
}

type Renderer struct {
	display display.Display
	notify  chan struct{}

	mu      sync.Mutex
	pending *submission

	accepted atomic.Uint64
	rendered atomic.Uint64
	failed   atomic.Uint64
}

type Stats struct {
	Accepted uint64 `json:"accepted"`
	Rendered uint64 `json:"rendered"`
	Failed   uint64 `json:"failed"`
}

func New(target display.Display) *Renderer {
	return &Renderer{display: target, notify: make(chan struct{}, 1)}
}

// Submit takes ownership of next.Pixels. A later submission may supersede it
// before it reaches the physical display.
func (r *Renderer) Submit(next frame.Frame) uint64 {
	r.mu.Lock()
	sequence := r.accepted.Add(1)
	r.pending = &submission{sequence: sequence, frame: next}
	r.mu.Unlock()
	select {
	case r.notify <- struct{}{}:
	default:
	}
	return sequence
}

func (r *Renderer) Stats() Stats {
	return Stats{
		Accepted: r.accepted.Load(),
		Rendered: r.rendered.Load(),
		Failed:   r.failed.Load(),
	}
}

// Run blocks until ctx is cancelled. The native matrix driver keeps the last
// rendered buffer scanning continuously; Run only acts when a new frame arrives.
func (r *Renderer) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.notify:
			for {
				next := r.takePending()
				if next == nil {
					break
				}
				if err := r.display.Present(ctx, next.frame); err != nil {
					r.failed.Add(1)
					slog.Error("unable to present frame", "sequence", next.sequence, "error", err)
					continue
				}
				r.rendered.Store(next.sequence)
			}
		}
	}
}

func (r *Renderer) takePending() *submission {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.pending
	r.pending = nil
	return next
}
