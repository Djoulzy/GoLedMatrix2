package clientgui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/animation"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
)

func TestIndexAndServerInfo(t *testing.T) {
	matrix := &fakeMatrixClient{info: testInfo()}
	handler := newHandler(matrix)

	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK || !bytes.Contains(index.Body.Bytes(), []byte("Matrix control")) {
		t.Fatalf("index response = %d %q", index.Code, index.Body.String())
	}
	if index.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("index has no content security policy")
	}

	info := httptest.NewRecorder()
	handler.ServeHTTP(info, httptest.NewRequest(http.MethodGet, "/api/info", nil))
	if info.Code != http.StatusOK || !bytes.Contains(info.Body.Bytes(), []byte(`"width":2`)) {
		t.Fatalf("info response = %d %q", info.Code, info.Body.String())
	}
}

func TestColorAndClockCommands(t *testing.T) {
	matrix := &fakeMatrixClient{info: testInfo()}
	handler := newHandler(matrix)

	colorRequest := httptest.NewRequest(http.MethodPost, "/api/color", bytes.NewBufferString(`{"color":"#123456"}`))
	colorRequest.Header.Set("Content-Type", "application/json")
	colorResponse := httptest.NewRecorder()
	handler.ServeHTTP(colorResponse, colorRequest)
	if colorResponse.Code != http.StatusAccepted {
		t.Fatalf("color response = %d %q", colorResponse.Code, colorResponse.Body.String())
	}
	if got := matrix.sent.Pixels; !bytes.Equal(got, []byte{0x12, 0x34, 0x56, 0x12, 0x34, 0x56}) {
		t.Fatalf("color pixels = %v", got)
	}

	clockRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/clock",
		bytes.NewBufferString(`{"mode":"round","color1":"#010203","color2":"#aabbcc"}`),
	)
	clockRequest.Header.Set("Content-Type", "application/json")
	clockResponse := httptest.NewRecorder()
	handler.ServeHTTP(clockResponse, clockRequest)
	if clockResponse.Code != http.StatusAccepted {
		t.Fatalf("clock response = %d %q", clockResponse.Code, clockResponse.Body.String())
	}
	if matrix.clockMode != "round" || matrix.clockColor1 != "#010203" || matrix.clockColor2 != "#aabbcc" {
		t.Fatalf("clock command = %q %q %q", matrix.clockMode, matrix.clockColor1, matrix.clockColor2)
	}
}

func TestImageUpload(t *testing.T) {
	matrix := &fakeMatrixClient{info: testInfo()}
	handler := newHandler(matrix)
	source := image.NewRGBA(image.Rect(0, 0, 2, 1))
	source.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	source.Set(1, 0, color.RGBA{R: 4, G: 5, B: 6, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	body, contentType := multipartBody(t, "frame.png", encoded.Bytes(), map[string]string{})
	request := httptest.NewRequest(http.MethodPost, "/api/image", body)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("image response = %d %q", response.Code, response.Body.String())
	}
	if !bytes.Equal(matrix.sent.Pixels, []byte{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("image pixels = %v", matrix.sent.Pixels)
	}
}

func TestGIFUploadAndStoredAnimationPlayback(t *testing.T) {
	matrix := &fakeMatrixClient{info: testInfo()}
	handler := newHandler(matrix)
	animated := &gif.GIF{
		Image: []*image.Paletted{
			image.NewPaletted(image.Rect(0, 0, 2, 1), color.Palette{color.Black, color.White}),
		},
		Delay: []int{5},
	}
	animated.Image[0].Pix = []byte{0, 1}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, animated); err != nil {
		t.Fatal(err)
	}
	body, contentType := multipartBody(t, "demo.gif", encoded.Bytes(), map[string]string{
		"name": "demo", "loops": "2",
	})
	request := httptest.NewRequest(http.MethodPost, "/api/animations", body)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("GIF response = %d %q", response.Code, response.Body.String())
	}
	if matrix.uploadName != "demo" || matrix.upload.Loops != 2 || len(matrix.upload.Frames) != 1 {
		t.Fatalf("uploaded animation = name %q, loops %d, frames %d",
			matrix.uploadName, matrix.upload.Loops, len(matrix.upload.Frames))
	}

	playRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/animations/play",
		bytes.NewBufferString(`{"name":"demo"}`),
	)
	playRequest.Header.Set("Content-Type", "application/json")
	playResponse := httptest.NewRecorder()
	handler.ServeHTTP(playResponse, playRequest)
	if playResponse.Code != http.StatusAccepted || matrix.playName != "demo" {
		t.Fatalf("play response = %d %q, name %q", playResponse.Code, playResponse.Body.String(), matrix.playName)
	}
}

func multipartBody(
	t *testing.T,
	filename string,
	content []byte,
	fields map[string]string,
) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}

func testInfo() server.Info {
	return server.Info{
		ProtocolVersion: server.ProtocolVersion,
		Width:           2, Height: 1, PixelFormat: frame.PixelFormat, FrameBytes: 6,
		Backend: "memory",
	}
}

type fakeMatrixClient struct {
	info server.Info
	sent frame.Frame

	clockMode   string
	clockColor1 string
	clockColor2 string

	uploadName string
	upload     animation.Bundle
	playName   string
}

func (f *fakeMatrixClient) Info(context.Context) (server.Info, error) {
	return f.info, nil
}

func (f *fakeMatrixClient) Send(_ context.Context, next frame.Frame) (uint64, error) {
	f.sent = next
	return 3, nil
}

func (f *fakeMatrixClient) DisplayInfo(context.Context) error {
	return nil
}

func (f *fakeMatrixClient) DisplayClock(_ context.Context, mode, color1, color2 string) error {
	f.clockMode, f.clockColor1, f.clockColor2 = mode, color1, color2
	return nil
}

func (f *fakeMatrixClient) UploadAnimation(
	_ context.Context,
	name string,
	bundle animation.Bundle,
	_ bool,
) (animation.Metadata, error) {
	f.uploadName, f.upload = name, bundle
	return bundle.Metadata(name), nil
}

func (f *fakeMatrixClient) PlayAnimation(_ context.Context, name string) (animation.Metadata, error) {
	f.playName = name
	return animation.Metadata{Name: name, FrameCount: 1, DurationMS: int64((50 * time.Millisecond).Milliseconds())}, nil
}
