# VoltTest contract

The public VoltTest test-definition format and, soon, the verdict rules
(thresholds) that decide whether a run passes. Tools that write or read
VoltTest test definitions can depend on this module. The VoltTest engine
itself is proprietary and is not part of this repository.

## Packages

| Package | Contents |
|---|---|
| `definition` | Test-definition format and schema_version check (in progress) |
| `thresholds` | Verdict rules (coming) |

## schema_version

v2 and later definitions carry `schema_version` as `"MAJOR.MINOR"`.

- Missing, `1` or `"1"` means v1.
- v2 is `"2.0"`.
- Each new definition field bumps the minor.
- A reader rejects an unknown major, or a minor newer than it supports.

The module version and `schema_version` move independently: schema `"2.0"`
does not mean module v2.

## Compatibility

Filled in at each engine release.

| schema_version | Minimum engine release |
|---|---|

## Usage

```sh
go get github.com/volt-test/contract@<tag-or-commit>
```

```go
import "github.com/volt-test/contract/definition"
```

Pin a tag or commit. For local work across several checkouts, use an
uncommitted `go.work` rather than a `replace` directive.

## License

MIT. See [LICENSE](LICENSE).
