# Sift

Native Go desktop code review tool using MyGo. Keep the interface native; do not
add a web frontend without a demonstrated native limitation.

## Scope

- The complete feature branch is the review unit. Exclude ordinary changes
  imported by integration-branch merges, including changes in the same file.
- The normal flow selects a repository and feature branch in the header.
  Infer the integration branch and inclusive first feature commit from Git
  history and usable creation reflogs; remember the scope. Offer inline branch
  and commit choices only when inference is ambiguous. Integration updates are
  background; feature commits remain feature work after they enter the integration
  mainline. Follow recorded synchronization positions and persist commit provenance
  with review records. Do not pick a merge parent merely because it contains the
  feature start. Reject missing provenance instead of silently dropping shared
  commits or leaking unrelated changes. A tracking checkout is not proof of an
  empty feature or its original fork.
  Git commits do not record branch creation. Never guess a scope and silently include
  unrelated development. Remember the user's scope locally.
- Use first-parent history to identify feature commits. Author filtering is not
  a replacement for branch history.
- Preserve ambiguous conflict changes as separately labeled review material.
  Never hide them or present historical conflict patches as a clean final diff.
- Work in a temporary clone. Never checkout, reset, revert, stage, or commit in
  a reviewed repository. Preserve dirty working trees and the user's index.
- Store review data outside repositories. Anchor comments to file, side, lines,
  code context and revision. Detect stale comments after refresh.

## Structure

- `internal/review`: Git history, isolation, and review result. No UI dependency.
- `internal/store`: local review records and Markdown export.
- `cmd/sift/main.go`: thin executable entry point calling `app.Run`.
- `internal/app`: application startup, command line handling and native views.
  Retain Godiff's file tree, continuous diff cards, inline
  comments and keyboard navigation. Do not replace these with an independent UI.
- `internal/app/feature.go`: connects feature isolation and local review records
  to Godiff.
- `mygo.json`: points the development and packaging commands at `./cmd/sift`.
- Imported packages and permission status are documented in `UPSTREAM.md`.

## Conventions

- English documentation, UI text and conventional commits.
- Prefer Geist for interface text and Geist Mono for code. Match Godiff's
  compact metrics: 13-point code, 20-point line height.
- Keep repository/branch selection in the header. Use icon buttons with
  tooltips for layout, wrap, comments, export and refresh. No sidebar Scope action.
- Preserve Godiff's original commits unchanged. Sift is maintained as a
  standalone repository with its own changes committed after the upstream history.
- Sift's MIT license covers original contributions only. Godiff's author has
  permitted modifications, but no repository license has been declared for its
  code; do not present it as covered by Sift's MIT license.
- Keep packages small and separate stages with blank lines.
- Prefer the installed Git executable and Go's standard library.
- Keep I/O and Git operations off the UI thread. Publish changes through
  `Window.Update`; cancel or reject obsolete jobs.
- Read `.agents/skills/mygo-maintenance/SKILL.md` before changing native UI.
- Pin MyGo and verify APIs against its actual source. Do not store `ui.Element`
  or `*ui.Context` in application state or capture them in background jobs.

## Verification

Run `gofmt`, `go vet ./...`, `go tool mygo vet .`, and
`CGO_ENABLED=0 go test ./...`. Test isolation with real temporary Git histories:
repeated merges, same-file upstream changes, reversals, conflicts, and dirty
source repositories. Inspect the native app for layout changes.

Make atomic English Conventional Commits on `main`. The `origin` remote points
to `https://github.com/jinghaihan/sift`; do not add a Godiff remote unless requested.
