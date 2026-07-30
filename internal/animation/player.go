package animation

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/render"
)

type Player struct {
	ctx      context.Context
	store    *Store
	renderer *render.Renderer
	width    int
	height   int

	mu         sync.Mutex
	cancel     context.CancelFunc
	generation uint64
}

func NewPlayer(ctx context.Context, store *Store, renderer *render.Renderer, width, height int) *Player {
	return &Player{ctx: ctx, store: store, renderer: renderer, width: width, height: height}
}

func (p *Player) Upload(name string, reader io.Reader) (Metadata, error) {
	return p.store.Save(name, reader, func(bundle Bundle) error {
		if bundle.Width != p.width || bundle.Height != p.height {
			return fmt.Errorf(
				"animation geometry %dx%d does not match display %dx%d",
				bundle.Width, bundle.Height, p.width, p.height,
			)
		}
		return nil
	})
}

func (p *Player) Play(name string) (Metadata, error) {
	bundle, err := p.store.Load(name)
	if err != nil {
		return Metadata{}, err
	}
	if bundle.Width != p.width || bundle.Height != p.height {
		return Metadata{}, fmt.Errorf(
			"animation geometry %dx%d does not match display %dx%d",
			bundle.Width, bundle.Height, p.width, p.height,
		)
	}
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(p.ctx)
	p.cancel = cancel
	p.generation++
	generation := p.generation
	p.mu.Unlock()
	go p.run(ctx, generation, bundle)
	return bundle.Metadata(name), nil
}

func (p *Player) Stop() {
	p.mu.Lock()
	p.generation++
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.mu.Unlock()
}

func (p *Player) run(ctx context.Context, generation uint64, bundle Bundle) {
	nextDeadline := time.Now()
	for loop := uint32(0); bundle.Loops == 0 || loop < bundle.Loops; loop++ {
		for _, item := range bundle.Frames {
			if ctx.Err() != nil {
				return
			}
			p.mu.Lock()
			if p.generation != generation {
				p.mu.Unlock()
				return
			}
			p.renderer.SubmitPlayback(item.Frame)
			p.mu.Unlock()
			nextDeadline = nextDeadline.Add(item.Duration)
			wait := time.Until(nextDeadline)
			if wait <= 0 {
				continue
			}
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			}
		}
	}
	p.mu.Lock()
	if p.generation == generation {
		p.cancel = nil
		_ = p.renderer.ActivateDefault()
	}
	p.mu.Unlock()
}
