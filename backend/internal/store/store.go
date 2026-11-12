package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// User is an account. ChatUsername is the Agora Chat user id (UUID hex, no hyphens).
type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	Name         string
	AgoraUID     int
	CreatedAt    time.Time
}

func (u User) ChatUsername() string {
	return stringsReplaceHyphen(u.ID)
}

func stringsReplaceHyphen(id uuid.UUID) string {
	s := id.String()
	out := make([]byte, 0, 32)
	for i := 0; i < len(s); i++ {
		if s[i] != '-' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

// Room is a meeting. Slug is the Agora channel name.
type Room struct {
	ID              uuid.UUID
	Slug            string
	Title           string
	HostID          uuid.UUID
	MaxParticipants int
	ChatRoomID      *string
	IsActive        bool
	CreatedAt       time.Time
}

// CallSession is one continuous call in a room.
type CallSession struct {
	ID           uuid.UUID
	RoomID       uuid.UUID
	StartedAt    time.Time
	EndedAt      *time.Time
	RecordingURL *string
	Transcript   *string
	STTAgentID   *string
	Participants []Participant
}

// Participant is one user's membership in a call session.
type Participant struct {
	UserID   uuid.UUID
	Name     string
	AgoraUID *int
	JoinedAt time.Time
	LeftAt   *time.Time
}

// Store is the metadata persistence used by the API.
type Store interface {
	CreateUser(ctx context.Context, u *User) error
	DeleteUser(ctx context.Context, id uuid.UUID) error
	UserByEmail(ctx context.Context, email string) (User, error)
	UserByID(ctx context.Context, id uuid.UUID) (User, error)
	UserByAgoraUID(ctx context.Context, uid int) (User, error)
	MaxAgoraUID(ctx context.Context) (*int, error)

	CreateRoom(ctx context.Context, room *Room) error
	RoomBySlug(ctx context.Context, slug string) (Room, error)
	RoomsByHost(ctx context.Context, hostID uuid.UUID) ([]Room, error)

	GetOrOpenSession(ctx context.Context, roomID uuid.UUID) (CallSession, error)
	AddParticipant(ctx context.Context, sessionID, userID uuid.UUID) error
	SetSTTAgent(ctx context.Context, sessionID uuid.UUID, agentID string) error
	MarkLeft(ctx context.Context, roomID, userID uuid.UUID) error
	CloseSessionIfEmpty(ctx context.Context, roomID uuid.UUID, remaining int) (agentID string, closed bool, err error)
	ActiveSession(ctx context.Context, roomID uuid.UUID) (CallSession, error)
	AppendTranscript(ctx context.Context, sessionID uuid.UUID, line string) error
	ListSessions(ctx context.Context, roomID uuid.UUID) ([]CallSession, error)
}
