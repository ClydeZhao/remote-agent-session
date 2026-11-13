#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ ! -f "$ROOT/.installation-complete" ] || [ "$(cat "$ROOT/.installation-complete")" != remote-agent-session-install-v1 ]; then
    echo 'Refusing to remove an unowned directory.' >&2
    exit 1
fi
"$ROOT/.venv/bin/python" "$ROOT/tools/lifecycle.py" service-remove "$@"
"$ROOT/.venv/bin/python" "$ROOT/tools/lifecycle.py" stop-owned "$@"
printf 'Reviewed installation cleanup: %s\n' "$ROOT"
printf '%s\n' 'Remove this installation directory after disconnecting sessions and removing owned host hooks/plugins. Private state is retained.'
