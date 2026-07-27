package display

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

// MappedGeometry returns the logical canvas size exposed by
// rpi-rgb-led-matrix after applying PixelMapperConfig. Mappers are applied in
// order, as they are by RGBMatrix::Impl::ApplyNamedPixelMappers.
func MappedGeometry(cols, rows, chain, parallel int, mapperConfig string) (int, int, error) {
	width, err := multiplyDimension(cols, chain)
	if err != nil {
		return 0, 0, fmt.Errorf("physical matrix width: %w", err)
	}
	height, err := multiplyDimension(rows, parallel)
	if err != nil {
		return 0, 0, fmt.Errorf("physical matrix height: %w", err)
	}

	for _, specification := range strings.Split(mapperConfig, ";") {
		specification = strings.TrimSpace(specification)
		if specification == "" {
			continue
		}
		name, parameter, _ := strings.Cut(specification, ":")
		name = strings.TrimSpace(name)
		parameter = strings.TrimSpace(parameter)
		if name == "" {
			return 0, 0, fmt.Errorf("pixel mapper %q has no name", specification)
		}

		switch strings.ToLower(name) {
		case "v-mapper":
			if parameter != "" && !strings.EqualFold(parameter, "Z") {
				return 0, 0, fmt.Errorf("V-mapper parameter must be Z, got %q", parameter)
			}
			width = width * parallel / chain
			height = height * chain / parallel

		case "u-mapper":
			if chain < 2 || chain%2 != 0 {
				return 0, 0, fmt.Errorf("U-mapper requires an even ChainLength of at least 2")
			}
			width = (width / 64) * 32
			height *= 2

		case "stacktorow":
			for _, character := range parameter {
				if character != 'Z' && character != 'z' &&
					character != 'F' && character != 'f' &&
					character != ',' && character != ' ' {
					return 0, 0, fmt.Errorf("StackToRow parameter contains %q; only Z and F are supported", character)
				}
			}
			width *= parallel
			height /= parallel

		case "rotate":
			angle := 0
			if parameter != "" {
				angle, err = strconv.Atoi(parameter)
				if err != nil || angle%90 != 0 {
					return 0, 0, fmt.Errorf("Rotate parameter must be a multiple of 90, got %q", parameter)
				}
			}
			angle = (angle%360 + 360) % 360
			if angle == 90 || angle == 270 {
				width, height = height, width
			}

		case "mirror":
			if parameter != "" && !strings.EqualFold(parameter, "H") && !strings.EqualFold(parameter, "V") {
				return 0, 0, fmt.Errorf("Mirror parameter must be H or V, got %q", parameter)
			}

		default:
			return 0, 0, fmt.Errorf("pixel mapper %q is not supported by the simulator", name)
		}

		if _, err := frame.ByteLen(width, height); err != nil {
			return 0, 0, fmt.Errorf("geometry after pixel mapper %q: %w", name, err)
		}
	}

	if _, err := frame.ByteLen(width, height); err != nil {
		return 0, 0, err
	}
	return width, height, nil
}

func multiplyDimension(value, multiplier int) (int, error) {
	if value <= 0 || multiplier <= 0 {
		return 0, fmt.Errorf("dimensions must be positive")
	}
	const maxInt = int(^uint(0) >> 1)
	if value > maxInt/multiplier {
		return 0, fmt.Errorf("dimension is too large")
	}
	return value * multiplier, nil
}
