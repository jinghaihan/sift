#!/usr/bin/env python3
"""Bump mygo.json, commit, tag and push; GitHub Actions publishes the release."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
STABLE_VERSION = re.compile(r"(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)")


def git(*args: str, capture: bool = False) -> str:
    result = subprocess.run(
        ["git", *args], cwd=ROOT, check=True, text=True, capture_output=capture
    )
    return result.stdout.strip() if capture else ""


def read_version() -> str:
    version = json.loads((ROOT / "mygo.json").read_text(encoding="utf-8"))["version"]
    if not isinstance(version, str) or not STABLE_VERSION.fullmatch(version):
        raise ValueError(f"mygo.json has an invalid stable version: {version}")
    return version


def bump_version(version: str, bump: str) -> str:
    major, minor, patch = map(int, version.split("."))
    result = {
        "patch": (major, minor, patch + 1),
        "minor": (major, minor + 1, 0),
        "major": (major + 1, 0, 0),
    }[bump]
    return ".".join(map(str, result))


def check_repository() -> None:
    if git("branch", "--show-current", capture=True) != "main":
        raise ValueError("switch to main before releasing")
    if git("status", "--porcelain", capture=True):
        raise ValueError("commit working-tree changes before releasing")
    git("fetch", "origin", "main", "--tags")
    if git("rev-parse", "HEAD", capture=True) != git("rev-parse", "origin/main", capture=True):
        raise ValueError("synchronize main with origin/main before releasing")


def select_bump(version: str) -> str | None:
    print(f"Current version: v{version}\n")
    for index, bump in enumerate(("patch", "minor", "major"), 1):
        print(f"  {index}) {bump}: v{bump_version(version, bump)}")
    selected = input("Select release type [1-3/q]: ").strip().lower()
    if selected in ("", "q"):
        return None
    selected = {"1": "patch", "2": "minor", "3": "major"}.get(selected, selected)
    if selected not in ("patch", "minor", "major"):
        raise ValueError("unknown release type")
    return selected


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("bump", nargs="?", choices=("patch", "minor", "major"))
    parser.add_argument("--current", action="store_true", help="tag the current version for its first release")
    parser.add_argument("--check", action="store_true", help="show the plan without changing files or publishing")
    args = parser.parse_args()
    if args.current and args.bump:
        parser.error("--current cannot be combined with a version bump")

    check_repository()
    current = read_version()
    bump = None if args.current else args.bump or select_bump(current)
    if not args.current and bump is None:
        print("Release cancelled.")
        return
    version = current if args.current else bump_version(current, bump)
    tag = f"v{version}"
    if git("tag", "--list", tag, capture=True):
        raise ValueError(f"tag already exists: {tag}")
    print(f"Release plan: v{current} -> {tag}", flush=True)
    if args.check:
        return

    if not args.current:
        path = ROOT / "mygo.json"
        original = path.read_text(encoding="utf-8")
        data = json.loads(original)
        data["version"] = version
        committed = False
        try:
            path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
            git("add", "mygo.json")
            git("commit", "-m", f"chore: release {tag}")
            committed = True
        finally:
            if not committed:
                git("restore", "--staged", "mygo.json")
                path.write_text(original, encoding="utf-8")

    git("tag", "-a", tag, "-m", tag)
    git("push", "--atomic", "origin", "main", tag)
    print(f"Pushed {tag}; GitHub Actions will build and publish the release.")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, subprocess.CalledProcessError, KeyboardInterrupt) as error:
        print(f"Release failed: {error}", file=sys.stderr)
        raise SystemExit(1) from error
