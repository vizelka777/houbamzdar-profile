package media

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	imagedraw "image/draw"
	"image/jpeg"
	"net/http"

	"github.com/disintegration/imageorient"
	xdraw "golang.org/x/image/draw"
)

type ValidatorImage struct {
	Bytes       []byte
	ContentType string
	Width       int
	Height      int
}

func PrepareValidatorImage(raw []byte, contentType string, maxDimension int, quality int) (*ValidatorImage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty image payload")
	}
	if maxDimension <= 0 {
		return nil, fmt.Errorf("invalid max dimension %d", maxDimension)
	}
	if quality <= 0 || quality > 100 {
		quality = 75
	}

	img, _, err := imageorient.Decode(bytes.NewReader(raw))
	if err != nil {
		detected := http.DetectContentType(raw)
		return nil, fmt.Errorf("decode image (detected as %q, header %q): %w", detected, contentType, err)
	}

	sourceBounds := img.Bounds()
	targetWidth, targetHeight := fitWithinMaxDimension(sourceBounds.Dx(), sourceBounds.Dy(), maxDimension)
	if targetWidth <= 0 || targetHeight <= 0 {
		return nil, fmt.Errorf("invalid target dimensions %dx%d", targetWidth, targetHeight)
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	imagedraw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, imagedraw.Src)

	if targetWidth == sourceBounds.Dx() && targetHeight == sourceBounds.Dy() {
		imagedraw.Draw(dst, dst.Bounds(), img, sourceBounds.Min, imagedraw.Over)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, sourceBounds, xdraw.Over, nil)
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encode validator jpeg: %w", err)
	}

	return &ValidatorImage{
		Bytes:       out.Bytes(),
		ContentType: "image/jpeg",
		Width:       targetWidth,
		Height:      targetHeight,
	}, nil
}

func fitWithinMaxDimension(width, height, maxDimension int) (int, int) {
	if width <= 0 || height <= 0 || maxDimension <= 0 {
		return 0, 0
	}
	if width <= maxDimension && height <= maxDimension {
		return width, height
	}

	if width >= height {
		targetWidth := maxDimension
		targetHeight := int(float64(height) * (float64(maxDimension) / float64(width)))
		if targetHeight < 1 {
			targetHeight = 1
		}
		return targetWidth, targetHeight
	}

	targetHeight := maxDimension
	targetWidth := int(float64(width) * (float64(maxDimension) / float64(height)))
	if targetWidth < 1 {
		targetWidth = 1
	}
	return targetWidth, targetHeight
}
