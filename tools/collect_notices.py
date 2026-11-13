#!/usr/bin/env python3
import importlib.metadata
import json
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]
TARGET = ROOT / "third_party"


def copy_license(source, name, destination):
    if source.is_file() and any(word in source.name.lower() for word in ("license", "copying", "notice", "copyright")):
        destination.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, destination / name)


def main():
    if TARGET.exists():
        shutil.rmtree(TARGET)
    entries = []
    for package in sorted(importlib.metadata.distributions(), key=lambda item: item.metadata["Name"].lower()):
        name = package.metadata["Name"]
        if name.lower().replace("_", "-") == "remote-agent-session-server":
            continue
        licenses = [value for value in package.metadata.get_all("Classifier", []) if value.startswith("License ::")]
        entries.append({"ecosystem": "python", "name": name, "version": package.version, "license": package.metadata.get("License-Expression") or package.metadata.get("License") or licenses})
        for file in package.files or []:
            copy_license(Path(package.locate_file(file)), str(file).replace("/", "_"), TARGET / "licenses" / ("python-" + name + "-" + package.version))
    raw = subprocess.check_output(["go", "list", "-m", "-json", "all"], cwd=ROOT / "cli", text=True)
    decoder = json.JSONDecoder()
    while raw.strip():
        item, offset = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[offset:]
        if item.get("Main"):
            continue
        entries.append({"ecosystem": "go", "name": item["Path"], "version": item.get("Version", "")})
        if item.get("Dir"):
            directory = Path(item["Dir"])
            destination = TARGET / "licenses" / ("go-" + item["Path"].replace("/", "_") + "-" + item.get("Version", ""))
            for source in directory.iterdir():
                copy_license(source, source.name, destination)
    TARGET.mkdir(exist_ok=True)
    (TARGET / "dependencies.json").write_text(json.dumps(entries, indent=2) + "\n")


if __name__ == "__main__":
    main()
