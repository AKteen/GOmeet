package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Memory is an in-process store used by tests and local handler checks.
type Memory struct {
	mu         sync.Mutex
	users      map[uuid.UUID]*User
	byEmail    map[string]uuid.UUID
	rooms      map[uuid.UUID]*Room
	bySlug     map[string]uuid.UUID
	sessions   map[uuid.UUID]*CallSession
	openByRoom map[uuid.UUID]uuid.UUID
}

func NewMemory() *Memory {
	return &Memory{
		users:      map[uuid.UUID]*User{},
		byEmail:    map[string]uuid.UUID{},
		rooms:      map[uuid.UUID]*Room{},
		bySlug:     map[string]uuid.UUID{},
		sessions:   map[uuid.UUID]*CallSession{},
		openByRoom: map[uuid.UUID]uuid.UUID{},
	}
}

func (m *Memory) CreateUser(_ context.Context, u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byEmail[u.Email]; ok {
		return ErrConflict
	}
	for _, existing := range m.users {
		if existing.AgoraUID == u.AgoraUID {
			return ErrConflict
		}
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	cp := *u
	m.users[u.ID] = &cp
	m.byEmail[u.Email] = u.ID
	return nil
}

func (m *Memory) DeleteUser(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	delete(m.byEmail, u.Email)
	delete(m.users, id)
	return nil
}

func (m *Memory) UserByEmail(_ context.Context, email string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byEmail[email]
	if !ok {
		return User{}, ErrNotFound
	}
	return *m.users[id], nil
}

func (m *Memory) UserByID(_ context.Context, id uuid.UUID) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return *u, nil
}

func (m *Memory) UserByAgoraUID(_ context.Context, uid int) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.AgoraUID == uid {
			return *u, nil
		}
	}
	return User{}, ErrNotFound
}

func (m *Memory) MaxAgoraUID(_ context.Context) (*int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.users) == 0 {
		return nil, nil
	}
	var max int
	first := true
	for _, u := range m.users {
		if first || u.AgoraUID > max {
			max = u.AgoraUID
			first = false
		}
	}
	return &max, nil
}

func (m *Memory) CreateRoom(_ context.Context, room *Room) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.bySlug[room.Slug]; ok {
		return ErrConflict
	}
	if room.CreatedAt.IsZero() {
		room.CreatedAt = time.Now().UTC()
	}
	cp := *room
	m.rooms[room.ID] = &cp
	m.bySlug[room.Slug] = room.ID
	return nil
}

func (m *Memory) RoomBySlug(_ context.Context, slug string) (Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.bySlug[slug]
	if !ok {
		return Room{}, ErrNotFound
	}
	return *m.rooms[id], nil
}

func (m *Memory) RoomsByHost(_ context.Context, hostID uuid.UUID) ([]Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Room, 0)
	for _, room := range m.rooms {
		if room.HostID == hostID {
			out = append(out, *room)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (m *Memory) GetOrOpenSession(_ context.Context, roomID uuid.UUID) (CallSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.openByRoom[roomID]; ok {
		return copySession(m.sessions[id]), nil
	}
	s := &CallSession{ID: uuid.New(), RoomID: roomID, StartedAt: time.Now().UTC()}
	m.sessions[s.ID] = s
	m.openByRoom[roomID] = s.ID
	return copySession(s), nil
}

func (m *Memory) AddParticipant(_ context.Context, sessionID, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	for _, p := range s.Participants {
		if p.UserID == userID && p.LeftAt == nil {
			return nil
		}
	}
	part := Participant{UserID: userID, Name: "Unknown", JoinedAt: time.Now().UTC()}
	if u, ok := m.users[userID]; ok {
		uid := u.AgoraUID
		part.Name = u.Name
		part.AgoraUID = &uid
	}
	s.Participants = append(s.Participants, part)
	return nil
}

func (m *Memory) SetSTTAgent(_ context.Context, sessionID uuid.UUID, agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	v := agentID
	s.STTAgentID = &v
	return nil
}

func (m *Memory) MarkLeft(_ context.Context, roomID, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.openByRoom[roomID]
	if !ok {
		return nil
	}
	s := m.sessions[id]
	now := time.Now().UTC()
	for i := range s.Participants {
		if s.Participants[i].UserID == userID && s.Participants[i].LeftAt == nil {
			s.Participants[i].LeftAt = &now
		}
	}
	return nil
}

func (m *Memory) CloseSessionIfEmpty(_ context.Context, roomID uuid.UUID, remaining int) (string, bool, error) {
	if remaining > 0 {
		return "", false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.openByRoom[roomID]
	if !ok {
		return "", false, nil
	}
	s := m.sessions[id]
	now := time.Now().UTC()
	for i := range s.Participants {
		if s.Participants[i].LeftAt == nil {
			s.Participants[i].LeftAt = &now
		}
	}
	s.EndedAt = &now
	delete(m.openByRoom, roomID)
	if s.STTAgentID == nil {
		return "", true, nil
	}
	return *s.STTAgentID, true, nil
}

func (m *Memory) ActiveSession(_ context.Context, roomID uuid.UUID) (CallSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.openByRoom[roomID]
	if !ok {
		return CallSession{}, ErrNotFound
	}
	return copySession(m.sessions[id]), nil
}

func (m *Memory) AppendTranscript(_ context.Context, sessionID uuid.UUID, line string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	if s.Transcript == nil || *s.Transcript == "" {
		v := line
		s.Transcript = &v
		return nil
	}
	v := *s.Transcript + "\n" + line
	s.Transcript = &v
	return nil
}

func (m *Memory) ListSessions(_ context.Context, roomID uuid.UUID) ([]CallSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]CallSession, 0)
	for _, s := range m.sessions {
		if s.RoomID == roomID {
			out = append(out, copySession(s))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out, nil
}

func copySession(s *CallSession) CallSession {
	cp := *s
	cp.Participants = make([]Participant, len(s.Participants))
	for i, p := range s.Participants {
		cp.Participants[i] = p
		if p.AgoraUID != nil {
			uid := *p.AgoraUID
			cp.Participants[i].AgoraUID = &uid
		}
		if p.LeftAt != nil {
			left := *p.LeftAt
			cp.Participants[i].LeftAt = &left
		}
	}
	if s.STTAgentID != nil {
		v := *s.STTAgentID
		cp.STTAgentID = &v
	}
	if s.Transcript != nil {
		v := *s.Transcript
		cp.Transcript = &v
	}
	if s.RecordingURL != nil {
		v := *s.RecordingURL
		cp.RecordingURL = &v
	}
	if s.EndedAt != nil {
		v := *s.EndedAt
		cp.EndedAt = &v
	}
	return cp
}
