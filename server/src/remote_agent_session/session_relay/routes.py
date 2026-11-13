from uuid import UUID

from fastapi import Depends, Header

from remote_agent_session.errors import DomainError
from remote_agent_session.session_relay.models import (
    InteractionView,
    OriginView,
    RelayAsyncAnswer,
    RelayAttach,
    RelayGeneration,
    RelayInteraction,
    RelayInteractionClose,
    RelayOrigin,
    RelayReadiness,
    RelayResult,
    RelaySummary,
    RelaySummaryClaim,
    RelaySummaryClaimView,
    RelayView,
)


def register(app, auth, service):
    def relay_auth(
        user=Depends(auth),
        runtime_protocol: str | None = Header(
            default=None, alias="X-Remote-Agent-Session-Relay-Runtime"
        ),
    ):
        return {**user, "relay_runtime_aware": runtime_protocol == "1"}

    @app.post("/v1/session-relay/async-answers", response_model=dict[str, bool])
    def async_answer(body: RelayAsyncAnswer, user=Depends(relay_auth)):
        return service().async_answer(user, body)

    @app.post("/v1/session-relay/origin", response_model=OriginView)
    def origin(body: RelayOrigin, user=Depends(relay_auth)):
        return service().origin(user, body)

    @app.post("/v1/session-relay/interactions", response_model=InteractionView)
    def interaction_create(body: RelayInteraction, user=Depends(relay_auth)):
        return service().open_interaction(user, body)

    @app.get("/v1/session-relay/interactions/{ident}", response_model=InteractionView)
    def interaction_read(ident: UUID, user=Depends(relay_auth)):
        return service().interaction(user, ident)

    @app.post("/v1/session-relay/interactions/{ident}/close", response_model=InteractionView)
    def interaction_close(ident: UUID, body: RelayInteractionClose, user=Depends(relay_auth)):
        return service().close_interaction(user, ident, body)

    @app.post("/v1/session-relay/attach", response_model=RelayView)
    def attach(body: RelayAttach, user=Depends(relay_auth)):
        if body.runtime_kind != "codex" and not user["relay_runtime_aware"]:
            raise DomainError(
                "SESSION_CLIENT_UPGRADE_REQUIRED",
                "请更新 Remote Agent Session CLI 后连接这个 Agent。",
                "user_action",
                409,
            )
        return service().attach(user, body)

    @app.get("/v1/session-relay", response_model=RelayView)
    def status(user=Depends(relay_auth)):
        return service().status(user)

    @app.post("/v1/session-relay/readiness", response_model=RelayView)
    def readiness(body: RelayReadiness, user=Depends(relay_auth)):
        return service().readiness(user, body)

    @app.post("/v1/session-relay/detach", response_model=RelayView)
    def detach(body: RelayGeneration, user=Depends(relay_auth)):
        return service().detach(user, body)

    @app.post("/v1/session-relay/poll", response_model=RelayView)
    def poll(user=Depends(relay_auth)):
        return service().poll(user)

    @app.post("/v1/session-relay/messages/{message_id}/dispatch", response_model=dict[str, bool])
    def dispatch(message_id: UUID, body: RelayGeneration, user=Depends(relay_auth)):
        return service().dispatch(user, message_id, body.generation)

    @app.post("/v1/session-relay/messages/{message_id}/working", response_model=dict[str, bool])
    def working(message_id: UUID, body: RelayGeneration, user=Depends(relay_auth)):
        return service().working(user, message_id, body.generation)

    @app.post("/v1/session-relay/messages/{message_id}/result", response_model=dict[str, bool])
    def result(message_id: UUID, body: RelayResult, user=Depends(relay_auth)):
        return service().result(user, message_id, body)

    @app.post("/v1/session-relay/summaries/claim", response_model=RelaySummaryClaimView)
    def summary_claim(body: RelaySummaryClaim, user=Depends(relay_auth)):
        return service().claim_summary(user, body)

    @app.post("/v1/session-relay/messages/{message_id}/summary", response_model=dict[str, bool])
    def summary(message_id: UUID, body: RelaySummary, user=Depends(relay_auth)):
        return service().summary(user, message_id, body)

    @app.get("/v1/session-relay/messages/{message_id}/cancellation", response_model=dict[str, bool])
    def cancellation(message_id: UUID, user=Depends(relay_auth)):
        return service().cancellation(user, message_id)

    @app.post("/v1/session-relay/messages/{message_id}/cancelled", response_model=dict[str, bool])
    def cancelled(message_id: UUID, user=Depends(relay_auth)):
        return service().cancelled(user, message_id)

    @app.post("/v1/session-relay/messages/{message_id}/waiting", response_model=dict[str, bool])
    def waiting(message_id: UUID, body: RelayGeneration, user=Depends(relay_auth)):
        return service().waiting(user, message_id, body.generation)
