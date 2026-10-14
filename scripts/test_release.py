import base64
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import release
import release_assets


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.root = self.directory / "repo"
        self.root.mkdir()
        self.env = dict(os.environ, GIT_AUTHOR_NAME="Ada", GIT_AUTHOR_EMAIL="ada@example.com",
                        GIT_COMMITTER_NAME="Ada", GIT_COMMITTER_EMAIL="ada@example.com")
        self.command("git", "init", "-q", "-b", "main")
        self.command("git", "config", "commit.gpgsign", "false")
        self.command("git", "config", "tag.gpgsign", "false")
        (self.root / "mygo.json").write_text(json.dumps({
            "version": "0.1.0", "updates": {"github": "jinghaihan/sift"},
        }) + "\n", encoding="utf-8")
        scripts = self.root / "scripts"
        scripts.mkdir()
        shutil.copy(Path(release.__file__), scripts / "release.py")
        self.command("git", "add", ".")
        self.command("git", "commit", "-qm", "base")
        self.command("git", "init", "--bare", "-q", str(self.directory / "origin.git"))
        self.command("git", "remote", "add", "origin", str(self.directory / "origin.git"))
        self.command("git", "push", "-q", "-u", "origin", "main")
        for module in (release, release_assets):
            patcher = patch.object(module, "ROOT", self.root)
            patcher.start()
            self.addCleanup(patcher.stop)

    def command(self, *args):
        result = subprocess.run(args, cwd=self.root, env=self.env, text=True, capture_output=True, check=True)
        return result.stdout.strip()

    def script(self, *args, success=True):
        result = subprocess.run([sys.executable, "scripts/release.py", *args], cwd=self.root,
                                env=self.env, text=True, capture_output=True)
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        return result

    def test_preview_changes_neither_version_nor_tags(self):
        before = self.command("git", "rev-parse", "HEAD")
        result = self.script("minor", "--check")
        self.assertIn("v0.2.0", result.stdout)
        self.assertEqual(release.read_version(), "0.1.0")
        self.assertEqual(self.command("git", "tag", "--list"), "")
        self.assertEqual(self.command("git", "rev-parse", "HEAD"), before)

    def test_release_pushes_version_and_annotated_tag_together(self):
        self.script("patch")
        self.assertEqual(release.read_version(), "0.1.1")
        self.assertEqual(self.command("git", "log", "-1", "--format=%s"), "chore: release v0.1.1")
        self.assertEqual(self.command("git", "diff", "HEAD^", "HEAD", "--name-only"), "mygo.json")
        self.assertEqual(self.command("git", "cat-file", "-t", "v0.1.1"), "tag")
        remote = self.command("git", "ls-remote", "origin", "refs/heads/main", "refs/tags/v0.1.1^{}")
        hashes = [line.split()[0] for line in remote.splitlines()]
        self.assertEqual(hashes, [self.command("git", "rev-parse", "HEAD")] * 2)

    def test_first_release_and_dirty_tree_guard(self):
        self.script("--current", "--check")
        self.assertEqual(self.command("git", "tag", "--list"), "")
        (self.root / "uncommitted.txt").write_text("dirty", encoding="utf-8")
        self.script("patch", success=False)
        self.assertEqual(release.read_version(), "0.1.0")

    def test_notes_range_skips_upstream_and_chooses_previous_stable_tag(self):
        self.command("git", "tag", "v0.1.0")
        self.assertEqual(release_assets.previous_release("v0.1.0"), release_assets.UPSTREAM_BASE)
        self.command("git", "tag", "v0.0.9")
        self.command("git", "tag", "v0.0.10")
        self.command("git", "tag", "v0.1.0-beta.1")
        self.assertEqual(release_assets.previous_release("v0.1.0"), "v0.0.10")
        with self.assertRaises(ValueError):
            release_assets.verify_tag("v0.2.0")

    def test_installer_and_update_manifest_must_be_complete(self):
        directory = self.root / ".release/assets"
        directory.mkdir(parents=True)
        (self.root / ".release/release-notes.md").write_text("### Features\n- Review a feature branch.\n", encoding="utf-8")
        for target in release_assets.TARGETS:
            name = f"sift-0.1.0-{target}.tar.gz"
            (directory / name).write_bytes(b"archive")
            manifest = {
                "version": "0.1.0", "notes": "### Features\n- Review a feature branch.",
                "url": f"https://github.com/jinghaihan/sift/releases/download/v0.1.0/{name}",
                "size": 7, "signature": base64.b64encode(bytes(64)).decode(),
            }
            (directory / f"update-{target}.json").write_text(json.dumps(manifest), encoding="utf-8")
        for name in ("Sift 0.1.0.dmg", "Sift Setup 0.1.0.exe", "sift_0.1.0_amd64.deb", "install.sh"):
            (directory / name).write_bytes(b"package")
        release_assets.verify_assets(directory)
        self.assertIn("Sift 0.1.0.dmg", (directory / "SHA256SUMS").read_text(encoding="utf-8"))
        (directory / "sift-0.1.0-windows-amd64.tar.gz").write_bytes(b"truncated")
        with self.assertRaises(ValueError):
            release_assets.verify_assets(directory)


if __name__ == "__main__":
    unittest.main()
