package enrichment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/media"
	"github.com/houbamzdar/bff/internal/models"
	"golang.org/x/oauth2"
	_ "modernc.org/sqlite"
)

func TestPublicationValidationSwitch(t *testing.T) {
	for _, tc := range []struct {
		name, country, wantStatus, wantReason           string
		disabled, noValidator, missingGPS, hasMushrooms bool
		wantAICalls                                     int32
	}{
		{name: "paused", disabled: true, country: "cz", wantStatus: db.CapturePublicationReviewApprovedWithoutAI},
		{name: "paused without validator config", disabled: true, noValidator: true, country: "cz", wantStatus: db.CapturePublicationReviewApprovedWithoutAI},
		{name: "paused still rejects foreign location", disabled: true, country: "de", wantStatus: db.CapturePublicationReviewRejected, wantReason: db.CapturePublicationRejectOutsideCzechia},
		{name: "paused still requires GPS", disabled: true, missingGPS: true, wantStatus: db.CapturePublicationReviewRejected, wantReason: db.CapturePublicationRejectMissingCoordinates},
		{name: "enabled rejects missing mushrooms", country: "cz", wantStatus: db.CapturePublicationReviewRejected, wantReason: db.CapturePublicationRejectNoMushrooms, wantAICalls: 1},
		{name: "enabled approves mushrooms", country: "cz", hasMushrooms: true, wantStatus: db.CapturePublicationReviewApproved, wantAICalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var aiCalls atomic.Int32
			ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				aiCalls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "has_mushrooms": tc.hasMushrooms, "model_code": "test-model"})
			}))
			t.Cleanup(ai.Close)
			geo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"address": map[string]string{"country_code": tc.country, "city": "Brno"}})
			}))
			t.Cleanup(geo.Close)
			cfg := &config.Config{
				DBURL:                       "file:" + filepath.Join(t.TempDir(), "test.db"),
				CaptureAIValidationDisabled: tc.disabled,
				CaptureAIValidatorURL:       ai.URL,
				NominatimBaseURL:            geo.URL,
				BunnyPrivateZone:            "photos", BunnyPrivateStorageKey: "test-key",
				BunnyPublicZone: "photos", BunnyPublicStorageKey: "test-key",
				BunnyPublicBaseURL: "https://photos.example.test",
			}
			if tc.noValidator {
				cfg.CaptureAIValidatorURL = ""
			}
			database, err := db.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			user, _, err := database.UpsertUser(&models.OIDCClaims{
				Iss: "https://id.example.test", Sub: "publication-switch", EmailVerified: true,
			}, &oauth2.Token{AccessToken: "test", Expiry: time.Now().Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			lat, lon := 49.1951, 16.6068
			capture := &models.Capture{
				ID: "switch-capture", UserID: user.ID, Status: "private",
				PrivateStorageKey: "captures/switch.jpg", ContentType: "image/jpeg",
				OriginalFileName: "switch.jpg", Width: 640, Height: 480, SizeBytes: 100,
				Latitude: &lat, Longitude: &lon, CapturedAt: time.Now().UTC(), UploadedAt: time.Now().UTC(),
			}
			if tc.missingGPS {
				capture.Latitude = nil
			}
			if err := database.CreateCapture(capture); err != nil {
				t.Fatal(err)
			}
			if err := database.RequestCapturePublicationValidation(capture.ID, user.ID); err != nil {
				t.Fatal(err)
			}
			service := New(cfg, database, media.NewBunnyStorage(cfg))
			if !service.Enabled() {
				t.Fatal("publication worker must run")
			}
			if err := service.ProcessPendingOnce(context.Background(), 1); err != nil {
				t.Fatal(err)
			}
			got, err := database.GetCaptureByID(capture.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.PublicationReviewStatus != tc.wantStatus || got.PublicationReviewReasonCode != tc.wantReason {
				t.Fatalf("review = %s/%s, want %s/%s", got.PublicationReviewStatus, got.PublicationReviewReasonCode, tc.wantStatus, tc.wantReason)
			}
			if aiCalls.Load() != tc.wantAICalls {
				t.Fatalf("AI calls = %d, want %d", aiCalls.Load(), tc.wantAICalls)
			}
			if tc.wantStatus == db.CapturePublicationReviewApprovedWithoutAI {
				if got.Status != "published" || got.HasMushrooms || !got.MushroomAnalysisAt.IsZero() {
					t.Fatalf("expected published photo without invented analysis: %+v", got)
				}
				analysis, species, err := database.GetCaptureMushroomReview(capture.ID)
				if err != nil || analysis != nil || len(species) != 0 {
					t.Fatalf("unexpected stored AI review: %v, %v", analysis, err)
				}
				photos, err := database.ListPublicCapturesWithFilters(db.PublicCaptureFilters{}, 0)
				if err != nil || len(photos) != 1 {
					t.Fatalf("unreviewed photo missing from public gallery: %d, %v", len(photos), err)
				}
				if _, err := database.GetCaptureGeoIndex(capture.ID); err != nil {
					t.Fatalf("missing geographic index: %v", err)
				}
				if tc.name == "paused" {
					cfg.CaptureAIValidationDisabled = false
					if err := database.UnpublishCapture(capture.ID, user.ID); err != nil {
						t.Fatal(err)
					}
					if err := database.RequestCapturePublicationValidation(capture.ID, user.ID); err != nil {
						t.Fatal(err)
					}
					if err := New(cfg, database, media.NewBunnyStorage(cfg)).ProcessPendingOnce(context.Background(), 1); err != nil {
						t.Fatal(err)
					}
					rechecked, err := database.GetCaptureByID(capture.ID)
					if err != nil {
						t.Fatal(err)
					}
					if aiCalls.Load() != 1 || rechecked.Status != "private" || rechecked.PublicationReviewReasonCode != db.CapturePublicationRejectNoMushrooms {
						t.Fatal("restoring AI validation must reject a republished photo without mushrooms")
					}
				}
			}
		})
	}
}
