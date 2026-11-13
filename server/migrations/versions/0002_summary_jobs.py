from alembic import op

revision = "0002"
down_revision = "0001"


def upgrade():
    op.execute("""CREATE TABLE session_relay.summary_jobs (
        message_id uuid PRIMARY KEY REFERENCES session_relay.messages,
        user_id uuid NOT NULL REFERENCES identity.users,
        device_id uuid NOT NULL REFERENCES identity.sessions,
        generation bigint NOT NULL, text text NOT NULL,
        state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','claimed','completed','expired','cancelled')),
        claim_id uuid UNIQUE, lease_until timestamptz,
        created_at timestamptz NOT NULL DEFAULT clock_timestamp()
    )""")
    op.execute("""CREATE TABLE session_relay.summary_claims (
        claim_id uuid PRIMARY KEY,
        user_id uuid NOT NULL REFERENCES identity.users,
        device_id uuid NOT NULL REFERENCES identity.sessions,
        generation bigint NOT NULL,
        message_id uuid REFERENCES session_relay.summary_jobs
    )""")
    op.execute(
        "CREATE INDEX summary_pending ON session_relay.summary_jobs(user_id,device_id,generation,created_at) WHERE state='pending'"
    )


def downgrade():
    raise RuntimeError("Restore an explicit backup instead of destructive downgrade")
