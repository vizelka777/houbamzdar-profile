package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/houbamzdar/bff/internal/config"
)

func TestCORSPreflightAllowsDeleteForCaptures(t *testing.T) {
	t.Parallel()

	srv := New(&config.Config{
		FrontOrigin:       "https://houbamzdar.cz",
		SessionCookieName: "hzd_session",
	}, nil, nil, nil)

	req := httptest.NewRequest(http.MethodOptions, "/api/captures/test-capture", nil)
	req.Header.Set("Origin", "https://houbamzdar.cz")
	req.Header.Set("Access-Control-Request-Method", http.MethodDelete)

	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://houbamzdar.cz" {
		t.Fatalf("expected Access-Control-Allow-Origin to match front origin, got %q", got)
	}

	allowedMethods := rec.Header().Get("Access-Control-Allow-Methods")
	if !strings.Contains(allowedMethods, http.MethodDelete) {
		t.Fatalf("expected Access-Control-Allow-Methods to include DELETE, got %q", allowedMethods)
	}
}
