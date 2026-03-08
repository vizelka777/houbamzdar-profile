package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"
)

func TestNormalizeImageJPEG(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	img.Set(0, 0, color.RGBA{R: 120, G: 80, B: 40, A: 255})

	var input bytes.Buffer
	if err := jpeg.Encode(&input, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode jpeg fixture: %v", err)
	}

	asset, err := NormalizeImage(input.Bytes(), "image/jpeg")
	if err != nil {
		t.Fatalf("normalize jpeg: %v", err)
	}

	if asset.ContentType != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %q", asset.ContentType)
	}
	if asset.Extension != ".jpg" {
		t.Fatalf("expected .jpg extension, got %q", asset.Extension)
	}
	if asset.Width != 8 || asset.Height != 6 {
		t.Fatalf("unexpected dimensions %dx%d", asset.Width, asset.Height)
	}
	if len(asset.Bytes) == 0 {
		t.Fatalf("expected normalized bytes to be present")
	}
}

func TestCaptureKeys(t *testing.T) {
	t.Parallel()

	capturedAt := time.Date(2026, time.March, 8, 14, 15, 16, 0, time.UTC)

	privateKey := PrivateCaptureKey(42, "capture-123", capturedAt, ".jpg")
	if privateKey != "captures/42/2026/03/capture-123.jpg" {
		t.Fatalf("unexpected private key: %q", privateKey)
	}

	publicKey := PublicCaptureKey(42, "capture-123", capturedAt, ".jpg")
	if publicKey != "captures/published/42/2026/03/capture-123.jpg" {
		t.Fatalf("unexpected public key: %q", publicKey)
	}
}
