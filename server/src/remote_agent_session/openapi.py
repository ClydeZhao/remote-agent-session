import argparse
import json
from pathlib import Path

from remote_agent_session.api import create_app


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    path = Path(__file__).resolve().parents[3] / "contracts" / "openapi.json"
    value = (
        json.dumps(
            create_app(schema_only=True).openapi(), ensure_ascii=False, indent=2, sort_keys=True
        )
        + "\n"
    )
    if args.check:
        if path.read_text() != value:
            raise SystemExit("OpenAPI drift: run python -m remote_agent_session.openapi")
    else:
        path.write_text(value)


if __name__ == "__main__":
    main()
