package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/media"
	"github.com/houbamzdar/bff/internal/models"
	"golang.org/x/oauth2"
	_ "modernc.org/sqlite"
)

func TestParseCaptureListFiltersSupportsInclusiveDateRange(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/captures?date_from=2026-03-20&date_to=2026-03-22", nil)
	filters := parseCaptureListFilters(req)

	if filters.CapturedFrom == nil {
		t.Fatalf("expected captured_from to be parsed")
	}
	if filters.CapturedTo == nil {
		t.Fatalf("expected captured_to to be parsed")
	}

	wantFrom := time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, time.March, 23, 0, 0, 0, 0, time.UTC)

	if !filters.CapturedFrom.Equal(wantFrom) {
		t.Fatalf("unexpected captured_from: got %s want %s", filters.CapturedFrom.Format(time.RFC3339), wantFrom.Format(time.RFC3339))
	}
	if !filters.CapturedTo.Equal(wantTo) {
		t.Fatalf("unexpected captured_to: got %s want %s", filters.CapturedTo.Format(time.RFC3339), wantTo.Format(time.RFC3339))
	}
}

func TestAttachPublicURLsAlsoSetsImageURL(t *testing.T) {
	t.Parallel()

	storage := media.NewBunnyStorage(&config.Config{
		BunnyPrivateZone:       "foto-zone",
		BunnyPrivateStorageKey: "foto-key",
		BunnyPublicZone:        "foto-zone",
		BunnyPublicStorageKey:  "foto-key",
		BunnyPublicBaseURL:     "https://foto.houbamzdar.cz",
	})
	if storage == nil {
		t.Fatalf("expected bunny storage to be configured")
	}

	capture := &models.Capture{
		ID:                "capture-image-url-001",
		UserID:            7,
		Status:            "private",
		PrivateStorageKey: "captures/7/2026/03/capture-image-url-001.jpg",
	}

	attachPublicURLs([]*models.Capture{capture}, storage)

	wantImageURL := "https://foto.houbamzdar.cz/captures/7/2026/03/capture-image-url-001.jpg"
	if capture.ImageURL != wantImageURL {
		t.Fatalf("unexpected image_url: got %q want %q", capture.ImageURL, wantImageURL)
	}
	if capture.PublicURL != "" {
		t.Fatalf("expected empty public_url for unpublished capture, got %q", capture.PublicURL)
	}
}

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
		OK         bool              `json:"ok"`
		Captures   []*models.Capture `json:"captures"`
		Page       int               `json:"page"`
		PageSize   int               `json:"page_size"`
		Total      int               `json:"total"`
		TotalPages int               `json:"total_pages"`
		HasMore    bool              `json:"has_more"`
	}
	if err := json.NewDecoder(guestListRec.Body).Decode(&guestListPayload); err != nil {
		t.Fatalf("decode guest public captures: %v", err)
	}
	if guestListPayload.Page != 1 || guestListPayload.PageSize != 10 || guestListPayload.Total != 2 || guestListPayload.TotalPages != 1 || guestListPayload.HasMore {
		t.Fatalf("unexpected guest public capture pagination: %+v", guestListPayload)
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

	guestPagedReq := httptest.NewRequest(http.MethodGet, "/api/public/captures?page=2&page_size=1", nil)
	guestPagedRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(guestPagedRec, guestPagedReq)
	if guestPagedRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for guest public capture page 2, got %d: %s", guestPagedRec.Code, guestPagedRec.Body.String())
	}

	var guestPagedPayload struct {
		OK         bool              `json:"ok"`
		Captures   []*models.Capture `json:"captures"`
		Page       int               `json:"page"`
		PageSize   int               `json:"page_size"`
		Total      int               `json:"total"`
		TotalPages int               `json:"total_pages"`
		HasMore    bool              `json:"has_more"`
	}
	if err := json.NewDecoder(guestPagedRec.Body).Decode(&guestPagedPayload); err != nil {
		t.Fatalf("decode guest paged public captures: %v", err)
	}
	if guestPagedPayload.Page != 2 || guestPagedPayload.PageSize != 1 || guestPagedPayload.Total != 2 || guestPagedPayload.TotalPages != 2 || guestPagedPayload.HasMore {
		t.Fatalf("unexpected guest paged public capture pagination: %+v", guestPagedPayload)
	}
	if len(guestPagedPayload.Captures) != 1 {
		t.Fatalf("expected 1 capture on guest page 2, got %d", len(guestPagedPayload.Captures))
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
	if len(authMapPayload.Captures) != 1 || authMapPayload.Captures[0].ID != freeCapture.ID {
		t.Fatalf("expected only free capture on authenticated public map, got %+v", authMapPayload.Captures)
	}
	if authMapPayload.Captures[0].Latitude == nil || authMapPayload.Captures[0].Longitude == nil || authMapPayload.Captures[0].CoordinatesLocked {
		t.Fatalf("expected free capture to stay visible on authenticated public map, got %+v", authMapPayload.Captures[0])
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

func TestProfileMapEndpointsUseDedicatedWholeMapPayloads(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:             "file:" + filepath.Join(t.TempDir(), "profile-map.db"),
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
		Sub:               "profile-map-author",
		PreferredUsername: "mapar",
		Email:             "profile-map-author@example.test",
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
		Sub:               "profile-map-viewer",
		PreferredUsername: "mapviewer",
		Email:             "profile-map-viewer@example.test",
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

	makeCapture := func(id string, status string, free bool, lat, lon *float64, capturedAt time.Time) *models.Capture {
		capture := &models.Capture{
			ID:                id,
			UserID:            author.ID,
			OriginalFileName:  id + ".jpg",
			ContentType:       "image/jpeg",
			SizeBytes:         2048,
			Width:             1200,
			Height:            800,
			CapturedAt:        capturedAt,
			UploadedAt:        capturedAt,
			Latitude:          lat,
			Longitude:         lon,
			CoordinatesFree:   free,
			Status:            status,
			PrivateStorageKey: "captures/" + id + ".jpg",
		}
		if status == "published" {
			capture.PublicStorageKey = "captures/published/" + id + ".jpg"
			capture.PublishedAt = capturedAt
		}
		return capture
	}

	privateLat, privateLon := 49.1201, 16.6011
	paidLat, paidLon := 49.2202, 16.7022
	freeLat, freeLon := 49.3203, 16.8033
	privateCapture := makeCapture("profile-map-private", "private", false, &privateLat, &privateLon, time.Date(2026, time.March, 10, 10, 0, 0, 0, time.UTC))
	paidPublicCapture := makeCapture("profile-map-paid", "published", false, &paidLat, &paidLon, time.Date(2026, time.March, 11, 11, 0, 0, 0, time.UTC))
	freePublicCapture := makeCapture("profile-map-free", "published", true, &freeLat, &freeLon, time.Date(2026, time.March, 12, 12, 0, 0, 0, time.UTC))
	noCoordsCapture := makeCapture("profile-map-no-coords", "published", true, nil, nil, time.Date(2026, time.March, 13, 13, 0, 0, 0, time.UTC))

	for _, capture := range []*models.Capture{privateCapture, paidPublicCapture, freePublicCapture, noCoordsCapture} {
		if err := database.CreateCapture(capture); err != nil {
			t.Fatalf("create capture %s: %v", capture.ID, err)
		}
	}

	if _, _, err := database.UnlockCaptureCoordinates(viewer.ID, paidPublicCapture.ID); err != nil {
		t.Fatalf("unlock paid capture: %v", err)
	}

	authorSession := "profile-map-author-session"
	if err := database.CreateSession(&models.Session{
		SessionID: authorSession,
		UserID:    author.ID,
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("create author session: %v", err)
	}

	viewerSession := "profile-map-viewer-session"
	if err := database.CreateSession(&models.Session{
		SessionID: viewerSession,
		UserID:    viewer.ID,
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("create viewer session: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	authorOwnMapReq := httptest.NewRequest(http.MethodGet, "/api/me/map-captures", nil)
	authorOwnMapReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: authorSession})
	authorOwnMapRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(authorOwnMapRec, authorOwnMapReq)
	if authorOwnMapRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for own map captures, got %d: %s", authorOwnMapRec.Code, authorOwnMapRec.Body.String())
	}

	var authorOwnMapPayload struct {
		OK       bool              `json:"ok"`
		Captures []*models.Capture `json:"captures"`
	}
	if err := json.NewDecoder(authorOwnMapRec.Body).Decode(&authorOwnMapPayload); err != nil {
		t.Fatalf("decode own map captures: %v", err)
	}
	if len(authorOwnMapPayload.Captures) != 3 {
		t.Fatalf("expected 3 captures on own map, got %d", len(authorOwnMapPayload.Captures))
	}

	authorOwnIDs := make(map[string]bool, len(authorOwnMapPayload.Captures))
	for _, capture := range authorOwnMapPayload.Captures {
		authorOwnIDs[capture.ID] = true
		if capture.Latitude == nil || capture.Longitude == nil {
			t.Fatalf("expected own map capture %s to include coordinates", capture.ID)
		}
	}
	if !authorOwnIDs[privateCapture.ID] || !authorOwnIDs[paidPublicCapture.ID] || !authorOwnIDs[freePublicCapture.ID] {
		t.Fatalf("unexpected own map capture ids: %+v", authorOwnIDs)
	}
	if authorOwnIDs[noCoordsCapture.ID] {
		t.Fatalf("did not expect no-coordinates capture on own map")
	}

	viewedMapReq := httptest.NewRequest(http.MethodGet, "/api/me/viewed-map-captures", nil)
	viewedMapReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: viewerSession})
	viewedMapRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(viewedMapRec, viewedMapReq)
	if viewedMapRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for viewed map captures, got %d: %s", viewedMapRec.Code, viewedMapRec.Body.String())
	}

	var viewedMapPayload struct {
		OK       bool              `json:"ok"`
		Captures []*models.Capture `json:"captures"`
	}
	if err := json.NewDecoder(viewedMapRec.Body).Decode(&viewedMapPayload); err != nil {
		t.Fatalf("decode viewed map captures: %v", err)
	}
	if len(viewedMapPayload.Captures) != 1 || viewedMapPayload.Captures[0].ID != paidPublicCapture.ID {
		t.Fatalf("expected only unlocked paid capture on viewed map, got %+v", viewedMapPayload.Captures)
	}

	guestPublicMapReq := httptest.NewRequest(http.MethodGet, "/api/public/users/"+strconv.FormatInt(author.ID, 10)+"/map-captures", nil)
	guestPublicMapRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(guestPublicMapRec, guestPublicMapReq)
	if guestPublicMapRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for guest public user map, got %d: %s", guestPublicMapRec.Code, guestPublicMapRec.Body.String())
	}

	var guestPublicMapPayload struct {
		OK       bool              `json:"ok"`
		Captures []*models.Capture `json:"captures"`
	}
	if err := json.NewDecoder(guestPublicMapRec.Body).Decode(&guestPublicMapPayload); err != nil {
		t.Fatalf("decode guest public user map: %v", err)
	}
	if len(guestPublicMapPayload.Captures) != 1 || guestPublicMapPayload.Captures[0].ID != freePublicCapture.ID {
		t.Fatalf("expected only free published capture on guest public map, got %+v", guestPublicMapPayload.Captures)
	}

	viewerPublicMapReq := httptest.NewRequest(http.MethodGet, "/api/public/users/"+strconv.FormatInt(author.ID, 10)+"/map-captures", nil)
	viewerPublicMapReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: viewerSession})
	viewerPublicMapRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(viewerPublicMapRec, viewerPublicMapReq)
	if viewerPublicMapRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for unlocked public user map, got %d: %s", viewerPublicMapRec.Code, viewerPublicMapRec.Body.String())
	}

	var viewerPublicMapPayload struct {
		OK       bool              `json:"ok"`
		Captures []*models.Capture `json:"captures"`
	}
	if err := json.NewDecoder(viewerPublicMapRec.Body).Decode(&viewerPublicMapPayload); err != nil {
		t.Fatalf("decode unlocked public user map: %v", err)
	}
	if len(viewerPublicMapPayload.Captures) != 1 || viewerPublicMapPayload.Captures[0].ID != freePublicCapture.ID {
		t.Fatalf("expected only free capture on public profile map after unlock, got %+v", viewerPublicMapPayload.Captures)
	}
	if viewerPublicMapPayload.Captures[0].Latitude == nil || viewerPublicMapPayload.Captures[0].Longitude == nil {
		t.Fatalf("expected free public profile map capture to include coordinates, got %+v", viewerPublicMapPayload.Captures[0])
	}
}

func TestPublicCapturesIncludeAllDetectedMushroomSpecies(t *testing.T) {
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
		Sub:               "public-capture-multi-species-author",
		PreferredUsername: "atlas",
		Email:             "atlas@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "atlas-access",
		RefreshToken: "atlas-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	capturedAt := time.Date(2026, time.March, 16, 10, 0, 0, 0, time.UTC)
	capture := &models.Capture{
		ID:                "public-capture-multi-species",
		UserID:            author.ID,
		OriginalFileName:  "multi-species.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         4096,
		Width:             1600,
		Height:            1200,
		CapturedAt:        capturedAt,
		UploadedAt:        capturedAt.Add(30 * time.Second),
		Status:            "private",
		PrivateStorageKey: "captures/private/public-capture-multi-species.jpg",
	}
	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create capture: %v", err)
	}

	analysis := &models.CaptureMushroomAnalysis{
		CaptureID:                capture.ID,
		HasMushrooms:             true,
		PrimaryLatinName:         "Boletus edulis",
		PrimaryCzechOfficialName: "hřib smrkový",
		PrimaryProbability:       0.97,
		ModelCode:                "gemini-2.5-flash",
		RawJSON:                  "{}",
		AnalyzedAt:               capturedAt.Add(time.Minute),
	}
	species := []*models.CaptureMushroomSpecies{
		{
			CaptureID:         capture.ID,
			LatinName:         "Boletus edulis",
			CzechOfficialName: "hřib smrkový",
			Probability:       0.97,
		},
		{
			CaptureID:         capture.ID,
			LatinName:         "Cantharellus cibarius",
			CzechOfficialName: "liška obecná",
			Probability:       0.81,
		},
	}
	geo := &models.CaptureGeoIndex{
		CaptureID:   capture.ID,
		CountryCode: "CZ",
		KrajName:    "Středočeský kraj",
		RawJSON:     "{}",
		ResolvedAt:  capturedAt.Add(2 * time.Minute),
	}
	if err := database.FinalizeCapturePublicationApproved(
		capture.ID,
		author.ID,
		"captures/public/public-capture-multi-species.jpg",
		analysis,
		species,
		geo,
	); err != nil {
		t.Fatalf("finalize capture: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/public/captures?limit=10&offset=0", nil)
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK       bool              `json:"ok"`
		Captures []*models.Capture `json:"captures"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !payload.OK || len(payload.Captures) != 1 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if got := len(payload.Captures[0].MushroomSpecies); got != 2 {
		t.Fatalf("expected 2 mushroom species in public payload, got %d", got)
	}
	if payload.Captures[0].MushroomSpecies[0].LatinName != "Boletus edulis" {
		t.Fatalf("expected first species to stay ordered by probability, got %+v", payload.Captures[0].MushroomSpecies)
	}
	if payload.Captures[0].MushroomSpecies[1].LatinName != "Cantharellus cibarius" {
		t.Fatalf("expected second species in payload, got %+v", payload.Captures[0].MushroomSpecies)
	}
}

func TestAdminID20CanSeeAllMapMarkers(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:             "file:" + filepath.Join(t.TempDir(), "admin-map.db"),
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
		Sub:               "admin-map-author",
		PreferredUsername: "autor-mapy",
		Email:             "admin-map-author@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "author-access",
		RefreshToken: "author-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	now := time.Now().UTC()
	if _, err := database.Exec(`
		INSERT INTO users (
			id, idp_issuer, idp_sub, preferred_username, is_moderator, is_admin, email, email_verified, created_at, updated_at, last_idp_sync_at
		) VALUES (?, ?, ?, ?, 1, 1, ?, 1, ?, ?, ?)
	`, int64(20), "https://ahoj420.eu", "admin-map-special", "Houbamzdar", "admin-map@example.test", now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert admin id 20: %v", err)
	}

	makeCapture := func(id string, status string, publicKey string, free bool, hidden bool, lat, lon float64) *models.Capture {
		capture := &models.Capture{
			ID:                id,
			UserID:            author.ID,
			OriginalFileName:  id + ".jpg",
			ContentType:       "image/jpeg",
			SizeBytes:         4096,
			Width:             1200,
			Height:            900,
			CapturedAt:        now,
			UploadedAt:        now,
			Latitude:          &lat,
			Longitude:         &lon,
			Status:            "private",
			PrivateStorageKey: "captures/private/" + id + ".jpg",
			CoordinatesFree:   free,
		}
		if err := database.CreateCapture(capture); err != nil {
			t.Fatalf("create capture %s: %v", id, err)
		}
		if status == "published" {
			if err := database.PublishCapture(capture.ID, author.ID, publicKey); err != nil {
				t.Fatalf("publish capture %s: %v", id, err)
			}
		}
		if hidden {
			if _, err := database.Exec(`UPDATE photo_captures SET moderator_hidden = 1 WHERE id = ?`, capture.ID); err != nil {
				t.Fatalf("hide capture %s: %v", id, err)
			}
		}
		reloaded, err := database.GetCaptureByID(capture.ID)
		if err != nil {
			t.Fatalf("reload capture %s: %v", id, err)
		}
		return reloaded
	}

	freePublished := makeCapture("admin-map-free", "published", "captures/published/free.jpg", true, false, 50.08, 14.43)
	lockedPublished := makeCapture("admin-map-locked", "published", "captures/published/locked.jpg", false, false, 49.20, 16.61)
	hiddenPublished := makeCapture("admin-map-hidden", "published", "captures/published/hidden.jpg", false, true, 49.75, 13.38)
	privateCapture := makeCapture("admin-map-private", "private", "", false, false, 49.59, 17.25)

	srv := New(cfg, database, nil, nil)

	guestReq := httptest.NewRequest(http.MethodGet, "/api/public/map-captures?limit=10&offset=0", nil)
	guestRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(guestRec, guestReq)
	if guestRec.Code != http.StatusOK {
		t.Fatalf("expected guest status 200, got %d: %s", guestRec.Code, guestRec.Body.String())
	}

	var guestPayload struct {
		OK                bool              `json:"ok"`
		InternalAdminView bool              `json:"internal_admin_view"`
		Captures          []*models.Capture `json:"captures"`
	}
	if err := json.NewDecoder(guestRec.Body).Decode(&guestPayload); err != nil {
		t.Fatalf("decode guest map payload: %v", err)
	}
	if guestPayload.InternalAdminView {
		t.Fatalf("guest must not get internal admin map mode")
	}
	if len(guestPayload.Captures) != 1 || guestPayload.Captures[0].ID != freePublished.ID {
		t.Fatalf("expected only free published capture for guest, got %+v", guestPayload.Captures)
	}

	if err := database.CreateSession(&models.Session{
		SessionID: "admin-map-session",
		UserID:    20,
		CreatedAt: now,
		ExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create admin session: %v", err)
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/api/public/map-captures?limit=10&offset=0", nil)
	adminReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: "admin-map-session"})
	adminRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("expected admin status 200, got %d: %s", adminRec.Code, adminRec.Body.String())
	}

	var adminPayload struct {
		OK                bool              `json:"ok"`
		InternalAdminView bool              `json:"internal_admin_view"`
		Captures          []*models.Capture `json:"captures"`
	}
	if err := json.NewDecoder(adminRec.Body).Decode(&adminPayload); err != nil {
		t.Fatalf("decode admin map payload: %v", err)
	}
	if !adminPayload.InternalAdminView {
		t.Fatalf("expected internal admin map mode for admin id 20")
	}
	if len(adminPayload.Captures) != 4 {
		t.Fatalf("expected 4 captures for internal admin map, got %d", len(adminPayload.Captures))
	}

	capturesByID := make(map[string]*models.Capture, len(adminPayload.Captures))
	for _, capture := range adminPayload.Captures {
		capturesByID[capture.ID] = capture
	}
	for _, captureID := range []string{freePublished.ID, lockedPublished.ID, hiddenPublished.ID, privateCapture.ID} {
		capture := capturesByID[captureID]
		if capture == nil {
			t.Fatalf("expected capture %s in internal admin map", captureID)
		}
		if capture.Latitude == nil || capture.Longitude == nil || capture.CoordinatesLocked {
			t.Fatalf("expected visible coordinates for internal admin map capture %s: %+v", captureID, capture)
		}
	}
	if !capturesByID[hiddenPublished.ID].ModeratorHidden {
		t.Fatalf("expected hidden published capture to stay marked as hidden in internal admin map")
	}
	if capturesByID[privateCapture.ID].Status != "private" {
		t.Fatalf("expected private capture to stay private in internal admin map payload, got %q", capturesByID[privateCapture.ID].Status)
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
			ImageURL          string `json:"image_url"`
			ReviewMode        string `json:"review_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode validator request: %v", err)
		}
		if body.ReviewMode != "moderator_recheck" {
			t.Fatalf("expected moderator_recheck mode, got %q", body.ReviewMode)
		}
		if body.ImageURL == "" {
			t.Fatalf("expected optimizer image_url in validator request")
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

	var (
		actionKind       string
		actionTargetUser int64
	)
	if err := database.QueryRow(`
		SELECT action_kind, COALESCE(target_user_id, 0)
		FROM moderation_actions
		WHERE target_capture_id = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, capture.ID).Scan(&actionKind, &actionTargetUser); err != nil {
		t.Fatalf("load moderation action: %v", err)
	}
	if actionKind != "capture_ai_review_updated" {
		t.Fatalf("expected capture_ai_review_updated action, got %q", actionKind)
	}
	if actionTargetUser != owner.ID {
		t.Fatalf("expected target_user_id %d, got %d", owner.ID, actionTargetUser)
	}
}

func TestModeratorCanOverrideCaptureTaxonomy(t *testing.T) {
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

	owner, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "moderator-taxonomy-owner",
		PreferredUsername: "atlas",
		Email:             "atlas@example.test",
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
		Sub:               "moderator-taxonomy-moderator",
		PreferredUsername: "houbamzdar",
		Email:             "moderator-taxonomy@example.test",
	}, &oauth2.Token{
		AccessToken:  "moderator-access",
		RefreshToken: "moderator-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create moderator: %v", err)
	}

	sessionID := "moderator-taxonomy-session"
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
		ID:                "moderator-taxonomy-capture",
		UserID:            owner.ID,
		OriginalFileName:  "taxonomy.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         4096,
		Width:             1280,
		Height:            960,
		CapturedAt:        time.Date(2026, time.March, 16, 12, 0, 0, 0, time.UTC),
		UploadedAt:        time.Date(2026, time.March, 16, 12, 0, 0, 0, time.UTC),
		Latitude:          &lat,
		Longitude:         &lon,
		Status:            "published",
		PrivateStorageKey: "captures/private/moderator-taxonomy-capture.jpg",
		PublicStorageKey:  "captures/published/owner/moderator-taxonomy-capture.jpg",
		PublishedAt:       time.Date(2026, time.March, 16, 12, 5, 0, 0, time.UTC),
	}
	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create capture: %v", err)
	}

	srv := New(cfg, database, nil, &media.BunnyStorage{})

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/moderation/captures/"+capture.ID+"/taxonomy",
		strings.NewReader(`{"species":[{"latin_name":"Amanita muscaria","czech_official_name":"muchomůrka červená","probability":0.97},{"latin_name":"Amanita sp.","czech_official_name":"muchomůrka","probability":82}],"note":"manual correction"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK       bool                             `json:"ok"`
		Analysis *models.CaptureMushroomAnalysis  `json:"analysis"`
		Species  []*models.CaptureMushroomSpecies `json:"species"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.OK || payload.Analysis == nil {
		t.Fatalf("unexpected response payload: %+v", payload)
	}
	if payload.Analysis.ModelCode != "manual_override" {
		t.Fatalf("expected manual_override model code, got %q", payload.Analysis.ModelCode)
	}
	if payload.Analysis.ReviewSource != db.CaptureAnalysisSourceModeratorManualOverride {
		t.Fatalf("expected manual override review source, got %q", payload.Analysis.ReviewSource)
	}
	if len(payload.Species) != 2 {
		t.Fatalf("expected 2 species rows in response, got %d", len(payload.Species))
	}

	var (
		primaryLatin     string
		primaryProb      float64
		reviewSource     string
		reviewedByUser   int64
		modelCode        string
		storedSpecies    int
		actionKind       string
		actionTargetUser int64
	)
	if err := database.QueryRow(`
		SELECT primary_latin_name, primary_probability, review_source, COALESCE(reviewed_by_user_id, 0), COALESCE(model_code, '')
		FROM capture_mushroom_analysis
		WHERE capture_id = ?
	`, capture.ID).Scan(&primaryLatin, &primaryProb, &reviewSource, &reviewedByUser, &modelCode); err != nil {
		t.Fatalf("load stored manual analysis: %v", err)
	}
	if primaryLatin != "Amanita muscaria" {
		t.Fatalf("expected manual primary latin name, got %q", primaryLatin)
	}
	if reviewSource != db.CaptureAnalysisSourceModeratorManualOverride {
		t.Fatalf("expected manual review source, got %q", reviewSource)
	}
	if reviewedByUser != moderator.ID {
		t.Fatalf("expected reviewed_by_user_id %d, got %d", moderator.ID, reviewedByUser)
	}
	if modelCode != "manual_override" {
		t.Fatalf("expected model_code manual_override, got %q", modelCode)
	}
	if primaryProb < 0.96 || primaryProb > 0.98 {
		t.Fatalf("expected primary probability around 0.97, got %f", primaryProb)
	}

	if err := database.QueryRow(`SELECT COUNT(*) FROM capture_mushroom_species WHERE capture_id = ?`, capture.ID).Scan(&storedSpecies); err != nil {
		t.Fatalf("count stored species: %v", err)
	}
	if storedSpecies != 2 {
		t.Fatalf("expected 2 stored species rows, got %d", storedSpecies)
	}

	if err := database.QueryRow(`
		SELECT action_kind, COALESCE(target_user_id, 0)
		FROM moderation_actions
		WHERE target_capture_id = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, capture.ID).Scan(&actionKind, &actionTargetUser); err != nil {
		t.Fatalf("load moderation action: %v", err)
	}
	if actionKind != "capture_taxonomy_updated" {
		t.Fatalf("expected capture_taxonomy_updated action, got %q", actionKind)
	}
	if actionTargetUser != owner.ID {
		t.Fatalf("expected target_user_id %d, got %d", owner.ID, actionTargetUser)
	}
}

func TestModeratorGeoEditingRespectsUnlockPrivacy(t *testing.T) {
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

	owner, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "moderator-geo-owner",
		PreferredUsername: "geo-owner",
		Email:             "geo-owner@example.test",
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
		Sub:               "moderator-geo-moderator",
		PreferredUsername: "houbamzdar",
		Email:             "geo-moderator@example.test",
	}, &oauth2.Token{
		AccessToken:  "moderator-access",
		RefreshToken: "moderator-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create moderator: %v", err)
	}

	sessionID := "moderator-geo-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    moderator.ID,
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("create moderator session: %v", err)
	}

	lat := 49.3123
	lon := 16.4021
	capture := &models.Capture{
		ID:                "moderator-geo-capture",
		UserID:            owner.ID,
		OriginalFileName:  "geo.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         4096,
		Width:             1200,
		Height:            800,
		CapturedAt:        time.Date(2026, time.March, 16, 13, 0, 0, 0, time.UTC),
		UploadedAt:        time.Date(2026, time.March, 16, 13, 0, 0, 0, time.UTC),
		Latitude:          &lat,
		Longitude:         &lon,
		CoordinatesFree:   false,
		Status:            "published",
		PrivateStorageKey: "captures/private/moderator-geo-capture.jpg",
		PublicStorageKey:  "captures/published/owner/moderator-geo-capture.jpg",
		PublishedAt:       time.Date(2026, time.March, 16, 13, 5, 0, 0, time.UTC),
	}
	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create capture: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO capture_geo_index (
			capture_id, country_code, kraj_name, kraj_name_norm, okres_name, okres_name_norm, obec_name, obec_name_norm, raw_json, resolved_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		capture.ID,
		"CZ",
		"Jihomoravský kraj",
		"jihomoravsky kraj",
		"Brno-venkov",
		"brno-venkov",
		"Lomnice",
		"lomnice",
		"{}",
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert geo index: %v", err)
	}

	srv := New(cfg, database, nil, &media.BunnyStorage{})

	getReq := httptest.NewRequest(http.MethodGet, "/api/moderation/captures/"+capture.ID+"/geo", nil)
	getReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	getRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	var getPayload struct {
		OK      bool `json:"ok"`
		Capture struct {
			ID                      string `json:"id"`
			CoordinatesFree         bool   `json:"coordinates_free"`
			CanViewDetailedLocation bool   `json:"can_view_detailed_location"`
		} `json:"capture"`
		Geo struct {
			CountryCode string `json:"country_code"`
			KrajName    string `json:"kraj_name"`
			OkresName   string `json:"okres_name"`
			ObecName    string `json:"obec_name"`
		} `json:"geo"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&getPayload); err != nil {
		t.Fatalf("decode geo payload: %v", err)
	}
	if !getPayload.OK || getPayload.Capture.CanViewDetailedLocation {
		t.Fatalf("expected geo payload without detailed access, got %+v", getPayload)
	}
	if getPayload.Geo.CountryCode != "CZ" || getPayload.Geo.KrajName != "Jihomoravský kraj" {
		t.Fatalf("expected visible top-level geo values, got %+v", getPayload.Geo)
	}
	if getPayload.Geo.OkresName != "" || getPayload.Geo.ObecName != "" {
		t.Fatalf("expected hidden detailed geo values before unlock, got %+v", getPayload.Geo)
	}

	forbiddenReq := httptest.NewRequest(
		http.MethodPost,
		"/api/moderation/captures/"+capture.ID+"/geo",
		strings.NewReader(`{"country_code":"CZ","kraj_name":"Kraj Vysočina","okres_name":"Žďár nad Sázavou","note":"should fail"}`),
	)
	forbiddenReq.Header.Set("Content-Type", "application/json")
	forbiddenReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	forbiddenRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 when editing hidden detailed location, got %d: %s", forbiddenRec.Code, forbiddenRec.Body.String())
	}

	saveReq := httptest.NewRequest(
		http.MethodPost,
		"/api/moderation/captures/"+capture.ID+"/geo",
		strings.NewReader(`{"country_code":"CZ","kraj_name":"Kraj Vysočina","note":"manual geo fix"}`),
	)
	saveReq.Header.Set("Content-Type", "application/json")
	saveReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	saveRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(saveRec, saveReq)
	if saveRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 when saving top-level geo, got %d: %s", saveRec.Code, saveRec.Body.String())
	}

	storedGeo, err := database.GetCaptureGeoIndex(capture.ID)
	if err != nil {
		t.Fatalf("load stored geo index: %v", err)
	}
	if storedGeo.KrajName != "Kraj Vysočina" {
		t.Fatalf("expected updated kraj_name, got %q", storedGeo.KrajName)
	}
	if storedGeo.OkresName != "Brno-venkov" || storedGeo.ObecName != "Lomnice" {
		t.Fatalf("expected hidden detailed geo values to be preserved, got %+v", storedGeo)
	}

	if err := database.GrantAuthBonuses(moderator.ID, true, false, false); err != nil {
		t.Fatalf("grant moderator bonus: %v", err)
	}
	if _, _, err := database.UnlockCaptureCoordinates(moderator.ID, capture.ID); err != nil {
		t.Fatalf("unlock capture coordinates for moderator: %v", err)
	}

	getUnlockedReq := httptest.NewRequest(http.MethodGet, "/api/moderation/captures/"+capture.ID+"/geo", nil)
	getUnlockedReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	getUnlockedRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(getUnlockedRec, getUnlockedReq)
	if getUnlockedRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 after unlock, got %d: %s", getUnlockedRec.Code, getUnlockedRec.Body.String())
	}

	var unlockedPayload struct {
		OK      bool `json:"ok"`
		Capture struct {
			CanViewDetailedLocation bool `json:"can_view_detailed_location"`
		} `json:"capture"`
		Geo struct {
			OkresName string `json:"okres_name"`
			ObecName  string `json:"obec_name"`
		} `json:"geo"`
	}
	if err := json.NewDecoder(getUnlockedRec.Body).Decode(&unlockedPayload); err != nil {
		t.Fatalf("decode unlocked geo payload: %v", err)
	}
	if !unlockedPayload.OK || !unlockedPayload.Capture.CanViewDetailedLocation {
		t.Fatalf("expected detailed geo access after unlock, got %+v", unlockedPayload)
	}
	if unlockedPayload.Geo.OkresName != "Brno-venkov" || unlockedPayload.Geo.ObecName != "Lomnice" {
		t.Fatalf("expected detailed geo values after unlock, got %+v", unlockedPayload.Geo)
	}

	var (
		actionKind       string
		actionTargetUser int64
	)
	if err := database.QueryRow(`
		SELECT action_kind, COALESCE(target_user_id, 0)
		FROM moderation_actions
		WHERE target_capture_id = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, capture.ID).Scan(&actionKind, &actionTargetUser); err != nil {
		t.Fatalf("load moderation geo action: %v", err)
	}
	if actionKind != "capture_geo_updated" {
		t.Fatalf("expected capture_geo_updated action, got %q", actionKind)
	}
	if actionTargetUser != owner.ID {
		t.Fatalf("expected target_user_id %d, got %d", owner.ID, actionTargetUser)
	}
}
