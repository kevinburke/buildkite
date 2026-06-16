## 1.3.0

First release tagged in a Go-compatible `vX.Y.Z` form. Tags now carry a `v`
prefix (`bump_version --tag-prefix=v`) and the `Version` constant uses
three-component semver.

Library changes since 1.0:

- prefix: switch the UUID dependency from `github.com/kevinburke/go.uuid` to
  `github.com/gofrs/uuid/v5`.
- prefix: `GenerateUUID` now panics if the random source fails, since
  `gofrs/uuid.NewV4` can return an error.
- Replace `interface{}` with `any` on the `Scan` methods.

Tooling: Go 1.25, updated dependencies, and the move from Travis CI to GitHub
Actions.

## 1.0

Remove the mgo interface.
