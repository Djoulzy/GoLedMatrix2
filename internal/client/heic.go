package client

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var ErrHEICUnavailable = errors.New("HEIC decoder unavailable: macOS requires sips; other systems require libheif (heif-dec or heif-convert) on the client machine")

// Detect the HEIF file-type box, not the upload filename or MIME type. Phone
// uploads often arrive as application/octet-stream and use uppercase suffixes.
func isHEIF(header []byte) bool {
	if len(header) < 12 || string(header[4:8]) != "ftyp" {
		return false
	}
	for offset := 8; offset+4 <= len(header); offset += 4 {
		if offset == 12 { // minor version, not a compatible brand
			continue
		}
		switch string(header[offset : offset+4]) {
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs", "mif1", "msf1":
			return true
		}
	}
	return false
}

func heicConverter() (string, bool, error) {
	if runtime.GOOS == "darwin" {
		if program, err := exec.LookPath("/usr/bin/sips"); err == nil {
			return program, true, nil
		}
	}
	for _, name := range []string{"heif-dec", "heif-convert"} {
		if program, err := exec.LookPath(name); err == nil {
			return program, false, nil
		}
	}
	return "", false, ErrHEICUnavailable
}

func decodeHEIC(ctx context.Context, reader io.Reader) (image.Image, error) {
	program, native, err := heicConverter()
	if err != nil {
		return nil, err
	}
	return convertHEIC(ctx, reader, program, native)
}

func convertHEIC(ctx context.Context, reader io.Reader, program string, native bool) (image.Image, error) {
	directory, err := os.MkdirTemp("", "goledmatrix-heic-*")
	if err != nil {
		return nil, fmt.Errorf("create HEIC working directory: %w", err)
	}
	defer os.RemoveAll(directory)
	inputPath, outputPath := filepath.Join(directory, "input.heic"), filepath.Join(directory, "output.png")
	input, err := os.Create(inputPath)
	if err != nil {
		return nil, err
	}
	const maxHEICBytes = 256 << 20
	n, copyErr := io.Copy(input, io.LimitReader(reader, maxHEICBytes+1))
	closeErr := input.Close()
	if copyErr != nil {
		return nil, fmt.Errorf("read HEIC: %w", copyErr)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if n > maxHEICBytes {
		return nil, fmt.Errorf("HEIC upload exceeds 256 MiB")
	}
	args := []string{inputPath, outputPath}
	if native {
		args = []string{"-s", "format", "png", inputPath, "--out", outputPath}
	}
	conversionCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(conversionCtx, program, args...)
	command.Stdout = io.Discard
	stderr := &conversionLog{}
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if conversionCtx.Err() != nil {
			return nil, fmt.Errorf("HEIC conversion cancelled or timed out: %w", conversionCtx.Err())
		}
		return nil, fmt.Errorf("decode HEIC: %w: %s", err, strings.TrimSpace(string(stderr.data)))
	}
	output, err := os.Open(outputPath)
	if os.IsNotExist(err) && !native {
		// libheif adds a numeric suffix for files containing several images.
		output, err = os.Open(filepath.Join(directory, "output-1.png"))
	}
	if err != nil {
		return nil, fmt.Errorf("read converted HEIC: %w", err)
	}
	defer output.Close()
	return decodeStillImage(output)
}

// Keep conversion diagnostics bounded even for a malformed upload.
type conversionLog struct{ data []byte }

func (l *conversionLog) Write(data []byte) (int, error) {
	n := len(data)
	remaining := 4096 - len(l.data)
	l.data = append(l.data, data[:min(n, remaining)]...)
	return n, nil
}
