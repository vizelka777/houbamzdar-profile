package main

import "testing"

func TestParseExcludedExtensions(t *testing.T) {
	excluded := parseExcludedExtensions("heic, .HEIF, .jpg")

	for _, ext := range []string{".heic", ".heif", ".jpg"} {
		if _, ok := excluded[ext]; !ok {
			t.Fatalf("expected %s to be excluded", ext)
		}
	}
}

func TestIsSupportedImagePathWithExclusions(t *testing.T) {
	excluded := parseExcludedExtensions(".heic,.heif")

	if isSupportedImagePath("photo.jpg", excluded) != true {
		t.Fatal("expected jpg to stay supported")
	}
	if isSupportedImagePath("photo.HEIC", excluded) != false {
		t.Fatal("expected heic to be excluded")
	}
	if isSupportedImagePath("clip.mp4", excluded) != false {
		t.Fatal("expected mp4 to be unsupported")
	}
}
