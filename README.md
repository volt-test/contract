# VoltTest contract

The public VoltTest test-definition format and, soon, the verdict rules
(thresholds) that decide whether a run passes. Tools that write or read
VoltTest test definitions can depend on this module. The VoltTest engine
itself is proprietary and is not part of this repository.

## Packages

| Package | Contents |
|---|---|
| `definition` | Test-definition types, `Parse`, schema_version checks |
| `thresholds` | Verdict rules (coming) |

The JSON Schema for v2 definitions is
[`schema/test-definition.schema.json`](schema/test-definition.schema.json).
It is generated from the Go types; run
`go test ./definition -run TestSchema -update` after changing them.

## schema_version

v2 and later definitions carry `schema_version` as a `"MAJOR.MINOR"` string.

- Missing, `null`, `1`, `"1"` or `"1.0"` means v1.
- v2 is `"2.0"`. It is a preview until SDK 2.0.0 ships: fields may still
  join `"2.0"` until then. After that, each new definition field bumps the
  minor.
- A reader rejects an unknown major, or a minor newer than it supports.

The module version and `schema_version` move independently: schema `"2.0"`
does not mean module v2.

### v1 and v2

`definition.Parse` reads both formats.

- **v2** is strict. Every key must be defined for the definition's
  `schema_version` (exact spelling and case), values must have the right
  JSON type, and the structure is validated:
  - a name, and at least one scenario with at least one step;
  - unique scenario names, and unique step names within a scenario;
  - durations that parse ("30s", "1m30s");
  - a load model: either non-empty `stages`, or `virtual_users` (at
    least 1) with optional `duration` and `ramp_up`, never both. Without
    `duration` each virtual user runs its scenario once; a `duration` that
    is present must be greater than zero, and so must every stage's
    `duration`;
  - known extractor types, data formats and data modes.
- **v1** is read the way the v1 readers (the CLI, the cloud engine and the
  backend) read it. Unknown keys are ignored, keys match case-insensitively,
  and v2-only keys such as `target.base_url` are dropped unread. Values are
  checked for their JSON type only:
  - **core** fields, which every v1 reader decodes: a value of the wrong
    type fails `Parse`, as it fails every reader;
  - **loose** fields, which some v1 reader ignores: the top-level
    `description`, `weights`, `report_type`, `region_config`, `cloud`,
    `api_key`, `cloud_timeout`, `existing_test_id`, `skip_lookup`,
    `target.idle_timeout`, a request's `headers` and
    `data_config.data_download_url`. A value of the wrong type does not fail
    `Parse`: it is decoded as if the mistyped value were absent (with a
    duplicate key, an earlier good value survives), and the problem is
    listed in `Definition.V1TypeProblems`, once per field with a count.
    Each reader then fails on its own read set:

    ```go
    // Headless CLI.
    var headlessReads = definition.MustReadSet("description", "weights",
        "report_type", "region_config", "cloud", "api_key", "cloud_timeout",
        "existing_test_id", "skip_lookup", "target.idle_timeout")

    // Cloud engine.
    var cloudReads = definition.MustReadSet(
        "scenarios[].steps[].request.headers",
        "scenarios[].data_config.data_download_url")

    if ps := def.V1TypeProblemsIn(headlessReads); len(ps) > 0 { /* fail */ }
    ```

    A pattern uses canonical key names with `[]` for every array level it
    passes through, ends at a field name (`weights`, not `weights[]`: the
    field name already covers the whole field and every element), and
    stops at a map field such as `headers`. Never name a literal index or
    a map key: only the first mistyped value of each field keeps its path.
    A problem falls under a pattern when its path equals the pattern or
    continues it with `.` or `[`: `weights[1]` falls under `weights`,
    `weightsx` does not. `NewReadSet` rejects a pattern that breaks these
    rules or names an unknown or v2-only field; `MustReadSet` panics, so a
    bad package-level read set fails at program start and in tests.

These keys are v1-only and are rejected in v2: `weights`, `report_type`,
`cloud`, `api_key`, `cloud_timeout`, `existing_test_id`, `skip_lookup`,
a step's `validate` and a request's `header` (v2 uses `headers`).

In v2 a step URL is an absolute `http(s)` URL, a URL starting with `${...}`,
or a path starting with `/`. A path is appended to `target.base_url`
(required when any step uses a path) by plain string concatenation, so
`${...}` references stay as written; `definition.ResolveURL` does this.

`data_config.data_download_url` is dispatch-only: the VoltTest backend sets
it when it sends a run to the engines, and it counts as a data source in v2.
The backend must strip any value a user submits before calling `Parse` on
the definition.
It is accepted by `Parse` but is not part of the published schema.

### JSON Schema

The schema is necessary, not sufficient: every definition `Parse` accepts
as v2 passes the schema, but `Parse` is stricter. It also checks
durations, URL forms, unique names and parts of the load model.

### Error size

Errors are bounded: at most 1000 paths or problems are kept (the rest are
counted in `Omitted`), messages list at most 100, and echoed keys and
values are cut to about 128 bytes. `V1TypeProblems` keeps one problem per
field and message, with a `Count`.

## Compatibility

The first VoltTest engine release that reads each `schema_version`.

| schema_version | Minimum engine release |
|---|---|
| `1` | any |
| `2.0` | unreleased |

## Usage

```sh
go get github.com/volt-test/contract@<tag-or-commit>
```

```go
import "github.com/volt-test/contract/definition"

def, err := definition.Parse(data)
if err != nil {
	// errors.Is(err, definition.ErrInvalidDefinition) is true; use
	// errors.As with *definition.UnknownFieldError, *ValidationError and
	// the other error types for details.
}
```

Pin a tag or commit. For local work across several checkouts, use an
uncommitted `go.work` rather than a `replace` directive.

## License

MIT. See [LICENSE](LICENSE).
