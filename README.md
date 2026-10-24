# VoltTest contract

The public VoltTest test-definition format and the verdict rules
(thresholds) that decide whether a run passes. Tools that write or read
VoltTest test definitions can depend on this module. The VoltTest engine
itself is proprietary and is not part of this repository.

## Packages

| Package | Contents |
|---|---|
| `definition` | Test-definition types, `Parse`, schema_version checks |
| `thresholds` | Threshold expressions (parser, metric catalog, scopes) and the evaluator: snapshots, node merge, results |

Runtime dependency: `thresholds` uses
[hdrhistogram-go](https://github.com/HdrHistogram/hdrhistogram-go) for
percentiles, and `definition` imports `thresholds`, so both depend on it.
There are no other runtime dependencies.

The JSON Schema for v2 definitions is
[`schema/test-definition.schema.json`](schema/test-definition.schema.json).
It is generated from the Go types; run
`go test ./definition -run TestSchema -update` after changing them.

## schema_version

v2 and later definitions carry `schema_version` as a `"MAJOR.MINOR"` string.

- Missing, `null`, `1`, `"1"` or `"1.0"` means v1.
- v2 is `"2.0"`. It is a preview until SDK 2.0.0 ships: fields may still
  join `"2.0"` until then. After that, each new definition field bumps the
  minor. [Checks](#checks), `expected_statuses` and the extractor keys
  `required` and `on_fail` joined `"2.0"` under this rule.
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
  - known extractor types, data formats and data modes;
  - thresholds: every expression parses, each metric is allowed at its
    scope, and `delay_abort` is a non-negative duration set only with
    `abort_on_fail: true` (see [Thresholds](#thresholds));
  - checks: a known type, exactly one operator that the type takes, a
    selector when the type needs one, values of the right kind, status
    codes from 100 to 599, and a unique label per step (see
    [Checks](#checks)).
- **v1** is read the way the v1 readers (the CLI, the cloud engine and the
  backend) read it. Unknown keys are ignored, keys match case-insensitively,
  and v2-only keys (`target.base_url`, `thresholds` at every level,
  `expected_statuses`, a step's `checks`, and an extractor's `required` and
  `on_fail`) are dropped unread, whatever their value. Values are
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
durations, URL forms, unique names, parts of the load model, threshold
expressions and their scopes (the schema only requires a non-empty string),
and the check rules the schema cannot express: one operator per check and
only the operators its type takes, selectors, the value kind per type,
regular expressions, comparisons, `between` order and unique check
labels.

### Error size

Errors are bounded: at most 1000 paths or problems are kept (the rest are
counted in `Omitted`), messages list at most 100, and echoed keys and
values are cut to about 128 bytes. `V1TypeProblems` keeps one problem per
field and message, with a `Count`.

## Thresholds

A v2 definition can carry `thresholds` on the test, on each scenario and
on each step: pass/fail rules for the run. Each threshold is either an
expression string or an object with abort settings:

```json
"thresholds": [
  "p95 < 500ms",
  "error_rate < 1%",
  {"expr": "p99 < 2s", "abort_on_fail": true, "delay_abort": "30s"}
]
```

An expression is `<metric> <operator> <value>`. Spaces around the parts
are optional (`p99<=1s`); the value has no space before its unit
(`500ms`). Operators: `<`, `<=`, `>`, `>=`, `==` (no `=` or `!=`). A value
is digits with an optional fraction, then a unit: no sign, exponent or
leading dot, and a non-zero value must not round to zero. Units are ASCII
and case-insensitive, plus `µs` (micro sign) or `μs` (lowercase Greek mu)
for microseconds; Greek capital Mu is rejected because it reads as an ASCII
`M`. Metric names are lowercase.

| Metrics | Kind | Value |
|---|---|---|
| `avg` `min` `max` `p50` `p75` `p90` `p95` `p99` `p99.9` (request duration) | duration | `us` (`µs`), `ms`, `s`, `m`; unit required, no `ns` |
| `ttfb`, `ttfb_<agg>` (time to first byte) | duration | as above |
| `iteration_duration`, `iteration_duration_<agg>` | duration | as above |
| `error_rate`, `check_rate` | rate | a percentage: `%` required, at most 100% |
| `requests`, `failed`, `iterations`, `dropped_iterations` | count | a plain whole number |
| `rps` | throughput | a plain number (requests per second) |
| `bytes_sent`, `bytes_received` | size | `b`, `kb` `mb` `gb` (×1000), `kib` `mib` `gib` (×1024), or a plain number of bytes; the value must be a whole number of bytes |

`<agg>` is any of the request-duration names (`ttfb_p95`, `ttfb_p99.9`,
`iteration_duration_max`); plain `ttfb` and `iteration_duration` are the
average. Only the listed percentiles exist. `==` is exact for counts and
sizes (`dropped_iterations == 0` holds only when nothing was dropped,
`requests == 1000000000` fails at 1000000001); for durations, rates and
`rps` it allows for floating point: `|a-b| <= 1e-9 * max(1, |a|, |b|)`.

Scopes: every metric is allowed on the test, a scenario and a step,
except the iteration metrics (`iteration_duration*`, `iterations`,
`dropped_iterations`), which are allowed on the test and scenarios only.

`abort_on_fail` asks the engine to stop the run when the threshold fails.
`delay_abort` defers that abort: abort checks for the threshold start
`delay_abort` after the run starts. The threshold is still evaluated
during the delay and still decides the final verdict; only the abort is
deferred. The engine defines the exact run-start clock. `delay_abort` is a
Go duration such as `"30s"` or `"1m30s"`, like the other durations, must
not be negative, and requires `abort_on_fail: true`. Both are accepted by
the format; whether an engine acts on them depends on the engine release
(see [Compatibility](#compatibility)).

Problems with an expression are reported at `thresholds[i]` for the string
form and at `thresholds[i].expr` for the object form.

### Results

`thresholds.Evaluate` checks the bounds against a snapshot of the run's
measurements (per test, scenario and step) and returns one result per
threshold:

```json
[
  {"scope": "test", "expr": "p95 < 500ms", "actual": 412.5, "unit": "ms", "status": "passed", "passed": true},
  {"scope": "scenario", "scenario": "Browse", "expr": "error_rate < 1%", "actual": 1.6, "unit": "%", "status": "failed", "passed": false},
  {"scope": "step", "scenario": "Browse", "step": "Home", "expr": "p99 < 1s", "actual": null, "unit": "ms", "status": "no_data", "passed": false}
]
```

- `actual` is in `unit`: `ms` for durations (to the microsecond), `%` for
  rates, `bytes` for sizes, `req/s` for `rps`, and `""` for counts.
- `status` is `passed`, `failed` or `no_data`. `no_data` (with a null
  `actual`) means there was nothing to measure: no requests for a rate, no
  values for a duration, or a counter the engine did not measure. A
  scenario or step missing from the results is `no_data` for every
  metric, counts included, so engines report every scope of the
  definition, with zeros when nothing happened. The run passes only if
  every threshold passed, so `no_data` fails the final verdict; but it
  never aborts a running test.
- Percentiles come from the scope's HDR histogram (microseconds, 1µs to
  1h, 3 significant figures). Rates, averages and `rps` are computed from
  the scope's own counters over the run's wall-clock window, so a run
  merged from several nodes gets the run's numbers, not averages of the
  nodes' numbers. `rps` uses that whole window at every scope, so a
  scenario that ran for part of the test shows a diluted rate.
- A percentile reads the top of its HDR bucket, up to 0.1% above the
  recorded values (500ms exactly reads as 500.223ms), so compare
  latencies with `<` or `<=` and some headroom, not `==`.

In Go, `definition.ThresholdConfig` holds a threshold as written,
`thresholds.Parse` reads one expression, and `definition.Bounds` returns
every threshold of a parsed definition as a `thresholds.Bound` with the
scenario and step it applies to. Values are held in canonical units:
microseconds, fractions (1% is 0.01) and bytes.

## Checks

A v2 step can carry `checks`: assertions on its response. Each check has
a `type`, a `selector` when the type needs one, exactly one operator, and
optional `name` and `on_fail`.

```json
"checks": [
  {"type": "status", "in": [200, 201]},
  {"type": "header", "selector": "Content-Type", "contains": "application/json"},
  {"type": "json", "selector": "$.data.items[0].id", "equals": 42},
  {"type": "timing", "duration": "< 500ms", "on_fail": "abort"}
]
```

| Type | Selector | Operators | Value of `equals` / `contains` |
|---|---|---|---|
| `status` | none | `in`, `not_in`, `between` | |
| `header` | the header name | `equals`, `contains`, `exists`, `matches` | a string |
| `body` | none | `equals`, `contains`, `not_contains`, `matches`, `size`, `empty` | a string |
| `json` | a JSON path, e.g. `$.data.items[0].id` | `equals`, `exists`, `missing`, `is`, `count`, `matches`, `contains` | a string, number or `true`/`false` |
| `html` | a CSS selector | `exists`, `equals` | a string |
| `timing` | none | `duration` | |
| `url` | none | `equals`, `contains`, `matches` | a string |
| `cookie` | the cookie name | `exists` | |

- `in` and `not_in` are lists of status codes; `between` is `[low, high]`,
  both included, with `low <= high`. Codes run from 100 to 599.
- `matches` is a regular expression in RE2 syntax and must compile.
  `contains` must not be `""` for `header`, `body` and `url` (an empty
  string is in every value); `json` allows it.
- `count` (a JSON array's length), `size` (the decoded response body, in
  bytes) and `duration` (the total request time, the same measurement as
  the duration thresholds) are comparisons such as `"> 0"`, `"< 50kb"` or
  `"< 500ms"`. They use the operators, value forms, units and `==` rule of
  [threshold expressions](#thresholds) of the same kind, without the
  metric; `thresholds.ParseComparison` reads them.
- `is` is one of `array`, `object`, `string`, `number`, `bool`, `null`.
- `header` names are case-insensitive; when a header repeats, any of its
  values may match. `cookie` reads the `Set-Cookie` headers of this
  response. `url` is the final URL after redirects (the request URL when
  redirects are not followed).
- On `json`, `equals` compares scalars only (an object or array never
  equals), `count` applies to arrays, `matches` to strings and `contains`
  to arrays; any other JSON kind at the path fails the check. A number
  keeps its literal as written, so a 19-digit id loses no digits; a
  number outside float64 range, such as `1e999`, is rejected.
- On `json`, a string equals a string with the same text, `true`/`false`
  the same value, and a number a number with the same exact decimal value:
  `42` equals `42.0` and `4.2e1`, with no float64 rounding, so
  `1234567890123456789` does not equal `1234567890123456788`. A string
  never equals a number or `true`/`false`: `"42"` does not equal `42`.
  `contains` compares each array element the same way.
- On `html`, `equals` compares the text content of the first element the
  selector matches (the text of the element and its descendants, without
  tags), with leading and trailing white space trimmed: the same value an
  `html` extractor without `attribute` reads. `<h1> <b>Sale</b> </h1>`
  equals `"Sale"`. `exists` passes when any element matches.
- Check values and selectors are literal: a `${variable}` in them is
  compared as written, not replaced by the variable's value.
- When no response is received (a transport error or a timeout), the
  step's checks are not evaluated and not counted.

### Zero values

A key with its zero value counts as absent: `""` for `name`, `selector`,
`on_fail`, `not_contains`, `matches`, `is`, `count`, `size` and
`duration`, and `[]` for `in`, `not_in`, `between` and
`expected_statuses`. Marshalling a parsed definition leaves them out. Two
exceptions:

- `equals` and `contains` are values whenever they are written:
  `"equals": ""` checks for an empty value.
- `exists`, `missing` and `empty` must be `true` when written; `false` is
  rejected (use `missing` instead of `exists: false` on a JSON path).

### on_fail

| `on_fail` | A failure… |
|---|---|
| `record` | counts toward `check_rate` only; it does not fail the run |
| `fail` | counts toward `check_rate` and fails the run |
| `abort` | counts toward `check_rate`, fails the run, and ends the virtual user's current iteration once the step's other checks have been evaluated |

The default is `fail` for `status` checks and `record` for every other
type. `abort` ends the iteration, not the test: the test goes on, unlike
a threshold's `abort_on_fail`, which stops the test. Record-mode failures
still lower `check_rate`, so a `check_rate` threshold can fail the run on
them.

### Required extractors

An extractor with `"required": true` turns a miss (nothing found) into a
failed check labelled `extract <variable_name>`, with `on_fail` defaulting
to `abort`. `on_fail` is rejected on an extractor without
`"required": true`. Required or not, an extractor always assigns its
variable; a miss assigns `""`.

A required extractor counts as one check on every response its step
evaluates: a hit passes, a miss fails. An extractor without
`"required": true` is never counted. When no response is received,
required extractors are not counted, like checks. When a check or a
required extractor with `on_fail: "abort"` fails, the step's other checks
and extractors are still evaluated, and every extractor assigns its
variable, before the iteration ends.

### expected_statuses

`expected_statuses` on the test lists the status codes (100 to 599) that
count as a successful request. It applies to every step without a status
check whose `on_fail` is `fail` or `abort`; such a check decides for its
own step instead. A status check with `on_fail: "record"` does not change
which responses count as failed requests.

Without `expected_statuses` (or with `[]`) the engine decides. Its
current rule is that a status from 200 to 499 is a success and any other
status, such as a 5xx, is a failed request. This may change when the v2
default is decided.

### Identity

Results name each check by its label, which is unique within its step
(`Parse` rejects duplicates). `Check.Label` returns it:

- a `name` with a non-space character, as written (names are not cut);
- otherwise a label derived from the decoded check,
  `<type>[ <selector>] <operator>[ <value>]`:

  ```text
  status in [200,201]
  json $.data.items[0].id equals 42
  html h1 equals "Sale"
  timing duration < 500ms
  header Authorization equals
  ```

  The selector is written as it is. String values are JSON-encoded
  without HTML escaping, numbers keep their literal, status codes are
  written as a JSON array, and comparisons and `is` are written as they
  are. `exists`, `missing` and `empty` have no value. A derived label is built from decoded values, so it is the same
  however the JSON was escaped, and it is cut to 128 bytes on a UTF-8
  boundary.
- **Header labels leave the value out** for every operator, so header
  values such as tokens never reach results. Two header checks on one
  header with the same operator get the same label and need names.
- **Every other derived label contains the expected value** as written:
  a URL with a token in its query string, a JSON value or a `matches`
  pattern reaches results as written. Give a check whose value is
  sensitive a `name`.

Uniqueness is checked on the cut label, the string results carry. A
required extractor counts as a check labelled `extract <variable_name>`,
so two required extractors for one variable in a step collide, and so
does a check named `extract <variable_name>`.

A check in results is identified by its scenario's name, its step's name
and its label together. Each is unique at its level (scenario names in
the test, step names in their scenario, labels in their step), so a label
alone is not: steps in different scenarios can share one. Results also
carry the check's `type`, which describes the check and is not part of
the key.

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
