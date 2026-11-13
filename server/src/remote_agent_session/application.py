from remote_agent_session.db import Database
from remote_agent_session.identity.service import Identity
from remote_agent_session.notifications.session import SessionNotifications
from remote_agent_session.session_relay.service import SessionRelay


class Application:
    def __init__(self, settings):
        self.simulated = settings.mode == "offline"
        self.db = Database(settings.database_url)
        self.db.bind_mode(settings.mode)
        if not self.simulated:
            from remote_agent_session.feishu import Feishu

            self.feishu = Feishu(settings.feishu_app_id, settings.feishu_secret)
        self.identity = Identity(
            self.db,
            settings.key,
            "offline-test-app" if self.simulated else settings.feishu_app_id,
            self.simulated,
        )
        self.session_notifications = SessionNotifications(self.db, self.identity)
        self.session_relay = SessionRelay(
            self.db,
            self.session_notifications,
            None if self.simulated else self.feishu.quote_message,
            require_event_time=not self.simulated,
        )

    def session_notify_once(self):
        expired = self.session_relay.expire()
        if self.simulated:
            return expired
        delivered = self.session_notifications.once(self.feishu)
        activity = self.session_notifications.activity_once(self.feishu)
        reaction = self.session_relay.permission_reactions_once(self.feishu)
        return delivered or activity or reaction or expired
