package server

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/media"
	"github.com/houbamzdar/bff/internal/models"
)

const maxCaptureUploadSize = 20 << 20

func (s *Server) handleListCaptures(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)

	filters := parseCaptureListFilters(r)
	result, err := s.DB.ListCapturesPage(user.ID, filters)
	if err != nil {
		http.Error(w, "failed to load captures", http.StatusInternalServerError)
		return
	}

	attachPublicURLs(result.Captures, s.Media)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"captures":    result.Captures,
		"page":        result.Page,
		"page_size":   result.PageSize,
		"total":       result.Total,
		"total_pages": result.TotalPages,
	})
}

func (s *Server) handleCreateCapture(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)

	if s.Media == nil || !s.Media.Enabled() {
		http.Error(w, "capture storage is not configured", http.StatusServiceUnavailable)
		return
	}

	if err := r.ParseMultipartForm(maxCaptureUploadSize); err != nil {
		http.Error(w, "invalid multipart upload", http.StatusBadRequest)
		return
	}

	file, fileHeader, err := r.FormFile("photo")
	if err != nil {
		http.Error(w, "photo is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	raw, err := readUploadPayload(file, maxCaptureUploadSize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	asset, err := media.NormalizeImage(raw, fileHeader.Header.Get("Content-Type"))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid image: %v", err), http.StatusBadRequest)
		return
	}

	capturedAt := parseCapturedAt(r.FormValue("captured_at"))
	captureID := uuid.New().String()
	privateKey, err := s.Media.StorePrivateCapture(r.Context(), user.ID, captureID, capturedAt, asset)
	if err != nil {
		http.Error(w, "failed to store capture", http.StatusBadGateway)
		return
	}

	capture := &models.Capture{
		ID:                captureID,
		UserID:            user.ID,
		ClientLocalID:     r.FormValue("client_local_id"),
		OriginalFileName:  fileHeader.Filename,
		ContentType:       asset.ContentType,
		SizeBytes:         int64(len(asset.Bytes)),
		Width:             asset.Width,
		Height:            asset.Height,
		CapturedAt:        capturedAt,
		UploadedAt:        time.Now().UTC(),
		Latitude:          parseOptionalFloat(r.FormValue("latitude")),
		Longitude:         parseOptionalFloat(r.FormValue("longitude")),
		AccuracyMeters:    parseOptionalFloat(r.FormValue("accuracy_meters")),
		Status:            "private",
		PrivateStorageKey: privateKey,
	}

	if err := s.DB.CreateCapture(capture); err != nil {
		_ = s.Media.DeletePrivate(r.Context(), privateKey)
		http.Error(w, "failed to store capture metadata", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"capture": capture,
	})
}

func (s *Server) handlePublishCapture(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	captureID := chi.URLParam(r, "captureID")

	if s.Media == nil || !s.Media.Enabled() {
		http.Error(w, "capture storage is not configured", http.StatusServiceUnavailable)
		return
	}

	capture, err := s.DB.GetCaptureForUser(captureID, user.ID)
	if err != nil {
		http.Error(w, "capture not found", http.StatusNotFound)
		return
	}

	if capture.Status == "published" && capture.PublicStorageKey != "" {
		capture.PublicURL = s.Media.PublicURL(capture.PublicStorageKey)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      true,
			"capture": capture,
		})
		return
	}

	publicKey, publicURL, err := s.Media.PublishCapture(r.Context(), capture)
	if err != nil {
		http.Error(w, "failed to publish capture", http.StatusBadGateway)
		return
	}

	if err := s.DB.PublishCapture(capture.ID, user.ID, publicKey); err != nil {
		_ = s.Media.DeletePublic(r.Context(), publicKey)
		http.Error(w, "failed to update capture state", http.StatusInternalServerError)
		return
	}

	updated, err := s.DB.GetCaptureForUser(capture.ID, user.ID)
	if err != nil {
		http.Error(w, "failed to reload capture", http.StatusInternalServerError)
		return
	}
	updated.PublicURL = publicURL

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"capture": updated,
	})
}

func (s *Server) handlePreviewCapture(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	captureID := chi.URLParam(r, "captureID")

	if s.Media == nil || !s.Media.Enabled() {
		http.Error(w, "capture storage is not configured", http.StatusServiceUnavailable)
		return
	}

	capture, err := s.DB.GetCaptureForUser(captureID, user.ID)
	if err != nil {
		http.Error(w, "capture not found", http.StatusNotFound)
		return
	}

	content, contentType, err := s.Media.ReadPrivateCapture(r.Context(), capture.PrivateStorageKey)
	if err != nil {
		http.Error(w, "failed to load private capture", http.StatusBadGateway)
		return
	}
	if contentType == "" {
		contentType = capture.ContentType
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	fileName := capture.OriginalFileName
	if fileName == "" {
		fileName = path.Base(capture.PrivateStorageKey)
	}

	w.Header().Set("Cache-Control", "private, no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", fileName))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) handleUnpublishCapture(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	captureID := chi.URLParam(r, "captureID")

	if s.Media == nil || !s.Media.Enabled() {
		http.Error(w, "capture storage is not configured", http.StatusServiceUnavailable)
		return
	}

	capture, err := s.DB.GetCaptureForUser(captureID, user.ID)
	if err != nil {
		http.Error(w, "capture not found", http.StatusNotFound)
		return
	}

	if capture.PublicStorageKey != "" {
		if err := s.Media.DeletePublic(r.Context(), capture.PublicStorageKey); err != nil {
			http.Error(w, "failed to remove public capture", http.StatusBadGateway)
			return
		}
	}

	if err := s.DB.UnpublishCapture(capture.ID, user.ID); err != nil {
		http.Error(w, "failed to update capture state", http.StatusInternalServerError)
		return
	}

	updated, err := s.DB.GetCaptureForUser(capture.ID, user.ID)
	if err != nil {
		http.Error(w, "failed to reload capture", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"capture": updated,
	})
}

func (s *Server) handleDeleteCapture(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	captureID := chi.URLParam(r, "captureID")

	if s.Media == nil || !s.Media.Enabled() {
		http.Error(w, "capture storage is not configured", http.StatusServiceUnavailable)
		return
	}

	capture, err := s.DB.GetCaptureForUser(captureID, user.ID)
	if err != nil {
		http.Error(w, "capture not found", http.StatusNotFound)
		return
	}

	if capture.PublicStorageKey != "" {
		if err := s.Media.DeletePublic(r.Context(), capture.PublicStorageKey); err != nil {
			http.Error(w, "failed to delete public capture", http.StatusBadGateway)
			return
		}
	}
	if err := s.Media.DeletePrivate(r.Context(), capture.PrivateStorageKey); err != nil {
		http.Error(w, "failed to delete private capture", http.StatusBadGateway)
		return
	}
	if err := s.DB.DeleteCapture(capture.ID, user.ID); err != nil {
		http.Error(w, "failed to delete capture metadata", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true,
	})
}

func attachPublicURLs(captures []*models.Capture, storage *media.BunnyStorage) {
	if storage == nil || !storage.Enabled() {
		return
	}

	for _, capture := range captures {
		if capture.PublicStorageKey != "" {
			capture.PublicURL = storage.PublicURL(capture.PublicStorageKey)
		}
	}
}

func parseCapturedAt(value string) time.Time {
	if value == "" {
		return time.Now().UTC()
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Now().UTC()
	}
	return parsed.UTC()
}

func parseOptionalFloat(value string) *float64 {
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func parseCaptureListFilters(r *http.Request) db.CaptureListFilters {
	query := r.URL.Query()

	filters := db.CaptureListFilters{
		Page:     parsePositiveInt(query.Get("page"), 1),
		PageSize: parsePositiveInt(query.Get("page_size"), 12),
	}

	switch query.Get("status") {
	case "private", "published":
		filters.Status = query.Get("status")
	}

	if capturedOn := query.Get("captured_on"); capturedOn != "" {
		if day, err := time.Parse("2006-01-02", capturedOn); err == nil {
			from := day.UTC()
			to := from.Add(24 * time.Hour)
			filters.CapturedFrom = &from
			filters.CapturedTo = &to
			return filters
		}
	}

	if from := parseOptionalDateTime(query.Get("date_from")); from != nil {
		filters.CapturedFrom = from
	}
	if to := parseOptionalDateTime(query.Get("date_to")); to != nil {
		filters.CapturedTo = to
	}

	return filters
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func parseOptionalDateTime(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		value := parsed.UTC()
		return &value
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		value := parsed.UTC()
		return &value
	}
	return nil
}

func readUploadPayload(file multipart.File, maxSize int64) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(payload)) > maxSize {
		return nil, fmt.Errorf("photo is too large")
	}
	return payload, nil
}
