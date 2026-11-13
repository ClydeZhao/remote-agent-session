#!/usr/bin/env python3
import argparse
import hashlib
import json
from pathlib import Path
import tarfile


def package(source, output):
    manifest = json.loads((source / "package.json").read_text())
    output.parent.mkdir(parents=True, exist_ok=True)
    with tarfile.open(output, "w:gz") as stream:
        for name in ["package.json", *manifest["files"]]:
            path = source / name
            if not path.is_file() or not path.resolve().is_relative_to(source.resolve()):
                raise RuntimeError("Invalid plugin file: " + name)
            stream.add(path, arcname="package/" + name)
    return hashlib.sha256(output.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    print(package(args.source, args.output))


if __name__ == "__main__":
    main()
