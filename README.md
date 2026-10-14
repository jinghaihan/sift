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

When Git history does not identify the range reliably, Sift asks for the integration branch and first feature commit. The first commit is included in the review, and the range is remembered for that branch.

Reviews leave the repository's current branch, working files, and index untouched.

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

## Credits

- [Godiff](https://github.com/egoist/godiff) by [EGOIST](https://github.com/egoist): native review interface, diff rendering, file navigation and inline comments.
- [MyGo](https://github.com/egoist/mygo): native desktop framework.
- Godiff's original credits include [codiff](https://github.com/nkzw-tech/codiff) for the review design and [@pierre/trees](https://www.npmjs.com/package/@pierre/trees) for file icons. Their existing notices are retained.

See [UPSTREAM.md](./UPSTREAM.md) for the imported revision and attribution.

## License

[MIT](./LICENSE) License © [jinghaihan](https://github.com/jinghaihan)

MIT applies to Sift's original contributions. Godiff's author has permitted modifications to the demo, but Godiff has no declared repository license and its code is not covered by Sift's MIT license. See [UPSTREAM.md](./UPSTREAM.md).
