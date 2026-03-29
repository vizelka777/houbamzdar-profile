package chatserver

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/houbamzdar/bff/internal/chatdb"
	"github.com/houbamzdar/bff/internal/chatidentity"
)

func TestRoomIDParamUnescapesPathValue(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/chat/rooms/room%3Ageneral/messages", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("roomID", "room%3Ageneral")
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))

	roomID, err := roomIDParam(request)
	if err != nil {
		t.Fatalf("roomIDParam returned error: %v", err)
	}
	if roomID != "room:general" {
		t.Fatalf("roomIDParam = %q, want %q", roomID, "room:general")
	}
}

func TestCanDeleteMessageModerationOnlyForPublicRooms(t *testing.T) {
	moderator := &chatidentity.User{
		ID:          99,
		IsModerator: true,
	}
	message := &chatdb.Message{
		ID:           "message-1",
		AuthorUserID: 7,
	}
	publicRoom := &chatdb.Room{
		ID:   "room:general",
		Kind: "public",
	}
	directRoom := &chatdb.Room{
		ID:   "room:dm",
		Kind: "dm",
	}

	if !canDeleteMessage(moderator, publicRoom, message) {
		t.Fatal("expected moderator to delete message in public room")
	}
	if canDeleteMessage(moderator, directRoom, message) {
		t.Fatal("did not expect moderator to delete another user's message in direct room")
	}
}
