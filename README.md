# Sift

> [!NOTE]
> Sift's native review interface is based on [Godiff](https://github.com/egoist/godiff) by [EGOIST](https://github.com/egoist).

Review a complete feature branch without unrelated changes imported from integration branches.

Choose a repository and branch in the header. Sift detects the review range and shows your feature's changes in Godiff's native interface, built with [MyGo](https://github.com/egoist/mygo).

- Split and unified diffs with syntax highlighting and expandable context.
- File tree, reviewed files, search, and keyboard navigation.
- Inline review comments, saved locally and exportable as Markdown.

Conflict resolutions and overlapping changes that cannot be safely combined appear as separate historical patches, available from the warning icon in the header. They are labeled separately from the net feature diff.

## Why ?

Our team still reviews core features by hand, often near the end of an iteration. By then, feature branches may have merged the shared development branch several times. Opening a merge request against the main branch mixes the feature's own changes with unrelated work from the rest of the team.

I wanted a way to review one complete feature and leave comments, without manually separating its changes from everything brought in by those merges. That's why I built Sift.

## Run

Requires Go 1.27.2 or later and Git 2.36 or later.

```sh
go run ./cmd/sift /path/to/repository
```

To open a particular feature branch:

```sh
go run ./cmd/sift --branch feature/my-change /path/to/repository
```

The branch picker includes local and fetched remote-tracking branches, such as `origin/feature/my-change`, and reloads its choices each time you open it. Fetch in your Git client to update the remote records before reviewing.

Local branch operation records and saved commit provenance retain feature work after it is merged or fast-forwarded into the integration branch. Recorded synchronization points keep unrelated development out of the review, including when another branch contains the same feature commits.

When Git history does not identify the range reliably, choose the integration branch from a searchable list, then search the commit messages and select the first feature commit. The selected commit is included in the review; the range extends to the branch's latest commit and is remembered for that branch.

Reviews leave the repository's current branch, working files, and index untouched.

## File exclusions

Use the filter button beside the file search to expand a single exclusion field. Separate patterns with commas, for example `*.md, *.map, src/generated/**`. Collapsing the field keeps the exclusions active. Changes take effect immediately and are saved automatically. The initial rules are `*.md, *.spec.*, *.test.*`, hiding Markdown and test files; edit or clear these patterns to include them. Patterns without `/` match file names in any directory; patterns with `/` match paths relative to the repository. Rules apply to all repositories. Clearing every rule keeps all files included after restarting.

The filter button is highlighted while exclusions are active; hover to see the hidden-file count. Exclusions affect the file tree and displayed diff; comments are retained, and opening a comment temporarily reveals its file.

## Review comments

Click a code line or select a range to write a comment. Comments are saved automatically. Open the comments panel in the toolbar to browse your notes and use Previous or Next to walk through them during a review. Comments whose code can no longer be found remain available under Location changed.

To continue on another computer, choose **Review files → Export review**, then open the corresponding feature branch in the other checkout and choose **Import review**. Check the branch and version in the preview before importing. Repeated imports keep one copy of each comment; when a comment has changed, choose which copy to keep.

Use **Export Markdown** or **Copy Markdown** to share readable notes. Markdown exports cannot be imported as reviews.

## Develop

```sh
go tool mygo dev
CGO_ENABLED=0 go test ./...
go vet ./...
go tool mygo vet .
```

## Build

```sh
go tool mygo build
```

The macOS app is written to `build/darwin-arm64/Sift.app` on Apple Silicon.

## Updates

Packaged apps check GitHub Releases for signed updates and show release notes in a native window. Use **Sift → Check for Updates…** to check manually. Development builds do not check automatically. Updates are downloaded only after you choose to install, unless you enable automatic installation in the update window.

See [Releasing Sift](docs/releasing.md) for version upgrades, tag builds and GitHub Release publication.

## Credits

- [Godiff](https://github.com/egoist/godiff) by [EGOIST](https://github.com/egoist): native review interface, diff rendering, file navigation and inline comments.
- [MyGo](https://github.com/egoist/mygo): native desktop framework.
- Godiff's original credits include [codiff](https://github.com/nkzw-tech/codiff) for the review design and [@pierre/trees](https://www.npmjs.com/package/@pierre/trees) for file icons. Their existing notices are retained.

See [UPSTREAM.md](./UPSTREAM.md) for the imported revision and attribution.

## License

[MIT](./LICENSE) License © [jinghaihan](https://github.com/jinghaihan)

MIT applies to Sift's original contributions. Godiff's author has permitted modifications to the demo, but Godiff has no declared repository license and its code is not covered by Sift's MIT license. See [UPSTREAM.md](./UPSTREAM.md).
