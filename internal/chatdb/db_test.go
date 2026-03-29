package chatdb

import (
	"path/filepath"
	"testing"
	"time"
)

func TestNewSeedsGeneralRoomAndEnsuresDirectRooms(t *testing.T) {
	dbPath := "file:" + filepath.Join(t.TempDir(), "chat.db")
	database, err := New(dbPath, "", "general", "Obecný chat")
	if err != nil {
		t.Fatalf("new chat db: %v", err)
	}
	defer database.Close()

	rooms, err := database.ListPublicRooms(1)
	if err != nil {
		t.Fatalf("list public rooms: %v", err)
	}
	if len(rooms) != 1 {
		t.Fatalf("expected 1 seeded public room, got %d", len(rooms))
	}
	if rooms[0].Slug != "general" {
		t.Fatalf("expected general slug, got %q", rooms[0].Slug)
	}

	firstRoom, err := database.EnsureDirectRoom(2, 5)
	if err != nil {
		t.Fatalf("ensure direct room first call: %v", err)
	}
	secondRoom, err := database.EnsureDirectRoom(5, 2)
	if err != nil {
		t.Fatalf("ensure direct room second call: %v", err)
	}
	if firstRoom.ID != secondRoom.ID {
		t.Fatalf("expected stable direct room id, got %q and %q", firstRoom.ID, secondRoom.ID)
	}
}

func TestSoftDeleteMessageMarksDeletedAtAndClearsContent(t *testing.T) {
	dbPath := "file:" + filepath.Join(t.TempDir(), "chat.db")
	database, err := New(dbPath, "", "general", "Obecný chat")
	if err != nil {
		t.Fatalf("new chat db: %v", err)
	}
	defer database.Close()

	message := &Message{
		RoomID:             "room:general",
		AuthorUserID:       7,
		AuthorNameSnapshot: "tester",
		Content:            "Ahoj",
		CreatedAt:          time.Now().UTC(),
	}
	if err := database.CreateMessage(message); err != nil {
		t.Fatalf("create message: %v", err)
	}
	deletedAt := time.Now().UTC().Add(2 * time.Minute)
	if err := database.SoftDeleteMessage(message.ID, deletedAt); err != nil {
		t.Fatalf("soft delete message: %v", err)
	}

	storedMessage, err := database.GetMessage(message.ID)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if storedMessage.Content != "" {
		t.Fatalf("expected deleted message content to be cleared, got %q", storedMessage.Content)
	}
	if storedMessage.DeletedAt.IsZero() {
		t.Fatal("expected deleted_at to be set")
	}
}

func TestTrimRoomMessagesKeepsNewestMessagesOnly(t *testing.T) {
	dbPath := "file:" + filepath.Join(t.TempDir(), "chat.db")
	database, err := New(dbPath, "", "general", "Obecný chat")
	if err != nil {
		t.Fatalf("new chat db: %v", err)
	}
	defer database.Close()

	baseTime := time.Now().UTC().Add(-5 * time.Minute)
	for index := 0; index < 5; index++ {
		message := &Message{
			RoomID:             "room:general",
			AuthorUserID:       int64(index + 1),
			AuthorNameSnapshot: "tester",
			Content:            "message",
			CreatedAt:          baseTime.Add(time.Duration(index) * time.Minute),
		}
		if err := database.CreateMessage(message); err != nil {
			t.Fatalf("create message %d: %v", index, err)
		}
	}

	if err := database.TrimRoomMessages("room:general", 2); err != nil {
		t.Fatalf("trim room messages: %v", err)
	}

	messages, err := database.ListMessages("room:general", 10)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages after trim, got %d", len(messages))
	}
	if !messages[0].CreatedAt.Before(messages[1].CreatedAt) {
		t.Fatal("expected messages to remain in ascending order")
	}
}

func TestRemoveDirectRoomForUserAllowsReopeningConversation(t *testing.T) {
	dbPath := "file:" + filepath.Join(t.TempDir(), "chat.db")
	database, err := New(dbPath, "", "general", "Obecný chat")
	if err != nil {
		t.Fatalf("new chat db: %v", err)
	}
	defer database.Close()

	room, err := database.EnsureDirectRoom(2, 5)
	if err != nil {
		t.Fatalf("ensure direct room: %v", err)
	}
	if err := database.CreateMessage(&Message{
		RoomID:             room.ID,
		AuthorUserID:       5,
		AuthorNameSnapshot: "other-user",
		Content:            "old message",
		CreatedAt:          time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("create direct message before removal: %v", err)
	}
	if err := database.RemoveDirectRoomForUser(room.ID, 2); err != nil {
		t.Fatalf("remove direct room for user: %v", err)
	}

	allowedRoom, allowed, err := database.CanUserAccessRoom(2, room.ID)
	if err != nil {
		t.Fatalf("check room access after removal: %v", err)
	}
	if allowed {
		t.Fatalf("expected user 2 to lose access to %q after removal", allowedRoom.ID)
	}

	reopenedRoom, err := database.EnsureDirectRoom(2, 5)
	if err != nil {
		t.Fatalf("reopen direct room: %v", err)
	}
	if reopenedRoom.ID != room.ID {
		t.Fatalf("expected reopened room to reuse %q, got %q", room.ID, reopenedRoom.ID)
	}

	_, allowed, err = database.CanUserAccessRoom(2, room.ID)
	if err != nil {
		t.Fatalf("check room access after reopen: %v", err)
	}
	if !allowed {
		t.Fatal("expected user 2 to regain access after reopening direct room")
	}

	reopenedRoom.Kind = "dm"
	messages, err := database.ListMessagesForUser(reopenedRoom, 2, 20)
	if err != nil {
		t.Fatalf("list messages after reopen: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected reopened direct room to hide previous history for removed user, got %d messages", len(messages))
	}
}

func TestListDirectRoomsForUserSortsByLastMessageAndPaginates(t *testing.T) {
	dbPath := "file:" + filepath.Join(t.TempDir(), "chat.db")
	database, err := New(dbPath, "", "general", "Obecný chat")
	if err != nil {
		t.Fatalf("new chat db: %v", err)
	}
	defer database.Close()

	baseTime := time.Now().UTC().Add(1 * time.Minute)
	roomA, err := database.EnsureDirectRoom(2, 5)
	if err != nil {
		t.Fatalf("ensure room A: %v", err)
	}
	roomB, err := database.EnsureDirectRoom(2, 6)
	if err != nil {
		t.Fatalf("ensure room B: %v", err)
	}
	roomC, err := database.EnsureDirectRoom(2, 7)
	if err != nil {
		t.Fatalf("ensure room C: %v", err)
	}

	messages := []struct {
		roomID    string
		authorID  int64
		content   string
		createdAt time.Time
	}{
		{roomID: roomA.ID, authorID: 5, content: "first", createdAt: baseTime.Add(1 * time.Minute)},
		{roomID: roomC.ID, authorID: 7, content: "second", createdAt: baseTime.Add(2 * time.Minute)},
		{roomID: roomB.ID, authorID: 6, content: "third", createdAt: baseTime.Add(3 * time.Minute)},
	}
	for _, message := range messages {
		if err := database.CreateMessage(&Message{
			RoomID:             message.roomID,
			AuthorUserID:       message.authorID,
			AuthorNameSnapshot: "tester",
			Content:            message.content,
			CreatedAt:          message.createdAt,
		}); err != nil {
			t.Fatalf("create message for room %q: %v", message.roomID, err)
		}
	}

	firstPage, total, totalUnread, hasMore, err := database.ListDirectRoomsForUser(2, 2, 0)
	if err != nil {
		t.Fatalf("list direct rooms first page: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total 3 direct rooms, got %d", total)
	}
	if totalUnread != 3 {
		t.Fatalf("expected total unread 3, got %d", totalUnread)
	}
	if !hasMore {
		t.Fatal("expected first page to report hasMore")
	}
	if len(firstPage) != 2 {
		t.Fatalf("expected first page to have 2 rooms, got %d", len(firstPage))
	}
	if firstPage[0].ID != roomB.ID || firstPage[1].ID != roomC.ID {
		t.Fatalf("unexpected room order on first page: got %q then %q", firstPage[0].ID, firstPage[1].ID)
	}

	secondPage, total, totalUnread, hasMore, err := database.ListDirectRoomsForUser(2, 2, 2)
	if err != nil {
		t.Fatalf("list direct rooms second page: %v", err)
	}
	if total != 3 || totalUnread != 3 {
		t.Fatalf("unexpected metadata on second page: total=%d unread=%d", total, totalUnread)
	}
	if hasMore {
		t.Fatal("did not expect second page to report hasMore")
	}
	if len(secondPage) != 1 {
		t.Fatalf("expected second page to have 1 room, got %d", len(secondPage))
	}
	if secondPage[0].ID != roomA.ID {
		t.Fatalf("expected last room on second page to be %q, got %q", roomA.ID, secondPage[0].ID)
	}
}

func TestUserRestrictionAndAbuseEvents(t *testing.T) {
	dbPath := "file:" + filepath.Join(t.TempDir(), "chat.db")
	database, err := New(dbPath, "", "general", "Obecný chat")
	if err != nil {
		t.Fatalf("new chat db: %v", err)
	}
	defer database.Close()

	now := time.Now().UTC()
	restriction, err := database.GetActiveUserRestriction(99, now)
	if err != nil {
		t.Fatalf("get empty active user restriction: %v", err)
	}
	if restriction != nil {
		t.Fatal("did not expect active restriction for fresh user")
	}

	if err := database.RecordAbuseEvent(99, "203.0.113.10", "send_public_message", true, now.Add(-2*time.Minute)); err != nil {
		t.Fatalf("record escalating abuse event: %v", err)
	}
	if err := database.RecordAbuseEvent(99, "203.0.113.10", "list_room_messages", false, now.Add(-time.Minute)); err != nil {
		t.Fatalf("record non-escalating abuse event: %v", err)
	}

	escalatingOnly, err := database.CountRecentAbuseEvents(99, now.Add(-5*time.Minute), true)
	if err != nil {
		t.Fatalf("count escalating abuse events: %v", err)
	}
	if escalatingOnly != 1 {
		t.Fatalf("expected 1 escalating event, got %d", escalatingOnly)
	}

	allEvents, err := database.CountRecentAbuseEvents(99, now.Add(-5*time.Minute), false)
	if err != nil {
		t.Fatalf("count all abuse events: %v", err)
	}
	if allEvents != 2 {
		t.Fatalf("expected 2 total events, got %d", allEvents)
	}

	firstMuteUntil := now.Add(15 * time.Minute)
	if err := database.UpsertUserRestriction(99, firstMuteUntil, "automatic chat abuse protection", now); err != nil {
		t.Fatalf("upsert user restriction: %v", err)
	}

	restriction, err = database.GetActiveUserRestriction(99, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("get active user restriction: %v", err)
	}
	if restriction == nil {
		t.Fatal("expected active restriction after upsert")
	}
	expectedMuteUntil := firstMuteUntil.Truncate(time.Second)
	if !restriction.MutedUntil.Equal(expectedMuteUntil) {
		t.Fatalf("expected muted until %s, got %s", expectedMuteUntil, restriction.MutedUntil)
	}

	if err := database.UpsertUserRestriction(99, now.Add(5*time.Minute), "shorter mute should not replace", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("upsert shorter restriction: %v", err)
	}
	restriction, err = database.GetActiveUserRestriction(99, now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("get active user restriction after shorter update: %v", err)
	}
	if restriction == nil || !restriction.MutedUntil.Equal(expectedMuteUntil) {
		t.Fatalf("expected longer mute to remain in effect, got %+v", restriction)
	}
}
