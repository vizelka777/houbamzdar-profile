package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/backup"
	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/models"
	"golang.org/x/oauth2"
)

type fakeBackupStorage struct {
	lastKey     string
	lastPayload []byte
}

func (f *fakeBackupStorage) CanReadPrivate() bool { return true }

func (f *fakeBackupStorage) StorePrivateObject(_ context.Context, key string, payload []byte, _ string) error {
	f.lastKey = key
	f.lastPayload = append([]byte(nil), payload...)
	return nil
}

func TestAdminOverviewAndUserDirectoryEndpoints(t *testing.T) {
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

	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}

	adminUser, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "admin-overview-admin",
		PreferredUsername: "site-admin",
		Email:             "admin@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	adminUser, err = database.BootstrapAdminByUserID(adminUser.ID, false)
	if err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}

	moderator, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "admin-overview-moderator",
		PreferredUsername: "lesni-moderator",
		Email:             "moderator@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create moderator: %v", err)
	}
	if _, err := database.Exec(`UPDATE users SET is_moderator = 1 WHERE id = ?`, moderator.ID); err != nil {
		t.Fatalf("promote moderator: %v", err)
	}
	moderator, err = database.GetUser(moderator.ID)
	if err != nil {
		t.Fatalf("reload moderator: %v", err)
	}

	restrictedUser, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "admin-overview-restricted",
		PreferredUsername: "restricted-user",
		Email:             "restricted@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create restricted user: %v", err)
	}

	if err := database.SetUserRestrictions(
		restrictedUser.ID,
		moderator.ID,
		time.Now().UTC().Add(4*time.Hour),
		time.Time{},
		time.Now().UTC().Add(2*time.Hour),
		"manual_review",
		"restricted by moderator",
	); err != nil {
		t.Fatalf("set restrictions: %v", err)
	}

	sessionID := "admin-overview-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    adminUser.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create admin session: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	t.Run("overview", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
		req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
		rec := httptest.NewRecorder()
		srv.Router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var payload struct {
			OK       bool `json:"ok"`
			Overview struct {
				TotalUsers      int `json:"total_users"`
				AdminUsers      int `json:"admin_users"`
				ModeratorUsers  int `json:"moderator_users"`
				StaffUsers      int `json:"staff_users"`
				RestrictedUsers int `json:"restricted_users"`
				BannedUsers     int `json:"banned_users"`
			} `json:"overview"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode overview payload: %v", err)
		}

		if !payload.OK {
			t.Fatalf("expected ok=true")
		}
		if payload.Overview.TotalUsers != 3 {
			t.Fatalf("expected total_users=3, got %d", payload.Overview.TotalUsers)
		}
		if payload.Overview.AdminUsers != 1 {
			t.Fatalf("expected admin_users=1, got %d", payload.Overview.AdminUsers)
		}
		if payload.Overview.ModeratorUsers != 1 {
			t.Fatalf("expected moderator_users=1, got %d", payload.Overview.ModeratorUsers)
		}
		if payload.Overview.StaffUsers != 2 {
			t.Fatalf("expected staff_users=2, got %d", payload.Overview.StaffUsers)
		}
		if payload.Overview.RestrictedUsers != 1 || payload.Overview.BannedUsers != 1 {
			t.Fatalf("unexpected restriction counters: %+v", payload.Overview)
		}
	})

	t.Run("filtered users", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/users?query=restricted&status=restricted&limit=10&offset=0", nil)
		req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
		rec := httptest.NewRecorder()
		srv.Router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var payload struct {
			OK      bool `json:"ok"`
			Total   int  `json:"total"`
			HasMore bool `json:"has_more"`
			Users   []struct {
				ID                  int64  `json:"id"`
				PreferredUsername   string `json:"preferred_username"`
				IsAdmin             bool   `json:"is_admin"`
				IsModerator         bool   `json:"is_moderator"`
				IsBanned            bool   `json:"is_banned"`
				PublishingSuspended bool   `json:"publishing_suspended"`
				ModerationNote      string `json:"moderation_note"`
			} `json:"users"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode users payload: %v", err)
		}

		if !payload.OK || payload.Total != 1 || payload.HasMore || len(payload.Users) != 1 {
			t.Fatalf("unexpected users payload: %+v", payload)
		}
		if payload.Users[0].ID != restrictedUser.ID {
			t.Fatalf("expected user id %d, got %d", restrictedUser.ID, payload.Users[0].ID)
		}
		if payload.Users[0].PreferredUsername != restrictedUser.PreferredUsername {
			t.Fatalf("expected username %q, got %q", restrictedUser.PreferredUsername, payload.Users[0].PreferredUsername)
		}
		if !payload.Users[0].IsBanned || !payload.Users[0].PublishingSuspended {
			t.Fatalf("expected active restrictions in response, got %+v", payload.Users[0])
		}
		if payload.Users[0].IsAdmin || payload.Users[0].IsModerator {
			t.Fatalf("expected regular user, got %+v", payload.Users[0])
		}
		if payload.Users[0].ModerationNote != "restricted by moderator" {
			t.Fatalf("expected moderation note to survive, got %q", payload.Users[0].ModerationNote)
		}
	})
}

func TestAdminRoleManagementRouteDisabled(t *testing.T) {
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

	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}

	adminUser, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "admin-role-disabled-admin",
		PreferredUsername: "site-admin",
		Email:             "role-disabled-admin@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	adminUser, err = database.BootstrapAdminByUserID(adminUser.ID, false)
	if err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}

	targetUser, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "admin-role-target",
		PreferredUsername: "target-role-user",
		Email:             "target-role-user@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create target user: %v", err)
	}

	sessionID := "admin-role-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    adminUser.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create admin session: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/admin/users/"+strconv.FormatInt(targetUser.ID, 10)+"/roles",
		nil,
	)
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestModeratorCannotAccessAdminEndpoints(t *testing.T) {
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

	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}

	moderator, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "admin-access-moderator",
		PreferredUsername: "field-moderator",
		Email:             "admin-access-moderator@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create moderator: %v", err)
	}
	if _, err := database.Exec(`UPDATE users SET is_moderator = 1 WHERE id = ?`, moderator.ID); err != nil {
		t.Fatalf("promote moderator: %v", err)
	}
	moderator, err = database.GetUser(moderator.ID)
	if err != nil {
		t.Fatalf("reload moderator: %v", err)
	}
	if !moderator.IsModerator || moderator.IsAdmin {
		t.Fatalf("expected plain moderator, got %+v", moderator)
	}

	sessionID := "admin-access-moderator-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    moderator.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create moderator session: %v", err)
	}

	srv := New(cfg, database, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	req.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminBackupEndpoints(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		DBURL:             "file:" + filepath.Join(t.TempDir(), "test.db"),
		FrontOrigin:       "https://houbamzdar.cz",
		AppBaseURL:        "https://api.houbamzdar.cz",
		SessionCookieName: "hzd_session",
	}

	database, err := db.New(cfg)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}

	adminUser, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "backup-admin",
		PreferredUsername: "site-admin",
		Email:             "backup-admin@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	adminUser, err = database.BootstrapAdminByUserID(adminUser.ID, false)
	if err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}

	storage := &fakeBackupStorage{}
	sessionID := "admin-backup-session"
	if err := database.CreateSession(&models.Session{
		SessionID: sessionID,
		UserID:    adminUser.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create admin session: %v", err)
	}

	srv := New(cfg, database, nil, nil)
	srv.Backup = backup.NewWithStorage(cfg, database, storage)

	runReq := httptest.NewRequest(http.MethodPost, "/api/admin/backups/run", nil)
	runReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	runRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(runRec, runReq)

	if runRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", runRec.Code, runRec.Body.String())
	}
	if storage.lastKey == "" || len(storage.lastPayload) == 0 {
		t.Fatalf("expected backup payload to be uploaded")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/backups?limit=10&offset=0", nil)
	listReq.AddCookie(&http.Cookie{Name: cfg.SessionCookieName, Value: sessionID})
	listRec := httptest.NewRecorder()
	srv.Router.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", listRec.Code, listRec.Body.String())
	}

	var payload struct {
		OK      bool `json:"ok"`
		Total   int  `json:"total"`
		Backups []struct {
			Status      string `json:"status"`
			TriggerKind string `json:"trigger_kind"`
			StorageKey  string `json:"storage_key"`
		} `json:"backups"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode backup list payload: %v", err)
	}
	if !payload.OK || payload.Total != 1 || len(payload.Backups) != 1 {
		t.Fatalf("unexpected backup payload: %+v", payload)
	}
	if payload.Backups[0].Status != "completed" || payload.Backups[0].TriggerKind != "manual" {
		t.Fatalf("unexpected backup row: %+v", payload.Backups[0])
	}
	if payload.Backups[0].StorageKey == "" {
		t.Fatalf("expected storage key in backup history")
	}
}
