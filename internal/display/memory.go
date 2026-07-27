package display

import (
	"context"
	"fmt"
	"sync"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

// Memory is a headless backend used on macOS, development machines and tests.
type Memory struct {
	width  int
	height int

	mu     sync.RWMutex
	latest frame.Frame
}

func NewMemory(width, height int) (*Memory, error) {
	if _, err := frame.ByteLen(width, height); err != nil {
		return nil, err
	}
	return &Memory{width: width, height: height}, nil
}

func (m *Memory) Geometry() (int, int) { return m.width, m.height }

func (m *Memory) Present(_ context.Context, next frame.Frame) error {
	if next.Width != m.width || next.Height != m.height {
		return fmt.Errorf("frame geometry %dx%d does not match display %dx%d", next.Width, next.Height, m.width, m.height)
	}
	copyOfPixels := append([]byte(nil), next.Pixels...)
	m.mu.Lock()
	m.latest = frame.Frame{Width: next.Width, Height: next.Height, Pixels: copyOfPixels}
	m.mu.Unlock()
	return nil
}

func (m *Memory) Close() error { return nil }

func (m *Memory) Latest() frame.Frame {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return frame.Frame{
		Width:  m.latest.Width,
		Height: m.latest.Height,
		Pixels: append([]byte(nil), m.latest.Pixels...),
	}
}
