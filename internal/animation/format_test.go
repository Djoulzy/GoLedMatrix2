package animation

import (
	"bytes"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

func TestEncodeDecode(t *testing.T) {
	one, _ := frame.New(2, 1, []byte{1, 2, 3, 4, 5, 6})
	two, _ := frame.New(2, 1, []byte{7, 8, 9, 10, 11, 12})
	want := Bundle{
		Width: 2, Height: 1, Loops: 3,
		Frames: []TimedFrame{
			{Frame: one, Duration: 20 * time.Millisecond},
			{Frame: two, Duration: 40 * time.Millisecond},
		},
	}
	var encoded bytes.Buffer
	if err := Encode(&encoded, want); err != nil {
		t.Fatal(err)
	}
	got, err := Decode(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 2 || got.Height != 1 || got.Loops != 3 || len(got.Frames) != 2 {
		t.Fatalf("decoded bundle = %+v", got)
	}
	if !bytes.Equal(got.Frames[1].Frame.Pixels, two.Pixels) ||
		got.Frames[1].Duration != 40*time.Millisecond {
		t.Fatalf("decoded second frame = %+v", got.Frames[1])
	}
}

func TestStorePersistsAnimation(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	next, _ := frame.New(1, 1, []byte{1, 2, 3})
	bundle := Bundle{
		Width: 1, Height: 1, Loops: 1,
		Frames: []TimedFrame{{Frame: next, Duration: time.Second}},
	}
	var encoded bytes.Buffer
	if err := Encode(&encoded, bundle); err != nil {
		t.Fatal(err)
	}
	metadata, err := store.Save("demo", &encoded, nil)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Name != "demo" || metadata.FrameCount != 1 {
		t.Fatalf("metadata = %+v", metadata)
	}
	if _, err := store.Load("demo"); err != nil {
		t.Fatal(err)
	}
}
