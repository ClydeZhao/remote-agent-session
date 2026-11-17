import argparse
import base64
import ctypes
import hashlib
import http.server
import json
import os
from pathlib import Path
import queue
import secrets
import struct
import subprocess
import sys
import threading
import time
import traceback


def emit(name, **values):
    print(json.dumps({"check": name, **values}), flush=True)


def elevation():
    if os.name != "nt":
        return False
    from ctypes import wintypes
    api = ctypes.WinDLL("advapi32", use_last_error=True)
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.GetCurrentProcess.restype = wintypes.HANDLE
    api.OpenProcessToken.argtypes = [wintypes.HANDLE, wintypes.DWORD, ctypes.POINTER(wintypes.HANDLE)]
    token = wintypes.HANDLE()
    if not api.OpenProcessToken(kernel.GetCurrentProcess(), 8, ctypes.byref(token)):
        raise ctypes.WinError(ctypes.get_last_error())
    value, length = wintypes.DWORD(), wintypes.DWORD()
    api.GetTokenInformation.argtypes = [wintypes.HANDLE, ctypes.c_int, ctypes.c_void_p, wintypes.DWORD, ctypes.POINTER(wintypes.DWORD)]
    try:
        if not api.GetTokenInformation(token, 20, ctypes.byref(value), 4, ctypes.byref(length)):
            raise ctypes.WinError(ctypes.get_last_error())
        return bool(value.value)
    finally:
        kernel.CloseHandle(token)


def run_limited():
    from ctypes import wintypes
    import msvcrt
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    api = ctypes.WinDLL("advapi32", use_last_error=True)
    kernel.GetCurrentProcess.restype = wintypes.HANDLE
    api.OpenProcessToken.argtypes = [wintypes.HANDLE, wintypes.DWORD, ctypes.POINTER(wintypes.HANDLE)]
    api.CreateRestrictedToken.argtypes = [wintypes.HANDLE, wintypes.DWORD, wintypes.DWORD, ctypes.c_void_p, wintypes.DWORD, ctypes.c_void_p, wintypes.DWORD, ctypes.c_void_p, ctypes.POINTER(wintypes.HANDLE)]
    api.SetTokenInformation.argtypes = [wintypes.HANDLE, ctypes.c_int, ctypes.c_void_p, wintypes.DWORD]
    api.ConvertStringSidToSidW.argtypes = [wintypes.LPCWSTR, ctypes.POINTER(ctypes.c_void_p)]
    class Sid(ctypes.Structure):
        _fields_ = [("sid", ctypes.c_void_p), ("attributes", wintypes.DWORD)]
    class Startup(ctypes.Structure):
        _fields_ = [("cb", wintypes.DWORD), ("reserved", wintypes.LPWSTR), ("desktop", wintypes.LPWSTR), ("title", wintypes.LPWSTR), ("x", wintypes.DWORD), ("y", wintypes.DWORD), ("xs", wintypes.DWORD), ("ys", wintypes.DWORD), ("xc", wintypes.DWORD), ("yc", wintypes.DWORD), ("fill", wintypes.DWORD), ("flags", wintypes.DWORD), ("show", wintypes.WORD), ("size", wintypes.WORD), ("extra", ctypes.c_void_p), ("stdin", wintypes.HANDLE), ("stdout", wintypes.HANDLE), ("stderr", wintypes.HANDLE)]
    class Process(ctypes.Structure):
        _fields_ = [("process", wintypes.HANDLE), ("thread", wintypes.HANDLE), ("pid", wintypes.DWORD), ("tid", wintypes.DWORD)]
    api.CreateProcessAsUserW.argtypes = [wintypes.HANDLE, wintypes.LPCWSTR, wintypes.LPWSTR, ctypes.c_void_p, ctypes.c_void_p, wintypes.BOOL, wintypes.DWORD, ctypes.c_void_p, wintypes.LPCWSTR, ctypes.POINTER(Startup), ctypes.POINTER(Process)]
    token, limited = wintypes.HANDLE(), wintypes.HANDLE()
    if not api.OpenProcessToken(kernel.GetCurrentProcess(), 0xF01FF, ctypes.byref(token)):
        raise ctypes.WinError(ctypes.get_last_error())
    if not api.CreateRestrictedToken(token, 4, 0, None, 0, None, 0, None, ctypes.byref(limited)):
        raise ctypes.WinError(ctypes.get_last_error())
    length = wintypes.DWORD()
    api.GetTokenInformation.argtypes = [wintypes.HANDLE, ctypes.c_int, ctypes.c_void_p, wintypes.DWORD, ctypes.POINTER(wintypes.DWORD)]
    api.GetTokenInformation(token, 1, None, 0, ctypes.byref(length))
    user = ctypes.create_string_buffer(length.value)
    if not api.GetTokenInformation(token, 1, user, length, ctypes.byref(length)):
        raise ctypes.WinError(ctypes.get_last_error())
    user_sid = ctypes.cast(user, ctypes.POINTER(Sid)).contents.sid
    user_text = wintypes.LPWSTR()
    api.ConvertSidToStringSidW.argtypes = [ctypes.c_void_p, ctypes.POINTER(wintypes.LPWSTR)]
    if not api.ConvertSidToStringSidW(user_sid, ctypes.byref(user_text)):
        raise ctypes.WinError(ctypes.get_last_error())
    descriptor = ctypes.c_void_p()
    api.ConvertStringSecurityDescriptorToSecurityDescriptorW.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, ctypes.POINTER(ctypes.c_void_p), ctypes.c_void_p]
    if not api.ConvertStringSecurityDescriptorToSecurityDescriptorW("D:(A;;GA;;;" + user_text.value + ")(A;;GA;;;SY)", 1, ctypes.byref(descriptor), None):
        raise ctypes.WinError(ctypes.get_last_error())
    present, inherited, acl = wintypes.BOOL(), wintypes.BOOL(), ctypes.c_void_p()
    api.GetSecurityDescriptorDacl.argtypes = [ctypes.c_void_p, ctypes.POINTER(wintypes.BOOL), ctypes.POINTER(ctypes.c_void_p), ctypes.POINTER(wintypes.BOOL)]
    if not api.GetSecurityDescriptorDacl(descriptor, ctypes.byref(present), ctypes.byref(acl), ctypes.byref(inherited)):
        raise ctypes.WinError(ctypes.get_last_error())
    if not api.SetTokenInformation(limited, 6, ctypes.byref(acl), ctypes.sizeof(acl)):
        raise ctypes.WinError(ctypes.get_last_error())
    sid = ctypes.c_void_p()
    if not api.ConvertStringSidToSidW("S-1-16-8192", ctypes.byref(sid)):
        raise ctypes.WinError(ctypes.get_last_error())
    integrity = Sid(sid, 0x20)
    if not api.SetTokenInformation(limited, 25, ctypes.byref(integrity), ctypes.sizeof(integrity) + 12):
        raise ctypes.WinError(ctypes.get_last_error())
    startup, process = Startup(), Process()
    startup.cb, startup.flags = ctypes.sizeof(startup), 0x100
    handles = [msvcrt.get_osfhandle(stream.fileno()) for stream in (sys.stdin, sys.stdout, sys.stderr)]
    for handle in handles:
        os.set_handle_inheritable(handle, True)
    startup.stdin, startup.stdout, startup.stderr = handles
    command = ctypes.create_unicode_buffer(subprocess.list2cmdline([sys.executable, str(Path(__file__).resolve()), *[a for a in sys.argv[1:] if a != "--limited"]]))
    if not api.CreateProcessAsUserW(limited, sys.executable, command, None, None, True, 0, None, str(Path.cwd()), ctypes.byref(startup), ctypes.byref(process)):
        raise ctypes.WinError(ctypes.get_last_error())
    kernel.WaitForSingleObject.argtypes = [wintypes.HANDLE, wintypes.DWORD]
    kernel.WaitForSingleObject(process.process, 480000)
    code = wintypes.DWORD()
    kernel.GetExitCodeProcess.argtypes = [wintypes.HANDLE, ctypes.POINTER(wintypes.DWORD)]
    kernel.GetExitCodeProcess(process.process, ctypes.byref(code))
    for handle in (process.process, process.thread, token, limited):
        kernel.CloseHandle(handle)
    return code.value


class RPC:
    def __init__(self, binary, endpoint, env, log):
        self.process = subprocess.Popen([binary, "app-server", "proxy", "--sock", str(endpoint)], env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=log)
        self.lock, self.pending, self.events = threading.Lock(), {}, queue.Queue()
        self.next_id = 0
        key = base64.b64encode(secrets.token_bytes(16)).decode()
        self.process.stdin.write(("GET / HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + key + "\r\n\r\n").encode())
        self.process.stdin.flush()
        header = b""
        while not header.endswith(b"\r\n\r\n"):
            data = self.process.stdout.read(1)
            if not data:
                raise RuntimeError("proxy closed before WebSocket handshake")
            header += data
            if len(header) > 16384:
                raise RuntimeError("oversized WebSocket handshake")
        expected = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest())
        assert header.startswith(b"HTTP/1.1 101") and expected in header, header
        threading.Thread(target=self.read, daemon=True).start()
        self.call("initialize", {"clientInfo": {"name": "ras_windows_probe", "version": "0.1"}, "capabilities": {"experimentalApi": True}})
        self.send({"method": "initialized"})

    def exact(self, size):
        result = b""
        while len(result) < size:
            part = self.process.stdout.read(size - len(result))
            if not part:
                raise EOFError("proxy closed")
            result += part
        return result

    def frame(self, payload, opcode=1):
        size, mask = len(payload), secrets.token_bytes(4)
        header = bytes([0x80 | opcode, 0x80 | size]) if size < 126 else bytes([0x80 | opcode, 0xFE]) + struct.pack("!H", size) if size < 65536 else bytes([0x80 | opcode, 0xFF]) + struct.pack("!Q", size)
        with self.lock:
            self.process.stdin.write(header + mask + bytes(b ^ mask[i % 4] for i, b in enumerate(payload)))
            self.process.stdin.flush()

    def send(self, value):
        self.frame(json.dumps(value).encode())

    def read(self):
        try:
            fragments = b""
            while True:
                first, second = self.exact(2)
                size = second & 127
                if size == 126:
                    size = struct.unpack("!H", self.exact(2))[0]
                elif size == 127:
                    size = struct.unpack("!Q", self.exact(8))[0]
                assert size <= 16 * 1024 * 1024
                mask = self.exact(4) if second & 128 else None
                payload = self.exact(size)
                if mask:
                    payload = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
                opcode = first & 15
                if opcode == 8:
                    return
                if opcode == 9:
                    self.frame(payload, 10)
                    continue
                if opcode == 10:
                    continue
                fragments += payload
                if not first & 128:
                    continue
                message = json.loads(fragments)
                fragments = b""
                if "method" not in message and message.get("id") in self.pending:
                    self.pending[message["id"]].put(message)
                else:
                    self.events.put(message)
        except Exception as error:
            self.events.put({"readerError": str(error)})

    def call(self, method, params, timeout=30):
        self.next_id += 1
        number = self.next_id
        result = self.pending[number] = queue.Queue()
        self.send({"id": number, "method": method, "params": params})
        response = result.get(timeout=timeout)
        del self.pending[number]
        if "error" in response:
            raise RuntimeError(json.dumps(response["error"]))
        return response["result"]

    def event(self, method, timeout=30):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            value = self.events.get(timeout=max(0.1, deadline - time.monotonic()))
            if "readerError" in value:
                raise RuntimeError(value["readerError"])
            if value.get("method") == method:
                return value
        raise TimeoutError(method)

    def close(self):
        self.process.terminate()
        self.process.wait(timeout=10)


class Model(http.server.BaseHTTPRequestHandler):
    waiting = threading.Event()
    release = threading.Event()
    requests = []
    approval_call = None
    marker = None

    def log_message(self, *args):
        pass

    def do_POST(self):
        body = self.rfile.read(int(self.headers["Content-Length"]))
        if self.headers.get("Content-Encoding") == "zstd":
            import zstandard
            body = zstandard.ZstdDecompressor().decompress(body, max_output_size=16777216)
        value = json.loads(body)
        Model.requests.append(value)
        if len(Model.requests) > 20:
            raise RuntimeError("unexpected repeated model requests")
        users = [x for x in value.get("input", []) if x.get("role") == "user"]
        prompt = json.dumps(users[-1]) if users else ""
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        response_id = "resp_" + secrets.token_hex(8)
        self.sse("response.created", {"response": {"id": response_id, "object": "response", "status": "in_progress", "output": []}})
        if "RAS_WAIT" in prompt:
            Model.waiting.set()
            Model.release.wait(40)
            return
        last_input = value.get("input", [])[-1] if value.get("input") else {}
        if "RAS_APPROVAL" in prompt and not any(x.get("type") == "function_call_output" for x in value.get("input", [])):
            tools = value.get("tools", [])
            tool = next(t for t in tools if t.get("name") in ("exec_command", "shell_command", "shell"))
            name = tool["name"]
            arguments = {"sandbox_permissions": "require_escalated", "justification": "Synthetic probe: deny this one-time request."}
            command = 'echo RAS_UNEXPECTED > "' + str(Model.marker) + '"'
            if name == "exec_command":
                arguments["cmd"] = command
            elif name == "shell_command":
                arguments["command"] = command
            else:
                arguments["command"] = ["cmd.exe", "/c", command]
            output = {"id": "fc_" + response_id, "type": "function_call", "call_id": "call_" + response_id, "name": name, "arguments": json.dumps(arguments), "status": "completed"}
            Model.approval_call = output["call_id"]
        else:
            text = "RAS_DENIED" if "RAS_APPROVAL" in prompt else "RAS_RECONNECTED" if "RAS_RECONNECT" in prompt else "RAS_REMOTE" if "RAS_REMOTE" in prompt else "RAS_ORIGINAL"
            output = {"id": "msg_" + response_id, "type": "message", "role": "assistant", "status": "completed", "content": [{"type": "output_text", "text": text, "annotations": []}]}
        self.sse("response.output_item.added", {"output_index": 0, "item": output})
        self.sse("response.output_item.done", {"output_index": 0, "item": output})
        self.sse("response.completed", {"response": {"id": response_id, "object": "response", "status": "completed", "output": [output], "usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})

    def sse(self, kind, fields):
        try:
            self.wfile.write(("event: " + kind + "\ndata: " + json.dumps({"type": kind, **fields}) + "\n\n").encode())
            self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            pass


class ManagedOwner:
    def __init__(self, binary, env, log):
        self.binary, self.env, self.log = binary, env, log
        result = subprocess.run([binary, "app-server", "daemon", "start"], env=env, stdout=subprocess.PIPE, stderr=log, text=True, timeout=60)
        assert result.returncode == 0, "managed daemon start failed"
        output = json.loads(result.stdout)
        self.pid, self.endpoint = output["pid"], Path(output["socketPath"])
        emit("managed_daemon_start", passed=True, output=output)

    def poll(self):
        result = subprocess.run([self.binary, "app-server", "daemon", "version"], env=self.env, stdout=subprocess.PIPE, stderr=self.log, text=True, timeout=15)
        return None if result.returncode == 0 and json.loads(result.stdout).get("status") == "running" else 0

    def terminate(self):
        result = subprocess.run([self.binary, "app-server", "daemon", "stop"], env=self.env, stdout=subprocess.PIPE, stderr=self.log, text=True, timeout=30)
        assert result.returncode == 0, "managed daemon stop failed"
        emit("managed_daemon_stop", passed=True, output=json.loads(result.stdout))

    def wait(self, timeout):
        assert self.poll() is not None, "managed daemon remains running"

    def kill(self):
        self.terminate()


def check_turn(client, thread, turn_id, client_id, prompt, answer):
    history = client.call("thread/turns/list", {"threadId": thread, "itemsView": "full", "limit": 50})
    turn = next(x for x in history["data"] if x["id"] == turn_id)
    assert turn["status"] == "completed", turn
    users = [x for x in turn["items"] if x["type"] == "userMessage"]
    assert any(x.get("clientUserMessageId", x.get("clientId")) == client_id and prompt in json.dumps(x) for x in users), turn
    assert any(x["type"] == "agentMessage" and x.get("text") == answer for x in turn["items"]), turn
    return turn


def probe_tui(args, env, endpoint, work, client):
    from winpty import PtyProcess
    before = set(client.call("thread/loaded/list", {})["data"])
    command = [args.codex, "--remote", "unix://" + str(endpoint), "--no-alt-screen", "-C", str(work), "RAS_ORIGINAL_CLI"]
    terminal = PtyProcess.spawn(subprocess.list2cmdline(command), cwd=str(work), env=env, dimensions=(40, 140))
    output = []
    def read():
        try:
            while terminal.isalive():
                chunk = terminal.read(8192)
                output.append(chunk)
                if "\x1b[6n" in chunk:
                    terminal.write("\x1b[1;1R")
        except EOFError:
            pass
    threading.Thread(target=read, daemon=True).start()
    try:
        deadline, thread = time.monotonic() + 30, None
        while time.monotonic() < deadline:
            loaded = set(client.call("thread/loaded/list", {})["data"]) - before
            if loaded:
                assert len(loaded) == 1, loaded
                thread = loaded.pop()
                break
            time.sleep(0.2)
        assert thread, "CLI did not create a loaded thread: " + "".join(output)[-5000:]
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            turns = client.call("thread/turns/list", {"threadId": thread, "itemsView": "full", "limit": 50})["data"]
            if turns and any(x["type"] == "agentMessage" and x.get("text") == "RAS_ORIGINAL" for x in turns[0]["items"]):
                break
            time.sleep(0.2)
        else:
            raise AssertionError("CLI original prompt did not complete: " + "".join(output)[-5000:])
        assert terminal.isalive()
        remote = client.call("turn/start", {"threadId": thread, "clientUserMessageId": "probe-tui-remote", "input": [{"type": "text", "text": "RAS_REMOTE", "text_elements": []}]})
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            try:
                check_turn(client, thread, remote["turn"]["id"], "probe-tui-remote", "RAS_REMOTE", "RAS_REMOTE")
                if "RAS_REMOTE" in "".join(output):
                    break
            except (StopIteration, AssertionError):
                pass
            time.sleep(0.2)
        else:
            raise AssertionError("CLI did not observe the remote turn: " + "".join(output)[-5000:])
        assert terminal.isalive()
        emit("real_windows_cli_original_and_remote_input", passed=True, thread_id=thread, cli_pid=terminal.pid, explicit_remote=True)
    finally:
        terminal.close(force=True)
        (Path(args.state) / "tui.log").write_text("".join(output), encoding="utf-8")


def main(args):
    emit("environment", platform=sys.platform, elevated=elevation(), binary=args.codex)
    if args.limited and elevation():
        return run_limited()
    assert not elevation(), "probe must run without administrator elevation"
    root = Path(args.state).resolve()
    root.mkdir(parents=True, exist_ok=True)
    home, work = root / "home", root / "work"
    Model.marker = root / "denied-command-must-not-create.txt"
    assert not Model.marker.exists()
    home.mkdir(exist_ok=True)
    work.mkdir(exist_ok=True)
    model = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Model)
    threading.Thread(target=model.serve_forever, daemon=True).start()
    port = model.server_port
    (home / "config.toml").write_text(f'model = "probe-model"\nmodel_provider = "probe"\napproval_policy = "on-request"\nsandbox_mode = "read-only"\n[features]\nenable_request_compression = false\n[model_providers.probe]\nname = "Probe"\nbase_url = "http://127.0.0.1:{port}/v1"\nwire_api = "responses"\nrequires_openai_auth = false\n[analytics]\nenabled = false\n[projects.{json.dumps(str(work))}]\ntrust_level = "trusted"\n', encoding="utf-8")
    env = {k: v for k, v in os.environ.items() if not any(s in k.upper() for s in ("OPENAI", "TOKEN", "SECRET", "API_KEY"))}
    env["CODEX_HOME"] = str(home)
    log = (root / "codex.log").open("wb")
    owner, clients = None, []
    try:
        version = subprocess.check_output([args.codex, "--version"], env=env, text=True).strip()
        emit("binary", version=version)
        endpoint = home / "app-server-control" / "app-server-control.sock"
        if args.managed:
            owner = ManagedOwner(args.codex, env, log)
            endpoint = owner.endpoint
        else:
            owner = subprocess.Popen([args.codex, "app-server", "--listen", "unix://" + str(endpoint)], env=env, stdin=subprocess.DEVNULL, stdout=log, stderr=log)
        deadline = time.monotonic() + 25
        while time.monotonic() < deadline and not endpoint.exists() and owner.poll() is None:
            time.sleep(0.1)
        assert owner.poll() is None, (root / "codex.log").read_text(errors="replace")
        original = RPC(args.codex, endpoint, env, log)
        clients.append(original)
        emit("proxy_websocket_initialize", passed=True, owner_pid=owner.pid)
        created = original.call("thread/start", {"cwd": str(work), "approvalPolicy": "on-request", "sandbox": "read-only", "baseInstructions": "Isolated native transport probe.", "experimentalRawEvents": False})
        thread = created["thread"]["id"]
        first = original.call("turn/start", {"threadId": thread, "clientUserMessageId": "probe-original", "input": [{"type": "text", "text": "RAS_ORIGINAL", "text_elements": []}]})
        completed = original.event("turn/completed")
        assert completed["params"]["turn"]["status"] == "completed", completed
        emit("original_session_created", passed=True, thread_id=thread, turn_id=first["turn"]["id"])
        relay = RPC(args.codex, endpoint, env, log)
        clients.append(relay)
        loaded = relay.call("thread/loaded/list", {})
        assert thread in loaded["data"], loaded
        inspected = relay.call("thread/read", {"threadId": thread, "includeTurns": False})
        assert inspected["thread"]["status"]["type"] == "idle", inspected
        emit("same_loaded_original", passed=True, owner_pid_unchanged=owner.poll() is None, thread_id=thread)
        remote = relay.call("turn/start", {"threadId": thread, "clientUserMessageId": "probe-remote", "input": [{"type": "text", "text": "RAS_REMOTE", "text_elements": []}]})
        complete = original.event("turn/completed")
        assert complete["params"]["turn"]["id"] == remote["turn"]["id"], complete
        assert complete["params"]["turn"]["status"] == "completed", complete
        check_turn(relay, thread, first["turn"]["id"], "probe-original", "RAS_ORIGINAL", "RAS_ORIGINAL")
        check_turn(relay, thread, remote["turn"]["id"], "probe-remote", "RAS_REMOTE", "RAS_REMOTE")
        emit("remote_input_original_receipt_and_history", passed=True)
        waiting = relay.call("turn/start", {"threadId": thread, "clientUserMessageId": "probe-wait", "input": [{"type": "text", "text": "RAS_WAIT", "text_elements": []}]})
        assert Model.waiting.wait(20), "model never entered waiting turn"
        relay.call("turn/interrupt", {"threadId": thread, "turnId": waiting["turn"]["id"]})
        interrupted = original.event("turn/completed")
        assert interrupted["params"]["turn"]["status"] == "interrupted", interrupted
        assert interrupted["params"]["turn"]["id"] == waiting["turn"]["id"], interrupted
        emit("interrupt_same_original_turn", passed=True)
        approval = relay.call("turn/start", {"threadId": thread, "clientUserMessageId": "probe-approval", "input": [{"type": "text", "text": "RAS_APPROVAL", "text_elements": []}]})
        request = original.event("item/commandExecution/requestApproval")
        assert request["params"]["threadId"] == thread
        assert request["params"]["turnId"] == approval["turn"]["id"]
        original.send({"id": request["id"], "result": {"decision": "decline"}})
        done = original.event("turn/completed")
        assert done["params"]["turn"]["status"] == "completed", done
        assert done["params"]["turn"]["id"] == approval["turn"]["id"], done
        assert any(i.get("type") == "function_call_output" and i.get("call_id") == Model.approval_call and "rejected by user" in i.get("output", "").lower() for request in Model.requests for i in request.get("input", []))
        assert not Model.marker.exists(), "denied command executed"
        emit("native_one_time_approval_denied", passed=True, decision_channel="original client")
        relay.close()
        clients.remove(relay)
        reconnected = RPC(args.codex, endpoint, env, log)
        clients.append(reconnected)
        assert thread in reconnected.call("thread/loaded/list", {})["data"]
        continued = reconnected.call("turn/start", {"threadId": thread, "clientUserMessageId": "probe-reconnect", "input": [{"type": "text", "text": "RAS_RECONNECT", "text_elements": []}]})
        done = original.event("turn/completed")
        assert done["params"]["turn"]["id"] == continued["turn"]["id"]
        check_turn(reconnected, thread, continued["turn"]["id"], "probe-reconnect", "RAS_RECONNECT", "RAS_RECONNECTED")
        emit("relay_reconnect_keeps_original", passed=True, owner_pid=owner.pid)
        if args.tui:
            probe_tui(args, env, endpoint, work, reconnected)
        emit("result", passed=True, model="local fixture", desktop_tested=False, feishu_tested=False)
        return 0
    finally:
        Model.release.set()
        (root / "model-requests.json").write_text(json.dumps(Model.requests), encoding="utf-8")
        for client in clients:
            client.close()
        if owner is not None and owner.poll() is None:
            owner.terminate()
            try:
                owner.wait(timeout=5)
            except subprocess.TimeoutExpired:
                owner.kill()
                owner.wait(timeout=5)
        model.shutdown()
        log.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--codex", required=True)
    parser.add_argument("--state", required=True)
    parser.add_argument("--limited", action="store_true")
    parser.add_argument("--managed", action="store_true")
    parser.add_argument("--tui", action="store_true")
    args = parser.parse_args()
    try:
        sys.exit(main(args))
    except Exception:
        traceback.print_exc()
        log = Path(args.state) / "codex.log"
        if log.exists():
            print(log.read_text(errors="replace")[-16000:])
        sys.exit(1)
