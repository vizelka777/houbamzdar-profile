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
