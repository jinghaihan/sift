# Headless review workflow

Run `sift help` for commands, or use `go run ./cmd/sift` during development.
The CLI uses the app's local review directory (`~/.sift` by default).
Use `--data-dir` or `SIFT_DATA_DIR` to select an isolated directory.

```sh
sift analyze --repo /path/to/repository --branch feature/my-change
sift draft upsert --repo /path/to/repository --branch feature/my-change --input /path/to/drafts.json --dry-run
sift script show --repo /path/to/repository --branch feature/my-change
sift validate --repo /path/to/repository --branch feature/my-change
sift export --repo /path/to/repository --branch feature/my-change --output /path/to/review.json
sift import --repo /path/to/repository --input /path/to/review.json --dry-run
sift script export --repo /path/to/repository --branch feature/my-change --format markdown --output /path/to/script.md
```

Analysis and validation do not write review data. Mutations require a versioned
JSON input with `version: 1`, the `revision` returned by analysis, and
`reviewVersion` from `analyze.review.version`. A changed revision or record
version rejects an outdated update. `--dry-run` validates without saving.
Errors are JSON and return a nonzero exit status; validation returns status 2
when a location is stale or missing. All drafts remain local.

## Drafts

`draft upsert` accepts this structure:

```json
{
  "version": 1,
  "revision": "<revision from analyze>",
  "reviewVersion": 0,
  "owner": "review-bot",
  "comments": [
    {
      "id": "cancellation",
      "path": "internal/worker.go",
      "side": "new",
      "start": 42,
      "end": 44,
      "body": "Explain how cancellation reaches the worker."
    }
  ]
}
```

Paths are relative to the repository. The CLI captures the code and surrounding
lines. IDs are namespaced by owner; returned IDs can be reused directly.
Updates preserve untargeted notes and reject a conflicting record version.

## Review scripts

`script upsert` uses the same version/revision header and a `sections` array:

```json
{
  "version": 1,
  "revision": "<revision from analyze>",
  "reviewVersion": 1,
  "sections": [
    {
      "id": "cancellation-flow",
      "title": "Cancellation flow",
      "body": "Explain **why** cancellation is propagated here.",
      "references": [
        {"id": "worker", "kind": "code", "path": "internal/worker.go", "side": "new", "start": 42},
        {"id": "note", "kind": "comment", "commentId": "agent:review-bot:cancellation"}
      ]
    }
  ],
  "order": ["cancellation-flow"]
}
```

Sections and references have stable IDs. Omitted fields, references and sections
are preserved. `order` moves the listed sections first and retains the others.
`delete` removes explicitly listed section IDs; each section's `deleteReferences`
removes explicitly listed reference IDs. Titles and Markdown bodies support any
language. Context-only lines in reviewed files are valid code references.

`script validate` reports `valid`, `stale`, `unresolved`, or `missing_comment` for
each reference. After a branch update, refresh the header and upsert a reference
ID without new coordinates to relocate its unique code context. If the context
is missing or ambiguous, supply a replacement location. Historical code
references include a `historical` commit ID and remain explicitly labeled in
Markdown exports. The native script editor is not available yet.

## Portable review files

JSON exports use format v2, including commit provenance, notes and ordered script
sections. Older v1 files remain readable. Import restores provenance before
analyzing the branch; `--bundle FILE` can supply provenance for other commands.
Local absolute repository paths are omitted from exports.

Import rejects conflicting content by default. Select `--on-conflict keep-local`
or `--on-conflict incoming` explicitly. `--allow-stale` retains unresolved or
stale imported references for repair. Use the app's historical view to import a
historical comment bundle. Data and export destinations must be outside the
reviewed repository.

The app restores a selected JSON file directly, replacing the current review's
comments and script sections with its contents. CLI conflict options apply only
to headless imports.
