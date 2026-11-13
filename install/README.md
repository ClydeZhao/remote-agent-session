# Release preparation

Build from the repository root with `python3 tools/build_release.py --output /private/output/path`. The native archive includes the CLI binary, Python server and migrations, pinned dependency list, lifecycle controller, plugin, skill and instructions. The clean installer requires Python and PostgreSQL tools; source builds also require Go. Dependencies may be fetched from the package index, so this is not an offline installer.

Install into a fresh `REMOTE_AGENT_SESSION_INSTALL_DIR` using `sh install/install.sh`. Verify start, repeated start, status, stop, restart and cleanup in a private temporary service directory separate from client state, using `--service-state-dir` / `REMOTE_AGENT_SESSION_SERVICE_STATE_DIR`; client `--state-dir` remains independent. Then verify both native hosts and a separate Feishu application. Never use a real application's credentials in offline tests or share an event consumer with an existing deployment.

Run Python, Go and plugin tests before creating the candidate. Check checksum, distribution contents, secret scanning, license ownership and included third-party notices before publishing. The extracted project is authorized for publication under MIT. Include the root LICENSE and third-party notices in every distribution.

The dependency lock is regenerated from `server/uv.lock` with `uv export --project server --no-dev --no-emit-project --format requirements-txt --output-file install/requirements.lock`. Refresh third-party license metadata whenever dependencies change. Do not publish a candidate whose license notices or versioned resources are incomplete.
