from datetime import datetime
from typing import Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, SecretStr


class DeviceStart(BaseModel):
    model_config = ConfigDict(extra="forbid")
    device_name: str = Field(min_length=1, max_length=100, examples=["workstation"])


class DeviceCreated(BaseModel):
    device_id: UUID
    user_code: str
    claim_secret: str
    expires_at: datetime
    poll_interval: int = 3
    verification_uri: str
    simulated: bool = True


class DeviceClaim(BaseModel):
    model_config = ConfigDict(extra="forbid")
    claim_secret: SecretStr


class DeviceToken(BaseModel):
    state: Literal["pending", "awaiting_confirmation", "authorized", "expired"]
    access_token: str | None = None
    token_type: Literal["Bearer"] = "Bearer"
    expires_at: datetime | None = None


class UserView(BaseModel):
    user_id: UUID
    session_id: UUID
    display_name: str | None = None
    feishu_account: str | None = None


class VerifiedPairingEvent(BaseModel):
    event_id: str
    app: str
    tenant: str
    subject: str
    private_chat: bool
    user_code: str
    confirmation: str | None = None
