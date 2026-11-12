-- +goose Up
-- GOmeet lives in its own schema. public.users and Django tables are not modified.
CREATE SCHEMA gomeet;

CREATE TABLE gomeet.users (
    id UUID PRIMARY KEY,
    email VARCHAR(320) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(120) NOT NULL,
    agora_uid INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ix_gomeet_users_email ON gomeet.users (email);
CREATE UNIQUE INDEX ix_gomeet_users_agora_uid ON gomeet.users (agora_uid);

CREATE TABLE gomeet.rooms (
    id UUID PRIMARY KEY,
    slug VARCHAR(64) NOT NULL,
    title VARCHAR(200) NOT NULL,
    host_id UUID NOT NULL REFERENCES gomeet.users (id),
    max_participants INTEGER NOT NULL DEFAULT 10,
    chat_room_id TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ix_gomeet_rooms_slug ON gomeet.rooms (slug);

CREATE TABLE gomeet.call_sessions (
    id UUID PRIMARY KEY,
    room_id UUID NOT NULL REFERENCES gomeet.rooms (id),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at TIMESTAMPTZ,
    recording_url TEXT,
    transcript TEXT,
    stt_agent_id TEXT
);
CREATE INDEX ix_gomeet_call_sessions_room_id_started_at ON gomeet.call_sessions (room_id, started_at);

CREATE TABLE gomeet.participants (
    id UUID PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES gomeet.call_sessions (id),
    user_id UUID NOT NULL REFERENCES gomeet.users (id),
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    left_at TIMESTAMPTZ
);
CREATE INDEX ix_gomeet_participants_session_id ON gomeet.participants (session_id);
CREATE INDEX ix_gomeet_participants_user_id ON gomeet.participants (user_id);

-- +goose Down
-- No-op. Do not drop public.users or any existing application data.
SELECT 1;
