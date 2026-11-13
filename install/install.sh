#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DEST=${REMOTE_AGENT_SESSION_INSTALL_DIR:-"$HOME/.local/share/remote-agent-session"}
PYTHON=${REMOTE_AGENT_SESSION_PYTHON:-python3}
umask 077
if [ -e "$DEST" ]; then
    echo "Installation path already exists. Choose a new REMOTE_AGENT_SESSION_INSTALL_DIR; stop the old service before switching." >&2
    exit 1
fi
command -v "$PYTHON" >/dev/null
"$PYTHON" -c 'import sys; assert sys.version_info >= (3,11), "Python 3.11+ required"'
mkdir -p "$DEST"
DEST=$(CDPATH= cd -- "$DEST" && pwd)
cleanup() {
    if [ ! -f "$DEST/.installation-complete" ]; then
        rm -rf -- "$DEST"
    fi
}
trap cleanup EXIT HUP INT TERM
for part in server tools integrations skills docs config install third_party share; do
    if [ -d "$ROOT/$part" ]; then
        "$PYTHON" -c 'import shutil,sys; shutil.copytree(sys.argv[1],sys.argv[2],ignore=shutil.ignore_patterns(".venv","__pycache__",".pytest_cache","node_modules","*.pyc"))' "$ROOT/$part" "$DEST/$part"
    fi
done
cp "$ROOT/README.md" "$ROOT/THIRD_PARTY_NOTICES.md" "$ROOT/LICENSE" "$DEST/"
mkdir -p "$DEST/bin"
if [ -f "$ROOT/bin/remote-agent-session" ]; then
    cp "$ROOT/bin/remote-agent-session" "$DEST/bin/remote-agent-session"
else
    command -v go >/dev/null
    PLUGIN_DIGEST=$("$PYTHON" "$ROOT/tools/package_plugin.py" "$ROOT/integrations/deepseek-harness" "$DEST/share/remote-agent-session/deepseek-harness.tgz")
    (cd "$ROOT/cli" && go build -trimpath -ldflags "-X github.com/ClydeZhao/remote-agent-session/cli/internal/buildinfo.Version=0.1.0 -X github.com/ClydeZhao/remote-agent-session/cli/internal/buildinfo.DSHPluginSHA256=$PLUGIN_DIGEST" -o "$DEST/bin/remote-agent-session" ./cmd/remote-agent-session)
fi
"$PYTHON" -m venv "$DEST/.venv"
"$DEST/.venv/bin/python" -m pip install --disable-pip-version-check -r "$DEST/install/requirements.lock"
"$DEST/.venv/bin/python" -m pip install --disable-pip-version-check --no-deps "$DEST/server"
"$DEST/.venv/bin/python" "$DEST/tools/lifecycle.py" doctor --mode offline
printf '%s\n' 'remote-agent-session-install-v1' > "$DEST/.installation-complete"
printf 'Installed: %s/bin/remote-agent-session\n' "$DEST"
printf '%s\n' 'Add its bin directory to PATH explicitly. No shell profiles or existing services were changed.'
