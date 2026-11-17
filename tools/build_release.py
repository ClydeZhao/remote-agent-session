#!/usr/bin/env python3
import argparse
import hashlib
import json
from pathlib import Path
import platform
import shutil
import subprocess
import tarfile
import tempfile
import zipfile
from package_plugin import package

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    host_os = platform.system().lower()
    go_os, go_arch = subprocess.check_output(["go", "env", "GOOS", "GOARCH"], cwd=ROOT / "cli", text=True).split()
    if go_os != host_os:
        parser.error("Native releases must be built on their target OS; unset GOOS or use a native runner")
    host_arch = {"amd64": "amd64", "x86_64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(platform.machine().lower())
    if go_arch != host_arch:
        parser.error("Native releases must be built for the host architecture; unset GOARCH")
    version = "0.1.0"
    name = "remote-agent-session-" + version + "-" + platform.system().lower() + "-" + platform.machine()
    with tempfile.TemporaryDirectory() as temp:
        staging = Path(temp) / name
        staging.mkdir()
        for part in ("server", "tools", "install", "integrations", "skills", "docs", "config", "third_party"):
            source = ROOT / part
            if source.exists():
                shutil.copytree(source, staging / part, ignore=shutil.ignore_patterns("__pycache__", ".venv", ".pytest_cache", "node_modules", "*.pyc"))
        for file in ("README.md", "THIRD_PARTY_NOTICES.md", "LICENSE"):
            shutil.copy2(ROOT / file, staging / file)
        (staging / "bin").mkdir()
        plugin_digest = package(ROOT / "integrations/deepseek-harness", staging / "share/remote-agent-session/deepseek-harness.tgz")
        binary_name = "remote-agent-session.exe" if host_os == "windows" else "remote-agent-session"
        subprocess.run(["go", "build", "-trimpath", "-ldflags", "-X github.com/ClydeZhao/remote-agent-session/cli/internal/buildinfo.Version=" + version + " -X github.com/ClydeZhao/remote-agent-session/cli/internal/buildinfo.DSHPluginSHA256=" + plugin_digest, "-o", str(staging / "bin" / binary_name), "./cmd/remote-agent-session"], cwd=ROOT / "cli", check=True)
        if host_os == "windows":
            archive = args.output / (name + ".zip")
            with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as stream:
                for path in sorted(staging.rglob("*")):
                    if path.is_file():
                        stream.write(path, path.relative_to(staging.parent).as_posix())
        else:
            archive = args.output / (name + ".tar.gz")
            with tarfile.open(archive, "w:gz") as stream:
                stream.add(staging, arcname=name)
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        (args.output / (archive.name + ".sha256")).write_text(digest + "  " + archive.name + "\n")
        print(json.dumps({"archive": str(archive.resolve()), "sha256": digest, "public_release": False}))


if __name__ == "__main__":
    main()
