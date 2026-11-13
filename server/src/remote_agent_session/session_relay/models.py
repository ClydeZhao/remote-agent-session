from datetime import datetime
from typing import Annotated, Literal
from uuid import UUID

from pydantic import BaseModel, BeforeValidator, ConfigDict, Field, model_validator

NativeSession = Annotated[
    str,
    BeforeValidator(lambda value: str(value) if isinstance(value, UUID) else value),
    Field(min_length=1, max_length=200, pattern=r"^[A-Za-z0-9][A-Za-z0-9._:-]*$"),
]
RuntimeKind = Literal["codex", "deepseek-harness"]


class RelayAttach(BaseModel):
    model_config = ConfigDict(extra="forbid")
    native_session: NativeSession
    runtime_kind: RuntimeKind = "codex"
    runtime_instance: str = Field(default="", max_length=200, pattern=r"^[A-Za-z0-9._:-]*$")
    device_name: str = Field(min_length=1, max_length=100)
    project: str = Field(min_length=1, max_length=200)
    session_name: str = Field(min_length=1, max_length=200)

    @model_validator(mode="after")
    def runtime_identity_required(self):
        if self.runtime_kind != "codex" and not self.runtime_instance:
            raise ValueError("runtime instance required for this agent")
        if self.runtime_kind == "codex":
            self.native_session = str(UUID(self.native_session))
        return self


class RelayGeneration(BaseModel):
    model_config = ConfigDict(extra="forbid")
    generation: int = Field(ge=1)


class RelaySummaryClaim(RelayGeneration):
    claim_id: UUID


class RelaySummaryJob(BaseModel):
    message_id: UUID
    generation: int
    text: str


class RelaySummaryClaimView(BaseModel):
    job: RelaySummaryJob | None = None


class RelaySummary(RelayGeneration):
    claim_id: UUID | None = None
    summary: str = Field(min_length=1, max_length=2000)


class RelayReadiness(RelayGeneration):
    available: bool


class RelayResult(RelayGeneration):
    message_ids: list[str] = Field(default_factory=list, max_length=100)
    outcome: Literal[
        "completed",
        "failed",
        "mixed",
        "unknown",
        "output_too_large",
        "empty_output",
        "interrupted",
        "interrupt_unavailable",
        "not_submitted",
    ]
    text: str = Field(default="", max_length=262144)
    summary: str = Field(default="", max_length=2000)
    summary_pending: bool = False

    @model_validator(mode="after")
    def pending_summary_requires_body(self):
        if self.summary_pending and (self.outcome != "completed" or not self.text or self.summary):
            raise ValueError("pending summary requires completed body without inline summary")
        return self


class RelayMessage(BaseModel):
    id: UUID
    generation: int
    kind: str
    content: str
    state: str
    created_at: datetime
    dispatched_at: datetime | None = None


class RelayCancellation(BaseModel):
    id: UUID
    native_session: str
    runtime_kind: RuntimeKind = "codex"
    runtime_instance: str = ""


class RelayView(BaseModel):
    user_id: UUID
    native_session: str
    runtime_kind: RuntimeKind = "codex"
    runtime_instance: str = ""
    device_name: str
    project: str
    session_name: str
    generation: int
    desired: bool
    status: str
    online: bool
    guidance: str
    delivery_warning: bool = False
    messages: list[RelayMessage] = Field(default_factory=list)
    cancellations: list[RelayCancellation] = Field(default_factory=list)
    async_interactions: list[UUID] = Field(default_factory=list)


class RelayOrigin(RelayGeneration):
    for_result: bool = False
    native_session: NativeSession
    message_ids: list[str] = Field(min_length=1, max_length=100)


class OriginView(BaseModel):
    scope: Literal["local", "remote", "mixed", "inactive"]


class RelayInteraction(RelayGeneration):
    id: UUID
    native_session: NativeSession
    turn_id: str = Field(min_length=1, max_length=200)
    message_ids: list[UUID] = Field(min_length=1, max_length=5)
    kind: Literal["permission", "question", "attention"]
    async_question: bool = False
    prompt: str = Field(min_length=1, max_length=12000)
    choices: (
        dict[
            Annotated[str, Field(pattern="^[A-Z]$")],
            Annotated[str, Field(min_length=1, max_length=8000)],
        ]
        | None
    ) = None

    @model_validator(mode="after")
    def choices_only_for_question(self):
        if self.async_question and self.kind != "question":
            raise ValueError("only questions can be asynchronous")
        if self.choices is not None and self.kind != "question":
            raise ValueError("only questions have choices")
        return self


class RelayAsyncAnswer(RelayGeneration):
    id: UUID
    interaction_ids: list[UUID] = Field(min_length=1, max_length=20)


class RelayInteractionClose(RelayGeneration):
    state: Literal["resolved", "cancelled"]


class InteractionView(BaseModel):
    id: UUID
    state: str
    answer: str | None = None
