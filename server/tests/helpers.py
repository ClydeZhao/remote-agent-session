from uuid import uuid4

from remote_agent_session.identity.models import VerifiedPairingEvent


def login(client, app, subject="alice"):
    device = client.post("/v1/auth/devices", json={"device_name": "test"}).json()
    event = dict(
        app="offline-test-app",
        tenant="offline-test-tenant",
        subject=subject,
        private_chat=True,
        user_code=device["user_code"],
    )
    challenge = app.state.identity.pairing_event(
        VerifiedPairingEvent(event_id=str(uuid4()), **event)
    )
    app.state.identity.pairing_event(
        VerifiedPairingEvent(event_id=str(uuid4()), confirmation=challenge, **event)
    )
    result = client.post(
        f"/v1/auth/devices/{device['device_id']}/token",
        json={"claim_secret": device["claim_secret"]},
    )
    token = result.json()["access_token"]
    return {"Authorization": f"Bearer {token}"}
