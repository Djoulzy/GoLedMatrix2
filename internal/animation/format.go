// Package animation defines the preprocessed animation format stored by the
// server and the autonomous playback controller.
package animation

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

const (
	MediaType       = "application/vnd.goledmatrix.animation+gzip"
	magic           = "GLMANIM1"
	MaxFrames       = 10_000
	MaxDecodedBytes = 512 << 20
)

type TimedFrame struct {
	Frame    frame.Frame
	Duration time.Duration
}

type Bundle struct {
	Width  int
	Height int
	// Loops is the total number of repetitions. Zero means infinite.
	Loops  uint32
	Frames []TimedFrame
}

type Metadata struct {
	Name       string `json:"name"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	FrameCount int    `json:"frame_count"`
	Loops      uint32 `json:"loops"`
	DurationMS int64  `json:"duration_ms"`
}

func (b Bundle) Metadata(name string) Metadata {
	var duration time.Duration
	for _, item := range b.Frames {
		duration += item.Duration
	}
	return Metadata{
		Name: name, Width: b.Width, Height: b.Height,
		FrameCount: len(b.Frames), Loops: b.Loops,
		DurationMS: duration.Milliseconds(),
	}
}

func (b Bundle) Validate() error {
	frameBytes, err := frame.ByteLen(b.Width, b.Height)
	if err != nil {
		return err
	}
	if len(b.Frames) == 0 || len(b.Frames) > MaxFrames {
		return fmt.Errorf("animation frame count must be between 1 and %d", MaxFrames)
	}
	if frameBytes > MaxDecodedBytes/len(b.Frames) {
		return fmt.Errorf("decoded animation exceeds %d bytes", MaxDecodedBytes)
	}
	for index, item := range b.Frames {
		if item.Frame.Width != b.Width || item.Frame.Height != b.Height || len(item.Frame.Pixels) != frameBytes {
			return fmt.Errorf("frame %d has invalid geometry or payload", index)
		}
		if item.Duration < time.Millisecond || item.Duration > time.Hour {
			return fmt.Errorf("frame %d duration must be between 1ms and 1h", index)
		}
	}
	return nil
}

func Encode(writer io.Writer, bundle Bundle) error {
	if err := bundle.Validate(); err != nil {
		return err
	}
	compressed := gzip.NewWriter(writer)
	buffered := bufio.NewWriter(compressed)
	if _, err := buffered.WriteString(magic); err != nil {
		return err
	}
	header := []uint32{
		uint32(bundle.Width), uint32(bundle.Height),
		uint32(len(bundle.Frames)), bundle.Loops,
	}
	for _, value := range header {
		if err := binary.Write(buffered, binary.BigEndian, value); err != nil {
			return err
		}
	}
	for _, item := range bundle.Frames {
		durationMS := item.Duration.Milliseconds()
		if err := binary.Write(buffered, binary.BigEndian, uint32(durationMS)); err != nil {
			return err
		}
		if _, err := buffered.Write(item.Frame.Pixels); err != nil {
			return err
		}
	}
	if err := buffered.Flush(); err != nil {
		return err
	}
	return compressed.Close()
}

func Decode(reader io.Reader) (Bundle, error) {
	compressed, err := gzip.NewReader(reader)
	if err != nil {
		return Bundle{}, fmt.Errorf("open animation compression: %w", err)
	}
	defer compressed.Close()
	buffered := bufio.NewReader(compressed)
	signature := make([]byte, len(magic))
	if _, err := io.ReadFull(buffered, signature); err != nil {
		return Bundle{}, fmt.Errorf("read animation signature: %w", err)
	}
	if string(signature) != magic {
		return Bundle{}, errors.New("invalid animation signature")
	}
	header := make([]uint32, 4)
	for index := range header {
		if err := binary.Read(buffered, binary.BigEndian, &header[index]); err != nil {
			return Bundle{}, fmt.Errorf("read animation header: %w", err)
		}
	}
	if header[2] < 1 || header[2] > MaxFrames {
		return Bundle{}, fmt.Errorf("animation frame count must be between 1 and %d", MaxFrames)
	}
	width, height, count, loops := int(header[0]), int(header[1]), int(header[2]), header[3]
	frameBytes, err := frame.ByteLen(width, height)
	if err != nil {
		return Bundle{}, err
	}
	if frameBytes > MaxDecodedBytes/count {
		return Bundle{}, fmt.Errorf("decoded animation exceeds %d bytes", MaxDecodedBytes)
	}
	bundle := Bundle{Width: width, Height: height, Loops: loops, Frames: make([]TimedFrame, count)}
	for index := range bundle.Frames {
		var durationMS uint32
		if err := binary.Read(buffered, binary.BigEndian, &durationMS); err != nil {
			return Bundle{}, fmt.Errorf("read frame %d duration: %w", index, err)
		}
		pixels := make([]byte, frameBytes)
		if _, err := io.ReadFull(buffered, pixels); err != nil {
			return Bundle{}, fmt.Errorf("read frame %d pixels: %w", index, err)
		}
		next, _ := frame.New(width, height, pixels)
		bundle.Frames[index] = TimedFrame{Frame: next, Duration: time.Duration(durationMS) * time.Millisecond}
	}
	if _, err := buffered.ReadByte(); err != io.EOF {
		if err == nil {
			return Bundle{}, errors.New("animation contains trailing data")
		}
		return Bundle{}, fmt.Errorf("finish animation stream: %w", err)
	}
	if err := bundle.Validate(); err != nil {
		return Bundle{}, err
	}
	return bundle, nil
}
