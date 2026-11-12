-- +goose Up
CREATE TABLE users (
    id UUID PRIMARY KEY,
    email VARCHAR(320) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(120) NOT NULL,
    agora_uid INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ix_users_email ON users (email);
CREATE UNIQUE INDEX ix_users_agora_uid ON users (agora_uid);

CREATE TABLE rooms (
    id UUID PRIMARY KEY,
    slug VARCHAR(64) NOT NULL,
    title VARCHAR(200) NOT NULL,
    host_id UUID NOT NULL REFERENCES users (id),
    max_participants INTEGER NOT NULL DEFAULT 10,
    chat_room_id TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ix_rooms_slug ON rooms (slug);

CREATE TABLE call_sessions (
    id UUID PRIMARY KEY,
    room_id UUID NOT NULL REFERENCES rooms (id),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at TIMESTAMPTZ,
    recording_url TEXT,
    transcript TEXT,
    stt_agent_id TEXT
);
CREATE INDEX ix_call_sessions_room_id_started_at ON call_sessions (room_id, started_at);

CREATE TABLE participants (
    id UUID PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES call_sessions (id),
    user_id UUID NOT NULL REFERENCES users (id),
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    left_at TIMESTAMPTZ
);
CREATE INDEX ix_participants_session_id ON participants (session_id);
CREATE INDEX ix_participants_user_id ON participants (user_id);

-- +goose Down
DROP TABLE participants;
DROP TABLE call_sessions;
DROP TABLE rooms;
DROP TABLE users;
