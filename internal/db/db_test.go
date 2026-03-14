package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/houbamzdar/bff/internal/models"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"golang.org/x/oauth2"
	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("libsql", "file:"+path)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := db.Ping(); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	return db
}

func userColumnSet(t *testing.T, db *sql.DB) map[string]struct{} {
	t.Helper()

	rows, err := db.Query("PRAGMA table_info(users)")
	if err != nil {
		t.Fatalf("pragma table_info(users): %v", err)
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			t.Fatalf("scan pragma row: %v", err)
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pragma rows: %v", err)
	}
	return columns
}

func tableColumnSet(t *testing.T, db *sql.DB, tableName string) map[string]struct{} {
	t.Helper()

	rows, err := db.Query("PRAGMA table_info(" + tableName + ")")
	if err != nil {
		t.Fatalf("pragma table_info(%s): %v", tableName, err)
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			t.Fatalf("scan pragma row for %s: %v", tableName, err)
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pragma rows for %s: %v", tableName, err)
	}
	return columns
}

func TestMigrateUpgradesLegacyUsersTable(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	legacySchema := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			idp_issuer TEXT NOT NULL,
			idp_sub TEXT NOT NULL,
			preferred_username TEXT,
			email TEXT,
			email_verified INTEGER NOT NULL DEFAULT 0,
			phone_number TEXT,
			phone_number_verified INTEGER NOT NULL DEFAULT 0,
			picture TEXT,
			about_me TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			last_idp_sync_at TEXT,
			UNIQUE(idp_issuer, idp_sub)
		);`,
		`CREATE TABLE sessions (
			session_id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			expires_at TEXT NOT NULL
		);`,
		`CREATE TABLE oidc_login_state (
			state TEXT PRIMARY KEY,
			nonce TEXT NOT NULL,
			pkce_verifier TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			expires_at TEXT NOT NULL,
			post_login_redirect TEXT
		);`,
	}
	for _, query := range legacySchema {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("apply legacy schema: %v", err)
		}
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}

	columns := userColumnSet(t, db)
	for _, name := range []string{"access_token", "refresh_token", "token_expires_at"} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("expected users.%s to exist after migration", name)
		}
	}

	var applied int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE id = ?`,
		migrationUsersTokenColumnsID,
	).Scan(&applied); err != nil {
		t.Fatalf("query schema migration row: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected migration row to be recorded once, got %d", applied)
	}

	wrapped := &DB{db}
	token := &oauth2.Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	}
	user, isNew, err := wrapped.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "user-123",
		PreferredUsername: "houbar",
		Email:             "houbar@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("upsert user on migrated schema: %v", err)
	}
	if !isNew {
		t.Fatalf("expected inserted user to be marked as new")
	}

	got, err := wrapped.GetUser(user.ID)
	if err != nil {
		t.Fatalf("get migrated user: %v", err)
	}
	if got.AccessToken != token.AccessToken {
		t.Fatalf("expected access token to persist, got %q", got.AccessToken)
	}
	if got.RefreshToken != token.RefreshToken {
		t.Fatalf("expected refresh token to persist, got %q", got.RefreshToken)
	}
	if got.TokenExpiresAt.IsZero() {
		t.Fatalf("expected token expiry to persist")
	}
}

func TestMigrateIsIdempotentForFreshSchema(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	if err := migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	columns := userColumnSet(t, db)
	for _, name := range []string{"access_token", "refresh_token", "token_expires_at"} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("expected users.%s to exist after fresh migrate", name)
		}
	}

	var applied int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE id = ?`,
		migrationUsersTokenColumnsID,
	).Scan(&applied); err != nil {
		t.Fatalf("query schema migration row: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected one recorded migration row after repeated migrate, got %d", applied)
	}
}

func TestMigrateCreatesPhotoCapturesTableAndSupportsCRUD(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	columns := tableColumnSet(t, rawDB, "photo_captures")
	for _, name := range []string{
		"id",
		"user_id",
		"client_local_id",
		"original_file_name",
		"content_type",
		"size_bytes",
		"width",
		"height",
		"captured_at",
		"uploaded_at",
		"latitude",
		"longitude",
		"accuracy_meters",
		"status",
		"private_storage_key",
		"public_storage_key",
		"published_at",
		"coordinates_free",
	} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("expected photo_captures.%s to exist", name)
		}
	}

	var applied int
	if err := rawDB.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE id = ?`,
		migrationPhotoCapturesTableID,
	).Scan(&applied); err != nil {
		t.Fatalf("query photo capture migration row: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected photo capture migration row once, got %d", applied)
	}

	database := &DB{rawDB}
	token := &oauth2.Token{
		AccessToken:  "capture-access",
		RefreshToken: "capture-refresh",
		Expiry:       time.Now().Add(2 * time.Hour).UTC(),
	}
	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "capture-user",
		PreferredUsername: "mykolog",
		Email:             "mykolog@example.test",
		EmailVerified:     true,
	}, token)
	if err != nil {
		t.Fatalf("create user for capture test: %v", err)
	}

	lat := 49.12345
	lng := 17.45678
	acc := 6.5
	capturedAt := time.Date(2026, time.March, 8, 10, 11, 12, 0, time.UTC)
	uploadedAt := capturedAt.Add(3 * time.Minute)

	capture := &models.Capture{
		ID:                "capture-001",
		UserID:            user.ID,
		ClientLocalID:     "local-001",
		OriginalFileName:  "lesni-nalez.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         2048,
		Width:             1600,
		Height:            1200,
		CapturedAt:        capturedAt,
		UploadedAt:        uploadedAt,
		Latitude:          &lat,
		Longitude:         &lng,
		AccuracyMeters:    &acc,
		Status:            "private",
		CoordinatesFree:   true,
		PrivateStorageKey: "captures/1/2026/03/capture-001/original.jpg",
	}

	if err := database.CreateCapture(capture); err != nil {
		t.Fatalf("create capture: %v", err)
	}

	captures, err := database.ListCapturesByUser(user.ID)
	if err != nil {
		t.Fatalf("list captures: %v", err)
	}
	if len(captures) != 1 {
		t.Fatalf("expected one capture, got %d", len(captures))
	}
	if captures[0].PrivateStorageKey != capture.PrivateStorageKey {
		t.Fatalf("unexpected private storage key: %q", captures[0].PrivateStorageKey)
	}
	if !captures[0].CoordinatesFree {
		t.Fatalf("expected coordinates_free to persist")
	}

	if err := database.PublishCapture(capture.ID, user.ID, "captures/published/1/2026/03/capture-001.jpg"); err != nil {
		t.Fatalf("publish capture: %v", err)
	}

	published, err := database.GetCaptureForUser(capture.ID, user.ID)
	if err != nil {
		t.Fatalf("get published capture: %v", err)
	}
	if published.Status != "published" {
		t.Fatalf("expected published status, got %q", published.Status)
	}
	if published.PublicStorageKey == "" {
		t.Fatalf("expected public storage key after publish")
	}
	if published.PublishedAt.IsZero() {
		t.Fatalf("expected published_at to be set")
	}

	if err := database.UnpublishCapture(capture.ID, user.ID); err != nil {
		t.Fatalf("unpublish capture: %v", err)
	}

	unpublished, err := database.GetCaptureForUser(capture.ID, user.ID)
	if err != nil {
		t.Fatalf("get unpublished capture: %v", err)
	}
	if unpublished.Status != "private" {
		t.Fatalf("expected private status after unpublish, got %q", unpublished.Status)
	}
	if unpublished.PublicStorageKey != "" {
		t.Fatalf("expected empty public storage key after unpublish, got %q", unpublished.PublicStorageKey)
	}

	if err := database.DeleteCapture(capture.ID, user.ID); err != nil {
		t.Fatalf("delete capture: %v", err)
	}

	if _, err := database.GetCaptureForUser(capture.ID, user.ID); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows after delete, got %v", err)
	}
}

func TestListCapturesPageSupportsStatusDateAndPagination(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	database := &DB{rawDB}
	user, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "paged-user",
		PreferredUsername: "paged",
		Email:             "paged@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "paged-access",
		RefreshToken: "paged-refresh",
		Expiry:       time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	firstDay := time.Date(2026, time.March, 8, 9, 0, 0, 0, time.UTC)
	secondDay := time.Date(2026, time.March, 9, 11, 30, 0, 0, time.UTC)

	captures := []*models.Capture{
		{
			ID:                "cap-1",
			UserID:            user.ID,
			OriginalFileName:  "one.jpg",
			ContentType:       "image/jpeg",
			SizeBytes:         100,
			Width:             1000,
			Height:            800,
			CapturedAt:        firstDay,
			UploadedAt:        firstDay,
			Status:            "private",
			PrivateStorageKey: "captures/1/2026/03/cap-1.jpg",
		},
		{
			ID:                "cap-2",
			UserID:            user.ID,
			OriginalFileName:  "two.jpg",
			ContentType:       "image/jpeg",
			SizeBytes:         200,
			Width:             1200,
			Height:            900,
			CapturedAt:        secondDay,
			UploadedAt:        secondDay,
			Status:            "published",
			PrivateStorageKey: "captures/1/2026/03/cap-2.jpg",
			PublicStorageKey:  "captures/published/1/2026/03/cap-2.jpg",
			PublishedAt:       secondDay.Add(10 * time.Minute),
		},
		{
			ID:                "cap-3",
			UserID:            user.ID,
			OriginalFileName:  "three.jpg",
			ContentType:       "image/jpeg",
			SizeBytes:         300,
			Width:             1400,
			Height:            1000,
			CapturedAt:        secondDay.Add(5 * time.Minute),
			UploadedAt:        secondDay.Add(5 * time.Minute),
			Status:            "private",
			PrivateStorageKey: "captures/1/2026/03/cap-3.jpg",
		},
	}

	for _, capture := range captures {
		if err := database.CreateCapture(capture); err != nil {
			t.Fatalf("create capture %s: %v", capture.ID, err)
		}
	}
	if err := database.PublishCapture("cap-2", user.ID, "captures/published/1/2026/03/cap-2.jpg"); err != nil {
		t.Fatalf("publish cap-2: %v", err)
	}

	privatePage, err := database.ListCapturesPage(user.ID, CaptureListFilters{
		Status:   "private",
		Page:     1,
		PageSize: 1,
	})
	if err != nil {
		t.Fatalf("list private page: %v", err)
	}
	if privatePage.Total != 2 || privatePage.TotalPages != 2 {
		t.Fatalf("unexpected private pagination totals: %+v", privatePage)
	}
	if len(privatePage.Captures) != 1 || privatePage.Captures[0].ID != "cap-3" {
		t.Fatalf("unexpected first private page: %+v", privatePage.Captures)
	}

	capturedFrom := time.Date(2026, time.March, 9, 0, 0, 0, 0, time.UTC)
	capturedTo := capturedFrom.Add(24 * time.Hour)
	dayPage, err := database.ListCapturesPage(user.ID, CaptureListFilters{
		CapturedFrom: &capturedFrom,
		CapturedTo:   &capturedTo,
		Page:         1,
		PageSize:     10,
	})
	if err != nil {
		t.Fatalf("list day page: %v", err)
	}
	if dayPage.Total != 2 {
		t.Fatalf("expected 2 captures on second day, got %d", dayPage.Total)
	}

	publishedPage, err := database.ListCapturesPage(user.ID, CaptureListFilters{
		Status:   "published",
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("list published page: %v", err)
	}
	if publishedPage.Total != 1 || len(publishedPage.Captures) != 1 || publishedPage.Captures[0].ID != "cap-2" {
		t.Fatalf("unexpected published page: %+v", publishedPage.Captures)
	}
}

func TestPostsSupportComments(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	columns := tableColumnSet(t, rawDB, "post_comments")
	for _, name := range []string{"id", "post_id", "user_id", "content", "created_at", "updated_at"} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("expected post_comments.%s to exist", name)
		}
	}

	var applied int
	if err := rawDB.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE id = ?`,
		migrationPostCommentsTableID,
	).Scan(&applied); err != nil {
		t.Fatalf("query post comments migration row: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected post comments migration row once, got %d", applied)
	}

	database := &DB{rawDB}
	author, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "author-user",
		PreferredUsername: "autor",
		Email:             "author@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "author-access",
		RefreshToken: "author-refresh",
		Expiry:       time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	commenter, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "commenter-user",
		PreferredUsername: "komentator",
		Email:             "commenter@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "commenter-access",
		RefreshToken: "commenter-refresh",
		Expiry:       time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create commenter: %v", err)
	}

	postCreatedAt := time.Date(2026, time.March, 14, 10, 0, 0, 0, time.UTC)
	post := &models.Post{
		ID:        "post-comments-001",
		UserID:    author.ID,
		Content:   "Prispevek s komentari",
		Status:    "published",
		CreatedAt: postCreatedAt,
		UpdatedAt: postCreatedAt,
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	firstComment := &models.Comment{
		ID:        "comment-001",
		PostID:    post.ID,
		UserID:    commenter.ID,
		Content:   "Tohle je skvely nalez.",
		CreatedAt: postCreatedAt.Add(5 * time.Minute),
		UpdatedAt: postCreatedAt.Add(5 * time.Minute),
	}
	if err := database.CreatePostComment(firstComment); err != nil {
		t.Fatalf("create first comment: %v", err)
	}

	secondComment := &models.Comment{
		ID:        "comment-002",
		PostID:    post.ID,
		UserID:    author.ID,
		Content:   "Diky, zitra jdu znovu.",
		CreatedAt: postCreatedAt.Add(8 * time.Minute),
		UpdatedAt: postCreatedAt.Add(8 * time.Minute),
	}
	if err := database.CreatePostComment(secondComment); err != nil {
		t.Fatalf("create second comment: %v", err)
	}

	publicPosts, err := database.ListPublicPosts(10, 0, 0)
	if err != nil {
		t.Fatalf("list public posts: %v", err)
	}
	if len(publicPosts) != 1 {
		t.Fatalf("expected 1 public post, got %d", len(publicPosts))
	}
	if len(publicPosts[0].Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(publicPosts[0].Comments))
	}
	if publicPosts[0].Comments[0].Content != firstComment.Content {
		t.Fatalf("unexpected first comment content: %q", publicPosts[0].Comments[0].Content)
	}
	if publicPosts[0].Comments[0].AuthorName != commenter.PreferredUsername {
		t.Fatalf("unexpected first comment author: %q", publicPosts[0].Comments[0].AuthorName)
	}
	if publicPosts[0].Comments[1].AuthorName != author.PreferredUsername {
		t.Fatalf("unexpected second comment author: %q", publicPosts[0].Comments[1].AuthorName)
	}

	ownerPost, err := database.GetPost(post.ID, author.ID)
	if err != nil {
		t.Fatalf("get owner post: %v", err)
	}
	if len(ownerPost.Comments) != 2 {
		t.Fatalf("expected comments on owner post, got %d", len(ownerPost.Comments))
	}

	if err := database.DeletePost(post.ID, author.ID); err != nil {
		t.Fatalf("delete post: %v", err)
	}

	var remainingComments int
	if err := rawDB.QueryRow(`SELECT COUNT(*) FROM post_comments WHERE post_id = ?`, post.ID).Scan(&remainingComments); err != nil {
		t.Fatalf("count remaining comments: %v", err)
	}
	if remainingComments != 0 {
		t.Fatalf("expected 0 remaining comments after delete, got %d", remainingComments)
	}

	var remainingPosts int
	if err := rawDB.QueryRow(`SELECT COUNT(*) FROM posts WHERE id = ?`, post.ID).Scan(&remainingPosts); err != nil {
		t.Fatalf("count remaining posts: %v", err)
	}
	if remainingPosts != 0 {
		t.Fatalf("expected post to be deleted, got %d rows", remainingPosts)
	}
}

func TestPostLikesSupportFeedStateAndDeleteCleanup(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	columns := tableColumnSet(t, rawDB, "post_likes")
	for _, name := range []string{"post_id", "user_id", "created_at"} {
		if _, ok := columns[name]; !ok {
			t.Fatalf("expected post_likes.%s to exist after migration", name)
		}
	}

	var applied int
	if err := rawDB.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE id = ?`,
		migrationPostLikesTableID,
	).Scan(&applied); err != nil {
		t.Fatalf("query likes migration row: %v", err)
	}
	if applied != 1 {
		t.Fatalf("expected likes migration row to be recorded once, got %d", applied)
	}

	database := &DB{rawDB}

	author, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "likes-author",
		PreferredUsername: "autor",
		Email:             "likes-author@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "author-access",
		RefreshToken: "author-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	liker, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "likes-liker",
		PreferredUsername: "fanousek",
		Email:             "likes-liker@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "liker-access",
		RefreshToken: "liker-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create liker: %v", err)
	}

	postCreatedAt := time.Date(2026, time.March, 14, 14, 0, 0, 0, time.UTC)
	post := &models.Post{
		ID:        "post-likes-001",
		UserID:    author.ID,
		Content:   "Prispevek s lajky",
		Status:    "published",
		CreatedAt: postCreatedAt,
		UpdatedAt: postCreatedAt,
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	likesCount, isLiked, err := database.TogglePostLike(post.ID, liker.ID)
	if err != nil {
		t.Fatalf("toggle like: %v", err)
	}
	if likesCount != 1 || !isLiked {
		t.Fatalf("unexpected toggle like result: count=%d isLiked=%v", likesCount, isLiked)
	}

	likedPosts, err := database.ListPublicPosts(10, 0, liker.ID)
	if err != nil {
		t.Fatalf("list public posts for liker: %v", err)
	}
	if len(likedPosts) != 1 {
		t.Fatalf("expected 1 public post for liker, got %d", len(likedPosts))
	}
	if likedPosts[0].LikesCount != 1 || !likedPosts[0].IsLikedByMe {
		t.Fatalf("unexpected liker feed state: likes=%d likedByMe=%v", likedPosts[0].LikesCount, likedPosts[0].IsLikedByMe)
	}

	guestPosts, err := database.ListPublicPosts(10, 0, 0)
	if err != nil {
		t.Fatalf("list public posts for guest: %v", err)
	}
	if len(guestPosts) != 1 {
		t.Fatalf("expected 1 public post for guest, got %d", len(guestPosts))
	}
	if guestPosts[0].LikesCount != 1 || guestPosts[0].IsLikedByMe {
		t.Fatalf("unexpected guest feed state: likes=%d likedByMe=%v", guestPosts[0].LikesCount, guestPosts[0].IsLikedByMe)
	}

	if err := database.DeletePost(post.ID, author.ID); err != nil {
		t.Fatalf("delete post: %v", err)
	}

	var remainingLikes int
	if err := rawDB.QueryRow(`SELECT COUNT(*) FROM post_likes WHERE post_id = ?`, post.ID).Scan(&remainingLikes); err != nil {
		t.Fatalf("count remaining likes: %v", err)
	}
	if remainingLikes != 0 {
		t.Fatalf("expected 0 remaining likes after delete, got %d", remainingLikes)
	}
}

func TestPublicFeedsMaskCoordinatesWhenNotFree(t *testing.T) {
	t.Parallel()

	rawDB := openTestDB(t)
	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	database := &DB{rawDB}
	author, _, err := database.UpsertUser(&models.OIDCClaims{
		Iss:               "https://ahoj420.eu",
		Sub:               "masking-author",
		PreferredUsername: "mask-author",
		Email:             "masking-author@example.test",
		EmailVerified:     true,
	}, &oauth2.Token{
		AccessToken:  "masking-author-access",
		RefreshToken: "masking-author-refresh",
		Expiry:       time.Now().Add(time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	lat := 49.98765
	lng := 15.12345
	acc := 11.2
	capturedAt := time.Date(2026, time.March, 14, 15, 30, 0, 0, time.UTC)
	uploadedAt := capturedAt.Add(time.Minute)

	maskedCapture := &models.Capture{
		ID:                "capture-masked-001",
		UserID:            author.ID,
		ClientLocalID:     "masked-001",
		OriginalFileName:  "masked.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         1024,
		Width:             640,
		Height:            480,
		CapturedAt:        capturedAt,
		UploadedAt:        uploadedAt,
		Latitude:          &lat,
		Longitude:         &lng,
		AccuracyMeters:    &acc,
		Status:            "private",
		CoordinatesFree:   false,
		PrivateStorageKey: "captures/private/masked.jpg",
	}
	if err := database.CreateCapture(maskedCapture); err != nil {
		t.Fatalf("create masked capture: %v", err)
	}
	if err := database.PublishCapture(maskedCapture.ID, author.ID, "captures/public/masked.jpg"); err != nil {
		t.Fatalf("publish masked capture: %v", err)
	}

	freeCapture := &models.Capture{
		ID:                "capture-free-001",
		UserID:            author.ID,
		ClientLocalID:     "free-001",
		OriginalFileName:  "free.jpg",
		ContentType:       "image/jpeg",
		SizeBytes:         2048,
		Width:             1200,
		Height:            900,
		CapturedAt:        capturedAt.Add(2 * time.Minute),
		UploadedAt:        uploadedAt.Add(2 * time.Minute),
		Latitude:          &lat,
		Longitude:         &lng,
		AccuracyMeters:    &acc,
		Status:            "private",
		CoordinatesFree:   true,
		PrivateStorageKey: "captures/private/free.jpg",
	}
	if err := database.CreateCapture(freeCapture); err != nil {
		t.Fatalf("create free capture: %v", err)
	}
	if err := database.PublishCapture(freeCapture.ID, author.ID, "captures/public/free.jpg"); err != nil {
		t.Fatalf("publish free capture: %v", err)
	}

	postCreatedAt := capturedAt.Add(4 * time.Minute)
	post := &models.Post{
		ID:        "post-mask-001",
		UserID:    author.ID,
		Content:   "Post with mixed coordinate visibility",
		Status:    "published",
		CreatedAt: postCreatedAt,
		UpdatedAt: postCreatedAt,
		Captures: []*models.Capture{
			{ID: maskedCapture.ID},
			{ID: freeCapture.ID},
		},
	}
	if err := database.CreatePost(post); err != nil {
		t.Fatalf("create post: %v", err)
	}

	publicCaptures, err := database.ListPublicCaptures(10, 0)
	if err != nil {
		t.Fatalf("list public captures: %v", err)
	}
	if len(publicCaptures) != 2 {
		t.Fatalf("expected 2 public captures, got %d", len(publicCaptures))
	}

	captureByID := map[string]*models.Capture{}
	for _, capture := range publicCaptures {
		captureByID[capture.ID] = capture
	}

	if captureByID[maskedCapture.ID].CoordinatesFree {
		t.Fatalf("expected masked capture to keep coordinates_free=false")
	}
	if captureByID[maskedCapture.ID].Latitude != nil || captureByID[maskedCapture.ID].Longitude != nil || captureByID[maskedCapture.ID].AccuracyMeters != nil {
		t.Fatalf("expected masked capture coordinates to be hidden in public captures")
	}
	if captureByID[freeCapture.ID].Latitude == nil || captureByID[freeCapture.ID].Longitude == nil || captureByID[freeCapture.ID].AccuracyMeters == nil {
		t.Fatalf("expected free capture coordinates to remain visible in public captures")
	}

	publicPosts, err := database.ListPublicPosts(10, 0, 0)
	if err != nil {
		t.Fatalf("list public posts: %v", err)
	}
	if len(publicPosts) != 1 {
		t.Fatalf("expected 1 public post, got %d", len(publicPosts))
	}
	if len(publicPosts[0].Captures) != 2 {
		t.Fatalf("expected 2 post captures, got %d", len(publicPosts[0].Captures))
	}

	postCaptureByID := map[string]*models.Capture{}
	for _, capture := range publicPosts[0].Captures {
		postCaptureByID[capture.ID] = capture
	}

	if postCaptureByID[maskedCapture.ID].Latitude != nil || postCaptureByID[maskedCapture.ID].Longitude != nil || postCaptureByID[maskedCapture.ID].AccuracyMeters != nil {
		t.Fatalf("expected masked capture coordinates to be hidden in public posts")
	}
	if postCaptureByID[freeCapture.ID].Latitude == nil || postCaptureByID[freeCapture.ID].Longitude == nil || postCaptureByID[freeCapture.ID].AccuracyMeters == nil {
		t.Fatalf("expected free capture coordinates to remain visible in public posts")
	}

	ownerPost, err := database.GetPost(post.ID, author.ID)
	if err != nil {
		t.Fatalf("get owner post: %v", err)
	}
	ownerCaptureByID := map[string]*models.Capture{}
	for _, capture := range ownerPost.Captures {
		ownerCaptureByID[capture.ID] = capture
	}
	if ownerCaptureByID[maskedCapture.ID].Latitude == nil || ownerCaptureByID[maskedCapture.ID].Longitude == nil {
		t.Fatalf("expected owner to still see full coordinates")
	}
}
