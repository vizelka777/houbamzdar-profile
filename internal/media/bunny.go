package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/models"
	_ "image/jpeg"
	_ "image/png"
)

type NormalizedAsset struct {
	Bytes       []byte
	ContentType string
	Extension   string
	Width       int
	Height      int
}

type BunnyStorage struct {
	host          string
	privateZone   string
	privateKey    string
	publicZone    string
	publicKey     string
	publicBaseURL string
	httpClient    *http.Client
}

func NewBunnyStorage(cfg *config.Config) *BunnyStorage {
	if cfg.BunnyPrivateZone == "" || cfg.BunnyPrivateStorageKey == "" || cfg.BunnyPublicZone == "" || cfg.BunnyPublicStorageKey == "" || cfg.BunnyPublicBaseURL == "" {
		return nil
	}

	return &BunnyStorage{
		host:          cfg.BunnyStorageHost,
		privateZone:   cfg.BunnyPrivateZone,
		privateKey:    cfg.BunnyPrivateStorageKey,
		publicZone:    cfg.BunnyPublicZone,
		publicKey:     cfg.BunnyPublicStorageKey,
		publicBaseURL: strings.TrimRight(cfg.BunnyPublicBaseURL, "/"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (b *BunnyStorage) Enabled() bool {
	return b != nil
}

func NormalizeImage(raw []byte, contentType string) (*NormalizedAsset, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty image payload")
	}

	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()
	var out bytes.Buffer

	switch format {
	case "jpeg":
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 90}); err != nil {
			return nil, fmt.Errorf("encode jpeg: %w", err)
		}
		return &NormalizedAsset{
			Bytes:       out.Bytes(),
			ContentType: "image/jpeg",
			Extension:   ".jpg",
			Width:       bounds.Dx(),
			Height:      bounds.Dy(),
		}, nil
	case "png":
		if err := png.Encode(&out, img); err != nil {
			return nil, fmt.Errorf("encode png: %w", err)
		}
		return &NormalizedAsset{
			Bytes:       out.Bytes(),
			ContentType: "image/png",
			Extension:   ".png",
			Width:       bounds.Dx(),
			Height:      bounds.Dy(),
		}, nil
	default:
		detected := http.DetectContentType(raw)
		if contentType != "" {
			detected = contentType
		}
		return nil, fmt.Errorf("unsupported image format %q (%s)", format, detected)
	}
}

func (b *BunnyStorage) StorePrivateCapture(ctx context.Context, userID int64, captureID string, capturedAt time.Time, asset *NormalizedAsset) (string, error) {
	key := PrivateCaptureKey(userID, captureID, capturedAt, asset.Extension)
	if err := b.putObject(ctx, b.privateZone, b.privateKey, key, asset.Bytes, asset.ContentType); err != nil {
		return "", err
	}
	return key, nil
}

func (b *BunnyStorage) PublishCapture(ctx context.Context, capture *models.Capture) (string, string, error) {
	content, contentType, err := b.getObject(ctx, b.privateZone, b.privateKey, capture.PrivateStorageKey)
	if err != nil {
		return "", "", err
	}

	extension := path.Ext(capture.PrivateStorageKey)
	if extension == "" {
		extension = extensionFromContentType(contentType)
	}
	publicKey := PublicCaptureKey(capture.UserID, capture.ID, capture.CapturedAt, extension)

	if err := b.putObject(ctx, b.publicZone, b.publicKey, publicKey, content, contentType); err != nil {
		return "", "", err
	}

	return publicKey, fmt.Sprintf("%s/%s", b.publicBaseURL, publicKey), nil
}

func (b *BunnyStorage) DeletePrivate(ctx context.Context, key string) error {
	return b.deleteObject(ctx, b.privateZone, b.privateKey, key)
}

func (b *BunnyStorage) DeletePublic(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	return b.deleteObject(ctx, b.publicZone, b.publicKey, key)
}

func (b *BunnyStorage) PublicURL(key string) string {
	if key == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s", b.publicBaseURL, key)
}

func PrivateCaptureKey(userID int64, captureID string, capturedAt time.Time, extension string) string {
	return path.Join(
		"captures",
		fmt.Sprintf("%d", userID),
		capturedAt.UTC().Format("2006"),
		capturedAt.UTC().Format("01"),
		captureID,
		"original"+extension,
	)
}

func PublicCaptureKey(userID int64, captureID string, capturedAt time.Time, extension string) string {
	return path.Join(
		"captures",
		"published",
		fmt.Sprintf("%d", userID),
		capturedAt.UTC().Format("2006"),
		capturedAt.UTC().Format("01"),
		captureID+extension,
	)
}

func extensionFromContentType(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	default:
		return ".jpg"
	}
}

func (b *BunnyStorage) putObject(ctx context.Context, zone, accessKey, objectKey string, payload []byte, contentType string) error {
	endpoint := fmt.Sprintf("https://%s/%s/%s", b.host, zone, objectKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create put request: %w", err)
	}

	req.Header.Set("AccessKey", accessKey)
	req.Header.Set("Content-Type", contentType)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("put object %s: %w", objectKey, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("put object %s: status %d: %s", objectKey, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

func (b *BunnyStorage) getObject(ctx context.Context, zone, accessKey, objectKey string) ([]byte, string, error) {
	endpoint := fmt.Sprintf("https://%s/%s/%s", b.host, zone, objectKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create get request: %w", err)
	}

	req.Header.Set("AccessKey", accessKey)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("get object %s: %w", objectKey, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, "", fmt.Errorf("get object %s: status %d: %s", objectKey, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read object %s: %w", objectKey, err)
	}

	return body, resp.Header.Get("Content-Type"), nil
}

func (b *BunnyStorage) deleteObject(ctx context.Context, zone, accessKey, objectKey string) error {
	endpoint := fmt.Sprintf("https://%s/%s/%s", b.host, zone, objectKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}

	req.Header.Set("AccessKey", accessKey)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete object %s: %w", objectKey, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("delete object %s: status %d: %s", objectKey, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
