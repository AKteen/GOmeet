package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres stores users, rooms, and call logs.
type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}

func (p *Postgres) CreateUser(ctx context.Context, u *User) error {
	err := p.pool.QueryRow(ctx, `
		INSERT INTO gomeet.users (id, email, password_hash, name, agora_uid)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at
	`, u.ID, u.Email, u.PasswordHash, u.Name, u.AgoraUID).Scan(&u.CreatedAt)
	return mapErr(err)
}

func (p *Postgres) DeleteUser(ctx context.Context, id uuid.UUID) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM gomeet.users WHERE id=$1`, id)
	return err
}

func (p *Postgres) UserByEmail(ctx context.Context, email string) (User, error) {
	return p.scanUser(ctx, `SELECT id, email, password_hash, name, agora_uid, created_at FROM gomeet.users WHERE email=$1`, email)
}

func (p *Postgres) UserByID(ctx context.Context, id uuid.UUID) (User, error) {
	return p.scanUser(ctx, `SELECT id, email, password_hash, name, agora_uid, created_at FROM gomeet.users WHERE id=$1`, id)
}

func (p *Postgres) UserByAgoraUID(ctx context.Context, uid int) (User, error) {
	return p.scanUser(ctx, `SELECT id, email, password_hash, name, agora_uid, created_at FROM gomeet.users WHERE agora_uid=$1`, uid)
}

func (p *Postgres) scanUser(ctx context.Context, query string, arg any) (User, error) {
	var u User
	err := p.pool.QueryRow(ctx, query, arg).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.AgoraUID, &u.CreatedAt)
	if err != nil {
		return User{}, mapErr(err)
	}
	return u, nil
}

func (p *Postgres) MaxAgoraUID(ctx context.Context) (*int, error) {
	var n *int
	err := p.pool.QueryRow(ctx, `SELECT MAX(agora_uid) FROM gomeet.users`).Scan(&n)
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (p *Postgres) CreateRoom(ctx context.Context, room *Room) error {
	err := p.pool.QueryRow(ctx, `
		INSERT INTO gomeet.rooms (id, slug, title, host_id, max_participants, chat_room_id, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at
	`, room.ID, room.Slug, room.Title, room.HostID, room.MaxParticipants, room.ChatRoomID, room.IsActive).Scan(&room.CreatedAt)
	return mapErr(err)
}

func (p *Postgres) RoomBySlug(ctx context.Context, slug string) (Room, error) {
	var room Room
	err := p.pool.QueryRow(ctx, `
		SELECT id, slug, title, host_id, max_participants, chat_room_id, is_active, created_at
		FROM gomeet.rooms WHERE slug=$1
	`, slug).Scan(&room.ID, &room.Slug, &room.Title, &room.HostID, &room.MaxParticipants, &room.ChatRoomID, &room.IsActive, &room.CreatedAt)
	if err != nil {
		return Room{}, mapErr(err)
	}
	return room, nil
}

func (p *Postgres) RoomsByHost(ctx context.Context, hostID uuid.UUID) ([]Room, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, slug, title, host_id, max_participants, chat_room_id, is_active, created_at
		FROM gomeet.rooms WHERE host_id=$1 ORDER BY created_at DESC
	`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Room, 0)
	for rows.Next() {
		var room Room
		if err := rows.Scan(&room.ID, &room.Slug, &room.Title, &room.HostID, &room.MaxParticipants, &room.ChatRoomID, &room.IsActive, &room.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, room)
	}
	return out, rows.Err()
}

func scanSession(row pgx.Row) (CallSession, error) {
	var s CallSession
	err := row.Scan(&s.ID, &s.RoomID, &s.StartedAt, &s.EndedAt, &s.RecordingURL, &s.Transcript, &s.STTAgentID)
	return s, err
}

const sessionCols = `id, room_id, started_at, ended_at, recording_url, transcript, stt_agent_id`

func (p *Postgres) GetOrOpenSession(ctx context.Context, roomID uuid.UUID) (CallSession, error) {
	s, err := scanSession(p.pool.QueryRow(ctx, `
		SELECT `+sessionCols+`
		FROM gomeet.call_sessions
		WHERE room_id=$1 AND ended_at IS NULL
		ORDER BY started_at DESC
		LIMIT 1
	`, roomID))
	if err == nil {
		return s, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CallSession{}, err
	}
	s = CallSession{ID: uuid.New(), RoomID: roomID}
	err = p.pool.QueryRow(ctx, `
		INSERT INTO gomeet.call_sessions (id, room_id) VALUES ($1, $2) RETURNING started_at
	`, s.ID, s.RoomID).Scan(&s.StartedAt)
	return s, err
}

func (p *Postgres) AddParticipant(ctx context.Context, sessionID, userID uuid.UUID) error {
	var existing uuid.UUID
	err := p.pool.QueryRow(ctx, `
		SELECT id FROM gomeet.participants
		WHERE session_id=$1 AND user_id=$2 AND left_at IS NULL
	`, sessionID, userID).Scan(&existing)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO gomeet.participants (id, session_id, user_id) VALUES ($1, $2, $3)
	`, uuid.New(), sessionID, userID)
	return err
}

func (p *Postgres) SetSTTAgent(ctx context.Context, sessionID uuid.UUID, agentID string) error {
	_, err := p.pool.Exec(ctx, `UPDATE gomeet.call_sessions SET stt_agent_id=$2 WHERE id=$1`, sessionID, agentID)
	return err
}

func (p *Postgres) MarkLeft(ctx context.Context, roomID, userID uuid.UUID) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE gomeet.participants SET left_at=$3
		WHERE user_id=$2 AND left_at IS NULL AND session_id IN (
			SELECT id FROM gomeet.call_sessions WHERE room_id=$1 AND ended_at IS NULL
		)
	`, roomID, userID, time.Now().UTC())
	return err
}

func (p *Postgres) CloseSessionIfEmpty(ctx context.Context, roomID uuid.UUID, remaining int) (string, bool, error) {
	if remaining > 0 {
		return "", false, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)

	var id uuid.UUID
	var agent *string
	err = tx.QueryRow(ctx, `
		SELECT id, stt_agent_id FROM gomeet.call_sessions
		WHERE room_id=$1 AND ended_at IS NULL
		ORDER BY started_at DESC
		LIMIT 1
	`, roomID).Scan(&id, &agent)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE gomeet.participants SET left_at=$2 WHERE session_id=$1 AND left_at IS NULL
	`, id, now); err != nil {
		return "", false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE gomeet.call_sessions SET ended_at=$2 WHERE id=$1`, id, now); err != nil {
		return "", false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	if agent == nil {
		return "", true, nil
	}
	return *agent, true, nil
}

func (p *Postgres) ActiveSession(ctx context.Context, roomID uuid.UUID) (CallSession, error) {
	s, err := scanSession(p.pool.QueryRow(ctx, `
		SELECT `+sessionCols+`
		FROM gomeet.call_sessions
		WHERE room_id=$1 AND ended_at IS NULL
		ORDER BY started_at DESC
		LIMIT 1
	`, roomID))
	if err != nil {
		return CallSession{}, mapErr(err)
	}
	return s, nil
}

func (p *Postgres) AppendTranscript(ctx context.Context, sessionID uuid.UUID, line string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE gomeet.call_sessions
		SET transcript = CASE
			WHEN transcript IS NULL OR transcript = '' THEN $2
			ELSE transcript || E'\n' || $2
		END
		WHERE id=$1
	`, sessionID, line)
	return err
}

func (p *Postgres) ListSessions(ctx context.Context, roomID uuid.UUID) ([]CallSession, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT `+sessionCols+`
		FROM gomeet.call_sessions WHERE room_id=$1
		ORDER BY started_at DESC
	`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CallSession, 0)
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		parts, err := p.participants(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		s.Participants = parts
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Postgres) participants(ctx context.Context, sessionID uuid.UUID) ([]Participant, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT p.user_id, u.name, u.agora_uid, p.joined_at, p.left_at
		FROM gomeet.participants p
		LEFT JOIN gomeet.users u ON u.id = p.user_id
		WHERE p.session_id=$1
		ORDER BY p.joined_at
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Participant, 0)
	for rows.Next() {
		var part Participant
		var name *string
		if err := rows.Scan(&part.UserID, &name, &part.AgoraUID, &part.JoinedAt, &part.LeftAt); err != nil {
			return nil, err
		}
		if name == nil || *name == "" {
			part.Name = "Unknown"
			part.AgoraUID = nil
		} else {
			part.Name = *name
		}
		out = append(out, part)
	}
	return out, rows.Err()
}
