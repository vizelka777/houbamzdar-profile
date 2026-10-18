package enrichment

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/houbamzdar/bff/internal/media"
	"github.com/houbamzdar/bff/internal/models"
)

const (
	validatorImageMaxDimension = 384
	validatorJPEGQuality       = 70
)

func PrepareValidatorImageSource(ctx context.Context, storage *media.BunnyStorage, capture *models.Capture, reviewMode AIReviewMode) (*AIValidatorImageSource, error) {
	if storage == nil {
		return nil, fmt.Errorf("storage is not configured")
	}
	if capture == nil {
		return nil, fmt.Errorf("capture is required")
	}
	if capture.PrivateStorageKey == "" {
		return nil, fmt.Errorf("capture has no private storage key")
	}

	maxDimension, quality := validatorImageProfile(reviewMode)
	if !storage.CanReadPrivate() {
		return nil, fmt.Errorf("private storage is not configured")
	}
	raw, contentType, err := storage.ReadPrivateCapture(ctx, capture.PrivateStorageKey)
	if err != nil {
		return nil, fmt.Errorf("read validator image: %w", err)
	}
	prepared, err := media.PrepareValidatorImage(raw, contentType, maxDimension, quality)
	if err != nil {
		return nil, err
	}

	return &AIValidatorImageSource{
		Data:     base64.StdEncoding.EncodeToString(prepared.Bytes),
		MimeType: prepared.ContentType,
	}, nil
}

func validatorImageProfile(reviewMode AIReviewMode) (int, int) {
	switch reviewMode {
	case AIReviewModeModeratorRecheck:
		return validatorImageMaxDimension, validatorJPEGQuality
	default:
		return validatorImageMaxDimension, validatorJPEGQuality
	}
}
