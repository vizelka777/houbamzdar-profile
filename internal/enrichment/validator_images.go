package enrichment

import (
	"fmt"

	"github.com/houbamzdar/bff/internal/media"
	"github.com/houbamzdar/bff/internal/models"
)

const (
	validatorImageMaxDimension = 384
	validatorJPEGQuality       = 70
)

func PrepareValidatorImageSource(storage *media.BunnyStorage, capture *models.Capture, reviewMode AIReviewMode) (*AIValidatorImageSource, error) {
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
	imageURL := storage.OptimizerURL(capture.PrivateStorageKey, maxDimension, maxDimension, quality, "jpeg")
	if imageURL == "" {
		return nil, fmt.Errorf("capture has no optimizer url")
	}

	return &AIValidatorImageSource{
		URL: imageURL,
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
