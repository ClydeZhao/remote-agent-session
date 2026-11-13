CREATE SCHEMA identity;
CREATE SCHEMA notifications;
CREATE TABLE identity.users (
 id uuid PRIMARY KEY, app text NOT NULL, tenant text NOT NULL, subject text NOT NULL, display_name text,
 UNIQUE(app,tenant,subject)
);
CREATE TABLE identity.devices (
 id uuid PRIMARY KEY, label text NOT NULL, code_hash text UNIQUE NOT NULL,
 claim_hash text NOT NULL, expires_at timestamptz NOT NULL,
 state text NOT NULL DEFAULT 'pending', candidate jsonb, confirmation_hash text,
 user_id uuid REFERENCES identity.users, token_cipher text, session_id uuid
);
CREATE TABLE identity.events (id text PRIMARY KEY);
CREATE TABLE identity.sessions (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES identity.users,
 token_hash text UNIQUE NOT NULL, expires_at timestamptz NOT NULL, revoked boolean NOT NULL DEFAULT false
);

CREATE TABLE identity.rate_limits (scope text PRIMARY KEY, window_started_at timestamptz NOT NULL, attempts int NOT NULL);

ALTER TABLE identity.devices ADD CONSTRAINT device_states CHECK(state IN ('pending','awaiting_confirmation','authorized'));
CREATE TABLE public.remote_agent_session_environment (singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton), mode text NOT NULL CHECK(mode IN ('offline','live')));
CREATE SCHEMA session_relay;

CREATE TABLE session_relay.bindings (
        user_id uuid PRIMARY KEY REFERENCES identity.users,
        device_id uuid NOT NULL REFERENCES identity.sessions,
        native_session text NOT NULL, device_name text NOT NULL,
        project text NOT NULL, session_name text NOT NULL,
        generation bigint NOT NULL DEFAULT 1,
        desired boolean NOT NULL DEFAULT true,
        status text NOT NULL DEFAULT 'connecting',
        heartbeat timestamptz NOT NULL DEFAULT clock_timestamp(),
        updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
    );

CREATE TABLE session_relay.messages (
        id uuid PRIMARY KEY, event_id text UNIQUE NOT NULL,
        user_id uuid NOT NULL REFERENCES identity.users,
        generation bigint NOT NULL, kind text NOT NULL,
        device_id uuid NOT NULL REFERENCES identity.sessions, native_session text NOT NULL,
        content text NOT NULL, parent_id text, chat_id text, context_ready boolean NOT NULL DEFAULT false,
        state text NOT NULL DEFAULT 'pending',
        created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
        dispatched_at timestamptz, result_digest text
    );

CREATE TABLE notifications.session_deliveries (
        id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES identity.users,
        generation bigint NOT NULL, message_key text NOT NULL UNIQUE, group_key text NOT NULL,
        attempts integer NOT NULL DEFAULT 0, last_error text,
        not_before timestamptz NOT NULL DEFAULT clock_timestamp(),
        content text NOT NULL, parent_id text, chat_id text, context_ready boolean NOT NULL DEFAULT false,
        state text NOT NULL DEFAULT 'pending',
        created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
        receipt text, started_at timestamptz
    );

CREATE INDEX ON session_relay.messages(user_id,generation,created_at);

ALTER TABLE notifications.session_deliveries ADD COLUMN device_id uuid REFERENCES identity.sessions, ADD COLUMN card jsonb, ADD COLUMN interaction_id uuid;

CREATE TABLE session_relay.interactions (
        id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES identity.users,
        device_id uuid NOT NULL REFERENCES identity.sessions,
        generation bigint NOT NULL, native_session text NOT NULL, turn_id text NOT NULL,
        kind text NOT NULL, prompt text NOT NULL, request_digest text NOT NULL,
        message_ids uuid[] NOT NULL,
        state text NOT NULL DEFAULT 'pending', answer text,
        expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '5 minutes'
    );

ALTER TABLE session_relay.messages ADD COLUMN platform_message_id text;

ALTER TABLE notifications.session_deliveries ADD COLUMN kind text NOT NULL DEFAULT 'message';

CREATE TABLE notifications.session_activity (
        message_id uuid PRIMARY KEY REFERENCES session_relay.messages,
        platform_message_id text NOT NULL,
        applied boolean, lease_id uuid,
        next_attempt timestamptz NOT NULL DEFAULT clock_timestamp()
    );

ALTER TABLE session_relay.bindings ADD COLUMN previous_binding jsonb;

CREATE TABLE session_relay.interaction_cancellations (
        id uuid NOT NULL,
        user_id uuid NOT NULL REFERENCES identity.users,
        device_id uuid NOT NULL REFERENCES identity.sessions,
        generation bigint NOT NULL,
        PRIMARY KEY (user_id,device_id,generation,id)
    );

ALTER TABLE session_relay.interactions ADD COLUMN reaction_enabled boolean NOT NULL DEFAULT false, ADD COLUMN reaction_next_check timestamptz NOT NULL DEFAULT clock_timestamp();

ALTER TABLE session_relay.interactions ADD COLUMN choices jsonb;

ALTER TABLE session_relay.messages ADD COLUMN answer_target uuid;

ALTER TABLE session_relay.interactions ADD COLUMN async_question boolean NOT NULL DEFAULT false;

ALTER TABLE session_relay.bindings ADD COLUMN runtime_kind text NOT NULL DEFAULT 'codex' CHECK (runtime_kind IN ('codex','deepseek-harness')), ADD COLUMN runtime_instance text NOT NULL DEFAULT '';

ALTER TABLE session_relay.messages ADD COLUMN runtime_kind text NOT NULL DEFAULT 'codex' CHECK (runtime_kind IN ('codex','deepseek-harness')), ADD COLUMN runtime_instance text NOT NULL DEFAULT '';

ALTER TABLE session_relay.messages ADD COLUMN summary_digest text;
