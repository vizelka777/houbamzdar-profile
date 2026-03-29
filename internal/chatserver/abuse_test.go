package chatserver

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestPenaltyDurationForEscalatingAbuse(t *testing.T) {
	cases := []struct {
		name   string
		count  int
		expect time.Duration
	}{
		{name: "below threshold", count: 2, expect: 0},
		{name: "first mute", count: 3, expect: 15 * time.Minute},
		{name: "second mute", count: 5, expect: time.Hour},
		{name: "long mute", count: 8, expect: 24 * time.Hour},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := penaltyDurationForEscalatingAbuse(testCase.count); got != testCase.expect {
				t.Fatalf("penaltyDurationForEscalatingAbuse(%d) = %s, want %s", testCase.count, got, testCase.expect)
			}
		})
	}
}

func TestRequestClientIPPrefersForwardedHeaders(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/chat/rooms/public", nil)
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("X-Forwarded-For", "198.51.100.10, 127.0.0.1")

	if got := requestClientIP(request); got != "198.51.100.10" {
		t.Fatalf("requestClientIP() = %q, want %q", got, "198.51.100.10")
	}
}
