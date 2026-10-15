#!/usr/bin/env python3
"""Prepare shared release notes and verify MyGo packages before publication."""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import subprocess
from pathlib import Path
from urllib.parse import unquote, urlsplit

from release import ROOT, STABLE_VERSION, git, read_version


# Godiff's preserved history ends here. The first release describes Sift's changes.
UPSTREAM_BASE = "ff30a21259e971b51f36480aec1b6f51f9a575b9"
TARGETS = ("darwin-universal", "windows-amd64", "linux-amd64")


def verify_tag(tag: str) -> str:
    version = read_version()
    if tag != f"v{version}":
        raise ValueError(f"tag {tag} does not match mygo.json version {version}")
    return version


def previous_release(tag: str) -> str:
    current = tuple(map(int, tag[1:].split(".")))
    tags = git("tag", "--merged", tag, capture=True).splitlines()
    candidates = [
        (tuple(map(int, item[1:].split("."))), item)
        for item in tags
        if item.startswith("v") and STABLE_VERSION.fullmatch(item[1:])
        and tuple(map(int, item[1:].split("."))) < current
    ]
    return max(candidates)[1] if candidates else UPSTREAM_BASE


def prepare(tag: str) -> None:
    version = verify_tag(tag)
    directory = ROOT / ".release"
    directory.mkdir(exist_ok=True)
    notes = directory / "release-notes.md"
    subprocess.run([
        "npx", "--yes", "changelogithub@15.0.5",
        "--github", "jinghaihan/sift", "--from", previous_release(tag),
        "--to", tag, "--output", str(notes),
    ], cwd=ROOT, check=True)
    text = notes.read_text(encoding="utf-8").strip()
    if not text:
        raise ValueError("changelogithub generated empty release notes")
    # MyGo embeds this version section in every platform's update manifest.
    (directory / "CHANGELOG.md").write_text(f"## {version}\n\n{text}\n", encoding="utf-8")


def verify_assets(directory: Path, targets: tuple[str, ...] = TARGETS) -> None:
    config = json.loads((ROOT / "mygo.json").read_text(encoding="utf-8"))
    version = read_version()
    expected_notes = (ROOT / ".release/release-notes.md").read_text(encoding="utf-8").strip()
    for target in targets:
        manifest = json.loads((directory / f"update-{target}.json").read_text(encoding="utf-8"))
        if manifest["version"] != version or manifest.get("notes", "").strip() != expected_notes:
            raise ValueError(f"{target}: version or release notes mismatch")
        expected = f"https://github.com/{config['updates']['github']}/releases/download/v{version}/"
        for artifact in [manifest, *manifest.get("deltas", [])]:
            url = artifact["url"]
            name = unquote(urlsplit(url).path.rsplit("/", 1)[-1])
            if not url.startswith(expected) or not name or Path(name).name != name:
                raise ValueError(f"{target}: unexpected update URL: {url}")
            file = directory / name
            if not file.is_file() or file.stat().st_size != artifact["size"]:
                raise ValueError(f"{target}: missing or incomplete update file: {name}")
            if len(base64.b64decode(artifact["signature"], validate=True)) != 64:
                raise ValueError(f"{target}: missing Ed25519 signature")

    downloads = {
        "darwin-universal": f"Sift {version}.dmg",
        "windows-amd64": f"Sift Setup {version} amd64.exe",
        "linux-amd64": f"sift_{version}_amd64.deb",
    }
    for target in targets:
        original = directory / downloads[target]
        file = directory / original.name.replace(" ", "-")
        # GitHub changes spaces in uploaded asset names into dots. Rename
        # installers before generating checksums so local and downloaded
        # filenames agree. Accept normalized files when verification repeats.
        if original != file and original.is_file():
            if file.exists():
                raise ValueError(f"duplicate installer: {file.name}")
            original.rename(file)
        if not file.is_file() or not file.stat().st_size:
            raise ValueError(f"missing installer: {file.name}")
    if "linux-amd64" in targets and not (directory / "install.sh").is_file():
        raise ValueError("missing Linux install script")
    files = sorted(p for p in directory.iterdir() if p.is_file() and p.name != "SHA256SUMS")
    checksums = "".join(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n" for p in files)
    (directory / "SHA256SUMS").write_text(checksums, encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("prepare").add_argument("tag")
    sub.add_parser("check-tag").add_argument("tag")
    sub.add_parser("verify").add_argument("directory", type=Path)
    args = parser.parse_args()
    if args.command == "prepare":
        prepare(args.tag)
    elif args.command == "check-tag":
        verify_tag(args.tag)
    else:
        verify_assets(args.directory)


if __name__ == "__main__":
    main()
