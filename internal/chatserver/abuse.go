package chatserver

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/houbamzdar/bff/internal/chatdb"
)

type windowLimit struct {
	Max    int
	Window time.Duration
}

type ratePolicy struct {
	Name       string
	UserLimits []windowLimit
	IPLimits   []windowLimit
	Escalate   bool
}

type slidingWindowLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newSlidingWindowLimiter() *slidingWindowLimiter {
	return &slidingWindowLimiter{
		hits: make(map[string][]time.Time),
	}
}

func (l *slidingWindowLimiter) allow(key string, limits []windowLimit, now time.Time) (bool, time.Duration) {
	if len(limits) == 0 {
		return true, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	maxWindow := limits[0].Window
	for _, limit := range limits[1:] {
		if limit.Window > maxWindow {
			maxWindow = limit.Window
		}
	}

	cutoff := now.Add(-maxWindow)
	entries := l.hits[key]
	pruned := entries[:0]
	for _, entry := range entries {
		if !entry.Before(cutoff) {
			pruned = append(pruned, entry)
		}
	}
	entries = pruned

	allowed := true
	retryAfter := time.Duration(0)
	for _, limit := range limits {
		if limit.Max <= 0 || limit.Window <= 0 {
			continue
		}
		windowCutoff := now.Add(-limit.Window)
		firstIndex := len(entries)
		for index, entry := range entries {
			if !entry.Before(windowCutoff) {
				firstIndex = index
				break
			}
		}
		count := len(entries) - firstIndex
		if count >= limit.Max {
			allowed = false
			candidate := limit.Window - now.Sub(entries[firstIndex])
			if candidate < time.Second {
				candidate = time.Second
			}
			if candidate > retryAfter {
				retryAfter = candidate
			}
		}
	}

	if allowed {
		entries = append(entries, now)
		l.hits[key] = entries
	} else if len(entries) == 0 {
		delete(l.hits, key)
	} else {
		l.hits[key] = entries
	}

	return allowed, retryAfter
}

func globalRatePolicy() ratePolicy {
	return ratePolicy{
		Name: "global_authenticated_chat_request",
		UserLimits: []windowLimit{
			{Max: 120, Window: time.Minute},
		},
		IPLimits: []windowLimit{
			{Max: 240, Window: time.Minute},
		},
	}
}

func ratePolicyForRequest(r *http.Request) (ratePolicy, bool) {
	path := strings.TrimSpace(r.URL.Path)
	switch {
	case r.Method == http.MethodGet && path == "/api/chat/me":
		return ratePolicy{
			Name: "get_chat_me",
			UserLimits: []windowLimit{
				{Max: 20, Window: time.Minute},
			},
			IPLimits: []windowLimit{
				{Max: 60, Window: time.Minute},
			},
		}, true
	case r.Method == http.MethodGet && path == "/api/chat/users/search":
		return ratePolicy{
			Name: "search_chat_users",
			UserLimits: []windowLimit{
				{Max: 10, Window: time.Minute},
			},
			IPLimits: []windowLimit{
				{Max: 30, Window: time.Minute},
			},
		}, true
	case r.Method == http.MethodGet && path == "/api/chat/rooms/public":
		return ratePolicy{
			Name: "list_public_rooms",
			UserLimits: []windowLimit{
				{Max: 20, Window: time.Minute},
			},
			IPLimits: []windowLimit{
				{Max: 60, Window: time.Minute},
			},
		}, true
	case r.Method == http.MethodPost && path == "/api/chat/rooms/public":
		return ratePolicy{
			Name: "create_public_room",
			UserLimits: []windowLimit{
				{Max: 2, Window: 24 * time.Hour},
			},
			IPLimits: []windowLimit{
				{Max: 10, Window: 24 * time.Hour},
			},
		}, true
	case r.Method == http.MethodGet && path == "/api/chat/rooms/direct":
		return ratePolicy{
			Name: "list_direct_rooms",
			UserLimits: []windowLimit{
				{Max: 20, Window: time.Minute},
			},
			IPLimits: []windowLimit{
				{Max: 60, Window: time.Minute},
			},
		}, true
	case r.Method == http.MethodPost && path == "/api/chat/rooms/direct":
		return ratePolicy{
			Name: "create_direct_room",
			UserLimits: []windowLimit{
				{Max: 10, Window: time.Minute},
				{Max: 3, Window: 10 * time.Second},
			},
			IPLimits: []windowLimit{
				{Max: 30, Window: time.Minute},
			},
		}, true
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/chat/rooms/direct/"):
		return ratePolicy{
			Name: "delete_direct_room",
			UserLimits: []windowLimit{
				{Max: 10, Window: time.Minute},
			},
			IPLimits: []windowLimit{
				{Max: 30, Window: time.Minute},
			},
		}, true
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/chat/rooms/") && strings.HasSuffix(path, "/messages"):
		return ratePolicy{
			Name: "list_room_messages",
			UserLimits: []windowLimit{
				{Max: 40, Window: time.Minute},
			},
			IPLimits: []windowLimit{
				{Max: 120, Window: time.Minute},
			},
		}, true
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/chat/rooms/") && strings.HasSuffix(path, "/read"):
		return ratePolicy{
			Name: "mark_room_read",
			UserLimits: []windowLimit{
				{Max: 60, Window: time.Minute},
			},
			IPLimits: []windowLimit{
				{Max: 180, Window: time.Minute},
			},
		}, true
	default:
		return ratePolicy{}, false
	}
}

func sendMessagePolicy(room *chatdb.Room) ratePolicy {
	if room != nil && room.Kind == "public" {
		return ratePolicy{
			Name: "send_public_message",
			UserLimits: []windowLimit{
				{Max: 6, Window: time.Minute},
				{Max: 2, Window: 10 * time.Second},
			},
			IPLimits: []windowLimit{
				{Max: 18, Window: time.Minute},
				{Max: 6, Window: 10 * time.Second},
			},
			Escalate: true,
		}
	}
	return ratePolicy{
		Name: "send_direct_message",
		UserLimits: []windowLimit{
			{Max: 20, Window: time.Minute},
			{Max: 5, Window: 10 * time.Second},
		},
		IPLimits: []windowLimit{
			{Max: 60, Window: time.Minute},
			{Max: 15, Window: 10 * time.Second},
		},
		Escalate: true,
	}
}

func deleteMessagePolicy(room *chatdb.Room) ratePolicy {
	name := "delete_direct_message"
	if room != nil && room.Kind == "public" {
		name = "delete_public_message"
	}
	return ratePolicy{
		Name: name,
		UserLimits: []windowLimit{
			{Max: 10, Window: time.Minute},
		},
		IPLimits: []windowLimit{
			{Max: 30, Window: time.Minute},
		},
	}
}

func penaltyDurationForEscalatingAbuse(totalRecentEvents int) time.Duration {
	switch {
	case totalRecentEvents >= 8:
		return 24 * time.Hour
	case totalRecentEvents >= 5:
		return time.Hour
	case totalRecentEvents >= 3:
		return 15 * time.Minute
	default:
		return 0
	}
}

func requestClientIP(r *http.Request) string {
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		value := strings.TrimSpace(r.Header.Get(header))
		if value == "" {
			continue
		}
		if header == "X-Forwarded-For" {
			parts := strings.Split(value, ",")
			if len(parts) > 0 {
				value = strings.TrimSpace(parts[0])
			}
		}
		if value != "" {
			return value
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authUser := currentAuthUser(r)
		if authUser == nil || authUser.User == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if s.rejectByRatePolicy(w, r, authUser, globalRatePolicy()) {
			return
		}
		if policy, ok := ratePolicyForRequest(r); ok && s.rejectByRatePolicy(w, r, authUser, policy) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) rejectByRatePolicy(w http.ResponseWriter, r *http.Request, authUser *AuthUser, policy ratePolicy) bool {
	if s == nil || authUser == nil || authUser.User == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return true
	}

	now := time.Now().UTC()
	userID := authUser.User.ID
	ip := requestClientIP(r)
	userAllowed, userRetryAfter := s.allowRateKey(fmt.Sprintf("user:%d:%s", userID, policy.Name), policy.UserLimits, now)
	ipAllowed, ipRetryAfter := s.allowRateKey(fmt.Sprintf("ip:%s:%s", ip, policy.Name), policy.IPLimits, now)
	if userAllowed && ipAllowed {
		return false
	}

	retryAfter := userRetryAfter
	if ipRetryAfter > retryAfter {
		retryAfter = ipRetryAfter
	}
	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Round(time.Second).Seconds())))
	}

	if policy.Escalate {
		mutedUntil, err := s.recordEscalatingAbuse(userID, ip, policy.Name, now)
		if err != nil {
			http.Error(w, "failed to enforce chat rate limit", http.StatusInternalServerError)
			return true
		}
		if !mutedUntil.IsZero() {
			http.Error(w, fmt.Sprintf("chat is temporarily disabled for your account until %s", mutedUntil.Format(time.RFC3339)), http.StatusForbidden)
			return true
		}
	}

	http.Error(w, "too many chat requests", http.StatusTooManyRequests)
	return true
}

func (s *Server) allowRateKey(key string, limits []windowLimit, now time.Time) (bool, time.Duration) {
	if s == nil || s.Limiter == nil {
		return true, 0
	}
	return s.Limiter.allow(key, limits, now)
}

func (s *Server) recordEscalatingAbuse(userID int64, ip string, action string, now time.Time) (time.Time, error) {
	if err := s.DB.RecordAbuseEvent(userID, ip, action, true, now); err != nil {
		return time.Time{}, err
	}
	totalRecent, err := s.DB.CountRecentAbuseEvents(userID, now.Add(-10*time.Minute), true)
	if err != nil {
		return time.Time{}, err
	}

	duration := penaltyDurationForEscalatingAbuse(totalRecent)
	if duration <= 0 {
		return time.Time{}, nil
	}

	mutedUntil := now.Add(duration)
	if err := s.DB.UpsertUserRestriction(userID, mutedUntil, fmt.Sprintf("automatic chat abuse protection triggered by %s", action), now); err != nil {
		return time.Time{}, err
	}
	return mutedUntil, nil
}
