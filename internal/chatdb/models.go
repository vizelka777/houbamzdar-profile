package chatdb

import "time"

type Room struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	Slug            string    `json:"slug,omitempty"`
	Title           string    `json:"title"`
	CreatedByUserID int64     `json:"created_by_user_id"`
	DirectPairKey   string    `json:"-"`
	CreatedAt       time.Time `json:"created_at"`
	ArchivedAt      time.Time `json:"archived_at,omitempty"`
	LastMessage     *Message  `json:"last_message,omitempty"`
	UnreadCount     int       `json:"unread_count"`
	OtherUserID     int64     `json:"other_user_id,omitempty"`
	OtherUserName   string    `json:"other_user_name,omitempty"`
	OtherUserAvatar string    `json:"other_user_avatar,omitempty"`
}

type Message struct {
	ID                 string    `json:"id"`
	RoomID             string    `json:"room_id"`
	AuthorUserID       int64     `json:"author_user_id"`
	AuthorNameSnapshot string    `json:"author_name"`
	AuthorAvatar       string    `json:"author_avatar,omitempty"`
	Content            string    `json:"content"`
	CreatedAt          time.Time `json:"created_at"`
	EditedAt           time.Time `json:"edited_at,omitempty"`
	DeletedAt          time.Time `json:"deleted_at,omitempty"`
}

type UserRestriction struct {
	UserID     int64     `json:"user_id"`
	MutedUntil time.Time `json:"muted_until,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}
