package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"gomeet/internal/agora"
	"gomeet/internal/ratelimit"
	"gomeet/internal/store"
)

type roomCreateRequest struct {
	Title           string `json:"title"`
	MaxParticipants *int   `json:"max_participants"`
}

type roomOut struct {
	ID              uuid.UUID `json:"id"`
	Slug            string    `json:"slug"`
	Title           string    `json:"title"`
	HostID          uuid.UUID `json:"host_id"`
	MaxParticipants int       `json:"max_participants"`
	ChatRoomID      *string   `json:"chat_room_id"`
	IsActive        bool      `json:"is_active"`
	CreatedAt       time.Time `json:"created_at"`
	SharePath       string    `json:"share_path"`
}

type joinResponse struct {
	AppID          string     `json:"app_id"`
	Channel        string     `json:"channel"`
	RTCToken       string     `json:"rtc_token"`
	UID            int        `json:"uid"`
	ScreenRTCToken string     `json:"screen_rtc_token"`
	ScreenUID      int        `json:"screen_uid"`
	ChatToken      string     `json:"chat_token"`
	ChatUser       string     `json:"chat_user"`
	ChatRoomID     *string    `json:"chat_room_id"`
	SessionID      *uuid.UUID `json:"session_id"`
	STTBotUID      *int       `json:"stt_bot_uid"`
}

type transcriptIn struct {
	SentenceID int    `json:"sentence_id"`
	SpeakerUID int    `json:"speaker_uid"`
	Text       string `json:"text"`
}

type participantOut struct {
	UserID   uuid.UUID  `json:"user_id"`
	Name     string     `json:"name"`
	AgoraUID *int       `json:"agora_uid"`
	JoinedAt time.Time  `json:"joined_at"`
	LeftAt   *time.Time `json:"left_at"`
}

type sessionOut struct {
	ID           uuid.UUID        `json:"id"`
	StartedAt    time.Time        `json:"started_at"`
	EndedAt      *time.Time       `json:"ended_at"`
	RecordingURL *string          `json:"recording_url"`
	Transcript   *string          `json:"transcript"`
	Participants []participantOut `json:"participants"`
}

func (a *app) createRoom(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(w, r, false)
	if !ok {
		return
	}
	var body roomCreateRequest
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" || utf8.RuneCountInString(title) > 200 {
		writeErr(w, &httpError{status: http.StatusUnprocessableEntity, detail: "Title must be 1 to 200 characters"})
		return
	}
	maxParticipants := 10
	if body.MaxParticipants != nil {
		maxParticipants = *body.MaxParticipants
	}
	if maxParticipants < 2 || maxParticipants > 50 {
		writeErr(w, &httpError{status: http.StatusUnprocessableEntity, detail: "max_participants must be from 2 to 50"})
		return
	}

	slug := newSlug()
	if err := a.agora.RegisterChatUser(r.Context(), user.ChatUsername()); err != nil {
		writeErr(w, err)
		return
	}
	chatRoomID, err := a.agora.CreateChatRoom(r.Context(), title, "GOmeet room "+slug, user.ChatUsername(), maxParticipants)
	if err != nil {
		writeErr(w, err)
		return
	}
	room := store.Room{
		ID:              uuid.New(),
		Slug:            slug,
		Title:           title,
		HostID:          user.ID,
		MaxParticipants: maxParticipants,
		ChatRoomID:      &chatRoomID,
		IsActive:        true,
	}
	if err := a.store.CreateRoom(r.Context(), &room); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toRoomOut(room))
}

func (a *app) myRooms(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(w, r, false)
	if !ok {
		return
	}
	rooms, err := a.store.RoomsByHost(r.Context(), user.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]roomOut, 0, len(rooms))
	for _, room := range rooms {
		out = append(out, toRoomOut(room))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) getRoom(w http.ResponseWriter, r *http.Request) {
	room, err := a.activeRoom(r, chi.URLParam(r, "slug"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRoomOut(room))
}

func (a *app) joinRoom(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(w, r, false)
	if !ok {
		return
	}
	if err := ratelimit.Check(r.Context(), a.redis, "rl:join:"+user.ID.String(), 20, time.Minute); err != nil {
		writeErr(w, err)
		return
	}
	room, err := a.activeRoom(r, chi.URLParam(r, "slug"))
	if err != nil {
		writeErr(w, err)
		return
	}
	stale, err := a.presence.Prune(r.Context(), room.Slug)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := a.markStale(r, room.ID, stale); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := a.presence.Join(r.Context(), room.Slug, user.ID, room.MaxParticipants); err != nil {
		writeErr(w, err)
		return
	}
	session, err := a.store.GetOrOpenSession(r.Context(), room.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := a.store.AddParticipant(r.Context(), session.ID, user.ID); err != nil {
		writeErr(w, err)
		return
	}
	sttReady := session.STTAgentID != nil && *session.STTAgentID != ""
	if !sttReady {
		agentID, err := a.agora.StartSTTAgent(r.Context(), room.Slug, "stt-"+strings.ReplaceAll(session.ID.String(), "-", ""))
		if err != nil {
			writeErr(w, err)
			return
		}
		if agentID != "" {
			if err := a.store.SetSTTAgent(r.Context(), session.ID, agentID); err != nil {
				writeErr(w, err)
				return
			}
			sttReady = true
		}
	}

	if err := a.agora.RegisterChatUser(r.Context(), user.ChatUsername()); err != nil {
		writeErr(w, err)
		return
	}
	rtcToken, err := a.agora.BuildRTCToken(room.Slug, uint32(user.AgoraUID))
	if err != nil {
		writeErr(w, err)
		return
	}
	screenUID, err := a.agora.ScreenUID(user.AgoraUID)
	if err != nil {
		writeErr(w, err)
		return
	}
	screenToken, err := a.agora.BuildRTCToken(room.Slug, screenUID)
	if err != nil {
		writeErr(w, err)
		return
	}
	chatToken, err := a.agora.BuildChatUserToken(user.ChatUsername())
	if err != nil {
		writeErr(w, err)
		return
	}
	var bot *int
	if sttReady {
		v := int(agora.STTBotUID)
		bot = &v
	}
	sessionID := session.ID
	writeJSON(w, http.StatusOK, joinResponse{
		AppID:          a.agora.AppID(),
		Channel:        room.Slug,
		RTCToken:       rtcToken,
		UID:            user.AgoraUID,
		ScreenRTCToken: screenToken,
		ScreenUID:      int(screenUID),
		ChatToken:      chatToken,
		ChatUser:       user.ChatUsername(),
		ChatRoomID:     room.ChatRoomID,
		SessionID:      &sessionID,
		STTBotUID:      bot,
	})
}

func (a *app) heartbeat(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(w, r, false)
	if !ok {
		return
	}
	room, err := a.activeRoom(r, chi.URLParam(r, "slug"))
	if err != nil {
		writeErr(w, err)
		return
	}
	stale, err := a.presence.Prune(r.Context(), room.Slug)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := a.markStale(r, room.ID, stale); err != nil {
		writeErr(w, err)
		return
	}
	if err := a.presence.Heartbeat(r.Context(), room.Slug, user.ID); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *app) leaveRoom(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(w, r, true)
	if !ok {
		return
	}
	room, err := a.activeRoom(r, chi.URLParam(r, "slug"))
	if err != nil {
		writeErr(w, err)
		return
	}
	remaining, err := a.presence.Leave(r.Context(), room.Slug, user.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := a.store.MarkLeft(r.Context(), room.ID, user.ID); err != nil {
		writeErr(w, err)
		return
	}
	agentID, closed, err := a.store.CloseSessionIfEmpty(r.Context(), room.ID, remaining)
	if err != nil {
		writeErr(w, err)
		return
	}
	if closed {
		a.agora.StopSTTAgent(r.Context(), agentID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "left"})
}

func (a *app) transcript(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(w, r, false); !ok {
		return
	}
	var body transcriptIn
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	room, err := a.activeRoom(r, chi.URLParam(r, "slug"))
	if err != nil {
		writeErr(w, err)
		return
	}
	session, err := a.store.ActiveSession(r.Context(), room.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, &httpError{status: http.StatusNotFound, detail: "No active call"})
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	key := fmt.Sprintf("stt:%s:%d", session.ID, body.SentenceID)
	stored, err := a.redis.SetNX(r.Context(), key, "1", 24*time.Hour).Result()
	if err != nil {
		writeErr(w, err)
		return
	}
	if !stored {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	name := "Guest"
	speaker, err := a.store.UserByAgoraUID(r.Context(), body.SpeakerUID)
	if err == nil {
		name = speaker.Name
	} else if !errors.Is(err, store.ErrNotFound) {
		writeErr(w, err)
		return
	}
	if err := a.store.AppendTranscript(r.Context(), session.ID, name+": "+text); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) sessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(w, r, false); !ok {
		return
	}
	room, err := a.activeRoom(r, chi.URLParam(r, "slug"))
	if err != nil {
		writeErr(w, err)
		return
	}
	sessions, err := a.store.ListSessions(r.Context(), room.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]sessionOut, 0, len(sessions))
	for _, s := range sessions {
		parts := make([]participantOut, 0, len(s.Participants))
		for _, p := range s.Participants {
			name := p.Name
			if name == "" {
				name = "Unknown"
			}
			parts = append(parts, participantOut{
				UserID:   p.UserID,
				Name:     name,
				AgoraUID: p.AgoraUID,
				JoinedAt: p.JoinedAt,
				LeftAt:   p.LeftAt,
			})
		}
		out = append(out, sessionOut{
			ID:           s.ID,
			StartedAt:    s.StartedAt,
			EndedAt:      s.EndedAt,
			RecordingURL: s.RecordingURL,
			Transcript:   s.Transcript,
			Participants: parts,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) activeRoom(r *http.Request, slug string) (store.Room, error) {
	room, err := a.store.RoomBySlug(r.Context(), slug)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !room.IsActive) {
		return store.Room{}, &httpError{status: http.StatusNotFound, detail: "Room not found"}
	}
	if err != nil {
		return store.Room{}, err
	}
	return room, nil
}

func (a *app) markStale(r *http.Request, roomID uuid.UUID, raw []string) error {
	for _, id := range raw {
		uid, err := uuid.Parse(id)
		if err != nil {
			continue
		}
		if err := a.store.MarkLeft(r.Context(), roomID, uid); err != nil {
			return err
		}
	}
	return nil
}

func toRoomOut(room store.Room) roomOut {
	return roomOut{
		ID:              room.ID,
		Slug:            room.Slug,
		Title:           room.Title,
		HostID:          room.HostID,
		MaxParticipants: room.MaxParticipants,
		ChatRoomID:      room.ChatRoomID,
		IsActive:        room.IsActive,
		CreatedAt:       room.CreatedAt,
		SharePath:       "/room/" + room.Slug,
	}
}

func newSlug() string {
	buf := make([]byte, 9)
	if _, err := rand.Read(buf); err != nil {
		return strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	}
	s := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(base64.RawURLEncoding.EncodeToString(buf)))
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
