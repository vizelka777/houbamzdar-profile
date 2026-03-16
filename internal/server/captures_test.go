package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/media"
	"github.com/houbamzdar/bff/internal/models"
	"golang.org/x/oauth2"
	_ "modernc.org/sqlite"
)

func TestCaptureCoordinateUnlockFlow(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:             "file:" + filepath.Join(t.TempDir(), "test.db"),
		FrontOrigin:       "https://houbamzdar.cz",
		SessionCookieName: "hzd_session",
	}

	database, err := db.New(cfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	author, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "capture-flow-author",
		PreferredUsername: "autor",
		Email:             "capture-flow-author@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "author-access",
		RefreshToken: "author-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	viewer, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "capture-flow-viewer",
		PreferredUsername: "divak",
		Email:             "capture-flow-viewer@example.test",
	}, &oauth2.Token{
		AccessToken:  "viewer-access",
		RefreshToken: "viewer-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}

	if err := database.GrantAuthBonuses(viewer.ID, true, false, false); err != nil {
		t.Fatalf("grant viewer signup bonus: %v", err)
	}

	makeCapture := func(id string, free bool, lat, lon float64, publishedAt time.Time) *models.Capture {
		return &models.Capture{
			ID:                id,
			UserID:            author.ID,
			OriginalFileName:  id + ".jpg",
			ContentType:       "image/jpeg",
			SizeBytes:         2048,
			Width:             1200,
			Height:            800,
			CapturedAt:        publishedAt,
			UploadedAt:        publishedAt,
			Latitude:          &lat,
			Longitude:         &lon,
			CoordinatesFree:   free,
			Status:            "published",
			PrivateStorageKey: "captures/private/" + id + ".jpg",
			PublicStorageKey:  "captures/public/" + id + ".jpg",
			PublishedAt:       publishedAt,
		}
	}

	paidCapture := makeCapture("capture-paid-001", false, 49.1234, 17.9876, time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC))
	freeCapture := makeCapture("capture-free-001", true, 49.2345, 17.8765, time.Date(2026, time.March, 15, 12, 5, 0, 0, time.UTC))

	if err := database.CreateCapture(paidCapture); err != nil {
		t.Fatalf("create paid capture: %v", err)
	}
	if err := database.CreateCapture(freeCapture); err != nil {
		t.Fatalf("create free capture: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	guestListReq := httptest.NewRequest(http.MethodGet, "/api/public/captures?limit=10&offset=0", nil)
	guestListRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(guestListRec, guestListReq)
	if guestListRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for guest public captures, got %d: %s", guestListRec.Code, guestListRec.Body.String())
	}

	var guestListPayload struct {
		OK       bool              `json:"ok"`
		Captures []*models.Capture `json:"captures"`
	}
	if err := json.NewDecoder(guestListRec.Body).Decode(&guestListPayload); err != nil {
		t.Fatalf("decode guest public captures: %v", err)
	}

	captureByID := make(map[string]*models.Capture, len(guestListPayload.Captures))
	for _, capture := range guestListPayload.Captures {
		captureByID[capture.ID] = capture
	}

	if captureByID[paidCapture.ID] == nil || !captureByID[paidCapture.ID].CoordinatesLocked {
		t.Fatalf("expected paid capture to be locked for guest")
	}
	if captureByID[paidCapture.ID].Latitude != nil || captureByID[paidCapture.ID].Longitude != nil {
		t.Fatalf("expected paid capture coordinates to be hidden for guest")
	}
	if captureByID[freeCapture.ID] == nil {
		t.Fatalf("expected free capture to be present for guest")
	}
	if captureByID[freeCapture.ID].CoordinatesLocked {
		t.Fatalf("expected free capture to stay unlocked for guest")
	}

	guestMapReq := httptest.NewRequest(http.MethodGet, "/api/public/map-captures?limit=10&offset=0", nil)
	guestMapRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(guestMapRec, guestMapReq)
	if guestMapRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for guest public map captures, got %d: %s", guestMapRec.Code, guestMapRec.Body.String())
	}

	var guestMapPayload struct {
		OK       bool              `json:"ok"`
		Captures []*models.Capture `json:"captures"`
	}
	if err := json.NewDecoder(guestMapRec.Body).Decode(&guestMapPayload); err != nil {
		t.Fatalf("decode guest public map captures: %v", err)
	}
	if len(guestMapPayload.Captures) != 1 || guestMapPayload.Captures[0].ID != freeCapture.ID {
		t.Fatalf("expected only free capture on guest map, got %+v", guestMapPayload.Captures)
	}
	if guestMapPayload.Captures[0].Latitude == nil || guestMapPayload.Captures[0].Longitude == nil || guestMapPayload.Captures[0].CoordinatesLocked {
		t.Fatalf("expected free guest map capture to expose coordinates, got %+v", guestMapPayload.Captures[0])
	}

	guestUnlockReq := httptest.NewRequest(http.MethodPost, "/api/captures/"+paidCapture.ID+"/unlock-coordinates", nil)
	guestUnlockRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(guestUnlockRec, guestUnlockReq)
	if guestUnlockRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for guest unlock, got %d", guestUnlockRec.Code)
	}

	sessionID := "capture-flow-session-001"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    viewer.ID,
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("create viewer session: %v", err)
	}

	authUnlockReq := httptest.NewRequest(http.MethodPost, "/api/captures/"+paidCapture.ID+"/unlock-coordinates", nil)
	authUnlockReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	authUnlockRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(authUnlockRec, authUnlockReq)
	if authUnlockRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for authenticated unlock, got %d: %s", authUnlockRec.Code, authUnlockRec.Body.String())
	}

	var unlockPayload struct {
		OK              bool            `json:"ok"`
		Balance         int64           `json:"balance"`
		AlreadyUnlocked bool            `json:"already_unlocked"`
		Capture         *models.Capture `json:"capture"`
	}
	if err := json.NewDecoder(authUnlockRec.Body).Decode(&unlockPayload); err != nil {
		t.Fatalf("decode unlock payload: %v", err)
	}
	if !unlockPayload.OK || unlockPayload.Capture == nil {
		t.Fatalf("unexpected unlock response: %+v", unlockPayload)
	}
	if unlockPayload.AlreadyUnlocked {
		t.Fatalf("expected first unlock to charge the viewer")
	}
	if unlockPayload.Balance != 0 {
		t.Fatalf("expected viewer balance 0 after unlock, got %d", unlockPayload.Balance)
	}
	if unlockPayload.Capture.Latitude == nil || unlockPayload.Capture.Longitude == nil {
		t.Fatalf("expected unlocked capture coordinates in response")
	}

	authListReq := httptest.NewRequest(http.MethodGet, "/api/public/captures?limit=10&offset=0", nil)
	authListReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	authListRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(authListRec, authListReq)
	if authListRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for authenticated public captures, got %d: %s", authListRec.Code, authListRec.Body.String())
	}

	var authListPayload struct {
		OK       bool              `json:"ok"`
		Captures []*models.Capture `json:"captures"`
	}
	if err := json.NewDecoder(authListRec.Body).Decode(&authListPayload); err != nil {
		t.Fatalf("decode authenticated public captures: %v", err)
	}

	captureByID = make(map[string]*models.Capture, len(authListPayload.Captures))
	for _, capture := range authListPayload.Captures {
		captureByID[capture.ID] = capture
	}
	if captureByID[paidCapture.ID] == nil {
		t.Fatalf("expected paid capture to stay present after unlock")
	}
	if captureByID[paidCapture.ID].CoordinatesLocked {
		t.Fatalf("expected paid capture to be unlocked for the viewer")
	}
	if captureByID[paidCapture.ID].Latitude == nil || captureByID[paidCapture.ID].Longitude == nil {
		t.Fatalf("expected unlocked coordinates in authenticated public list")
	}

	authMapReq := httptest.NewRequest(http.MethodGet, "/api/public/map-captures?limit=10&offset=0", nil)
	authMapReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	authMapRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(authMapRec, authMapReq)
	if authMapRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for authenticated public map captures, got %d: %s", authMapRec.Code, authMapRec.Body.String())
	}

	var authMapPayload struct {
		OK       bool              `json:"ok"`
		Captures []*models.Capture `json:"captures"`
	}
	if err := json.NewDecoder(authMapRec.Body).Decode(&authMapPayload); err != nil {
		t.Fatalf("decode authenticated public map captures: %v", err)
	}
	if len(authMapPayload.Captures) != 2 {
		t.Fatalf("expected both captures on unlocked viewer map, got %+v", authMapPayload.Captures)
	}
	captureByID = make(map[string]*models.Capture, len(authMapPayload.Captures))
	for _, capture := range authMapPayload.Captures {
		captureByID[capture.ID] = capture
	}
	if captureByID[paidCapture.ID] == nil || captureByID[paidCapture.ID].Latitude == nil || captureByID[paidCapture.ID].Longitude == nil || captureByID[paidCapture.ID].CoordinatesLocked {
		t.Fatalf("expected paid capture to appear on unlocked viewer map, got %+v", captureByID[paidCapture.ID])
	}

	viewedReq := httptest.NewRequest(http.MethodGet, "/api/me/viewed-captures?limit=10&offset=0", nil)
	viewedReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	viewedRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(viewedRec, viewedReq)
	if viewedRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for viewed captures, got %d: %s", viewedRec.Code, viewedRec.Body.String())
	}

	var viewedPayload struct {
		OK       bool              `json:"ok"`
		Captures []*models.Capture `json:"captures"`
	}
	if err := json.NewDecoder(viewedRec.Body).Decode(&viewedPayload); err != nil {
		t.Fatalf("decode viewed captures payload: %v", err)
	}
	if len(viewedPayload.Captures) != 1 || viewedPayload.Captures[0].ID != paidCapture.ID {
		t.Fatalf("unexpected viewed captures payload: %+v", viewedPayload.Captures)
	}
	if viewedPayload.Captures[0].UnlockedAt.IsZero() {
		t.Fatalf("expected unlock timestamp in viewed captures payload")
	}
}

func TestHandlePublishCaptureQueuesValidation(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:                  "file:" + filepath.Join(t.TempDir(), "test.db"),
		FrontOrigin:            "https://houbamzdar.cz",
		SessionCookieName:      "hzd_session",
		CaptureAIValidatorURL:  "https://validator.example/validate-capture",
		NominatimBaseURL:       "https://nominatim.openstreetmap.org",
		NominatimUserAgent:     "houbamzdar-tests/1.0",
		BunnyPrivateZone:       "private-zone",
		BunnyPrivateStorageKey: "private-key",
		BunnyPublicZone:        "public-zone",
		BunnyPublicStorageKey:  "public-key",
		BunnyPublicBaseURL:     "https://foto.houbamzdar.cz",
		BunnyStorageHost:       "storage.bunnycdn.com",
	}

	database, err := db.New(cfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "publish-queue-user",
		PreferredUsername: "queueuser",
		Email:             "publish-queue-user@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "queue-access",
		RefreshToken: "queue-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	sessionID := "publish-queue-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	lat := 49.1951
	lon := 16.6068
	capture := &models.Capture{
		ID:                "publish-queue-capture",
		UserID:            user.ID,
		OriginalFileName:  "queue.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         2048,
		Width:             1200,
		Height:            900,
		CapturedAt:        time.Date(2026, time.March, 16, 9, 0, 0, 0, time.UTC),
		UploadedAt:        time.Date(2026, time.March, 16, 9, 0, 0, 0, time.UTC),
		Latitude:          &lat,
		Longitude:         &lon,
		Status:            "private",
		PrivateStorageKey: "captures/private/publish-queue-capture.jpg",
	}
	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create capture: %v", err)
	}

	srv := New(cfg, database, nil, &media.BunnyStorage{})

	req := httptest.NewRequest(http.MethodPost, "/api/captures/"+capture.ID+"/publish", nil)
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK      bool            `json:"ok"`
		Capture *models.Capture `json:"capture"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.OK || payload.Capture == nil {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.Capture.Status != "private" {
		t.Fatalf("expected private status while validation is pending, got %q", payload.Capture.Status)
	}
	if payload.Capture.PublicationReviewStatus != db.CapturePublicationReviewPendingValidation {
		t.Fatalf("expected pending_validation status, got %q", payload.Capture.PublicationReviewStatus)
	}
	if payload.Capture.PublicationRequestedAt.IsZero() {
		t.Fatalf("expected publication_requested_at to be set")
	}

	reloaded, err := database.GetCaptureForUser(capture.ID, user.ID)
	if err != nil {
		t.Fatalf("reload capture: %v", err)
	}
	if reloaded.PublicationReviewStatus != db.CapturePublicationReviewPendingValidation {
		t.Fatalf("expected database capture to be pending_validation, got %q", reloaded.PublicationReviewStatus)
	}
}

func TestHandlePublishCaptureRejectsMissingCoordinates(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:                  "file:" + filepath.Join(t.TempDir(), "test.db"),
		FrontOrigin:            "https://houbamzdar.cz",
		SessionCookieName:      "hzd_session",
		CaptureAIValidatorURL:  "https://validator.example/validate-capture",
		NominatimBaseURL:       "https://nominatim.openstreetmap.org",
		NominatimUserAgent:     "houbamzdar-tests/1.0",
		BunnyPrivateZone:       "private-zone",
		BunnyPrivateStorageKey: "private-key",
		BunnyPublicZone:        "public-zone",
		BunnyPublicStorageKey:  "public-key",
		BunnyPublicBaseURL:     "https://foto.houbamzdar.cz",
		BunnyStorageHost:       "storage.bunnycdn.com",
	}

	database, err := db.New(cfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "publish-reject-user",
		PreferredUsername: "rejectuser",
		Email:             "publish-reject-user@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "reject-access",
		RefreshToken: "reject-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	sessionID := "publish-reject-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	capture := &models.Capture{
		ID:                "publish-reject-capture",
		UserID:            user.ID,
		OriginalFileName:  "reject.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         2048,
		Width:             1200,
		Height:            900,
		CapturedAt:        time.Date(2026, time.March, 16, 9, 0, 0, 0, time.UTC),
		UploadedAt:        time.Date(2026, time.March, 16, 9, 0, 0, 0, time.UTC),
		Status:            "private",
		PrivateStorageKey: "captures/private/publish-reject-capture.jpg",
	}
	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create capture: %v", err)
	}

	srv := New(cfg, database, nil, &media.BunnyStorage{})

	req := httptest.NewRequest(http.MethodPost, "/api/captures/"+capture.ID+"/publish", nil)
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK      bool            `json:"ok"`
		Error   string          `json:"error"`
		Capture *models.Capture `json:"capture"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.OK {
		t.Fatalf("expected ok=false for rejected publish response")
	}
	if payload.Error == "" {
		t.Fatalf("expected error message in response")
	}
	if payload.Capture == nil {
		t.Fatalf("expected rejected capture in response")
	}
	if payload.Capture.PublicationReviewStatus != db.CapturePublicationReviewRejected {
		t.Fatalf("expected rejected review status, got %q", payload.Capture.PublicationReviewStatus)
	}
	if payload.Capture.PublicationReviewReasonCode != db.CapturePublicationRejectMissingCoordinates {
		t.Fatalf("expected missing_coordinates rejection, got %q", payload.Capture.PublicationReviewReasonCode)
	}

	reloaded, err := database.GetCaptureForUser(capture.ID, user.ID)
	if err != nil {
		t.Fatalf("reload capture: %v", err)
	}
	if reloaded.PublicationReviewStatus != db.CapturePublicationReviewRejected {
		t.Fatalf("expected database capture to be rejected, got %q", reloaded.PublicationReviewStatus)
	}
	if reloaded.PublicationReviewReasonCode != db.CapturePublicationRejectMissingCoordinates {
		t.Fatalf("expected missing_coordinates rejection in database, got %q", reloaded.PublicationReviewReasonCode)
	}
}

func TestHandleModeratorRecheckCaptureUpdatesPublishedAnalysis(t *testing.T) {
	t.Parallel()

	validator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		var body struct {
			CaptureID         string `json:"capture_id"`
			PrivateStorageKey string `json:"private_storage_key"`
			ReviewMode        string `json:"review_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode validator request: %v", err)
		}
		if body.ReviewMode != "moderator_recheck" {
			t.Fatalf("expected moderator_recheck mode, got %q", body.ReviewMode)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":            true,
			"has_mushrooms": true,
			"model_code":    "gemini-2.5-pro",
			"species": []map[string]interface{}{
				{
					"latin_name":          "Boletus edulis",
					"czech_official_name": "hřib smrkový",
					"probability":         0.97,
				},
				{
					"latin_name":          "Amanita muscaria",
					"czech_official_name": "muchomůrka červená",
					"probability":         0.63,
				},
			},
		})
	}))
	defer validator.Close()

	cfg := &config.Config{
		DBURL:                 "file:" + filepath.Join(t.TempDir(), "test.db"),
		FrontOrigin:           "https://houbamzdar.cz",
		SessionCookieName:     "hzd_session",
		CaptureAIValidatorURL: validator.URL,
	}

	database, err := db.New(cfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	owner, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "moderated-owner",
		PreferredUsername: "owner",
		Email:             "owner@example.test",
	}, &oauth2.Token{
		AccessToken:  "owner-access",
		RefreshToken: "owner-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	moderator, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "moderator-user",
		PreferredUsername: "houbamzdar",
		Email:             "moderator@example.test",
	}, &oauth2.Token{
		AccessToken:  "moderator-access",
		RefreshToken: "moderator-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create moderator: %v", err)
	}
	if !moderator.IsModerator {
		t.Fatalf("expected houbamzdar user to be moderator")
	}

	sessionID := "moderator-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    moderator.ID,
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("create moderator session: %v", err)
	}

	lat := 49.1951
	lon := 16.6068
	capture := &models.Capture{
		ID:                "moderator-review-capture",
		UserID:            owner.ID,
		OriginalFileName:  "moderated.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         2048,
		Width:             1200,
		Height:            900,
		CapturedAt:        time.Date(2026, time.March, 16, 11, 0, 0, 0, time.UTC),
		UploadedAt:        time.Date(2026, time.March, 16, 11, 0, 0, 0, time.UTC),
		Latitude:          &lat,
		Longitude:         &lon,
		Status:            "published",
		PrivateStorageKey: "captures/private/moderator-review-capture.jpg",
		PublicStorageKey:  "captures/published/owner/moderator-review-capture.jpg",
		PublishedAt:       time.Date(2026, time.March, 16, 11, 5, 0, 0, time.UTC),
	}
	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create capture: %v", err)
	}

	srv := New(cfg, database, nil, &media.BunnyStorage{})

	req := httptest.NewRequest(http.MethodPost, "/api/captures/"+capture.ID+"/moderator-recheck", nil)
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK        bool                             `json:"ok"`
		CaptureID string                           `json:"capture_id"`
		ModelCode string                           `json:"model_code"`
		Species   []*models.CaptureMushroomSpecies `json:"species"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.OK || payload.CaptureID != capture.ID || payload.ModelCode != "gemini-2.5-pro" {
		t.Fatalf("unexpected response payload: %+v", payload)
	}
	if len(payload.Species) != 2 {
		t.Fatalf("expected 2 species in response, got %d", len(payload.Species))
	}

	captures, err := database.ListPublicCapturesWithFilters(db.PublicCaptureFilters{
		Limit:  10,
		Offset: 0,
	}, 0)
	if err != nil {
		t.Fatalf("list public captures: %v", err)
	}

	var reviewed *models.Capture
	for _, item := range captures {
		if item.ID == capture.ID {
			reviewed = item
			break
		}
	}
	if reviewed == nil {
		t.Fatalf("expected reviewed capture in public list")
	}
	if reviewed.MushroomPrimaryLatinName != "Boletus edulis" {
		t.Fatalf("expected primary latin name to be updated, got %q", reviewed.MushroomPrimaryLatinName)
	}

	var (
		storedSpeciesCount int
		reviewSource       string
		reviewedByUserID   int64
	)
	if err := database.QueryRow(`SELECT COUNT(*) FROM capture_mushroom_species WHERE capture_id = ?`, capture.ID).Scan(&storedSpeciesCount); err != nil {
		t.Fatalf("count stored species: %v", err)
	}
	if storedSpeciesCount != 2 {
		t.Fatalf("expected 2 stored species rows, got %d", storedSpeciesCount)
	}
	if err := database.QueryRow(`SELECT review_source, COALESCE(reviewed_by_user_id, 0) FROM capture_mushroom_analysis WHERE capture_id = ?`, capture.ID).Scan(&reviewSource, &reviewedByUserID); err != nil {
		t.Fatalf("load stored review metadata: %v", err)
	}
	if reviewSource != db.CaptureAnalysisSourceModeratorRecheck {
		t.Fatalf("expected moderator review source, got %q", reviewSource)
	}
	if reviewedByUserID != moderator.ID {
		t.Fatalf("expected reviewed_by_user_id %d, got %d", moderator.ID, reviewedByUserID)
	}
}
