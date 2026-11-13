import os

from alembic import context
from sqlalchemy import create_engine

url = os.environ["REMOTE_AGENT_SESSION_DATABASE_URL"]
if url.startswith("postgresql://"):
    url = url.replace("postgresql://", "postgresql+psycopg://", 1)
with create_engine(url).connect() as connection:
    context.configure(connection=connection)
    with context.begin_transaction():
        context.run_migrations()
