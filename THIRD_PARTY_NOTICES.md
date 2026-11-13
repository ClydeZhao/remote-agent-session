# Third-party notices and source licensing

This project extracts the original-session relay capability from BuddyBio with the rights holder's explicit authorization for personal publication under MIT. The project's own source is covered by [LICENSE](LICENSE), copyright 2026 ClydeZhao. This authorization does not replace third-party licenses.

Dependency code retains its own licenses and copyright notices. The Go module dependency graph is recorded in `cli/go.mod` and `cli/go.sum`; backend dependencies and their exact resolution are recorded in `server/pyproject.toml`, `server/uv.lock` and `install/requirements.lock`. Python installed distributions retain their `dist-info` license files. PostgreSQL, Python, Node and host applications are separately installed software, not bundled binaries.

`third_party/licenses/` contains actual license/notice files collected from resolved Go module cache entries and installed backend dependency distributions. `third_party/dependencies.json` records their names, versions and declared license metadata. These notices are attribution, not a legal conclusion about compatibility. Regenerate with `server/.venv/bin/python tools/collect_notices.py` after syncing the backend and downloading Go modules.
