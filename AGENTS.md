# Sift

Native Go desktop code review tool using MyGo. Keep the interface native; do not
add a web frontend without a demonstrated native limitation.

## Scope

- The complete feature branch is the review unit. Exclude ordinary changes
  imported by integration-branch merges, including changes in the same file.
- The normal flow selects a repository and feature branch in the header.
  Infer the integration branch and inclusive first feature commit from Git
  history and usable creation reflogs; remember the scope. Only show a range
  dialog when inference is ambiguous. Integration-mainline commits are background,
  including fast-forwarded updates. Reject ambiguous scopes instead of leaking
  unrelated changes.
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
- Root native views: retain Godiff's file tree, continuous diff cards, inline
  comments and keyboard navigation. Do not replace these with an independent UI.
- `feature.go`: connects feature isolation and local review records to Godiff.
- `main.go`: application startup and command line handling.
- Imported packages and permission status are documented in `UPSTREAM.md`.

## Conventions

- English documentation, UI text and conventional commits.
- Prefer Geist for interface text and Geist Mono for code. Match Godiff's
  compact metrics: 13-point code, 20-point line height.
- Keep repository/branch selection in the header. Use icon buttons with
  tooltips for layout, wrap, comments, export and refresh. No sidebar Scope action.
- Sift's MIT license covers original contributions only. Godiff currently has
  no repository license; do not publish or redistribute this imported prototype
  without resolving upstream permission.
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

Make atomic English Conventional Commits. Do not push or create a GitHub
repository until the user accepts this first version.
