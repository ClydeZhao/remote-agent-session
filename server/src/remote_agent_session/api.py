import os
from typing import Annotated
from uuid import UUID

import psycopg
from fastapi import Depends, FastAPI, Request, Response
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from remote_agent_session.application import Application
from remote_agent_session.buildinfo import headers as build_headers
from remote_agent_session.errors import DomainError, ErrorBody
from remote_agent_session.identity.models import (
    DeviceClaim,
    DeviceCreated,
    DeviceStart,
    DeviceToken,
    UserView,
)
from remote_agent_session.settings import Settings

VERSION = "0.1.0"
bearer = HTTPBearer(auto_error=False)


def create_app(settings: Settings | None = None, *, schema_only: bool = False):
    instance_id = os.environ.get("REMOTE_AGENT_SESSION_INSTANCE_ID", "")
    app = FastAPI(
        title="Remote Agent Session API",
        version=VERSION,
        description="Explicit offline or live service. Inspect simulated flags on results. Bearer device authentication; Idempotency-Key is scoped to the authenticated user.",
        responses={n: {"model": ErrorBody} for n in (401, 403, 404, 409, 422, 429, 503, 504)},
    )

    @app.middleware("http")
    async def identify_server(request, call_next):
        response = await call_next(request)
        response.headers.update(build_headers())
        return response

    if not schema_only:
        settings = settings or Settings.load()
        application = Application(settings)
        identity = application.identity
        app.state.identity = identity
        app.state.application = application

    @app.exception_handler(DomainError)
    async def domain_error(request, exc):
        return JSONResponse(status_code=exc.status, content=exc.body.model_dump(mode="json"))

    @app.exception_handler(psycopg.Error)
    async def database_error(request, exc):
        return JSONResponse(
            status_code=503,
            content=ErrorBody(
                code="STORAGE_UNAVAILABLE",
                message="Storage unavailable; retry the same request key.",
                retry_action="same_request",
                retry_after=5,
            ).model_dump(),
        )

    @app.exception_handler(RequestValidationError)
    async def validation_error(request, exc):
        return JSONResponse(
            status_code=422,
            content=ErrorBody(
                code="VALIDATION_ERROR",
                message="Request does not match the API schema.",
                retry_action="user_action",
            ).model_dump(),
        )

    def auth(credentials: Annotated[HTTPAuthorizationCredentials | None, Depends(bearer)]):
        if credentials is None:
            raise DomainError("AUTH_REQUIRED", "Bearer login required.", "user_action", 401)
        return identity.authenticate(credentials.credentials)

    from remote_agent_session.session_relay.routes import register as register_relay

    register_relay(app, auth, lambda: application.session_relay)

    @app.post("/v1/auth/devices", response_model=DeviceCreated, status_code=201)
    def start(body: DeviceStart, request: Request):
        identity.rate_limit("start:" + (request.client.host if request.client else "unknown"), 30)
        return identity.start(body.device_name)

    @app.post("/v1/auth/devices/{device_id}/token", response_model=DeviceToken)
    def claim(device_id: UUID, body: DeviceClaim):
        return identity.claim(device_id, body.claim_secret.get_secret_value())

    @app.get("/v1/me", response_model=UserView)
    def me(user=Depends(auth)):
        return user

    @app.delete("/v1/auth/session", status_code=204)
    def logout(user=Depends(auth)):
        application.session_relay.revoke(user)
        identity.logout(user["session_id"])
        return Response(status_code=204)

    @app.get("/health", include_in_schema=False)
    def health():
        with application.db.transaction() as c:
            c.execute("SELECT 1")
        return {"status": "ok", "instance_id": instance_id}

    return app
