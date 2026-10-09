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
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/client"
	"github.com/Djoulzy/GoLedMatrix2/internal/clientgui"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/marquee"
)

func main() {
	serverURL := flag.String("server", "http://localhost:8080", "matrix server URL")
	imagePath := flag.String("image", "", "PNG or JPEG image with the matrix dimensions")
	solid := flag.String("color", "", "solid color in #RRGGBB form")
	showInfo := flag.Bool("show-info", false, "temporarily display server connection information")
	showClock := flag.String("clock", "", "clock display mode: simple, fancy, or round")
	clockColor1 := flag.String("clock-color1", "", "clock primary color in #RRGGBB form")
	clockColor2 := flag.String("clock-color2", "", "clock secondary color in #RRGGBB form")
	marqueeText := flag.String("marquee", "", "text to scroll on the server")
	marqueeFont := flag.String("marquee-font", "regular", "marquee font name or path relative to assets/ttf (see -list-marquee-fonts)")
	listMarqueeFonts := flag.Bool("list-marquee-fonts", false, "list all bundled marquee fonts and exit (no server required)")
	marqueeSize := flag.Int("marquee-size", 16, "marquee font size in pixels (2-256)")
	marqueeColor := flag.String("marquee-color", "#ffffff", "marquee text color in #RRGGBB form")
	marqueeSpeed := flag.Float64("marquee-speed", 30, "marquee speed in pixels per second (1-500)")
	marqueeBounce := flag.Bool("marquee-bounce", false, "bounce horizontally instead of wrapping")
	marqueeVerticalBounce := flag.Bool("marquee-vertical-bounce", false, "add sinusoidal up-and-down movement while scrolling")
	marqueeVerticalBounceSeconds := flag.Float64("marquee-vertical-bounce-seconds", 4, "duration of a complete vertical round trip in seconds (0.1-3600)")
	marqueeCycle := flag.String("marquee-cycle", "", "rainbow or comma-separated #RRGGBB colors (2-16)")
	marqueeCycleSeconds := flag.Float64("marquee-cycle-seconds", 6, "duration of a complete marquee color cycle in seconds")
	gifPath := flag.String("gif", "", "GIF animation to preprocess, store, and play")
	animationName := flag.String("animation-name", "", "stored animation name (defaults to the GIF filename)")
	playAnimation := flag.String("play-animation", "", "play a previously stored animation")
	animationLoops := flag.Int("animation-loops", -1, "total loops: 0 infinite, -1 uses the GIF value")
	gui := flag.Bool("gui", false, "start the browser-based graphical client")
	guiListen := flag.String("gui-listen", "127.0.0.1:8090", "GUI HTTP listen address")
	timeout := flag.Duration("timeout", 2*time.Minute, "HTTP request timeout")
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
	if *marqueeText != "" {
		selectedActions++
	}
	if *listMarqueeFonts {
		selectedActions++
	}
	if *gifPath != "" {
		selectedActions++
	}
	if *playAnimation != "" {
		selectedActions++
	}
	if *gui {
		selectedActions++
	}
	if selectedActions != 1 {
		log.Fatal("provide exactly one of -image, -color, -show-info, -clock, -marquee, -list-marquee-fonts, -gif, -play-animation, or -gui")
	}
	flag.Visit(func(item *flag.Flag) {
		if strings.HasPrefix(item.Name, "marquee-") && *marqueeText == "" {
			log.Fatalf("-%s requires -marquee", item.Name)
		}
	})
	if *showClock == "" && (*clockColor1 != "" || *clockColor2 != "") {
		log.Fatal("-clock-color1 and -clock-color2 require -clock")
	}
	if *animationLoops < -1 {
		log.Fatal("-animation-loops must be -1, 0, or a positive value")
	}
	if int64(*animationLoops) > int64(^uint32(0)) {
		log.Fatal("-animation-loops is too large")
	}
	if *gifPath == "" && (*animationName != "" || *animationLoops != -1) {
		log.Fatal("-animation-name and -animation-loops require -gif")
	}
	if *listMarqueeFonts {
		fonts, err := marquee.Fonts()
		if err != nil {
			log.Fatal(err)
		}
		for _, font := range fonts {
			fmt.Printf("%-32s %s (%s)\n", font.Name, font.Label, font.Category)
		}
		return
	}
	api, err := client.New(*serverURL, *timeout)
	if err != nil {
		log.Fatal(err)
	}
	if *gui {
		handler, err := clientgui.New(api)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("graphical client available at http://%s\n", *guiListen)
		if err := http.ListenAndServe(*guiListen, handler); err != nil {
			log.Fatalf("serve graphical client: %v", err)
		}
		return
	}
	ctx := context.Background()
	if *marqueeText != "" {
		cycle, err := marquee.ParseCycle(*marqueeCycle)
		if err != nil {
			log.Fatal(err)
		}
		verticalSeconds := 0.0
		if *marqueeVerticalBounce {
			verticalSeconds = *marqueeVerticalBounceSeconds
		}
		state, err := api.DisplayMarquee(ctx, marquee.Options{
			Text: *marqueeText, Font: *marqueeFont, Size: *marqueeSize,
			Color: *marqueeColor, Speed: *marqueeSpeed, Bounce: *marqueeBounce,
			VerticalBounce: *marqueeVerticalBounce, VerticalBounceSeconds: verticalSeconds,
			ColorCycle: cycle, CycleSeconds: *marqueeCycleSeconds,
		})
		if err != nil {
			log.Fatalf("display marquee: %v", err)
		}
		motion := "scrolling"
		if state.Bounce {
			motion = "bouncing"
		}
		if state.VerticalBounce {
			motion += fmt.Sprintf(" with vertical sine wave (%.1f s)", state.VerticalBounceSeconds)
		}
		fmt.Printf("marquee %q %s (%s, %d px, %.1f px/s)\n", state.Text, motion, state.Font, state.Size, state.Speed)
		return
	}
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
	if *playAnimation != "" {
		metadata, err := api.PlayAnimation(ctx, *playAnimation)
		if err != nil {
			log.Fatalf("play animation: %v", err)
		}
		fmt.Printf("animation %q playing (%d frames, %d ms)\n",
			metadata.Name, metadata.FrameCount, metadata.DurationMS)
		return
	}
	info, err := api.Info(ctx)
	if err != nil {
		log.Fatalf("query server: %v", err)
	}
	if info.ProtocolVersion != "1" || info.PixelFormat != frame.PixelFormat {
		log.Fatalf("unsupported server protocol version=%q pixel_format=%q", info.ProtocolVersion, info.PixelFormat)
	}
	if *gifPath != "" {
		name := *animationName
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(*gifPath), filepath.Ext(*gifPath))
		}
		file, err := os.Open(*gifPath)
		if err != nil {
			log.Fatalf("open GIF: %v", err)
		}
		bundle, prepareErr := client.PrepareGIF(file, info.Width, info.Height)
		closeErr := file.Close()
		if prepareErr != nil {
			log.Fatalf("prepare GIF: %v", prepareErr)
		}
		if closeErr != nil {
			log.Fatalf("close GIF: %v", closeErr)
		}
		if *animationLoops >= 0 {
			bundle.Loops = uint32(*animationLoops)
		}
		metadata, err := api.UploadAnimation(ctx, name, bundle, true)
		if err != nil {
			log.Fatalf("upload animation: %v", err)
		}
		fmt.Printf("animation %q stored and playing (%d frames, %d ms, loops=%d)\n",
			metadata.Name, metadata.FrameCount, metadata.DurationMS, metadata.Loops)
		return
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
