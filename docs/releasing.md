# Releasing Sift

The version in `mygo.json` is the single source of truth. Releases use stable
SemVer tags (`v0.1.0`), matching the Python release scripts of the other native
projects.

## Upgrade the version

From a clean `main` synchronized with `origin/main`:

```sh
python3 scripts/release.py patch --check
python3 scripts/release.py patch
```

Use `minor` or `major` for the other version increments. Omit the argument for
an interactive choice. `--check` previews the version without changing files,
creating tags or pushing. It still fetches the remote to check synchronization.

For the first publication of the already committed `0.1.0` version:

```sh
python3 scripts/release.py --current --check
python3 scripts/release.py --current
```

The script updates only `mygo.json`, commits `chore: release vX.Y.Z`, creates
an annotated tag, and pushes `main` and the tag atomically. `--current` creates
the tag without a version commit. Pushing the tag starts publication.

## GitHub Actions

Normal pushes and pull requests run Go checks, release-script tests and native
builds on macOS, Windows and Linux. They do not publish anything.

The tag workflow:

1. Checks that the tag agrees with `mygo.json`.
2. Generates release notes with `changelogithub@15.0.5`. The first release starts
   after the preserved Godiff revision; later releases start at the previous
   stable Sift tag.
3. Builds the macOS universal DMG (Apple Silicon and Intel), Windows x64 installer,
   and Linux x64 archive and Debian package. MyGo also creates signed update
   archives, available delta updates and a manifest for each target.
4. Embeds the same generated release notes in each update manifest.
5. Normalizes installer filenames without spaces, then checks installers,
   manifests, archive sizes and checksums. Creates a draft
   GitHub Release, uploads all files, downloads them to verify their checksums,
   then publishes the complete release as latest.

If a build fails, no release is published. Re-run failed jobs to resume. An
already public release is not overwritten by a repeated publication job.

## Update signing

`mygo.json` contains the public key and `updates.github: "jinghaihan/sift"`.
GitHub Actions reads the matching private key from the repository secret
`MYGO_UPDATER_PRIVATE_KEY`. Keep a private backup of this key outside Git: installed
apps trust updates signed with it. The maintainer's local key pair is stored
outside the checkout in `~/.config/sift/update-keys/`.

To restore the CI secret from that backup:

```sh
gh secret set MYGO_UPDATER_PRIVATE_KEY --repo jinghaihan/sift < ~/.config/sift/update-keys/mygo-update.key
```

Update signatures are independent of Apple Developer ID signing and notarization.
Current macOS packages use ad-hoc signing, and Windows packages have no publisher
certificate. Those distribution certificates have not been configured.
