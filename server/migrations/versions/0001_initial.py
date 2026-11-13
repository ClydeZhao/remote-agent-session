from pathlib import Path

from alembic import op

revision = "0001"
down_revision = None


def upgrade():
    op.execute(Path(__file__).with_suffix(".sql").read_text())


def downgrade():
    raise RuntimeError("Restore an explicit backup instead of destructive downgrade")
