package marquee

import (
	"context"
	"sync"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/render"
)

const playbackInterval = time.Second / 60

type Player struct {
	ctx        context.Context
	renderer   *render.Renderer
	mu         sync.Mutex
	cancel     context.CancelFunc
	generation uint64
}

func NewPlayer(ctx context.Context, renderer *render.Renderer) *Player {
	return &Player{ctx: ctx, renderer: renderer}
}

// Play replaces the previous marquee. A generation guard prevents a stopped
// player from submitting a late frame over a newer display command.
func (p *Player) Play(text *Marquee) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(p.ctx)
	p.cancel = cancel
	p.generation++
	generation := p.generation
	p.renderer.SubmitPlayback(text.Render(0))
	go p.run(ctx, generation, text)
}

func (p *Player) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.generation++
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
}

func (p *Player) run(ctx context.Context, generation uint64, text *Marquee) {
	started := time.Now()
	ticker := time.NewTicker(playbackInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A delayed tick must render the current position, not its stale
			// scheduled timestamp. Keep speed independent of dropped frames.
			next := text.Render(time.Since(started))
			p.mu.Lock()
			if ctx.Err() != nil || p.generation != generation {
				p.mu.Unlock()
				return
			}
			p.renderer.SubmitPlayback(next)
			p.mu.Unlock()
		}
	}
}
