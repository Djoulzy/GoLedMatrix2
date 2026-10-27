// Package render serializes access to the display and keeps only the newest
// submitted frame when producers are faster than the hardware.
package render

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

type submission struct {
	sequence uint64
	frame    frame.Frame
	counted  bool
}

type temporarySubmission struct {
	frame    frame.Frame
	duration time.Duration
}

type brightnessRequest struct {
	ctx    context.Context
	value  int
	result chan error
}

type Renderer struct {
	display            display.Display
	notify             chan struct{}
	brightnessRequests chan brightnessRequest
	done               chan struct{}
	brightness         atomic.Int64

	mu            sync.Mutex
	pending       *submission
	latest        *submission
	temporary     *temporarySubmission
	defaultFrame  *frame.Frame
	defaultActive bool

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
	renderer := &Renderer{
		display: target, notify: make(chan struct{}, 1),
		brightnessRequests: make(chan brightnessRequest), done: make(chan struct{}),
		defaultActive: true,
	}
	renderer.brightness.Store(int64(target.Brightness()))
	return renderer
}

func (r *Renderer) Brightness() int { return int(r.brightness.Load()) }

// SetBrightness waits for the render goroutine to apply the setting. Native
// driver access stays serialized with frame presentation, including animations.
func (r *Renderer) SetBrightness(ctx context.Context, value int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := display.ValidateBrightness(value); err != nil {
		return err
	}
	request := brightnessRequest{ctx: ctx, value: value, result: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.done:
		return fmt.Errorf("renderer is stopped")
	case r.brightnessRequests <- request:
	}
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-r.done:
		return fmt.Errorf("renderer is stopped")
	}
}

// Submit takes ownership of next.Pixels. A later submission may supersede it
// before it reaches the physical display.
func (r *Renderer) Submit(next frame.Frame) uint64 {
	r.mu.Lock()
	sequence := r.accepted.Add(1)
	submitted := &submission{sequence: sequence, frame: next, counted: true}
	r.pending = submitted
	r.latest = submitted
	r.defaultActive = false
	r.mu.Unlock()
	r.wake()
	return sequence
}

// SubmitPlayback queues an autonomous animation frame without changing client
// acceptance statistics.
func (r *Renderer) SubmitPlayback(next frame.Frame) {
	r.mu.Lock()
	submitted := &submission{frame: next}
	r.pending = submitted
	r.latest = submitted
	r.defaultActive = false
	r.mu.Unlock()
	r.wake()
}

// SetDefault updates the frame shown while the default mode is active. It does
// not affect client frame statistics or replace the latest client submission.
func (r *Renderer) SetDefault(next frame.Frame) error {
	width, height := r.display.Geometry()
	if next.Width != width || next.Height != height {
		return fmt.Errorf("default frame geometry %dx%d does not match display %dx%d", next.Width, next.Height, width, height)
	}
	r.mu.Lock()
	r.defaultFrame = &next
	active := r.defaultActive
	r.mu.Unlock()
	if active {
		r.wake()
	}
	return nil
}

// ActivateDefault discards the last client frame and returns to the default
// display mode.
func (r *Renderer) ActivateDefault() error {
	r.mu.Lock()
	if r.defaultFrame == nil {
		r.mu.Unlock()
		return fmt.Errorf("default display is unavailable")
	}
	r.pending = nil
	r.latest = nil
	r.defaultActive = true
	r.mu.Unlock()
	r.wake()
	return nil
}

// ShowTemporary displays next for duration without changing the accepted frame
// sequence. The active client or default display is restored when it expires.
func (r *Renderer) ShowTemporary(next frame.Frame, duration time.Duration) error {
	width, height := r.display.Geometry()
	if next.Width != width || next.Height != height {
		return fmt.Errorf("temporary frame geometry %dx%d does not match display %dx%d", next.Width, next.Height, width, height)
	}
	if duration <= 0 {
		return fmt.Errorf("temporary frame duration must be positive")
	}
	r.mu.Lock()
	r.temporary = &temporarySubmission{frame: next, duration: duration}
	r.mu.Unlock()
	r.wake()
	return nil
}

func (r *Renderer) wake() {
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

func (r *Renderer) Stats() Stats {
	return Stats{
		Accepted: r.accepted.Load(),
		Rendered: r.rendered.Load(),
		Failed:   r.failed.Load(),
	}
}

// Run blocks until ctx is cancelled. The native matrix driver keeps the last
// rendered buffer scanning continuously; Run acts on new frames and settings.
func (r *Renderer) Run(ctx context.Context) {
	defer close(r.done)
	var temporaryTimer *time.Timer
	var temporaryDone <-chan time.Time
	showingTemporary := false
	defer func() {
		if temporaryTimer != nil {
			temporaryTimer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case request := <-r.brightnessRequests:
			err := r.display.SetBrightness(request.ctx, request.value)
			if err == nil {
				r.brightness.Store(int64(request.value))
			}
			request.result <- err
		case <-r.notify:
			if temporary := r.takeTemporary(); temporary != nil {
				if err := r.present(ctx, temporary.frame, 0, "technical information"); err == nil {
					showingTemporary = true
					if temporaryTimer == nil {
						temporaryTimer = time.NewTimer(temporary.duration)
					} else {
						if !temporaryTimer.Stop() {
							select {
							case <-temporaryTimer.C:
							default:
							}
						}
						temporaryTimer.Reset(temporary.duration)
					}
					temporaryDone = temporaryTimer.C
				}
			}
			if !showingTemporary {
				r.presentCurrent(ctx)
			}
		case <-temporaryDone:
			showingTemporary = false
			temporaryDone = nil
			r.restoreCurrent(ctx)
		}
	}
}

func (r *Renderer) presentCurrent(ctx context.Context) {
	// Handle one pending frame per wakeup so a saturated producer cannot starve
	// brightness commands or shutdown. Submissions already schedule a wakeup.
	if next := r.takePending(); next != nil {
		if err := r.present(ctx, next.frame, next.sequence, "frame"); err == nil && next.counted {
			r.rendered.Store(next.sequence)
		}
	}
	if next := r.takeActiveDefault(); next != nil {
		_ = r.present(ctx, *next, 0, "default frame")
	}
}

func (r *Renderer) restoreCurrent(ctx context.Context) {
	next, defaultFrame := r.takeCurrent()
	if defaultFrame != nil {
		_ = r.present(ctx, *defaultFrame, 0, "restored default frame")
		return
	}
	if next == nil {
		width, height := r.display.Geometry()
		byteLen, _ := frame.ByteLen(width, height)
		_ = r.present(ctx, frame.Frame{
			Width: width, Height: height, Pixels: make([]byte, byteLen),
		}, 0, "blank frame")
		return
	}
	if err := r.present(ctx, next.frame, next.sequence, "restored frame"); err == nil && next.counted {
		r.rendered.Store(next.sequence)
	}
}

func (r *Renderer) present(ctx context.Context, next frame.Frame, sequence uint64, kind string) error {
	if err := r.display.Present(ctx, next); err != nil {
		r.failed.Add(1)
		slog.Error("unable to present "+kind, "sequence", sequence, "error", err)
		return err
	}
	return nil
}

func (r *Renderer) takePending() *submission {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.pending
	r.pending = nil
	return next
}

func (r *Renderer) takeCurrent() (*submission, *frame.Frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = nil
	if r.defaultActive {
		return nil, r.defaultFrame
	}
	return r.latest, nil
}

func (r *Renderer) takeActiveDefault() *frame.Frame {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.defaultActive {
		return nil
	}
	return r.defaultFrame
}

func (r *Renderer) takeTemporary() *temporarySubmission {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.temporary
	r.temporary = nil
	return next
}
