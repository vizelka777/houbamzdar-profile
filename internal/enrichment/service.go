package enrichment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/houbamzdar/bff/internal/config"
	"github.com/houbamzdar/bff/internal/db"
	"github.com/houbamzdar/bff/internal/media"
	"github.com/houbamzdar/bff/internal/models"
)

type Service struct {
	db          *db.DB
	media       *media.BunnyStorage
	aiClient    *AIValidatorClient
	geoResolver *NominatimResolver
	logger      *log.Logger
}

func New(cfg *config.Config, database *db.DB, mediaStorage *media.BunnyStorage) *Service {
	return &Service{
		db:          database,
		media:       mediaStorage,
		aiClient:    NewAIValidatorClient(cfg),
		geoResolver: NewNominatimResolver(cfg),
		logger:      log.Default(),
	}
}

func (s *Service) Enabled() bool {
	return s != nil && s.db != nil && s.media != nil && s.media.Enabled() && s.aiClient.Enabled() && s.geoResolver.Enabled()
}

func (s *Service) Run(ctx context.Context) {
	if !s.Enabled() {
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		if err := s.ProcessPendingOnce(ctx, 8); err != nil {
			s.logger.Printf("capture enrichment worker error: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) ProcessPendingOnce(ctx context.Context, limit int) error {
	if !s.Enabled() {
		return nil
	}

	captures, err := s.db.ListCapturesPendingPublicationValidation(limit)
	if err != nil {
		return err
	}

	for _, capture := range captures {
		if err := s.processCapture(ctx, capture); err != nil {
			s.logger.Printf("capture enrichment failed for %s: %v", capture.ID, err)
		}
	}

	return nil
}

func (s *Service) processCapture(ctx context.Context, capture *models.Capture) error {
	if capture == nil {
		return nil
	}
	if capture.Status == "published" {
		return nil
	}
	if capture.Latitude == nil || capture.Longitude == nil {
		return s.db.RejectCapturePublication(capture.ID, db.CapturePublicationRejectMissingCoordinates)
	}

	geoResult, insideCzechia, geoErr := s.geoResolver.Resolve(ctx, capture)
	if geoErr != nil {
		return s.handleRetry(capture, fmt.Errorf("resolve coordinates: %w", geoErr))
	}
	if !insideCzechia {
		return s.db.RejectCapturePublication(capture.ID, db.CapturePublicationRejectOutsideCzechia)
	}

	inlineImage, inlineErr := PrepareValidatorInlineImage(ctx, s.media, capture, AIReviewModePublishValidation)
	if inlineErr != nil && s.logger != nil {
		s.logger.Printf("capture enrichment inline validator image fallback for %s: %v", capture.ID, inlineErr)
	}

	analysis, species, aiErr := s.aiClient.AnalyzeCaptureWithModeAndImage(ctx, capture, AIReviewModePublishValidation, "", inlineImage)
	if aiErr != nil {
		return s.handleRetry(capture, fmt.Errorf("analyze mushrooms: %w", aiErr))
	}
	if analysis == nil || !analysis.HasMushrooms {
		return s.db.RejectCapturePublication(capture.ID, db.CapturePublicationRejectNoMushrooms)
	}

	publicKey, _, publishErr := s.media.PublishCapture(ctx, capture)
	if publishErr != nil {
		return s.handleRetry(capture, fmt.Errorf("publish capture: %w", publishErr))
	}

	if err := s.db.FinalizeCapturePublicationApproved(capture.ID, capture.UserID, publicKey, analysis, species, geoResult); err != nil {
		_ = s.media.DeletePublic(ctx, publicKey)
		return s.handleRetry(capture, fmt.Errorf("finalize publication: %w", err))
	}

	return nil
}

func (s *Service) handleRetry(capture *models.Capture, err error) error {
	attempts := capture.PublicationReviewAttempts + 1
	if attempts >= 5 {
		if markErr := s.db.MarkCapturePublicationValidationError(capture.ID, err.Error(), attempts); markErr != nil {
			return markErr
		}
		return err
	}

	backoff := time.Duration(attempts*attempts) * 15 * time.Second
	nextAttemptAt := time.Now().UTC().Add(backoff)
	if markErr := s.db.MarkCapturePublicationValidationRetry(capture.ID, err.Error(), nextAttemptAt, attempts); markErr != nil {
		return markErr
	}
	return err
}

type AIValidatorClient struct {
	baseURL    string
	authToken  string
	httpClient *http.Client
}

type AIReviewMode string

const (
	AIReviewModePublishValidation AIReviewMode = "publish_validation"
	AIReviewModeModeratorRecheck  AIReviewMode = "moderator_recheck"
)

func NewAIValidatorClient(cfg *config.Config) *AIValidatorClient {
	return &AIValidatorClient{
		baseURL:   strings.TrimSpace(cfg.CaptureAIValidatorURL),
		authToken: strings.TrimSpace(cfg.CaptureAIValidatorToken),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *AIValidatorClient) Enabled() bool {
	return c != nil && c.baseURL != ""
}

type aiValidatorRequest struct {
	CaptureID         string `json:"capture_id"`
	PrivateStorageKey string `json:"private_storage_key"`
	InlineImageData   string `json:"inline_image_data,omitempty"`
	InlineImageMime   string `json:"inline_image_mime_type,omitempty"`
	ReviewMode        string `json:"review_mode,omitempty"`
	ModelCode         string `json:"model_code,omitempty"`
}

type aiValidatorResponse struct {
	OK           bool                             `json:"ok"`
	HasMushrooms bool                             `json:"has_mushrooms"`
	ModelCode    string                           `json:"model_code"`
	Species      []*models.CaptureMushroomSpecies `json:"species"`
}

type ModeratorModelOption struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type moderatorModelsResponse struct {
	OK           bool                    `json:"ok"`
	DefaultModel string                  `json:"default_model"`
	Models       []*ModeratorModelOption `json:"models"`
}

func (c *AIValidatorClient) AnalyzeCapture(ctx context.Context, capture *models.Capture) (*models.CaptureMushroomAnalysis, []*models.CaptureMushroomSpecies, error) {
	return c.AnalyzeCaptureWithModeAndImage(ctx, capture, AIReviewModePublishValidation, "", nil)
}

func (c *AIValidatorClient) AnalyzeCaptureWithMode(ctx context.Context, capture *models.Capture, reviewMode AIReviewMode, modelCode string) (*models.CaptureMushroomAnalysis, []*models.CaptureMushroomSpecies, error) {
	return c.AnalyzeCaptureWithModeAndImage(ctx, capture, reviewMode, modelCode, nil)
}

type AIValidatorInlineImage struct {
	Data     string
	MimeType string
}

func (c *AIValidatorClient) AnalyzeCaptureWithModeAndImage(ctx context.Context, capture *models.Capture, reviewMode AIReviewMode, modelCode string, inlineImage *AIValidatorInlineImage) (*models.CaptureMushroomAnalysis, []*models.CaptureMushroomSpecies, error) {
	if !c.Enabled() {
		return nil, nil, fmt.Errorf("AI validator is not configured")
	}

	payload := aiValidatorRequest{
		CaptureID:         capture.ID,
		PrivateStorageKey: capture.PrivateStorageKey,
		ReviewMode:        strings.TrimSpace(string(reviewMode)),
		ModelCode:         strings.TrimSpace(modelCode),
	}
	if inlineImage != nil {
		payload.InlineImageData = strings.TrimSpace(inlineImage.Data)
		payload.InlineImageMime = strings.TrimSpace(inlineImage.MimeType)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("AI validator status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded aiValidatorResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, nil, err
	}

	analysis := &models.CaptureMushroomAnalysis{
		CaptureID:    capture.ID,
		HasMushrooms: decoded.HasMushrooms,
		ModelCode:    decoded.ModelCode,
		RawJSON:      string(respBody),
		AnalyzedAt:   time.Now().UTC(),
	}

	species := make([]*models.CaptureMushroomSpecies, 0, len(decoded.Species))
	for _, item := range decoded.Species {
		if item == nil {
			continue
		}
		species = append(species, &models.CaptureMushroomSpecies{
			CaptureID:         capture.ID,
			LatinName:         strings.TrimSpace(item.LatinName),
			CzechOfficialName: strings.TrimSpace(item.CzechOfficialName),
			Probability:       item.Probability,
		})
	}

	if len(species) > 0 {
		analysis.PrimaryLatinName = species[0].LatinName
		analysis.PrimaryCzechOfficialName = species[0].CzechOfficialName
		analysis.PrimaryProbability = species[0].Probability
	}

	return analysis, species, nil
}

func (c *AIValidatorClient) ModelsURL() string {
	baseURL := strings.TrimSpace(c.baseURL)
	if baseURL == "" {
		return ""
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	parsed.Path = "/models"
	parsed.RawQuery = ""
	return parsed.String()
}

func (c *AIValidatorClient) ListModeratorModels(ctx context.Context) ([]*ModeratorModelOption, string, error) {
	modelsURL := c.ModelsURL()
	if modelsURL == "" {
		return nil, "", fmt.Errorf("AI validator models endpoint is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, "", err
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("AI validator models status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded moderatorModelsResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, "", err
	}
	return decoded.Models, strings.TrimSpace(decoded.DefaultModel), nil
}

type NominatimResolver struct {
	baseURL        string
	userAgent      string
	httpClient     *http.Client
	minInterval    time.Duration
	cacheTTL       time.Duration
	cachePrecision int
	mu             sync.Mutex
	lastRequestAt  time.Time
	cacheMu        sync.RWMutex
	cache          map[string]cachedGeoResult
}

func NewNominatimResolver(cfg *config.Config) *NominatimResolver {
	return &NominatimResolver{
		baseURL:   strings.TrimRight(strings.TrimSpace(cfg.NominatimBaseURL), "/"),
		userAgent: strings.TrimSpace(cfg.NominatimUserAgent),
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
		minInterval:    time.Second,
		cacheTTL:       24 * time.Hour,
		cachePrecision: 5,
		cache:          make(map[string]cachedGeoResult),
	}
}

func (r *NominatimResolver) Enabled() bool {
	return r != nil && r.baseURL != ""
}

type nominatimResponse struct {
	Address struct {
		CountryCode  string `json:"country_code"`
		State        string `json:"state"`
		County       string `json:"county"`
		Municipality string `json:"municipality"`
		City         string `json:"city"`
		Town         string `json:"town"`
		Village      string `json:"village"`
		Hamlet       string `json:"hamlet"`
		Suburb       string `json:"suburb"`
		CityDistrict string `json:"city_district"`
	} `json:"address"`
}

type cachedGeoResult struct {
	geo           *models.CaptureGeoIndex
	insideCzechia bool
	expiresAt     time.Time
}

func (r *NominatimResolver) Resolve(ctx context.Context, capture *models.Capture) (*models.CaptureGeoIndex, bool, error) {
	if !r.Enabled() {
		return nil, false, fmt.Errorf("Nominatim is not configured")
	}
	if capture == nil || capture.Latitude == nil || capture.Longitude == nil {
		return nil, false, fmt.Errorf("capture is missing coordinates")
	}
	if cached, insideCzechia, ok := r.lookupCache(capture); ok {
		return cached, insideCzechia, nil
	}
	if err := r.waitForRateLimit(ctx); err != nil {
		return nil, false, err
	}

	u, err := url.Parse(r.baseURL + "/reverse")
	if err != nil {
		return nil, false, err
	}
	query := u.Query()
	query.Set("format", "jsonv2")
	query.Set("addressdetails", "1")
	query.Set("accept-language", "cs")
	query.Set("lat", fmt.Sprintf("%.7f", *capture.Latitude))
	query.Set("lon", fmt.Sprintf("%.7f", *capture.Longitude))
	query.Set("zoom", "18")
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, false, err
	}
	if r.userAgent != "" {
		req.Header.Set("User-Agent", r.userAgent)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("Nominatim status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded nominatimResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, false, err
	}

	countryCode := strings.ToUpper(strings.TrimSpace(decoded.Address.CountryCode))
	insideCzechia := countryCode == "CZ"

	obecName := firstNonEmpty(
		decoded.Address.Municipality,
		decoded.Address.City,
		decoded.Address.Town,
		decoded.Address.Village,
		decoded.Address.Hamlet,
		decoded.Address.Suburb,
		decoded.Address.CityDistrict,
	)

	result := &models.CaptureGeoIndex{
		CaptureID:   capture.ID,
		CountryCode: countryCode,
		KrajName:    strings.TrimSpace(decoded.Address.State),
		OkresName:   strings.TrimSpace(decoded.Address.County),
		ObecName:    strings.TrimSpace(obecName),
		RawJSON:     string(body),
		ResolvedAt:  time.Now().UTC(),
	}
	r.storeCache(capture, result, insideCzechia)

	return result, insideCzechia, nil
}

func (r *NominatimResolver) waitForRateLimit(ctx context.Context) error {
	if r == nil || r.minInterval <= 0 {
		return nil
	}

	for {
		r.mu.Lock()
		now := time.Now().UTC()
		if r.lastRequestAt.IsZero() {
			r.lastRequestAt = now
			r.mu.Unlock()
			return nil
		}

		wait := r.lastRequestAt.Add(r.minInterval).Sub(now)
		if wait <= 0 {
			r.lastRequestAt = now
			r.mu.Unlock()
			return nil
		}
		r.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *NominatimResolver) lookupCache(capture *models.Capture) (*models.CaptureGeoIndex, bool, bool) {
	if r == nil || r.cacheTTL <= 0 || capture == nil || capture.Latitude == nil || capture.Longitude == nil {
		return nil, false, false
	}

	cacheKey := r.cacheKey(*capture.Latitude, *capture.Longitude)
	if cacheKey == "" {
		return nil, false, false
	}

	r.cacheMu.RLock()
	entry, ok := r.cache[cacheKey]
	r.cacheMu.RUnlock()
	if !ok {
		return nil, false, false
	}
	if time.Now().UTC().After(entry.expiresAt) {
		r.cacheMu.Lock()
		delete(r.cache, cacheKey)
		r.cacheMu.Unlock()
		return nil, false, false
	}
	if entry.geo == nil {
		return nil, false, false
	}

	cloned := cloneGeoIndex(entry.geo)
	cloned.CaptureID = capture.ID
	cloned.ResolvedAt = time.Now().UTC()
	return cloned, entry.insideCzechia, true
}

func (r *NominatimResolver) storeCache(capture *models.Capture, geo *models.CaptureGeoIndex, insideCzechia bool) {
	if r == nil || r.cacheTTL <= 0 || capture == nil || capture.Latitude == nil || capture.Longitude == nil || geo == nil {
		return
	}

	cacheKey := r.cacheKey(*capture.Latitude, *capture.Longitude)
	if cacheKey == "" {
		return
	}

	r.cacheMu.Lock()
	r.cache[cacheKey] = cachedGeoResult{
		geo:           cloneGeoIndex(geo),
		insideCzechia: insideCzechia,
		expiresAt:     time.Now().UTC().Add(r.cacheTTL),
	}
	r.cacheMu.Unlock()
}

func (r *NominatimResolver) cacheKey(latitude float64, longitude float64) string {
	if r == nil || r.cachePrecision < 0 {
		return ""
	}
	return fmt.Sprintf("%.*f:%.*f", r.cachePrecision, latitude, r.cachePrecision, longitude)
}

func cloneGeoIndex(source *models.CaptureGeoIndex) *models.CaptureGeoIndex {
	if source == nil {
		return nil
	}
	cloned := *source
	return &cloned
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
