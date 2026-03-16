package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/models"
)

func TestNominatimResolverWaitForRateLimit(t *testing.T) {
	t.Parallel()

	resolver := &NominatimResolver{
		minInterval: 30 * time.Millisecond,
	}

	start := time.Now()
	if err := resolver.waitForRateLimit(context.Background()); err != nil {
		t.Fatalf("first rate-limit wait: %v", err)
	}

	secondStart := time.Now()
	if err := resolver.waitForRateLimit(context.Background()); err != nil {
		t.Fatalf("second rate-limit wait: %v", err)
	}

	if elapsed := time.Since(secondStart); elapsed < 25*time.Millisecond {
		t.Fatalf("expected second call to wait close to min interval, got %v", elapsed)
	}
	if total := time.Since(start); total < 25*time.Millisecond {
		t.Fatalf("expected total runtime to include throttling, got %v", total)
	}
}

func TestNominatimResolverWaitForRateLimitHonorsContextCancel(t *testing.T) {
	t.Parallel()

	resolver := &NominatimResolver{
		minInterval:   100 * time.Millisecond,
		lastRequestAt: time.Now().UTC(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := resolver.waitForRateLimit(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 80*time.Millisecond {
		t.Fatalf("expected cancellation before full wait interval, got %v", elapsed)
	}
}

func TestNominatimResolverCacheHit(t *testing.T) {
	t.Parallel()

	resolver := &NominatimResolver{
		cacheTTL:       time.Hour,
		cachePrecision: 5,
		cache:          make(map[string]cachedGeoResult),
	}

	lat := 49.1950601
	lon := 16.6068372
	original := &models.Capture{
		ID:        "capture-original",
		Latitude:  &lat,
		Longitude: &lon,
	}
	geo := &models.CaptureGeoIndex{
		CaptureID:   original.ID,
		CountryCode: "CZ",
		KrajName:    "Jihomoravský kraj",
		OkresName:   "Brno-město",
		ObecName:    "Brno",
		RawJSON:     "{}",
		ResolvedAt:  time.Now().UTC().Add(-time.Minute),
	}

	resolver.storeCache(original, geo, true)

	nextCapture := &models.Capture{
		ID:        "capture-next",
		Latitude:  &lat,
		Longitude: &lon,
	}
	cached, insideCzechia, ok := resolver.lookupCache(nextCapture)
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if !insideCzechia {
		t.Fatalf("expected insideCzechia to remain true")
	}
	if cached == nil {
		t.Fatalf("expected cached geo result")
	}
	if cached.CaptureID != nextCapture.ID {
		t.Fatalf("expected cached geo to be rebound to current capture id, got %q", cached.CaptureID)
	}
	if cached.KrajName != geo.KrajName || cached.OkresName != geo.OkresName || cached.ObecName != geo.ObecName {
		t.Fatalf("unexpected cached geo payload: %+v", cached)
	}
	if cached.ResolvedAt.Before(geo.ResolvedAt) {
		t.Fatalf("expected cached lookup to refresh resolved timestamp")
	}
}

func TestNominatimResolverCacheExpiry(t *testing.T) {
	t.Parallel()

	resolver := &NominatimResolver{
		cacheTTL:       time.Hour,
		cachePrecision: 5,
		cache: map[string]cachedGeoResult{
			"49.19506:16.60684": {
				geo: &models.CaptureGeoIndex{
					CaptureID:   "capture-stale",
					CountryCode: "CZ",
				},
				insideCzechia: true,
				expiresAt:     time.Now().UTC().Add(-time.Second),
			},
		},
	}

	lat := 49.1950601
	lon := 16.6068372
	capture := &models.Capture{
		ID:        "capture-next",
		Latitude:  &lat,
		Longitude: &lon,
	}

	_, _, ok := resolver.lookupCache(capture)
	if ok {
		t.Fatalf("expected stale cache entry to be ignored")
	}
	if len(resolver.cache) != 0 {
		t.Fatalf("expected stale cache entry to be evicted")
	}
}

func TestAIValidatorClientAnalyzeCaptureWithInlineImage(t *testing.T) {
	t.Parallel()

	var got aiValidatorRequest
	validator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode validator request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":            true,
			"has_mushrooms": true,
			"model_code":    "gemini-2.5-flash",
			"species": []map[string]interface{}{
				{
					"latin_name":          "Boletus edulis",
					"czech_official_name": "hřib smrkový",
					"probability":         0.94,
				},
			},
		})
	}))
	defer validator.Close()

	client := NewAIValidatorClient(&config.Config{
		CaptureAIValidatorURL: validator.URL,
	})
	capture := &models.Capture{
		ID:                "capture-inline",
		PrivateStorageKey: "captures/private/capture-inline.jpg",
	}

	analysis, species, err := client.AnalyzeCaptureWithModeAndImage(context.Background(), capture, AIReviewModePublishValidation, "", &AIValidatorInlineImage{
		Data:     "ZmFrZS1pbWFnZS1ieXRlcw==",
		MimeType: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("analyze capture: %v", err)
	}
	if analysis == nil || !analysis.HasMushrooms {
		t.Fatalf("expected mushroom analysis, got %+v", analysis)
	}
	if len(species) != 1 {
		t.Fatalf("expected 1 species, got %d", len(species))
	}
	if got.CaptureID != capture.ID {
		t.Fatalf("expected capture id %q, got %q", capture.ID, got.CaptureID)
	}
	if got.PrivateStorageKey != capture.PrivateStorageKey {
		t.Fatalf("expected private storage key %q, got %q", capture.PrivateStorageKey, got.PrivateStorageKey)
	}
	if got.InlineImageData == "" || got.InlineImageMime != "image/jpeg" {
		t.Fatalf("expected inline image payload, got %+v", got)
	}
}
