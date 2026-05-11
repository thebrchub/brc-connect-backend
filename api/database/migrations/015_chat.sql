-- 015_chat.sql
-- Chat tables for 1:1 messaging (Phase 1)

-- Rooms (DM or group)
CREATE TABLE IF NOT EXISTS rooms (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id    UUID NOT NULL REFERENCES users(id),
    type        VARCHAR(10) NOT NULL DEFAULT 'dm' CHECK (type IN ('dm', 'group')),
    name        VARCHAR(100),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_rooms_admin_id ON rooms(admin_id);

-- Room members
CREATE TABLE IF NOT EXISTS room_members (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id     UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id),
    role        VARCHAR(10) NOT NULL DEFAULT 'member' CHECK (role IN ('member', 'admin')),
    status      VARCHAR(10) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'left')),
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at     TIMESTAMPTZ,
    last_read_at       TIMESTAMPTZ,
    last_delivered_at   TIMESTAMPTZ,
    UNIQUE(room_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_room_members_user_id ON room_members(user_id);
CREATE INDEX IF NOT EXISTS idx_room_members_room_user ON room_members(room_id, user_id);

-- Messages
CREATE TABLE IF NOT EXISTS messages (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id     UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    sender_id   UUID NOT NULL REFERENCES users(id),
    content     TEXT,
    media_url   TEXT,
    media_type  VARCHAR(20),
    reply_to    UUID REFERENCES messages(id),
    edited_at   TIMESTAMPTZ,
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_messages_room_created ON messages(room_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender_id);

-- Call logs
CREATE TABLE IF NOT EXISTS call_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    call_id         VARCHAR(64) NOT NULL UNIQUE,
    room_id         UUID REFERENCES rooms(id),
    initiated_by    UUID NOT NULL REFERENCES users(id),
    peer_id         UUID REFERENCES users(id),
    call_type       VARCHAR(10) NOT NULL DEFAULT 'audio' CHECK (call_type IN ('audio', 'video')),
    status          VARCHAR(20) NOT NULL DEFAULT 'ringing' CHECK (status IN ('ringing', 'answered', 'completed', 'missed', 'rejected', 'cancelled')),
    started_at      TIMESTAMPTZ,
    ended_at        TIMESTAMPTZ,
    duration_seconds INTEGER,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_call_logs_initiated_by ON call_logs(initiated_by, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_call_logs_peer ON call_logs(peer_id, created_at DESC);
