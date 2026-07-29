package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/client"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

func main() {
	serverURL := flag.String("server", "http://localhost:8080", "matrix server URL")
	imagePath := flag.String("image", "", "PNG or JPEG image with the matrix dimensions")
	solid := flag.String("color", "", "solid color in #RRGGBB form")
	showInfo := flag.Bool("show-info", false, "temporarily display server connection information")
	showClock := flag.String("clock", "", "clock display mode: simple, fancy, or round")
	clockColor1 := flag.String("clock-color1", "", "clock primary color in #RRGGBB form")
	clockColor2 := flag.String("clock-color2", "", "clock secondary color in #RRGGBB form")
	timeout := flag.Duration("timeout", 5*time.Second, "HTTP request timeout")
	flag.Parse()

	selectedActions := 0
	if *imagePath != "" {
		selectedActions++
	}
	if *solid != "" {
		selectedActions++
	}
	if *showInfo {
		selectedActions++
	}
	if *showClock != "" {
		selectedActions++
	}
	if selectedActions != 1 {
		log.Fatal("provide exactly one of -image, -color, -show-info, or -clock")
	}
	if *showClock == "" && (*clockColor1 != "" || *clockColor2 != "") {
		log.Fatal("-clock-color1 and -clock-color2 require -clock")
	}
	api, err := client.New(*serverURL, *timeout)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	if *showInfo {
		if err := api.DisplayInfo(ctx); err != nil {
			log.Fatalf("display server information: %v", err)
		}
		fmt.Println("server information display requested")
		return
	}
	if *showClock != "" {
		if err := api.DisplayClock(ctx, *showClock, *clockColor1, *clockColor2); err != nil {
			log.Fatalf("display clock: %v", err)
		}
		fmt.Printf("%s clock display requested\n", *showClock)
		return
	}
	info, err := api.Info(ctx)
	if err != nil {
		log.Fatalf("query server: %v", err)
	}
	if info.ProtocolVersion != "1" || info.PixelFormat != frame.PixelFormat {
		log.Fatalf("unsupported server protocol version=%q pixel_format=%q", info.ProtocolVersion, info.PixelFormat)
	}

	var next frame.Frame
	if *imagePath != "" {
		next, err = loadImage(*imagePath)
	} else {
		next, err = solidFrame(info.Width, info.Height, *solid)
	}
	if err != nil {
		log.Fatal(err)
	}
	if next.Width != info.Width || next.Height != info.Height {
		log.Fatalf("image is %dx%d; server requires %dx%d", next.Width, next.Height, info.Width, info.Height)
	}
	sequence, err := api.Send(ctx, next)
	if err != nil {
		log.Fatalf("send frame: %v", err)
	}
	fmt.Printf("frame %d accepted (%dx%d %s)\n", sequence, next.Width, next.Height, frame.PixelFormat)
}

func loadImage(path string) (frame.Frame, error) {
	file, err := os.Open(path)
	if err != nil {
		return frame.Frame{}, fmt.Errorf("open image: %w", err)
	}
	defer file.Close()
	src, _, err := image.Decode(file)
	if err != nil {
		return frame.Frame{}, fmt.Errorf("decode image: %w", err)
	}
	return frame.FromImage(src)
}

func solidFrame(width, height int, value string) (frame.Frame, error) {
	value = strings.TrimPrefix(value, "#")
	if len(value) != 6 {
		return frame.Frame{}, errors.New("color must use #RRGGBB form")
	}
	packed, err := strconv.ParseUint(value, 16, 24)
	if err != nil {
		return frame.Frame{}, errors.New("color must use #RRGGBB form")
	}
	size, err := frame.ByteLen(width, height)
	if err != nil {
		return frame.Frame{}, err
	}
	pixels := make([]byte, size)
	r, g, b := byte(packed>>16), byte(packed>>8), byte(packed)
	for offset := 0; offset < size; offset += 3 {
		pixels[offset], pixels[offset+1], pixels[offset+2] = r, g, b
	}
	return frame.New(width, height, pixels)
}
