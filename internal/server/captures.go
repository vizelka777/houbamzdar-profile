package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/enrichment"
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

func (s *Server) handleListPublicCaptures(w http.ResponseWriter, r *http.Request) {
	currentUserID := s.currentUserIDFromOptionalSession(r)
	filters := parsePublicCaptureFilters(r)
	captures, err := s.DB.ListPublicCapturesWithFilters(filters, currentUserID)
	if err != nil {
		http.Error(w, "failed to list public captures", http.StatusInternalServerError)
		return
	}

	if captures == nil {
		captures = []*models.Capture{}
	} else {
		attachPublicURLs(captures, s.Media)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"captures": captures,
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

	if s.Media == nil || !s.Media.Enabled() || s.Config.CaptureAIValidatorURL == "" || s.Config.NominatimBaseURL == "" {
		http.Error(w, "publication validation is not configured", http.StatusServiceUnavailable)
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

	if capture.PublicationReviewStatus == db.CapturePublicationReviewApproved {
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
		return
	}

	if capture.Latitude == nil || capture.Longitude == nil {
		if err := s.DB.RejectCapturePublication(capture.ID, db.CapturePublicationRejectMissingCoordinates); err != nil {
			http.Error(w, "failed to mark capture as rejected", http.StatusInternalServerError)
			return
		}
		updated, err := s.DB.GetCaptureForUser(capture.ID, user.ID)
		if err != nil {
			http.Error(w, "failed to reload capture", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      false,
			"capture": updated,
			"error":   "capture is missing coordinates",
		})
		return
	}

	if capture.PublicationReviewStatus != db.CapturePublicationReviewPendingValidation {
		if err := s.DB.RequestCapturePublicationValidation(capture.ID, user.ID); err != nil {
			http.Error(w, "failed to queue publication validation", http.StatusInternalServerError)
			return
		}
	}

	updated, err := s.DB.GetCaptureForUser(capture.ID, user.ID)
	if err != nil {
		http.Error(w, "failed to reload capture", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"capture": updated,
	})
}

func (s *Server) handleModeratorRecheckCapture(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	if user == nil || !user.IsModerator {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if s.Config.CaptureAIValidatorURL == "" {
		http.Error(w, "publication validation is not configured", http.StatusServiceUnavailable)
		return
	}

	captureID := chi.URLParam(r, "captureID")
	capture, err := s.DB.GetCaptureByID(captureID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "capture not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load capture", http.StatusInternalServerError)
		return
	}

	if capture.Status != "published" || strings.TrimSpace(capture.PublicStorageKey) == "" {
		http.Error(w, "capture is not publicly shared", http.StatusUnprocessableEntity)
		return
	}

	validator := enrichment.NewAIValidatorClient(s.Config)
	analysis, species, err := validator.AnalyzeCaptureWithMode(r.Context(), capture, enrichment.AIReviewModeModeratorRecheck)
	if err != nil {
		http.Error(w, "failed to recheck capture: "+err.Error(), http.StatusBadGateway)
		return
	}
	if analysis == nil || !analysis.HasMushrooms || len(species) == 0 {
		http.Error(w, "advanced recheck did not find identifiable mushrooms; no changes were applied", http.StatusUnprocessableEntity)
		return
	}

	if err := s.DB.ApplyCaptureModeratorReview(capture.ID, user.ID, analysis, species); err != nil {
		http.Error(w, "failed to save moderator review", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"capture_id": capture.ID,
		"model_code": analysis.ModelCode,
		"species":    species,
	})
}

func (s *Server) handleSetCaptureCoordinatesFree(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	captureID := chi.URLParam(r, "captureID")

	capture, err := s.DB.GetCaptureForUser(captureID, user.ID)
	if err != nil {
		http.Error(w, "capture not found", http.StatusNotFound)
		return
	}
	if capture.Latitude == nil || capture.Longitude == nil {
		http.Error(w, "capture has no coordinates", http.StatusBadRequest)
		return
	}

	var req struct {
		CoordinatesFree bool `json:"coordinates_free"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.DB.SetCaptureCoordinatesFree(captureID, user.ID, req.CoordinatesFree); err != nil {
		http.Error(w, "failed to update capture visibility", http.StatusInternalServerError)
		return
	}

	updated, err := s.DB.GetCaptureForUser(captureID, user.ID)
	if err != nil {
		http.Error(w, "failed to reload capture", http.StatusInternalServerError)
		return
	}
	attachPublicURLs([]*models.Capture{updated}, s.Media)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"capture": updated,
	})
}

func (s *Server) handleUnlockCaptureCoordinates(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	captureID := chi.URLParam(r, "captureID")

	balance, alreadyUnlocked, err := s.DB.UnlockCaptureCoordinates(user.ID, captureID)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			http.Error(w, "capture not found", http.StatusNotFound)
		case errors.Is(err, db.ErrCaptureHasNoCoordinates):
			http.Error(w, "capture has no coordinates", http.StatusBadRequest)
		case errors.Is(err, db.ErrInsufficientHoubickaBalance):
			http.Error(w, "insufficient houbicka balance", http.StatusPaymentRequired)
		default:
			http.Error(w, "failed to unlock coordinates", http.StatusInternalServerError)
		}
		return
	}

	capture, err := s.DB.GetPublicCaptureByID(captureID)
	if err != nil {
		http.Error(w, "failed to reload capture", http.StatusInternalServerError)
		return
	}
	attachPublicURLs([]*models.Capture{capture}, s.Media)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":               true,
		"capture":          capture,
		"balance":          balance,
		"already_unlocked": alreadyUnlocked,
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

func (s *Server) handleListViewedCaptures(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value("user").(*models.User)
	limit, offset := parseLimitOffset(r, 24)

	captures, err := s.DB.ListViewedCapturesByUser(user.ID, limit, offset)
	if err != nil {
		http.Error(w, "failed to list viewed captures", http.StatusInternalServerError)
		return
	}

	if captures == nil {
		captures = []*models.Capture{}
	} else {
		attachPublicURLs(captures, s.Media)
	}

	total, err := s.DB.CountViewedCapturesByUser(user.ID)
	if err != nil {
		http.Error(w, "failed to count viewed captures", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"captures": captures,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(captures) < total,
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

func parsePublicCaptureFilters(r *http.Request) db.PublicCaptureFilters {
	query := r.URL.Query()

	filters := db.PublicCaptureFilters{
		Limit:        parsePositiveInt(query.Get("limit"), 24),
		Offset:       parseNonNegativeInt(query.Get("offset"), 0),
		SpeciesQuery: query.Get("species"),
		KrajQuery:    query.Get("kraj"),
		OkresQuery:   query.Get("okres"),
		ObecQuery:    query.Get("obec"),
		Sort:         query.Get("sort"),
	}

	if raw := query.Get("has_mushrooms"); raw != "" {
		value := raw == "1" || strings.EqualFold(raw, "true")
		filters.HasMushrooms = &value
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

func parseNonNegativeInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
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
