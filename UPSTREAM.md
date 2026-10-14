# Upstream code

The native review interface is based on [Godiff](https://github.com/egoist/godiff)
by [EGOIST](https://github.com/egoist). Its original implementation is retained
in the root Go files and `internal/agent`, `internal/diff`, `internal/fileicon`,
`internal/git`, `internal/github`, `internal/highlight`, `internal/proc`, and
`internal/shellenv`. Changes to these files adapt it to Sift.

Imported revision: `ff30a21259e971b51f36480aec1b6f51f9a575b9`.

Godiff did not include a repository license when imported. Its code is **not
covered by Sift's MIT license**. This local prototype must not be published or
redistributed as an MIT-licensed copy of Godiff without resolving the upstream
permission. Existing third-party notices remain in place, including
`internal/fileicon/LICENSE`.

Sift's original contributions include `internal/review`, `internal/store`,
and the feature-range adapter. These contributions are licensed under MIT.

The House navigation icon is from [Lucide](https://github.com/lucide-icons/lucide/blob/main/icons/house.svg).
Its ISC license is retained in `resources/licenses/lucide.txt`.
