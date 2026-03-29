package server

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/houbamzdar/bff/internal/models"
)

func buildAssistantAdminOverviewPayload(overview *models.AssistantAdminOverview) map[string]interface{} {
	if overview == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"total_threads":      overview.TotalThreads,
		"open_threads":       overview.OpenThreads,
		"unique_clients":     overview.UniqueClients,
		"total_messages":     overview.TotalMessages,
		"user_messages":      overview.UserMessages,
		"assistant_messages": overview.AssistantPosts,
		"feedback_up":        overview.FeedbackUp,
		"feedback_down":      overview.FeedbackDown,
		"threads_today":      overview.ThreadsToday,
		"messages_today":     overview.MessagesToday,
		"last_message_at":    formatOptionalRFC3339(overview.LastMessageAt),
	}
}

func buildAssistantFrequentQuestionPayload(item *models.AssistantFrequentQuestion) map[string]interface{} {
	if item == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"normalized_question": item.NormalizedQuestion,
		"question":            item.Question,
		"count":               item.Count,
		"last_asked_at":       formatOptionalRFC3339(item.LastAskedAt),
	}
}

func buildAssistantThreadSummaryPayload(item *models.AssistantThreadSummary) map[string]interface{} {
	if item == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"id":                     item.ID,
		"client_id":              item.ClientID,
		"page_context":           item.PageContext,
		"locale":                 item.Locale,
		"status":                 item.Status,
		"created_at":             formatOptionalRFC3339(item.CreatedAt),
		"updated_at":             formatOptionalRFC3339(item.UpdatedAt),
		"last_message_at":        formatOptionalRFC3339(item.LastMessageAt),
		"total_messages":         item.TotalMessages,
		"user_messages":          item.UserMessages,
		"assistant_messages":     item.AssistantMessages,
		"last_user_message":      item.LastUserMessage,
		"last_assistant_message": item.LastAssistantMessage,
		"last_feedback_vote":     item.LastFeedbackVote,
	}
}

func buildAssistantThreadMessagePayload(item *models.AssistantThreadMessage) map[string]interface{} {
	if item == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"id":            item.ID,
		"thread_id":     item.ThreadID,
		"role":          item.Role,
		"content":       item.Content,
		"model":         item.Model,
		"response_id":   item.ResponseID,
		"created_at":    formatOptionalRFC3339(item.CreatedAt),
		"feedback_vote": item.FeedbackVote,
	}
}

func (s *Server) ensureAssistantAdminConfigured(w http.ResponseWriter) bool {
	if s.AIAdmin != nil {
		return true
	}
	http.Error(w, "assistant admin database is not configured", http.StatusServiceUnavailable)
	return false
}

func (s *Server) handleGetAssistantAdminOverview(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanAdmin(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !s.ensureAssistantAdminConfigured(w) {
		return
	}

	overview, err := s.AIAdmin.GetOverview()
	if err != nil {
		http.Error(w, "failed to load assistant overview", http.StatusInternalServerError)
		return
	}
	questions, err := s.AIAdmin.ListFrequentQuestions(10)
	if err != nil {
		http.Error(w, "failed to load assistant frequent questions", http.StatusInternalServerError)
		return
	}

	questionsPayload := make([]map[string]interface{}, 0, len(questions))
	for _, item := range questions {
		questionsPayload = append(questionsPayload, buildAssistantFrequentQuestionPayload(item))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":                 true,
		"overview":           buildAssistantAdminOverviewPayload(overview),
		"frequent_questions": questionsPayload,
	})
}

func (s *Server) handleListAssistantAdminThreads(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanAdmin(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !s.ensureAssistantAdminConfigured(w) {
		return
	}

	limit, offset := parseLimitOffset(r, 12)
	items, err := s.AIAdmin.ListThreads(limit, offset)
	if err != nil {
		http.Error(w, "failed to list assistant threads", http.StatusInternalServerError)
		return
	}
	total, err := s.AIAdmin.CountThreads()
	if err != nil {
		http.Error(w, "failed to count assistant threads", http.StatusInternalServerError)
		return
	}

	payload := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		payload = append(payload, buildAssistantThreadSummaryPayload(item))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"threads":  payload,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(items) < total,
	})
}

func (s *Server) handleGetAssistantAdminThread(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value("user").(*models.User)
	if !userCanAdmin(actor) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !s.ensureAssistantAdminConfigured(w) {
		return
	}

	threadID := chi.URLParam(r, "threadID")
	detail, err := s.AIAdmin.GetThread(threadID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "assistant thread not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to load assistant thread", http.StatusInternalServerError)
		return
	}

	messagesPayload := make([]map[string]interface{}, 0, len(detail.Messages))
	for _, item := range detail.Messages {
		messagesPayload = append(messagesPayload, buildAssistantThreadMessagePayload(item))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"thread":   buildAssistantThreadSummaryPayload(detail.Thread),
		"messages": messagesPayload,
	})
}
