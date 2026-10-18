package media

import (
	"bytes"
	"context"
	"fmt"
	"github.com/disintegration/imageorient"
	"net/http"
	"strings"

	"github.com/houbamzdar/bff/internal/models"
)

// Dimensions are longest-edge bounds, not srcset width descriptors.
var captureImageProfiles = []struct {
	name                  string
	maxDimension, quality int
}{
	{"mapMarker", 128, 65},
	{"thumb", 640, 75},
	{"popup", 960, 78},
	{"lightbox", 1600, 82},
}

func captureImageVariantKey(key, variant string) string {
	return strings.TrimRight(key, "/") + ".variants-v1/" + variant + ".jpg"
}

func (b *BunnyStorage) CaptureImageVariantURLs(capture *models.Capture) map[string]string {
	if b == nil || !b.imageVariantsEnabled || capture == nil {
		return nil
	}
	key := ""
	if b.usesSharedZone() {
		key = capture.PrivateStorageKey
	}
	if capture.IsPubliclyShared() {
		key = capture.PublishedStorageKey()
	}
	if strings.TrimSpace(key) == "" {
		return nil
	}
	urls := make(map[string]string, len(captureImageProfiles))
	for _, profile := range captureImageProfiles {
		urls[profile.name] = b.PublicURL(captureImageVariantKey(key, profile.name))
	}
	return urls
}

func (b *BunnyStorage) storeCaptureImageVariants(ctx context.Context, zone, accessKey, key string, raw []byte, contentType string) error {
	img, _, err := imageorient.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("decode capture variants: %w", err)
	}
	for _, profile := range captureImageProfiles {
		prepared, err := prepareJPEG(img, profile.maxDimension, profile.quality)
		if err != nil {
			return fmt.Errorf("prepare %s: %w", profile.name, err)
		}
		if err := b.putObject(ctx, zone, accessKey, captureImageVariantKey(key, profile.name), prepared.Bytes, prepared.ContentType); err != nil {
			return err
		}
	}
	return nil
}

func (b *BunnyStorage) deleteCaptureImageVariants(ctx context.Context, zone, accessKey, key string) error {
	if key == "" {
		return nil
	}
	for _, profile := range captureImageProfiles {
		if err := b.deleteObject(ctx, zone, accessKey, captureImageVariantKey(key, profile.name)); err != nil {
			return err
		}
	}
	return nil
}

// BackfillCaptureImageVariants writes only derived objects. Originals and DB rows
// stay unchanged; rerunning it produces the same versioned object keys.
func (b *BunnyStorage) BackfillCaptureImageVariants(ctx context.Context, capture *models.Capture) error {
	if b == nil || capture == nil || capture.PrivateStorageKey == "" {
		return fmt.Errorf("capture storage key is required")
	}
	if err := b.ensureCaptureImageVariants(ctx, b.privateZone, b.privateKey, capture.PrivateStorageKey); err != nil {
		return err
	}
	if capture.PublicStorageKey != "" && !b.UsesSharedPublishedObject(capture.PrivateStorageKey, capture.PublicStorageKey) {
		return b.ensureCaptureImageVariants(ctx, b.publicZone, b.publicKey, capture.PublicStorageKey)
	}
	return nil
}

func (b *BunnyStorage) ensureCaptureImageVariants(ctx context.Context, zone, accessKey, key string) error {
	// lightbox is written last; its presence marks a completed versioned set.
	lastKey := captureImageVariantKey(key, "lightbox")
	// Bunny Storage supports ranged GET, but rejects HEAD even with a valid key.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://%s/%s/%s", b.host, zone, lastKey), nil)
	if err != nil {
		return err
	}
	req.Header.Set("AccessKey", accessKey)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
		return nil
	}
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("check variant: status %d", resp.StatusCode)
	}
	raw, contentType, err := b.getObject(ctx, zone, accessKey, key)
	if err != nil {
		return err
	}
	return b.storeCaptureImageVariants(ctx, zone, accessKey, key, raw, contentType)
}
