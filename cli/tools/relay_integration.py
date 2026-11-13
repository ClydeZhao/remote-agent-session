import argparse
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from uuid import uuid4
from urllib.parse import quote
from remote_agent_session.identity.models import VerifiedPairingEvent

import httpx

from remote_agent_session.application import Application
from remote_agent_session.settings import Settings

ROOT = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description="Installed CLI/API/PostgreSQL relay regression with host and Feishu boundaries substituted")
parser.add_argument("--install-root", type=Path, required=True)
args = parser.parse_args()
INSTALL = args.install_root.resolve()
CLI = INSTALL / "bin/remote-agent-session"
if not CLI.is_file() or not (INSTALL / "tools/lifecycle.py").is_file():
    raise SystemExit("Complete isolated installation required")



def main():
    with tempfile.TemporaryDirectory(prefix="ras-e2e-", dir="/tmp") as directory:
        root = Path(directory).resolve()
        service = root / "service"
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        url = f"http://127.0.0.1:{port}"
        state = {"database_url": "postgresql://" + quote(os.environ.get("USER", ""),safe="") + "@/remote_agent_session?host=" + quote(str(service / "pgsocket"),safe=""), "key": str(service / "encryption.key")}

        cli_path = root / "remote-agent-session"
        shutil.copy2(CLI, cli_path)
        user = root / "user"
        test_home = root / "home"
        test_home.mkdir(mode=0o700)
        codex_home = root / "codex-home"
        codex_home.mkdir(mode=0o700)
        slow_cancel_thread = str(uuid4())
        tooling_env = {name:os.environ.get(name) or subprocess.check_output(["go","env",name],text=True).strip() for name in ("GOMODCACHE","GOCACHE")}
        env = {
            **tooling_env,
            **os.environ,
            "HOME": str(test_home),
            "DSH_HOME": str(test_home / ".dsh"),
            "PATH": str(ROOT / "fixtures/session-relay")
            + os.pathsep
            + str(INSTALL / ".venv/bin")
            + os.pathsep
            + os.environ["PATH"],
            "CODEX_HOME": str(codex_home),
            "REMOTE_AGENT_SESSION_TEST_CODEX_STORE": str(root / "codex.json"),
            "REMOTE_AGENT_SESSION_TEST_CODEX_SLOW_CANCEL_THREAD": slow_cancel_thread,
            "REMOTE_AGENT_SESSION_HOME": str(INSTALL),
            "REMOTE_AGENT_SESSION_RUNTIME_DIR": str(root / "runtime"),
            "REMOTE_AGENT_SESSION_SERVICE_STATE_DIR": str(service),
            "REMOTE_AGENT_SESSION_TEST_HOOK_COMMAND": "'"
            + str(cli_path)
            + "' --state-dir '"
            + str(user)
            + "' session hook",
        }
        env.pop("REMOTE_AGENT_SESSION_CODEX_SOCKET", None)
        env.pop("DSH_SESSION_ID", None)
        proxy = None
        server = None
        started = False

        def driver(action, *args):
            if action == "migrate":
                return
            if action == "pair":
                values = dict(zip(args[::2], args[1::2]))
                identity = Application(Settings(state["database_url"], Path(state["key"]).read_bytes().strip())).identity
                event = dict(app="offline-test-app",tenant="offline-test-tenant",subject=values["--subject"],private_chat=True,user_code=values["--code"])
                challenge = identity.pairing_event(VerifiedPairingEvent(event_id=str(uuid4()),**event))
                identity.pairing_event(VerifiedPairingEvent(event_id=str(uuid4()),confirmation=challenge,**event))
                return
            command = [str(CLI), "--state-dir", str(user), action, "--service-state-dir", str(service), "--mode", "offline", "--port", str(port)]
            result = subprocess.run(command,env=env,capture_output=True,timeout=120)
            if result.returncode:
                raise RuntimeError("Installed lifecycle failed: " + action + " " + result.stderr.decode()[:200])

        def cli(*args, expected=0):
            p = subprocess.run(
                [str(cli_path), "--json", "--state-dir", str(user), *args],
                capture_output=True,
                text=True,
                env=env,
                timeout=40,
            )
            assert p.returncode == expected, (
                "CLI failed: " + args[0] + " " + p.stdout[:300]
            )
            return json.loads(p.stdout).get("data")

        def until(predicate, timeout=20):
            deadline = time.monotonic() + timeout
            while time.monotonic() < deadline:
                if predicate():
                    return
                time.sleep(0.2)
            raise AssertionError("Timed out waiting for relay behavior")

        try:
            started = True
            driver("start")

            def ready():
                try:
                    return (
                        httpx.get(
                            url + "/v1/me", trust_env=False, timeout=0.3
                        ).status_code
                        == 401
                    )
                except httpx.HTTPError:
                    return False

            until(ready)
            backend_url = url
            dropped = set()
            faults = set()
            fault_lock = threading.Lock()
            pause_claims = threading.Event()
            class Proxy(BaseHTTPRequestHandler):
                def log_message(self, *args):
                    pass
                def do_GET(self):
                    self.forward()
                def do_POST(self):
                    self.forward()
                def do_DELETE(self):
                    self.forward()
                def forward(self):
                    body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
                    headers = {key:value for key,value in self.headers.items() if key.lower() not in ("host","content-length","connection")}
                    if self.path.endswith("/summaries/claim") and pause_claims.is_set():
                        self.send_response(503)
                        self.send_header("Content-Type","application/json")
                        self.end_headers()
                        self.wfile.write(b'{"code":"SUMMARY_PAUSED","message":"test fault","retry_action":"same_request"}')
                        return
                    response = httpx.request(self.command, backend_url+self.path, content=body, headers=headers, timeout=30, trust_env=False)
                    fault = "result" if self.path.endswith("/result") and json.loads(body).get("summary_pending") else "claim" if self.path.endswith("/summaries/claim") and response.status_code==200 and response.json().get("job") else "summary" if self.path.endswith("/summary") else None
                    with fault_lock:
                        lose = fault in faults and fault not in dropped
                        if lose:
                            dropped.add(fault)
                    if lose:
                        self.connection.shutdown(socket.SHUT_RDWR)
                        self.connection.close()
                        return
                    self.send_response(response.status_code)
                    self.send_header("Content-Type",response.headers.get("Content-Type","application/json"))
                    self.send_header("Content-Length",str(len(response.content)))
                    self.end_headers()
                    self.wfile.write(response.content)
            proxy = ThreadingHTTPServer(("127.0.0.1",0),Proxy)
            threading.Thread(target=proxy.serve_forever,daemon=True).start()
            url = f"http://127.0.0.1:{proxy.server_port}"
            cli("config", "set", "--server", url, "--allow-loopback-http")
            code = cli("login")["user_code"]
            driver("pair", "--code", code, "--subject", "relay-test-user")
            cli("login", "finish")
            a = Application(
                Settings(state["database_url"], Path(state["key"]).read_bytes().strip())
            )
            ident = str(uuid4())
            value = cli("session", "attach", "--session", ident)
            assert value["native_session"] == ident and value["status"] == "ready"
            assert not (root / "codex.json").exists()

            def incoming(text, event_id=None, parent_id=None):
                a.session_relay.receive(
                    {
                        "header": {
                            "app_id": "offline-test-app",
                            "tenant_key": "offline-test-tenant",
                            "event_type": "im.message.receive_v1",
                            "event_id": event_id or str(uuid4()),
                        },
                        "event": {
                            "sender": {
                                "sender_type": "user",
                                "tenant_key": "offline-test-tenant",
                                "sender_id": {"open_id": "relay-test-user"},
                            },
                            "message": {
                                "message_type": "text",
                                "chat_type": "p2p",
                                "content": json.dumps({"text": text}),
                                "parent_id": parent_id,
                            },
                        },
                    },
                    "offline-test-app",
                )

            def messages():
                with a.db.transaction() as c:
                    return c.execute(
                        "SELECT * FROM notifications.session_deliveries ORDER BY created_at,message_key"
                    ).fetchall()

            event_id = str(uuid4())
            incoming("普通飞书问题", event_id)
            incoming("普通飞书问题", event_id)
            until(
                lambda: any(
                    "原会话回复：普通飞书问题" in x["content"] for x in messages()
                )
            )
            assert (
                sum("原会话回复：普通飞书问题" in x["content"] for x in messages()) == 1
            )
            faults.update({"result","claim","summary"})
            incoming("长回答摘要贯穿验证")
            until(lambda: (root / "summary-started").exists())
            incoming("摘要等待期间继续")
            until(lambda: any("原会话回复：摘要等待期间继续" in row["content"] for row in messages()), timeout=8)
            assert not any("TL;DR" in row["content"] for row in messages())
            (root / "release-summary").touch()
            until(lambda: any("TL;DR" in row["content"] for row in messages()))
            until(lambda: dropped == {"result","claim","summary"})
            with a.db.transaction() as c:
                summary = c.execute("SELECT j.*, m.event_id FROM session_relay.summary_jobs j JOIN session_relay.messages m ON m.id=j.message_id").fetchone()
                assert summary["state"] == "completed"
                assert c.execute("SELECT count(*) AS count FROM session_relay.summary_claims WHERE message_id=%s",(summary["message_id"],)).fetchone()["count"] == 1
            assert (root / "summary-calls").read_text().splitlines() == ["model-call"]
            assert sum("TL;DR" in row["content"] for row in messages()) == 1
            assert sum("长回答正文" in row["content"] for row in messages()) == 1
            def stop_bridge():
                rows = subprocess.run(["ps","-axo","pid=,args="],capture_output=True,text=True).stdout.splitlines()
                targets = [int(row.strip().split()[0]) for row in rows if str(user) in row and "session _serve" in row]
                assert targets
                for pid in targets:
                    os.kill(pid,15)
                until(lambda: all(not Path(f"/proc/{pid}").exists() for pid in targets),timeout=2) if sys.platform.startswith("linux") else time.sleep(0.5)
            pause_claims.set()
            incoming("长回答摘要贯穿验证")
            def pending_summary():
                with a.db.transaction() as c:
                    return c.execute("SELECT count(*) AS count FROM session_relay.summary_jobs WHERE state='pending'").fetchone()["count"] == 1
            until(pending_summary)
            stop_bridge()
            pause_claims.clear()
            cli("session","attach","--session",ident)
            until(lambda: sum("TL;DR" in row["content"] for row in messages()) == 2)
            assert (root / "summary-calls").read_text().splitlines() == ["model-call"]*2
            (root / "summary-started").unlink()
            (root / "release-summary").unlink()
            incoming("长回答摘要贯穿验证")
            until(lambda: (root / "summary-started").exists())
            stop_bridge()
            with a.db.transaction() as c:
                c.execute("UPDATE session_relay.summary_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE state='claimed'")
            cli("session","attach","--session",ident)
            until(lambda: any("TL;DR 暂不可用" in row["content"] for row in messages()))
            with a.db.transaction() as c:
                assert c.execute("SELECT count(*) AS count FROM session_relay.summary_jobs WHERE state='expired'").fetchone()["count"] == 1
            assert (root / "summary-calls").read_text().splitlines() == ["model-call"]*3
            (root / "release-summary").touch()
            incoming("异步问答贯穿验证")

            def native_state():
                return json.loads((root / "codex.json").read_text())

            until(lambda: native_state().get("independent_work_done"))
            assert native_state()["async_hook_returned"]
            async_turn = native_state()["turns"][0]["id"]
            assert native_state()["turns"][0]["status"] == "inProgress"

            class Transport:
                def send_card(self, subject, content, key):
                    return {"message_id": "om_" + key}

                def send_post(self, subject, content, key):
                    return {"message_id": "om_" + key}

            def question_delivery():
                a.session_notifications.once(Transport())
                return next(
                    (
                        row
                        for row in messages()
                        if row["interaction_id"]
                        and row["state"] == "accepted"
                        and "选择颜色" in row["content"]
                    ),
                    None,
                )

            until(question_delivery)
            card = question_delivery()
            assert native_state()["turns"][0]["status"] == "inProgress"
            answer_event = str(uuid4())
            incoming("绿色", answer_event)
            incoming("绿色", answer_event)
            until(lambda: native_state().get("steer_count") == 1)
            until(
                lambda: any(
                    "异步回答已关联：绿色" in row["content"] for row in messages()
                )
            )
            assert native_state()["steer_count"] == 1
            assert native_state()["turns"][0]["id"] == async_turn
            assert native_state()["turns"][0]["status"] == "completed"
            assert not (root / "unexpected-interrupt").exists()
            incoming("B", parent_id=card["receipt"])
            until(
                lambda: any(
                    "这个问题已结束或已回答" in row["content"] for row in messages()
                )
            )
            assert native_state()["steer_count"] == 1
            assert native_state()["turns"][0]["id"] == async_turn

            previous_cards = {row["id"] for row in messages() if row["interaction_id"]}
            incoming("多问题异步贯穿验证")

            def multiple_cards():
                a.session_notifications.once(Transport())
                return [
                    row
                    for row in messages()
                    if row["interaction_id"]
                    and row["state"] == "accepted"
                    and row["id"] not in previous_cards
                ]

            until(lambda: len(multiple_cards()) == 2)
            cards = multiple_cards()
            assert native_state()["independent_work_done"]
            assert native_state()["turns"][0]["status"] == "inProgress"
            for card in cards:
                answer = "绿色" if "选择颜色" in card["content"] else "快速"
                incoming(answer, parent_id=card["receipt"])
            until(lambda: native_state().get("steer_count") == 2)
            until(
                lambda: (
                    sum("异步回答已关联：绿色" in row["content"] for row in messages())
                    == 2
                )
            )
            assert not (root / "unexpected-interrupt").exists()

            original_owner = json.loads((user / "relay-owner.json").read_text())
            updated_binary = root / "remote-agent-session-next"
            subprocess.run(
                [
                    "go",
                    "build",
                    "-ldflags=-X github.com/ClydeZhao/remote-agent-session/cli/internal/buildinfo.Revision=offline-relay-upgrade",
                    "-o",
                    str(updated_binary),
                    "./cmd/remote-agent-session",
                ],
                cwd=ROOT / "cli",
                check=True,
                capture_output=True,
                env=env,
            )
            old_work = cli("session", "status")
            with a.db.transaction() as c:
                completed_id = c.execute(
                    "SELECT id FROM session_relay.messages WHERE event_id=%s",
                    (event_id,),
                ).fetchone()["id"]
            handoff_path = user / "relay-handoff.json"
            handoff = {
                "runtime": original_owner.split(":")[1],
                "server": url,
                "user_id": value["user_id"],
                "native_session": ident,
            }
            unresolved = {**handoff, "client_id": str(uuid4())}
            handoff_path.write_text(
                json.dumps([unresolved, {**handoff, "client_id": str(completed_id)}])
            )
            handoff_path.chmod(0o600)
            updated_binary.replace(cli_path)
            until(
                lambda: (
                    json.loads((user / "relay-owner.json").read_text())
                    != original_owner
                )
            )
            until(lambda: len(json.loads(handoff_path.read_text())) == 1)
            assert (
                json.loads(handoff_path.read_text())[0]["client_id"]
                == unresolved["client_id"]
            )
            incoming("更新后继续")
            until(
                lambda: any(
                    "原会话回复：更新后继续" in x["content"] for x in messages()
                )
            )
            new_work = cli("session", "status")
            assert new_work["generation"] == old_work["generation"]
            assert new_work["native_session"] == old_work["native_session"]
            assert new_work["status"] == "ready"

            slow_cancel_id = uuid4()
            with a.db.transaction() as c:
                c.execute(
                    """INSERT INTO session_relay.messages
                    (id,event_id,user_id,generation,kind,content,state,device_id,native_session)
                    SELECT %s,%s,user_id,generation,kind,content,'cancel_requested',device_id,%s
                    FROM session_relay.messages WHERE id=%s""",
                    (slow_cancel_id, str(uuid4()), slow_cancel_thread, completed_id),
                )
            until(lambda: (root / "slow-cancel-started").exists())
            incoming("撤回等待期间继续")
            until(
                lambda: any(
                    "原会话回复：撤回等待期间继续" in x["content"] for x in messages()
                ),
                timeout=6,
            )
            with a.db.transaction() as c:
                assert (
                    c.execute(
                        "SELECT state FROM session_relay.messages WHERE id=%s",
                        (slow_cancel_id,),
                    ).fetchone()["state"]
                    == "cancel_requested"
                )

            unavailable = root / "history-unavailable"
            unavailable.touch()
            for index in range(5):
                incoming(f"历史故障 {index}")
            incoming("超出在途上限")
            until(
                lambda: any("已有多条消息等待处理" in x["content"] for x in messages())
            )
            until(
                lambda: (
                    sum("无法确认消息执行结果" in x["content"] for x in messages()) == 5
                ),
                timeout=50,
            )
            unavailable.unlink()
            incoming("历史恢复后继续")
            until(
                lambda: any(
                    "原会话回复：历史恢复后继续" in x["content"] for x in messages()
                )
            )

            incoming("mixed")
            until(lambda: any("本地输入介入" in x["content"] for x in messages()))
            assert not any("原会话回复：mixed" in x["content"] for x in messages())
            detached = cli("session", "detach")
            assert detached["status"] == "detached"
            incoming("不该执行")
            until(
                lambda: any(
                    "本条消息未交给 Agent。已断开飞书对话" in x["content"]
                    for x in messages()
                )
            )
            incoming("/session resume")
            until(lambda: cli("session", "status")["status"] == "ready")
            assert cli("session", "status")["native_session"] == ident
            incoming("/session detach")
            until(lambda: cli("session", "status")["status"] == "detached")
            next_login = cli("login")["user_code"]
            driver("pair", "--code", next_login, "--subject", "relay-test-user")
            cli("login", "finish")
            cli("session", "attach", "--session", ident)
            until(lambda: cli("session", "status")["status"] == "ready")
            cli("logout")
            print(
                "PASS: real CLI/API/PostgreSQL attach, source isolation, deduplication, bidirectional detach and resume, bridge replacement, nonblocking handoff, native asynchronous question continuation, durable TLDR lost-ACK/restart recovery and history failure recovery; external Codex and Feishu substituted"
            )
        except BaseException:
            log_file = service / "service.log"
            if log_file.exists():
                descriptor,failure_path = tempfile.mkstemp(prefix="ras-integration-failure-",suffix=".log")
                with os.fdopen(descriptor,"wb") as stream:
                    stream.write(log_file.read_bytes()[-32768:])
                print("Bounded isolated service diagnostics: " + failure_path, file=sys.stderr)
            raise
        finally:
            if proxy:
                proxy.shutdown()
                proxy.server_close()
            if server:
                server.terminate()
                server.wait(timeout=10)
            processes = subprocess.run(
                ["ps", "-axo", "pid=,args="], capture_output=True, text=True
            ).stdout
            for line in processes.splitlines():
                if str(user) in line and "session _serve" in line:
                    os.kill(int(line.strip().split()[0]), 15)
            if started:
                driver("stop")
                shutil.rmtree(service)
                driver("service-forget")


if __name__ == "__main__":
    main()
