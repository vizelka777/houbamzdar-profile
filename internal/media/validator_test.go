package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestPrepareValidatorImageDownscalesToMaxDimension(t *testing.T) {
	t.Parallel()

	src := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	for y := 0; y < 900; y++ {
		for x := 0; x < 1600; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 120, A: 255})
		}
	}

	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode source jpeg: %v", err)
	}

	prepared, err := PrepareValidatorImage(raw.Bytes(), "image/jpeg", 384, 70)
	if err != nil {
		t.Fatalf("prepare validator image: %v", err)
	}

	if prepared.ContentType != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %q", prepared.ContentType)
	}
	if prepared.Width != 384 || prepared.Height != 216 {
		t.Fatalf("expected 384x216, got %dx%d", prepared.Width, prepared.Height)
	}
	if len(prepared.Bytes) == 0 {
		t.Fatal("expected encoded bytes")
	}
}

func TestPrepareValidatorImageKeepsSmallerImages(t *testing.T) {
	t.Parallel()

	src := image.NewRGBA(image.Rect(0, 0, 320, 200))
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode source jpeg: %v", err)
	}

	prepared, err := PrepareValidatorImage(raw.Bytes(), "image/jpeg", 384, 70)
	if err != nil {
		t.Fatalf("prepare validator image: %v", err)
	}

	if prepared.Width != 320 || prepared.Height != 200 {
		t.Fatalf("expected 320x200, got %dx%d", prepared.Width, prepared.Height)
	}
}
