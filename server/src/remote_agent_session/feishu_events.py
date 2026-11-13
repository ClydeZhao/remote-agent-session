import json
import logging
import os
import threading

import lark_oapi as lark
import psycopg

from remote_agent_session.application import Application
from remote_agent_session.identity.feishu import FeishuPairing
from remote_agent_session.settings import Settings


def main():
    settings = Settings.load()
    if settings.mode != "live":
        raise RuntimeError("Live configuration required")
    app = Application(settings)
    pairing = FeishuPairing(app.identity, app.feishu, require_event_time=True)

    def receive(event):
        try:
            data = json.loads(lark.JSON.marshal(event))
            if data.get("header", {}).get("event_type", "").startswith("im.message.reaction."):
                answered = app.session_relay.receive_reaction(data, app.feishu)
                event_data = data.get("event", {})
                if event_data.get("operator_type") == "user" and event_data.get(
                    "reaction_type", {}
                ).get("emoji_type") in ("CheckMark", "CrossMark"):
                    print(
                        "Permission reaction event: " + ("answered" if answered else "observed"),
                        flush=True,
                    )
            elif not pairing.receive(data):
                app.session_relay.receive(data, app.feishu.app_id)
        except Exception:
            logging.error("Pairing event processing failed; no message content logged")
            raise RuntimeError("Pairing processing unavailable") from None

    handler = (
        lark.EventDispatcherHandler.builder("", "")
        .register_p2_im_message_receive_v1(receive)
        .register_p2_im_message_reaction_created_v1(receive)
        .register_p2_im_message_reaction_deleted_v1(receive)
        .build()
    )
    with psycopg.connect(settings.database_url, autocommit=True) as lock:
        if not lock.execute("SELECT pg_try_advisory_lock(781342102)").fetchone()[0]:
            raise RuntimeError("Another event receiver owns this database")

        def watch_lock():
            while True:
                threading.Event().wait(3)
                try:
                    lock.execute("SELECT 1")
                except Exception:
                    logging.error("Event receiver lost database ownership; exiting")
                    os._exit(70)

        threading.Thread(target=watch_lock, daemon=True).start()
        client = lark.ws.Client(
            settings.feishu_app_id,
            settings.feishu_secret,
            event_handler=handler,
            log_level=lark.LogLevel.ERROR,
        )
        logging.getLogger("Lark").disabled = True
        print("Feishu event receiver starting (private messages only)", flush=True)
        client.start()


if __name__ == "__main__":
    main()
