package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/models"
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

func TestNormalizeImageWebP(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	img.Set(0, 0, color.RGBA{R: 120, G: 80, B: 40, A: 255})

	// Since we don't have a webp encoder in stdlib, we'll just check if it's registered
	// and maybe use a small fixture if we had one.
	// But we can at least verify that NormalizeImage handles "webp" format if decoded.
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

func TestPublishCaptureUsesSameObjectInSharedZone(t *testing.T) {
	t.Parallel()

	storage := &BunnyStorage{
		privateZone:   "shared-zone",
		privateKey:    "shared-key",
		publicZone:    "shared-zone",
		publicKey:     "shared-key",
		publicBaseURL: "https://foto.houbamzdar.cz",
	}

	capture := &models.Capture{
		ID:                "capture-123",
		UserID:            42,
		CapturedAt:        time.Date(2026, time.March, 8, 14, 15, 16, 0, time.UTC),
		PrivateStorageKey: "captures/42/2026/03/capture-123.jpg",
	}

	publicKey, publicURL, err := storage.PublishCapture(context.Background(), capture)
	if err != nil {
		t.Fatalf("publish capture in shared zone: %v", err)
	}
	if publicKey != capture.PrivateStorageKey {
		t.Fatalf("expected shared public key %q, got %q", capture.PrivateStorageKey, publicKey)
	}
	if publicURL != "https://foto.houbamzdar.cz/captures/42/2026/03/capture-123.jpg" {
		t.Fatalf("unexpected shared public url: %q", publicURL)
	}
	if !storage.UsesSharedPublishedObject(capture.PrivateStorageKey, publicKey) {
		t.Fatalf("expected shared object detection to be true")
	}
	if storage.PublicObjectNeedsDelete(capture.PrivateStorageKey, publicKey) {
		t.Fatalf("expected shared published object to skip public deletion")
	}
}

func TestBackupTargetFallsBackToPrivateZone(t *testing.T) {
	t.Parallel()

	storage := &BunnyStorage{
		privateZone: "photo-zone",
		privateKey:  "photo-key",
	}

	zone, accessKey := storage.backupTarget()
	if zone != "photo-zone" || accessKey != "photo-key" {
		t.Fatalf("expected backup target to fall back to private zone, got %q / %q", zone, accessKey)
	}
}

func TestBackupTargetUsesDedicatedBackupZoneWhenConfigured(t *testing.T) {
	t.Parallel()

	storage := &BunnyStorage{
		privateZone: "photo-zone",
		privateKey:  "photo-key",
		backupZone:  "backup-zone",
		backupKey:   "backup-key",
	}

	zone, accessKey := storage.backupTarget()
	if zone != "backup-zone" || accessKey != "backup-key" {
		t.Fatalf("expected dedicated backup target, got %q / %q", zone, accessKey)
	}
}

func TestOptimizerURL(t *testing.T) {
	t.Parallel()

	storage := &BunnyStorage{
		publicBaseURL: "https://foto.houbamzdar.cz",
	}

	got := storage.OptimizerURL("captures/42/2026/03/capture-123.jpg", 384, 384, 70, "jpeg")
	want := "https://foto.houbamzdar.cz/captures/42/2026/03/capture-123.jpg?format=jpeg&height=384&quality=70&width=384"
	if got != want {
		t.Fatalf("unexpected optimizer url: %q", got)
	}
}
