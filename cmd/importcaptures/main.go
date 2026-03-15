package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"io/fs"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/disintegration/imageorient"
	"github.com/google/uuid"
	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/media"
	"github.com/houbamzdar/bff/internal/models"
	xdraw "golang.org/x/image/draw"
)

const (
	maxImageDimension = 1920
	jpegQuality       = 85
)

type importResult string

const (
	resultImported importResult = "imported"
	resultExisting importResult = "existing"
	resultSkipped  importResult = "skipped"
	resultFailed   importResult = "failed"
)

type importStats struct {
	totalFiles      int
	imageFiles      int
	imported        int
	existing        int
	skippedNoGPS    int
	skippedNotImage int
	failed          int
}

type importSummary struct {
	result     importResult
	reason     string
	captureID  string
	capturedAt time.Time
	latitude   float64
	longitude  float64
}

type extractedMetadata struct {
	OK         bool    `json:"ok"`
	Reason     string  `json:"reason"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	CapturedAt string  `json:"captured_at"`
}

type normalizedImageResult struct {
	OK          bool   `json:"ok"`
	Reason      string `json:"reason"`
	ContentType string `json:"content_type"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

func main() {
	dir := flag.String("dir", "", "source directory with photos")
	username := flag.String("user", "Standa", "target preferred_username")
	limit := flag.Int("limit", 0, "optional maximum number of image files to process")
	dryRun := flag.Bool("dry-run", false, "scan and normalize metadata without uploading or writing DB rows")
	verbose := flag.Bool("verbose", false, "print per-file details")
	metadataScript := flag.String("metadata-script", "tools/extract_capture_exif.py", "path to the Python EXIF extractor used for GPS and capture timestamps; HEIC/HEIF requires pillow-heif in PYTHONPATH")
	normalizeScript := flag.String("normalize-script", "tools/normalize_capture_image.py", "path to the Python normalizer used for formats unsupported by the Go image stack; HEIC/HEIF requires pillow-heif in PYTHONPATH")
	excludeExt := flag.String("exclude-ext", "", "comma-separated file extensions to skip, e.g. .heic,.heif")
	flag.Parse()

	if strings.TrimSpace(*dir) == "" {
		log.Fatal("--dir is required")
	}

	cfg := config.Load()
	if strings.TrimSpace(cfg.DBURL) == "" || strings.TrimSpace(cfg.DBToken) == "" {
		log.Fatal("DB_URL and DB_TOKEN must be set in the environment")
	}
	if strings.TrimSpace(cfg.BunnyPrivateZone) == "" || strings.TrimSpace(cfg.BunnyPrivateStorageKey) == "" || strings.TrimSpace(cfg.BunnyPublicZone) == "" || strings.TrimSpace(cfg.BunnyPublicStorageKey) == "" || strings.TrimSpace(cfg.BunnyPublicBaseURL) == "" {
		log.Fatal("Bunny storage environment variables are incomplete")
	}

	database, err := db.New(cfg)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	user, err := database.GetUserByPreferredUsername(*username)
	if err != nil {
		log.Fatalf("resolve user %q: %v", *username, err)
	}

	storage := media.NewBunnyStorage(cfg)
	if storage == nil || !storage.Enabled() {
		log.Fatal("Bunny storage is not configured")
	}

	paths, err := collectImagePaths(*dir, parseExcludedExtensions(*excludeExt))
	if err != nil {
		log.Fatalf("collect image paths: %v", err)
	}

	stats := importStats{
		totalFiles: len(paths),
	}

	if *limit > 0 && *limit < len(paths) {
		paths = paths[:*limit]
	}

	log.Printf("Import target: user=%s id=%d dry_run=%t files=%d", user.PreferredUsername, user.ID, *dryRun, len(paths))

	for index, photoPath := range paths {
		stats.imageFiles++

		summary, err := importOne(context.Background(), database, storage, user, photoPath, *metadataScript, *normalizeScript, *dryRun)
		if err != nil {
			stats.failed++
			log.Printf("[%d/%d] FAIL %s: %v", index+1, len(paths), photoPath, err)
			continue
		}

		switch summary.result {
		case resultImported:
			stats.imported++
		case resultExisting:
			stats.existing++
		case resultSkipped:
			if summary.reason == "missing_gps" {
				stats.skippedNoGPS++
			} else {
				stats.skippedNotImage++
			}
		default:
			stats.failed++
		}

		if *verbose || summary.result != resultSkipped {
			log.Printf("[%d/%d] %s %s (%s)", index+1, len(paths), strings.ToUpper(string(summary.result)), photoPath, summary.reason)
		}
	}

	log.Printf(
		"Summary: scanned=%d processed=%d imported=%d existing=%d skipped_no_gps=%d skipped_other=%d failed=%d dry_run=%t",
		stats.totalFiles,
		stats.imageFiles,
		stats.imported,
		stats.existing,
		stats.skippedNoGPS,
		stats.skippedNotImage,
		stats.failed,
		*dryRun,
	)
}

func collectImagePaths(root string, excludedExt map[string]struct{}) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !isSupportedImagePath(path, excludedExt) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func isSupportedImagePath(path string, excludedExt map[string]struct{}) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if _, skip := excludedExt[ext]; skip {
		return false
	}
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".heif":
		return true
	default:
		return false
	}
}

func parseExcludedExtensions(raw string) map[string]struct{} {
	if strings.TrimSpace(raw) == "" {
		return map[string]struct{}{}
	}

	excluded := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		ext := strings.ToLower(strings.TrimSpace(part))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		excluded[ext] = struct{}{}
	}
	return excluded
}

func importOne(ctx context.Context, database *db.DB, storage *media.BunnyStorage, user *models.User, photoPath, metadataScript, normalizeScript string, dryRun bool) (*importSummary, error) {
	raw, err := os.ReadFile(photoPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	fileInfo, err := os.Stat(photoPath)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	lat, lon, capturedAt, reason, err := extractMetadataWithPython(photoPath, metadataScript, filepath.Base(photoPath), fileInfo.ModTime())
	if err != nil {
		return nil, err
	}
	if reason != "" {
		return &importSummary{
			result: resultSkipped,
			reason: reason,
		}, nil
	}

	asset, err := normalizeAsset(photoPath, raw, normalizeScript)
	if err != nil {
		return nil, fmt.Errorf("normalize image: %w", err)
	}

	captureID := deterministicCaptureID(user.ID, filepath.Base(photoPath), raw)
	if _, err := database.GetCaptureForUser(captureID, user.ID); err == nil {
		return &importSummary{
			result:     resultExisting,
			reason:     "already_imported",
			captureID:  captureID,
			capturedAt: capturedAt,
			latitude:   lat,
			longitude:  lon,
		}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check existing capture: %w", err)
	}

	if dryRun {
		return &importSummary{
			result:     resultImported,
			reason:     "dry_run_ready",
			captureID:  captureID,
			capturedAt: capturedAt,
			latitude:   lat,
			longitude:  lon,
		}, nil
	}

	privateKey, err := storage.StorePrivateCapture(ctx, user.ID, captureID, capturedAt, asset)
	if err != nil {
		return nil, fmt.Errorf("store private capture: %w", err)
	}

	capture := &models.Capture{
		ID:                captureID,
		UserID:            user.ID,
		ClientLocalID:     deterministicClientLocalID(filepath.Base(photoPath), raw),
		OriginalFileName:  filepath.Base(photoPath),
		ContentType:       asset.ContentType,
		SizeBytes:         int64(len(asset.Bytes)),
		Width:             asset.Width,
		Height:            asset.Height,
		CapturedAt:        capturedAt,
		UploadedAt:        time.Now().UTC(),
		Latitude:          floatPointer(lat),
		Longitude:         floatPointer(lon),
		AccuracyMeters:    nil,
		Status:            "private",
		PrivateStorageKey: privateKey,
		CoordinatesFree:   false,
	}

	if err := database.CreateCapture(capture); err != nil {
		_ = storage.DeletePrivate(ctx, privateKey)
		return nil, fmt.Errorf("insert capture metadata: %w", err)
	}

	return &importSummary{
		result:     resultImported,
		reason:     "stored_private",
		captureID:  captureID,
		capturedAt: capturedAt,
		latitude:   lat,
		longitude:  lon,
	}, nil
}

func normalizeLikeMobileUpload(raw []byte) (*media.NormalizedAsset, error) {
	img, _, err := imageorient.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	targetImage := img

	if width > maxImageDimension || height > maxImageDimension {
		ratio := math.Min(float64(maxImageDimension)/float64(width), float64(maxImageDimension)/float64(height))
		nextWidth := int(math.Round(float64(width) * ratio))
		nextHeight := int(math.Round(float64(height) * ratio))
		dst := image.NewRGBA(image.Rect(0, 0, nextWidth, nextHeight))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, xdraw.Over, nil)
		targetImage = dst
		width = nextWidth
		height = nextHeight
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, targetImage, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}

	return &media.NormalizedAsset{
		Bytes:       out.Bytes(),
		ContentType: "image/jpeg",
		Extension:   ".jpg",
		Width:       width,
		Height:      height,
	}, nil
}

func normalizeAsset(photoPath string, raw []byte, normalizeScript string) (*media.NormalizedAsset, error) {
	switch strings.ToLower(filepath.Ext(photoPath)) {
	case ".heic", ".heif":
		return normalizeWithPython(photoPath, normalizeScript)
	default:
		return normalizeLikeMobileUpload(raw)
	}
}

func normalizeWithPython(photoPath, scriptPath string) (*media.NormalizedAsset, error) {
	outputFile, err := os.CreateTemp("", "capture-normalized-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("create temp output: %w", err)
	}
	outputPath := outputFile.Name()
	if err := outputFile.Close(); err != nil {
		return nil, fmt.Errorf("close temp output: %w", err)
	}
	defer os.Remove(outputPath)

	output, err := exec.Command("python3", scriptPath, photoPath, outputPath, fmt.Sprintf("%d", maxImageDimension), fmt.Sprintf("%d", jpegQuality)).Output()
	if err != nil {
		return nil, fmt.Errorf("normalize via python: %w", err)
	}

	var result normalizedImageResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("decode normalization json: %w", err)
	}
	if !result.OK {
		if result.Reason == "" {
			result.Reason = "normalize_unavailable"
		}
		return nil, errors.New(result.Reason)
	}

	normalizedBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("read normalized output: %w", err)
	}

	return &media.NormalizedAsset{
		Bytes:       normalizedBytes,
		ContentType: result.ContentType,
		Extension:   ".jpg",
		Width:       result.Width,
		Height:      result.Height,
	}, nil
}

func extractMetadataWithPython(photoPath, scriptPath, baseName string, fallback time.Time) (float64, float64, time.Time, string, error) {
	output, err := exec.Command("python3", scriptPath, photoPath).Output()
	if err != nil {
		return 0, 0, time.Time{}, "", fmt.Errorf("extract metadata via python: %w", err)
	}

	var metadata extractedMetadata
	if err := json.Unmarshal(output, &metadata); err != nil {
		return 0, 0, time.Time{}, "", fmt.Errorf("decode metadata json: %w", err)
	}
	if !metadata.OK {
		if metadata.Reason == "" {
			metadata.Reason = "metadata_unavailable"
		}
		return 0, 0, time.Time{}, metadata.Reason, nil
	}

	capturedAt := fallback.UTC()
	if strings.TrimSpace(metadata.CapturedAt) != "" {
		parsed, err := time.Parse(time.RFC3339, metadata.CapturedAt)
		if err == nil && !parsed.IsZero() {
			capturedAt = parsed.UTC()
		}
	} else if fromName, ok := parseTimestampFromFilename(baseName); ok {
		capturedAt = fromName.UTC()
	}

	return metadata.Latitude, metadata.Longitude, capturedAt, "", nil
}

func parseTimestampFromFilename(baseName string) (time.Time, bool) {
	name := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	if strings.HasPrefix(name, "IMG_") {
		name = strings.TrimPrefix(name, "IMG_")
	}
	parsed, err := time.ParseInLocation("20060102_150405", name, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func deterministicCaptureID(userID int64, baseName string, raw []byte) string {
	sum := sha256.Sum256(raw)
	name := fmt.Sprintf("import:%d:%s:%s", userID, baseName, hex.EncodeToString(sum[:]))
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(name)).String()
}

func deterministicClientLocalID(baseName string, raw []byte) string {
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("import:%s:%s", baseName, hex.EncodeToString(sum[:8]))
}

func floatPointer(value float64) *float64 {
	v := value
	return &v
}
