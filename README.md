# VoltTest contract

The public VoltTest test-definition format and the verdict rules
(thresholds) that decide whether a run passes. Tools that write or read
VoltTest test definitions can depend on this module. The VoltTest engine
itself is proprietary and is not part of this repository.

## Packages

| Package | Contents |
|---|---|
| `definition` | Test-definition types, `Parse`, schema_version checks, VoltTest Cloud refusals |
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
  minor. [Checks](#checks), `expected_statuses`, the extractor keys
  `required`, `on_fail` and [`pick`](#extracting-from-arrays), and the
  [HTTP and TLS options](#http-and-tls-options) (`http`, `tls` and a
  step's `follow_redirects` and `max_redirects`), the [arrival-rate
  load](#arrival-rate-load) (`load`), [setup and
  teardown](#setup-and-teardown) scenarios (`run_once`, `teardown`),
  [once-per-VU steps](#once-per-vu-steps) (`once_per_vu`), typed
  [request bodies](#request-bodies) (`body_type`, `body_json`, `fields`),
  multipart and binary bodies (`body_type: "multipart"`, `files`,
  `body_base64`), [header values](#headers) given as arrays, a request's
  `timeout`, the [response body](#response-bodies) keys
  (`http.max_body_size`, `http.discard_bodies`, a step's `discard_body`
  and `response_type`) and the [connection timeouts](#timeouts)
  (`http.connect_timeout`, `http.tls_handshake_timeout`,
  `http.response_header_timeout`), a [scenario's own
  load](#scenario-load) (a scenario's `load` and `start_at`, and the
  `constant` and `stages` load types), [ramp start and graceful
  stop](#ramp-start-and-graceful-stop) (`initial_virtual_users`,
  `stop_grace` and `ramp_down_grace`), [preset cookies and
  variables](#cookies-and-variables) (`cookies` at every level, a step's
  `clear_cookies`, `vars`), [JSON data files](#json-data-files)
  (`data_format: "json"`) and [dynamic steps](#dynamic-steps) (a step's
  `skip_if`, `only_if`, `repeat`, `foreach` and `steps`, the `assert`
  check type with `expr`, and `${expr: ...}` in templates) joined `"2.0"`
  under this rule.
- A reader rejects an unknown major, or a minor newer than it supports.

The module version and `schema_version` move independently: schema `"2.0"`
does not mean module v2.

### v1 and v2

`definition.Parse` reads both formats.

- **v2** is strict. Every key must be defined for the definition's
  `schema_version` (exact spelling and case), values must have the right
  JSON type, and the structure is validated:
  - a name, and at least one scenario with at least one step;
  - unique scenario names, and unique step names within a scenario,
    counting step groups and the steps inside them;
  - durations that parse ("30s", "1m30s"), and a `think_time` that is a
    duration or a range such as "1s-3s" (see [Think time](#think-time));
  - a load model: non-empty `stages`, `virtual_users` (at least 1) with
    optional `duration` and `ramp_up`, or a `load` object (see
    [Arrival-rate load](#arrival-rate-load)), exactly one of them, or none
    of them when every load scenario has its own `load` (see [Scenario
    load](#scenario-load)). Without `duration` each virtual user runs its
    scenario once; a `duration` that is present must be greater than
    zero, and so must every stage's `duration`; `initial_virtual_users`
    (0 or more) and `ramp_down_grace` only with `stages`, and
    `stop_grace` with any model (see [Ramp start and graceful
    stop](#ramp-start-and-graceful-stop));
  - a scenario's own `load`: a known type with at least one virtual user,
    a rate of at least 1 or a stage target (or `initial_virtual_users`)
    above 0, no `weight`, not on a `run_once` or `teardown` scenario, and
    `start_at` (a duration greater than zero) only next to it (see
    [Scenario load](#scenario-load));
  - `run_once` and `teardown` not both on one scenario, no `weight` on
    either, at least one load scenario, and no other scenario extracting a
    variable a `run_once` scenario sets (see [Setup and
    teardown](#setup-and-teardown));
  - `once_per_vu` not on a step of a `run_once` or `teardown` scenario,
    and at least one step without it in every load scenario (see
    [Once-per-VU steps](#once-per-vu-steps));
  - known extractor types, data formats and data modes, `has_header` only
    with `data_format: "csv"`, and an
    extractor's `pick` from its list, on `json`, `regex` and `html`
    extractors only (see [Extracting from arrays](#extracting-from-arrays));
  - cookie names that are HTTP tokens, cookie values of cookie-octets
    outside `${...}` references and without CR, LF or NUL anywhere,
    `clear_cookies` not in a scenario with `auto_handle_cookies: false`,
    and variable names of ASCII letters, digits, `_` and `-`, other than
    `env` and `fake` (see [Cookies and variables](#cookies-and-variables));
  - thresholds: every expression parses, each metric is allowed at its
    scope, and `delay_abort` is a non-negative duration set only with
    `abort_on_fail: true` (see [Thresholds](#thresholds));
  - checks: a known type, exactly one operator that the type takes, a
    selector when the type needs one, values of the right kind, status
    codes from 100 to 599, and a unique label per step (see
    [Checks](#checks));
  - HTTP and TLS options: `http.version` and `tls.min_version` from their
    lists, `max_redirects` not negative and set on a step only when that
    step follows redirects, `client_cert` and `client_key` given together,
    inline PEM holding complete blocks of the right kind, and no blank or
    multi-line file path (see [HTTP and TLS
    options](#http-and-tls-options));
  - request bodies: a known `body_type`, `body` or `body_base64` (not
    both) only with `raw`, `body_json` (an object or array) only with and
    required by `json`, `fields` (string values, each name once) only with
    `form` or `multipart`, and `files` only with `multipart`, which needs
    a field or a file; each file has a `path` or `content_base64`, and no
    name is both a field and a file (see [Request bodies](#request-bodies));
  - headers: a string or a non-empty array of strings, one value for
    `Host` under any spelling, and no `Content-Type` with `multipart` (see
    [Headers](#headers));
  - response bodies and timeouts: `max_body_size` a size above zero and
    at most `1gib`, timeouts durations above zero, no check or extractor
    that reads a body `discard_body` drops, and no json or html check or
    extractor with `response_type: "binary"` (see [Response
    bodies](#response-bodies));
  - dynamic steps: non-blank expressions, not `skip_if` and `only_if`
    together, a `repeat` with `until` and a `max` from 1 to 1000, a
    `foreach` over one `${name}`, not `repeat` and `foreach` together, step
    groups one level deep with only `name`, `repeat` and `steps`, no loop
    in a `run_once` or `teardown` scenario, and every `${expr: ...}` in a
    template closed, not empty and without comments (see [Dynamic
    steps](#dynamic-steps)).
- **v1** is read the way the v1 readers (the CLI, the cloud engine and the
  backend) read it. Unknown keys are ignored, keys match case-insensitively,
  and v2-only keys (`target.base_url`, `thresholds` at every level,
  `expected_statuses`, `http`, `tls`, `load`, `initial_virtual_users`,
  `stop_grace`, `ramp_down_grace`, `cookies` and `vars` at every level, a
  scenario's `run_once`, `teardown`, `load` and
  `start_at`, a step's `checks`,
  `follow_redirects`, `max_redirects`, `once_per_vu`, `discard_body`,
  `response_type`, `clear_cookies`, `skip_if`, `only_if`, `repeat`,
  `foreach` and `steps`, a request's `body_type`,
  `body_json`, `fields`, `files`, `body_base64` and `timeout`, and an
  extractor's `required`, `on_fail` and `pick`) are dropped unread,
  whatever their value. `data_format` is a v1 field: v1 keeps any value,
  `"json"` included, as written, unchecked. A header value is
  a string in v1: an array is a type problem in a loose field, as below,
  exactly as before arrays joined v2. Values are checked for their JSON
  type only:
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
labels. For the HTTP and TLS options it also checks the rules that span
keys or levels (a step's `max_redirects` against the `follow_redirects`
in effect for it, `client_cert` and `client_key` together), the PEM
contents of `client_cert`, `client_key` and `ca_cert`, and blank or
multi-line file paths. For `load`, at both levels, it also checks that
durations are greater than zero and that `max_virtual_users` is at least
`preallocated_virtual_users`, for a scenario that `start_at` is a
duration greater than zero, and at both levels that `stop_grace` and
`ramp_down_grace` are durations of 0 or more. For
`fields` it also checks that each name appears once (a JSON Schema
validator reads a repeated key as its last value). For headers, files and
binary bodies it also checks the `Host` and `Content-Type` rules (header
names match in any case), media types, line breaks in part names, file
paths, file names and media types, a name used as both a field and a
file, base64 values and their total size; for response bodies,
`max_body_size` and the checks and extractors that `discard_body` and
`response_type` allow.

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
| `requests`, `failed`, `iterations`, `dropped_iterations`, `interrupted_iterations` | count | a plain whole number |
| `rps` | throughput | a plain number (requests per second) |
| `bytes_sent`, `bytes_received` | size | `b`, `kb` `mb` `gb` (×1000), `kib` `mib` `gib` (×1024), or a plain number of bytes; the value must be a whole number of bytes |

`<agg>` is any of the request-duration names (`ttfb_p95`, `ttfb_p99.9`,
`iteration_duration_max`); plain `ttfb` and `iteration_duration` are the
average. Only the listed percentiles exist. `==` is exact for counts and
sizes (`dropped_iterations == 0` holds only when nothing was dropped,
`requests == 1000000000` fails at 1000000001); for durations, rates and
`rps` it allows for floating point: `|a-b| <= 1e-9 * max(1, |a|, |b|)`.

`interrupted_iterations` counts the iterations cut off when a
`stop_grace` or `ramp_down_grace` runs out (see [Ramp start and graceful
stop](#ramp-start-and-graceful-stop)), and only there, not in
`iterations` or `iteration_duration`. The request such an iteration had
in flight is not recorded: it counts in neither `requests` nor `failed`,
so not toward `error_rate`, nor in `duration`, `ttfb` or the byte
counters.

Scopes: every metric is allowed on the test, a scenario and a step,
except the iteration metrics (`iteration_duration*`, `iterations`,
`dropped_iterations`, `interrupted_iterations`), which are allowed on the
test and scenarios only.

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

### Snapshots on the wire

A run on several nodes is judged on the nodes' measurements merged:
each node sends a snapshot, and the reader merges them
(`thresholds.Merge`) and evaluates the result. `thresholds.EncodeSnapshot`
writes a snapshot as JSON and `thresholds.DecodeSnapshot` reads it back:

```json
{
  "start": "2026-10-16T12:00:00Z", "end": "2026-10-16T12:01:00Z", "responses": 1000,
  "test": {"requests": 1000, "failed": 2, "bytes_sent": 50000, "bytes_received": 900000,
           "checks": 2000, "checks_passed": 1998,
           "duration": {"count": 1000, "sum_us": 41250000, "min_us": 1200, "max_us": 980000, "hdr": "HISTFAAA..."}},
  "scenarios": {"Browse": {"requests": 1000, "failed": 2, "bytes_sent": 50000, "bytes_received": 900000,
                           "steps": {"Home": {"requests": 1000, "failed": 2, "bytes_sent": 50000, "bytes_received": 900000}}}}
}
```

- `requests`, `failed`, `bytes_sent` and `bytes_received` are required in
  every scope. `checks`, `checks_passed`, `iterations`,
  `dropped_iterations` and `interrupted_iterations` are left out when not
  measured, which is not the same as 0.
- `duration`, `ttfb` and `iteration_duration` are left out when there
  are no values. When present they need `count` (above 0), `sum_us`,
  `min_us` and `max_us`; `hdr` is the scope's HDR histogram as a JSON
  string, in the encoding of `thresholds.EncodeHistogram`.
- Keys are lower case, and unknown keys are ignored. Every key other
  than a scenario or step name is plain ASCII of at most 32 bytes,
  without escapes. Each key of the wire form appears once at most in
  its object, matched ignoring ASCII case; an unknown key may repeat.
- `DecodeSnapshot` is safe on input from another process. Its
  `SnapshotLimits` cap the size (32 MiB by default), the scopes (1024),
  the histograms (256) and the name length (4096 bytes), and are checked
  before any histogram is decoded. It refuses input that is not valid
  UTF-8, a name used twice, a missing required key and a scope that
  breaks the snapshot invariants.

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
| `assert` | none | `expr` (see [Dynamic steps](#dynamic-steps)) | |

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
  values may match. `cookie` reads the `Set-Cookie` headers of the
  response and of every redirect followed to reach it, a later one
  winning for the same name. `url` is the final URL after redirects (the
  request URL when redirects are not followed). See [Redirects and
  checks](#redirects-and-checks).
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
- A `json` selector is an optional `$.` prefix, then keys separated by
  `.`: `$.key`, `$.key[0]` or `$.a.b[1].c`, with `N` in `[N]` a decimal
  index. A key before `[N]` uses only letters, digits, `_` and `-`; any
  other key is any text but `*` without `.`, `[`, `]` or white space.
  `$` alone, wildcards, filters, bracket keys (`$['a']`), `..`, and
  negative or chained indexes (`a[0][1]`) are refused. `Parse` accepts
  any non-blank selector; `definition.JSONSelectorProblems(def)` lists
  the ones the VoltTest engine refuses, at `checks[k].selector`.
- Check values and selectors are literal: a `${variable}` in them is
  compared as written, not replaced by the variable's value.
- When no response is received (a transport error or a timeout), the
  step's checks are not evaluated and not counted.

### Zero values

A key with its zero value counts as absent: `""` for `name`, `selector`,
`on_fail`, an extractor's `pick`, `not_contains`, `matches`, `is`, `count`, `size` and
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

An extractor with `"required": true` turns a miss (nothing found, or a
JSON `null`) into a failed check labelled `extract <variable_name>`, with `on_fail` defaulting
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

### Extracting from arrays

`pick` tells a `json`, `regex` or `html` extractor which value to take
when its selector finds several, or finds an array: `random`, `first`,
`last` or `all`. It is rejected on `header` and `cookie` extractors. A
JSON path can match every element of an array with `[*]`, as in
`$.items[*].id`.

A `json` extractor's selector takes the [check grammar](#checks) plus
`key[*]` (`$.items[*].id`, `$.a[*].b[*]`), and `$[0]` or `$[*]` on a
document that is an array. A key before `[*]` follows the `[N]` rule.
`definition.JSONSelectorProblems(def)` lists the selectors outside this
grammar, at `extract[k].selector`; it returns nil for a v1 definition,
whose extractors keep the v1 rules.

```json
"extract": [
  {"variable_name": "ids", "selector": "$.items[*].id", "type": "json", "pick": "all"},
  {"variable_name": "item_id", "selector": "$.items[*].id", "type": "json", "pick": "random"},
  {"variable_name": "answer", "selector": "$.answers", "type": "json", "pick": "last"}
]
```

- `random` draws one candidate with the virtual user's own random
  generator, so virtual users draw independently of each other.
- `all` takes every candidate as a JSON array, such as `[3,7,9]`.
- `pick: all` keeps the whole list in each virtual user's variables until
  the variable is next assigned; a large list at a high virtual-user count
  costs memory.

**Hits and misses on `json`.** A JSON `null` is a miss and assigns `""`.
A JSON `""` is a hit. **Candidates** are the values `pick` chooses from:
`null` elements are never candidates, and they are dropped before
`first`, `last`, `random` and `all`, so `all` holds exactly the values
`random` draws from.

| The path finds | `all` | `first`, `last`, `random` | no `pick` |
|---|---|---|---|
| nothing, or `null` (path without `[*]`) | miss | miss | miss |
| an array (path without `[*]`) | the candidates as an array, a hit even when there are none (`[]`) | one candidate; no candidates is a miss | the array as it is, as compact JSON |
| a scalar or an object (path without `[*]`) | `[v]` | the value | the value |
| at least one candidate (path with `[*]`) | every candidate, in document order | one candidate | the first candidate |
| no candidates (path with `[*]`), including matches that are all `null` | `[]` (a hit) when the container before the first `[*]` is an array; otherwise a miss | miss | miss |

So `{"answers": []}` with `$.answers` gives `[]` for `all` and for no
`pick`, and a miss for `first`. `{"items": []}` with `$.items[*].id` and
`all` gives `[]`, while `{}` and `{"items": null}` are a miss.

**`regex` and `html`.** For `regex`, the candidates are group 1 of every
match; an empty group 1 is a hit, as in v1, and a pattern without a group
never hits. Without `pick`, `regex` reads the first match. For `html`, the
candidates are the trimmed text, or the `attribute`, of every element the
CSS selector matches; an empty value is never a candidate. Without `pick`,
`html` reads the first matching element, as in v1, and an empty value
there is a miss. When nothing matches, `regex` and `html` miss for every
`pick`, `all` included: unlike a JSON array, a regex or CSS match has no
container that tells an empty list from an absent one.

**`required`** fails exactly on a miss under these rules. With `all`, that
is a path that is missing or `null`, a container before the first `[*]`
that is missing or isn't an array, or a regex or CSS selector that matches
nothing.

**Values.** A v2 extractor keeps the JSON type of what it found and
renders it into `${variable}` as follows: a string as it is, a number
exactly as written (`1000000` stays `1000000`, and a 19-digit id keeps
every digit), `true` or `false`, and an array or object as compact JSON
with sorted keys and no HTML escaping (`<` and `&` stay as they are). An
empty array renders as `[]`.

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

## HTTP and TLS options

A v2 definition can carry `http` and `tls` objects at the top level. Both
are optional, every key in them is optional, and `{}` changes nothing.
`""` and `0` count as absent; `insecure_skip_verify: false` is its
default. `follow_redirects: false` is a value, not absent: it is kept and
turns following off.

```json
"http": {"follow_redirects": true, "max_redirects": 5, "version": "1.1"},
"tls": {
  "min_version": "1.2",
  "client_cert": "certs/client.pem",
  "client_key": "certs/client-key.pem",
  "ca_cert": "certs/staging-ca.pem"
}
```

| Key | Default | Meaning |
|---|---|---|
| `http.follow_redirects` | `true` | Follow 3xx redirects. |
| `http.max_redirects` | `10` | Most redirects followed for one request; a request that needs more fails. `0` means the default. |
| `http.version` | negotiated | `"1.1"` forces HTTP/1.1. Absent, HTTP/2 is used over TLS when the server offers it, else HTTP/1.1. `"1.1"` is the only value. |
| `http.max_body_size` | `"1mib"` | Most bytes of a response body kept in memory for checks and extractors, at most `"1gib"`. See [Response bodies](#response-bodies). |
| `http.discard_bodies` | `false` | Read and drop response bodies without keeping them, except on steps that read the body. See [Response bodies](#response-bodies). |
| `http.connect_timeout` | `"10s"` | Most time to open a TCP connection. |
| `http.tls_handshake_timeout` | `"10s"` | Most time for the TLS handshake. |
| `http.response_header_timeout` | none | Most time to wait for the response headers once the request is sent. |
| `tls.insecure_skip_verify` | `false` | Skip verification of the target's certificate, e.g. a staging server with a self-signed certificate. |
| `tls.min_version` | `"1.2"` | Lowest TLS version accepted: `"1.0"`, `"1.1"`, `"1.2"` or `"1.3"`. The old versions are there to test legacy targets. |
| `tls.client_cert` | none | Client certificate for mutual TLS: one or more `CERTIFICATE` blocks. Requires `client_key`. |
| `tls.client_key` | none | The client certificate's private key, unencrypted. Requires `client_cert`. |
| `tls.ca_cert` | none | Certificates trusted for the target in addition to the system roots: one or more `CERTIFICATE` blocks. |

`http_timeout` and `http_debug` stay at the top level; there is no
`http.timeout` or `http.debug`.

These keys joined `"2.0"` while it is a preview (see
[schema_version](#schema_version)). v1 definitions drop them unread.

### Timeouts

- `http_timeout` bounds each whole request, redirects and the body
  included. A request's own `timeout` replaces it for that request:

  ```json
  {"name": "Search", "request": {"method": "GET", "url": "/search?q=a", "timeout": "2s"}}
  ```

- `http.connect_timeout` and `http.tls_handshake_timeout` bound opening a
  connection; `http.response_header_timeout` bounds the wait for the
  status line and headers after the request is sent.
- Every timeout is a duration greater than zero; `""` means absent.

### Response bodies

```json
"http": {"max_body_size": "4mib"},
"steps": [
  {"name": "Download", "request": {"method": "GET", "url": "/report.pdf"},
   "discard_body": true, "checks": [{"type": "status", "in": [200]}, {"type": "body", "size": "> 0"}]},
  {"name": "Logo", "request": {"method": "GET", "url": "/logo.png"},
   "response_type": "binary", "checks": [{"type": "body", "matches": "PNG"}]}
]
```

- **`http.max_body_size`** is a size with the units of a size threshold
  (`b`, `kb`, `mb`, `gb`, `kib`, `mib`, `gib`, or a plain number of
  bytes; `thresholds.ParseSize` reads it), greater than zero and at most
  `1gib`. Up to that many bytes of each body are kept; the rest is read
  and dropped so the connection can be reused.
- **A step reads its body** when it has a `body` check with `equals`,
  `contains`, `not_contains` or `matches`, a `json` or `html` check, or a
  `json`, `regex` or `html` extractor (`Step.ReadsBody`). A `body` check
  with `size` or `empty`, `status`, `header`, `timing`, `url` and `cookie`
  checks, and `header` and `cookie` extractors do not read it.
- **`discard_body`** on a step overrides `http.discard_bodies` for that
  step: `true` reads and drops its body without keeping it, and `Parse`
  refuses it on a step that reads its body; `false` keeps the body.
- **`http.discard_bodies: true`** drops the body of every step that sets
  no `discard_body`, except a step that reads its body, which keeps it.
- **`response_type: "binary"`** on a step keeps the body as bytes: `body`
  checks and `regex` extractors read the raw bytes. `json` and `html`
  checks and extractors are refused on that step.
  `"text"` (or absent) is the default.

### Headers

A header value is a string, or an array of strings to send the header
once per value, in order:

```json
"headers": {"Accept": ["text/html", "application/json"], "Host": "shop.example.com"}
```

- An array must not be empty. One value, as a string or a one-element
  array, is written back as a string.
- `Host` sets the host the request is sent as, while it still goes to
  the URL's host: a test can target an IP with the site's name. It takes
  exactly one value, and only one key may name it, in any case (`Host`
  and `host` together are refused).
- `User-Agent` replaces the default one.
- In v1 a header value is a string; see [v1 and v2](#v1-and-v2).

### Redirects

A step can override the test-level values with its own `follow_redirects`
and `max_redirects`. There is no scenario-level setting, and a request
object takes neither key.

```json
{
  "name": "Old link",
  "request": {"method": "GET", "url": "/old"},
  "follow_redirects": false,
  "checks": [{"type": "status", "in": [301]}]
}
```

- Turn following off with `follow_redirects: false`. `max_redirects: 0`
  means absent, not "no redirects": the default of 10 at the test level,
  and the test-level maximum (else 10) on a step. A negative value is
  rejected.
- Whether a step follows redirects comes from its own
  `follow_redirects`, else `http.follow_redirects`, else the default
  (`true`). A step's
  `max_redirects` (above 0) is rejected when that step does not follow
  redirects. At the test level, `follow_redirects: false` with
  `max_redirects` is allowed: steps that set `follow_redirects: true`
  use that maximum.
- `max_redirects` has no upper limit. `http_timeout` bounds the whole
  request, redirects included.

### Redirects and checks

With redirects followed:

- `status`, `header`, `body`, `json` and `html` checks, and `header`,
  `json`, `regex` and `html` extractors, read the final response;
- `url` is the final URL;
- `cookie` checks and extractors read the `Set-Cookie` headers of the
  final response and of every redirect followed to reach it, a later one
  winning for the same name. A login that sets a session cookie on its
  302 still passes a `cookie` check.

With redirects not followed, checks and extractors see the 3xx response
itself: its status, its headers (the `Location` header included) and its
body, and `url` is the request URL.

### Certificates and keys

`client_cert`, `client_key` and `ca_cert` each take a file path or inline
PEM under the same key:

- A value is inline PEM when it contains `-----BEGIN` anywhere; any
  other value is a file path. `definition.IsInlinePEM` is this rule, so
  every reader tells the two forms apart the same way. A path must not
  be blank or contain a line break.
- Text before the first PEM block, such as comment lines, is allowed,
  but each block's `-----BEGIN` must be at the start of a line.
- Inline PEM must hold at least one complete block of the right kind:
  `CERTIFICATE` blocks, and no private key, for `client_cert` and
  `ca_cert`; a private key block for `client_key`. Encrypted private keys
  are not supported: there is no passphrase key. `Parse` checks the PEM
  structure only; the certificate and key are loaded, and matched, when
  the run starts.
- A path is a file on the machine that runs the test. A relative path
  resolves against the working directory of the process that runs the
  test, not the definition's directory: a definition can arrive on
  standard input and have none. VoltTest Cloud runs need the inline form.
- An inline `client_key` is stored with the definition as written, like
  every other value. Prefer a path for local and CI runs.
- With `insecure_skip_verify: true`, `ca_cert` is accepted but has no
  effect: verification is skipped.
- `Parse` errors for these three keys never repeat the value.

## Arrival-rate load

With `virtual_users` or `stages`, each virtual user starts its next
iteration when the last one ends, so a slower target gets fewer requests.
A `load` object instead starts iterations at a set rate, whatever the
response times. It replaces `virtual_users`, `duration`, `ramp_up` and
`stages`, which must then be absent (or written with their zero value).

```json
"load": {
  "type": "rate",
  "rate": 500,
  "time_unit": "1s",
  "duration": "5m",
  "preallocated_virtual_users": 200,
  "max_virtual_users": 1000
}
```

```json
"load": {
  "type": "ramping_rate",
  "time_unit": "1m",
  "stages": [
    {"duration": "2m", "target": 600},
    {"duration": "5m", "target": 600},
    {"duration": "1m", "target": 0}
  ],
  "preallocated_virtual_users": 50
}
```

| Key | Applies to | Meaning |
|---|---|---|
| `type` | both, required | `"rate"` or `"ramping_rate"`. |
| `rate` | `rate`, required | Iterations started per `time_unit`, at least 1. |
| `duration` | `rate`, required | How long the test runs, greater than zero. |
| `stages` | `ramping_rate`, required | At least one `{duration, target}`. The rate starts at 0 and moves linearly to each stage's `target` (0 or more) over that stage's `duration` (greater than zero). At least one `target` must be above 0. |
| `time_unit` | both | The period `rate` and `target` count over. Default `"1s"`; must be greater than zero. |
| `preallocated_virtual_users` | both, required | Virtual users started before the test begins, at least 1. |
| `max_virtual_users` | both | Most virtual users the test may run, at least `preallocated_virtual_users`. `0` or absent means `preallocated_virtual_users`. |

- `"rate"` takes no `stages`, and `"ramping_rate"` takes no `rate` or
  `duration`; each may still be written with its zero value.
- An iteration that is due starts on an idle virtual user. When none is
  idle and fewer than `max_virtual_users` run, a new one starts;
  otherwise the iteration is dropped and counted.
- A `"ramping_rate"` load whose every stage `target` is 0 would start no
  iterations, so `Parse` refuses it at `load`: "never exceeds a rate of 0,
  so the run would do nothing; set a stage target above 0". A negative
  `target` is reported at that stage instead.
- There is no `start_rate`.
- `Parse` leaves the defaults to the reader: `time_unit` stays `""` and
  `max_virtual_users` stays `0`.
- The test-level `load` takes `"rate"` and `"ramping_rate"` only.
  `"constant"` and `"stages"` are for a [scenario's own
  load](#scenario-load); at the top level `Parse` refuses them at
  `load.type`.
- The test-level `load` takes no `stop_grace` of its own: the top-level
  `stop_grace` applies to it, and `Parse` refuses `load.stop_grace` (see
  [Ramp start and graceful stop](#ramp-start-and-graceful-stop)).

## Scenario load

A scenario with a `load` runs its own pool of virtual users (or its own
arrival rate) alongside the test-level load model, independent of it: the
test-level virtual users never pick it, and its virtual users run only
this scenario. `start_at` delays its start.

```json
"scenarios": [
  {"name": "Browse", "weight": 1, "steps": [...]},
  {
    "name": "Checkout",
    "start_at": "30s",
    "load": {"type": "constant", "virtual_users": 5, "duration": "2m"},
    "steps": [...]
  },
  {
    "name": "Spike",
    "load": {"type": "stages", "stages": [
      {"duration": "30s", "target": 50},
      {"duration": "30s", "target": 0}
    ]},
    "steps": [...]
  }
]
```

| `type` | Required | Meaning |
|---|---|---|
| `"constant"` | `virtual_users`, `duration` | `virtual_users` (at least 1) run the scenario for `duration` (greater than zero). There is no `ramp_up`; use `"stages"` to ramp. |
| `"stages"` | `stages` | The number of virtual users starts at `initial_virtual_users` (default 1) and moves linearly to each stage's `target` over that stage's `duration`. At least one `target`, or `initial_virtual_users`, must be above 0. |
| `"rate"`, `"ramping_rate"` | as in [Arrival-rate load](#arrival-rate-load) | The scenario's own arrival rate, with the same keys and rules. |

- Each type takes only its own keys: `"constant"` takes no `rate`,
  `time_unit`, `stages`, `preallocated_virtual_users`,
  `max_virtual_users`, `initial_virtual_users` or `ramp_down_grace`;
  `"stages"` no `rate`, `time_unit`, `duration`, `virtual_users`,
  `preallocated_virtual_users` or `max_virtual_users`; the arrival-rate
  types no `virtual_users`, `initial_virtual_users` or `ramp_down_grace`.
  Each may still be written with its zero value, except
  `initial_virtual_users`, where 0 is a value. Every type takes
  `stop_grace` (see [Ramp start and graceful
  stop](#ramp-start-and-graceful-stop)).
- There is no pool of 0 virtual users: a `"constant"` load needs at
  least 1, and a `"stages"` load whose every `target` is 0 is refused at
  `load`, unless `initial_virtual_users` is above 0: "never exceeds 0
  virtual users, so the scenario would do nothing; set a stage target or
  initial_virtual_users above 0". A pool that starts with virtual users
  may ramp to 0.
- **`start_at`** is how long after the load starts (after setup) the
  scenario's own load starts, as a duration greater than zero, with no
  upper bound; absent, it starts with the load. The scenario sends
  nothing before then. It needs a `load` on the same scenario.
- **Run length.** A run lasts until every scenario ends: the latest
  `start_at` plus the pool's length over the pools (`duration`, or the
  sum of the stage durations), and the test-level model's length for the
  scenarios that share it.
- **Weight.** A scenario with a `load` takes no `weight` (0 reads as
  absent).
- **Setup and teardown.** A `run_once` or `teardown` scenario takes no
  `load`.
- **Test-level model.** When every load scenario has its own `load`, the
  test-level `virtual_users`, `duration`, `ramp_up`, `stages`, `load`,
  `initial_virtual_users`, `stop_grace` and `ramp_down_grace` are left
  out, and `Parse` refuses each one that is present: "applies to no
  scenario: every load scenario has its own load". Otherwise the
  test-level model is required, as before, and runs the scenarios without
  a `load`. A key holding its zero value (`0`, `""`, `[]`) reads as
  absent, except `initial_virtual_users`, where 0 is a value.
  `Definition.UsesTestLoad` reports whether any load scenario uses the
  test-level model.
- **VoltTest Cloud.** On VoltTest Cloud the backend refuses, before the
  run starts, a pool whose peak in a region is below that region's node
  count, the peak being `virtual_users` for `"constant"`, the larger of
  `initial_virtual_users` and the highest stage `target` for `"stages"`,
  and `max_virtual_users` (or `preallocated_virtual_users` when
  `max_virtual_users` is absent) for `"rate"` and `"ramping_rate"`.
  `CloudProblems` does not refuse a scenario's `load` or `start_at`;
  whether VoltTest Cloud runs them is the backend's decision.

A scenario's `load` and `start_at` and the `"constant"` and `"stages"`
types joined schema_version "2.0" while it is a preview.

## Ramp start and graceful stop

A staged load can start with virtual users already running, and every
load gives its virtual users time to finish the iteration they are in
before they stop:

```json
{
  "schema_version": "2.0",
  "name": "Checkout under a spike",
  "stages": [
    {"duration": "1m", "target": 50},
    {"duration": "30s", "target": 10}
  ],
  "initial_virtual_users": 10,
  "ramp_down_grace": "15s",
  "stop_grace": "1m",
  "thresholds": ["interrupted_iterations == 0"],
  "scenarios": [
    {
      "name": "Browse",
      "steps": [{"name": "Home", "request": {"method": "GET", "url": "https://shop.example.com/"}}]
    },
    {
      "name": "Flash sale",
      "start_at": "30s",
      "load": {
        "type": "stages",
        "initial_virtual_users": 20,
        "stages": [{"duration": "1m", "target": 0}],
        "stop_grace": "0s"
      },
      "steps": [{"name": "Deal", "request": {"method": "GET", "url": "https://shop.example.com/deal"}}]
    }
  ]
}
```

| Key | Where | Default | Meaning |
|---|---|---|---|
| `initial_virtual_users` | the test level with `stages`, a `"stages"` load | `1` | Virtual users running when the load starts (for a pool, at its `start_at`). The first stage moves linearly from this number to its `target`; `0` ramps from zero. |
| `ramp_down_grace` | the test level with `stages`, a `"stages"` load | `"30s"` | How long a virtual user removed by a stage that lowers the target may finish its current iteration before it exits. |
| `stop_grace` | the test level with any model, a scenario's `load` of any type | `"30s"` | How long each virtual user may finish its current iteration once the load ends (its `duration` or last stage). No new iteration starts. |

- A grace is a duration of 0 or more; `"0s"` cuts at once. An iteration
  still running when its grace runs out is cancelled and counts in
  `interrupted_iterations` only, not in `iterations` or
  `iteration_duration` (see [Thresholds](#thresholds)). Its in-flight
  request is not recorded: it counts in neither `requests` nor `failed`,
  so not toward `error_rate`, nor in `duration`, `ttfb` or the byte
  counters.
- `initial_virtual_users` is not negative. An explicit `0` is a value,
  kept through `Parse` and `Marshal` (`Definition.InitialVirtualUsers` and
  `Load.InitialVirtualUsers` are `*int`; nil means the default). An
  absent grace is `""`. `Parse` leaves the defaults to the reader.
- `initial_virtual_users` and `ramp_down_grace` need `stages`: at the
  test level `Parse` refuses them next to `virtual_users` or `load`
  ("needs stages"), and on a scenario's `load` of another type ("does
  not apply to type ...").
- The test-level `stop_grace` applies to the scenarios without their own
  `load`, whatever the test-level model; without `duration` (each
  virtual user runs its scenario once) it has no effect. A scenario's
  own `load` never reads it: each pool has its own `stop_grace`, with
  the same default.
- **v1.** These keys are v2 only. A v1 staged load starts at 0 virtual
  users and stops without a grace, as before; the v1 path drops the keys
  unread.

## Cookies and variables

`cookies` presets cookies by name, on the test, a scenario or a step.
`vars` presets variables, on the test or a scenario, readable as
`${name}`.

```json
"cookies": {"locale": "en-GB", "consent": "all"},
"vars": {"currency": "EUR"},
"scenarios": [{
  "name": "Shopper",
  "cookies": {"locale": "de-DE"},
  "vars": {"currency": "CHF"},
  "steps": [
    {"name": "Home", "cookies": {"session": "${session_id}"},
     "request": {"method": "GET", "url": "https://shop.example.com/?c=${currency}"}},
    {"name": "Fresh session", "clear_cookies": true,
     "request": {"method": "GET", "url": "https://shop.example.com/login"}}
  ]
}]
```

- **Preset cookies.** A scenario's cookie replaces the test's of the
  same name, and a step's replaces both. How preset cookies interact with
  the cookie jar, a step's `Cookie` header and `clear_cookies`, and which
  hosts receive them, is defined by the engine's HTTP options
  documentation.
- **Cookie names** are HTTP tokens (letters, digits and
  ``!#$%&'*+-.^_`|~``; no spaces, `=`, `;` or other separators), sent as
  written, so a name cannot be a `${...}` reference.
- **Cookie values** are RFC 6265 cookie-octets in their literal text: no
  space, `"`, `,`, `;`, `\`, control character or non-ASCII. Text inside a
  `${...}` reference (from `${` to the first `}`, or a whole `${expr: ...}`
  expression, see [Templates](#templates)) is exempt; the engine checks
  the resolved value when it sends the request. CR, LF and NUL are
  refused anywhere. An empty value is allowed. The published schema lets
  a value with a `${expr:` through whole.
- **`clear_cookies: true`** on a step drops the cookies the virtual user
  kept from earlier responses before that step's request. `Parse` refuses
  it in a scenario with `auto_handle_cookies: false`, where it has no
  effect.
- **`auto_handle_cookies`** is on by default for schema_version 2.x;
  `auto_handle_cookies: false` opts out. v1 keeps it off unless set. An
  explicit `false` survives when a parsed definition is marshalled again,
  in v1 and v2; v1 readers decode the same value either way.
- **Variables** have the lowest precedence: a CSV column, an extracted
  value (setup globals included) or a built-in variable of the same name
  wins over a var. So a var named like an extractor's `variable_name` is
  a default until the value is extracted. A scenario's var replaces the
  test's of the same name. A name is ASCII letters, digits, `_` and `-`
  (no `.`, so no name looks like `${env.NAME}`); `env` and `fake` are
  reserved for the `${env.NAME}` and `${fake.NAME}` built-ins. A value may
  contain `${...}` references; when they resolve is defined by the
  engine.
- `{}` reads as absent at every level.

`cookies`, `clear_cookies` and `vars` joined schema_version "2.0" while
it is a preview.

## JSON data files

A scenario's `data_config` takes `data_format: "json"` as well as
`"csv"`: an array of objects, one per row, whose keys name the
variables. The modes (`sequential`, `random`, `unique`) and the
`data_source`, `data_content` and dispatch-only `data_download_url` keys
are the same. `has_header` applies to CSV only; `Parse` refuses it with
`"json"`. `Parse` does not read the file's content. `"json"` joined
schema_version "2.0" while it is a preview; v1 never checks
`data_format`.

## Setup and teardown

A scenario with `"run_once": true` is a setup scenario, and one with
`"teardown": true` is a teardown scenario. Neither is picked for the load:
each runs once per test, by one virtual user. The other scenarios are load
scenarios. (Leaving out `duration` is unrelated: it makes each virtual user
run its load scenario once.)

```json
"scenarios": [
  {
    "name": "Log in",
    "run_once": true,
    "steps": [{
      "name": "Login",
      "request": {"method": "POST", "url": "https://shop.example.com/api/login",
                  "headers": {"Content-Type": "application/json"},
                  "body": "{\"email\":\"admin@example.com\"}"},
      "extract": [{"variable_name": "admin_token", "selector": "$.data.token",
                   "type": "json", "required": true}]
    }]
  },
  {
    "name": "Browse",
    "steps": [{
      "name": "Products",
      "request": {"method": "GET", "url": "https://shop.example.com/api/products",
                  "headers": {"Authorization": "Bearer ${admin_token}"}}
    }]
  },
  {
    "name": "Clean up",
    "teardown": true,
    "steps": [{
      "name": "Delete seeded data",
      "request": {"method": "DELETE", "url": "https://shop.example.com/api/seed",
                  "headers": {"Authorization": "Bearer ${admin_token}"}}
    }]
  }
]
```

- **Setup.** Setup scenarios run before the load starts, one at a time in
  definition order, each by a fresh virtual user whose `${vu_id}` is 0
  (load virtual users start at 1). Their `data_config` and `think_time`
  apply as usual.
- **Globals.** The variables a setup scenario extracts become globals:
  read-only, and readable as `${name}` by every load virtual user and by
  the teardown scenarios. A later setup scenario sees the globals of the
  earlier ones.
- **Precedence.** A CSV column, then an extracted value (a global or one
  the virtual user extracted itself), then a built-in variable, then
  [`vars`](#cookies-and-variables) (a scenario's before the test's), which
  lose to all of these. A CSV column named like a global shadows it, in
  the virtual user that reads that row only. A setup extractor named like
  a built-in (such as `vu_id`, `iteration` or `scenario`) overrides that
  built-in in every load virtual user, since extracted variables win over
  built-ins; this is allowed but best avoided.
- **Setup failure.** When a setup step fails (a `fail` or `abort` check
  that fails, a required extractor that misses, a transport error or an
  unexpected status), the run fails before the load starts, and neither
  the load nor the teardown runs. `on_fail: "record"` makes a check
  advisory. Stopping the run during setup ends it as stopped, never as
  failed, and the teardown does not run either.
- **Teardown.** Teardown scenarios run once each, in definition order,
  after every load virtual user has stopped, whether the load completed,
  was stopped or was aborted by a threshold, with the globals readable.
  They do not run after a failed or stopped setup. A failing teardown
  never changes the status; its checks count like any other (use
  `on_fail: "record"` to make them advisory).
- **Stopping during teardown.** The engine does not cancel a teardown on
  a stop: its `think_time` runs in full, only `http_timeout` bounds each
  request, and the engine keeps the load's status. The headless CLI
  still reports a run that received a signal as `stopped` (exit 130)
  unless the run had already failed or aborted. It waits at most 10
  seconds after the signal for the teardown to finish before it exits,
  and a second signal exits at once. A teardown cut short this way can
  leave seeded data behind.
- **Timing and results.** `duration`, `stages` and `load` time the load
  only, from the end of setup. Setup and teardown requests count in the
  test totals (including `rps`) and are also reported under their own
  scenario names, once, not per virtual user.
- **Rules.** `Parse` rejects `run_once` and `teardown` on the same
  scenario, a `weight` above 0 on either, a definition with no load
  scenario, and an extractor in any other scenario (another setup
  scenario included) whose `variable_name` a setup scenario already
  extracts. Two steps of the same setup scenario may extract the same
  variable. `false` and a `weight` of 0 read as absent.
- **Cloud.** VoltTest Cloud does not run setup or teardown scenarios yet
  and rejects a definition that has them (see
  [VoltTest Cloud](#volttest-cloud)).

`run_once` and `teardown` joined schema_version "2.0" while it is a
preview.

## Once-per-VU steps

A step with `"once_per_vu": true` runs on a virtual user's first
iteration of its scenario only, typically a login. The variables it
extracts stay set, and the virtual user's later iterations of that
scenario skip the step and keep using them.

```json
"steps": [
  {
    "name": "Login",
    "once_per_vu": true,
    "request": {"method": "POST", "url": "https://shop.example.com/api/login",
                "headers": {"Content-Type": "application/json"},
                "body": "{\"email\":\"user@example.com\"}"},
    "extract": [{"variable_name": "token", "selector": "$.data.token",
                 "type": "json", "required": true}]
  },
  {
    "name": "Cart",
    "request": {"method": "GET", "url": "https://shop.example.com/api/cart",
                "headers": {"Authorization": "Bearer ${token}"}}
  }
]
```

- **First iteration.** Each virtual user tracks each once step of each
  scenario on its own: a virtual user that runs two weighted scenarios
  runs each scenario's once steps on its first iteration of that
  scenario. With [`load`](#arrival-rate-load) the engine reuses idle
  virtual users, so the step runs on a virtual user's first iteration,
  not on every arrival. Virtual users added by a staged ramp-up are new
  virtual users and run it on their first iteration.
- **Retry.** The step is done only when it succeeded and every extractor
  on it hit. Otherwise (a `fail` or `abort` check that fails, a required
  or non-required extractor that misses, a transport error or an
  unexpected status) the virtual user runs it again on its next
  iteration of the scenario, until it is done. A failure that does not
  abort still lets that iteration's later steps run, and an extractor
  that missed assigns `""` (see [Required
  extractors](#required-extractors)), so they see `""`, never a stale
  value. A failing `record` check alone does not make the step run
  again.
- **Variables.** Later iterations never reset the step's variables. A
  CSV column or another extractor with the same name still overwrites
  them, as it would any variable.
- **Results.** The step's requests count in every total (including
  `rps`), in `check_rate` and in thresholds, under the step's own name.
  `iteration_duration` leaves out the step's time, its `think_time`
  included, so the first iteration is comparable with the others. An
  iteration ended by an `abort` on the once step still counts as an
  iteration, with the once step's time left out of its duration.
- **Rules.** `Parse` rejects `once_per_vu` on a step of a `run_once` or
  `teardown` scenario, which runs once anyway, and a load scenario
  whose every step has it, which would leave later iterations empty.
  `false` reads as absent.

`once_per_vu` joined schema_version "2.0" while it is a preview.

## Think time

`think_time` is the pause after a step. A scenario's `think_time` is the
default for each of its steps that sets none; a zero value counts as
none. In v2 it is a duration, or a range for a random pause:

```json
{"name": "Browse", "think_time": "1s-3s", "steps": [
  {"name": "Product", "think_time": "500ms-2s",
   "request": {"method": "GET", "url": "https://shop.example.com/products/1"}},
  {"name": "Cart", "think_time": "250ms",
   "request": {"method": "GET", "url": "https://shop.example.com/cart"}}
]}
```

- **Range.** `MIN-MAX`: two durations joined by one ASCII hyphen, with
  no spaces and no sign on either. Each takes its unit (`"1-3s"` is
  rejected), except a bare `0` (`"0-3s"`). `MIN` must not be negative
  and must not exceed `MAX`; `"2s-2s"` is a fixed 2s.
- **Draw.** The engine draws a uniformly random pause between `MIN` and
  `MAX`, both included, each time the step runs, in every iteration,
  whether the range is the step's or the scenario's.
- **Rules.** `Parse` rejects a negative value, a reversed range and any
  other form. `definition.ParseThinkTime` reads a value the way `Parse`
  validates it and returns the two bounds (equal for a duration).
- **v1.** v1 never validates `think_time`: `Parse` keeps a range or a
  negative value as written, and the engines read a range as no pause.

Think time ranges joined schema_version "2.0" while it is a preview.

## Request bodies

A request's `body_type` says how its body is built:

| `body_type` | Body key | Sent as | Default `Content-Type` |
|---|---|---|---|
| `"raw"` (or absent) | `body` | the string as written, after substitution | none |
| `"json"` | `body_json` | a JSON object or array | `application/json` |
| `"form"` | `fields` | URL-encoded `name=value` pairs | `application/x-www-form-urlencoded` |
| `"multipart"` | `fields` and `files` | `multipart/form-data`: one part per field, then one per file | `multipart/form-data` with the boundary, always |

```json
"steps": [
  {
    "name": "Add to cart",
    "request": {"method": "POST", "url": "https://shop.example.com/api/cart",
                "body_type": "json",
                "body_json": {"sku": "SKU-1", "qty": "${random_int(1,3)}",
                              "gift": "${gift}", "note": "n ${uuid}", "price": 19.90}}
  },
  {
    "name": "Search",
    "request": {"method": "POST", "url": "https://shop.example.com/search",
                "body_type": "form",
                "fields": {"q": "red shoes & socks", "page": "${page}"}}
  }
]
```

- **Content-Type.** With `json` and `form` the engine sets the
  `Content-Type` above, unless `headers` sets `Content-Type` (in any
  case); then the request carries exactly that one header. With
  `multipart` the engine always sets it, because it carries the part
  boundary, and `headers` must not set it.
- **`body_json`.** Must be an object or an array. Its keys, key order and
  number literals (`19.90`, a 20-digit integer) are kept as written; the
  definition stores it compacted. Each string value is substituted on its
  own:
  - a string that is exactly one `${...}` reference without a filter
    takes the type of its value. A variable holding a JSON number,
    `true`/`false`, an array or an object (a `json` extractor, `pick:
    all`, a `run_once` global) is sent unquoted, and so is an integer
    built-in (`random_int`, `counter`, `timestamp`, `timestamp_ms`,
    `vu_id`, `iteration`). In the example `qty` is sent as a number such
    as `2`, and `gift` as `true` when the variable came from a JSON
    `true`. A variable holding a string (a CSV column, a header, a regex
    match) and the other built-ins (`uuid`, `fake.*`, ...) are sent as
    strings;
  - a reference with a filter, such as `${timestamp|trim}`, is always a
    string; this is how to send a number as a string;
  - a string that mixes text and references, such as `"n ${uuid}"`, is
    always a string;
  - a reference to no variable or built-in is sent as the string written;
  - an extractor that misses, or reads a JSON `null`, assigns `""`, so
    its leaf is sent as `""`.

  Every string sent is JSON-escaped, so a value cannot add keys or break
  the JSON. Numbers, `true`, `false` and `null` written in `body_json` are
  sent as written.
- **`fields`.** An object whose values are strings, sent in the order
  written, as `name=value` pairs joined by `&`. Each value is substituted,
  then URL-encoded; a space becomes `+`. Do not add `|urlencode` to a
  field value: it would be encoded twice. A name may appear only once.
  `{}` or no `fields` sends an empty body.
- **Literal keys.** Object keys in `body_json` and field names in `fields`
  are sent as written (field names URL-encoded): a `${...}` in a key or a
  name is neither substituted nor refused.
- **Rules.** `Parse` rejects an unknown `body_type`, a non-empty `body`
  with `json` or `form`, `body_json` without `body_type: "json"` (which
  requires it), `fields` without `body_type: "form"`, a `body_json` that is
  not an object or array, a field value that is not a string and a field
  name used twice. Zero values (`""`, `fields: {}`) read as absent.
- **Marshalling.** `encoding/json` escapes `<`, `>` and `&` in every
  value, so `json.Marshal` of a `Definition` writes them escaped inside
  `body_json`, as it does in `body` and `headers`; the JSON value is the
  same, and the engine sends the characters themselves.
- **v1.** v1 has `body` only; `body_type`, `body_json`, `fields`, `files`
  and `body_base64` are dropped unread.

`body_type`, `body_json` and `fields` joined schema_version "2.0" while it
is a preview, and so did `multipart`, `files` and `body_base64`.

### Multipart and binary bodies

```json
"request": {
  "method": "POST", "url": "/api/reports",
  "body_type": "multipart",
  "fields": {"title": "Report ${uuid}"},
  "files": {
    "report": {"path": "fixtures/report.pdf", "filename": "report-${vu_id}.pdf", "content_type": "application/pdf"},
    "avatar": {"content_base64": "iVBORw0KGgo=", "content_type": "image/png"}
  }
}
```

- **Parts.** The fields come first, in the order written, then the
  files, by field name. Field values and file names are substituted;
  field names and file contents are sent as written.
- **A file** has exactly one of `path` (a file on the machine that runs
  the test; a relative path resolves against that process's working
  directory) and `content_base64`. `filename` defaults to the last
  element of `path`, or to the field name with `content_base64`;
  `content_type` defaults to `application/octet-stream` and must be a
  media type such as `image/png`. A `path` must not be blank, and no
  `path`, `filename`, `content_type` or field or file name may contain a
  line break or NUL.
- **Names.** A multipart body needs at least one field or file, and a
  name is either a field or a file, not both.
- **`body_base64`** sends binary bytes as the body of a `raw` request,
  without substitution. It cannot sit next to a non-empty `body`.
- **Base64** values (`content_base64`, `body_base64`) are standard
  base64 with `=` padding and no line breaks; do not wrap the encoded
  value (with GNU `base64`, use `base64 -w0`). Together they may decode to
  at most 10 MiB per definition (`definition.MaxInlineBinaryBytes`); send
  larger files by path. Problems never repeat the value.
- **VoltTest Cloud** does not take files or `body_base64` yet; a
  multipart body with `fields` only is fine. See
  [VoltTest Cloud](#volttest-cloud).

## Dynamic steps

A step can be skipped, repeated or run once per element of a list, steps
can repeat as a group, a check can assert an expression, and a template
can compute a value with `${expr: ...}`.

```json
"steps": [
  {"name": "Products", "request": {"method": "GET", "url": "https://shop.example.com/api/products"},
   "extract": [{"variable_name": "ids", "selector": "$.items[*].id", "type": "json", "pick": "all"}],
   "checks": [{"type": "assert", "name": "has products", "expr": "status == 200 && len(ids) > 0"}]},
  {"name": "Product", "foreach": {"var": "id", "in": "${ids}"},
   "request": {"method": "GET", "url": "https://shop.example.com/api/products/${id}"}},
  {"name": "Add to cart", "skip_if": "len(ids) == 0",
   "request": {"method": "POST", "url": "https://shop.example.com/api/cart", "body_type": "json",
               "body_json": {"qty": "${expr: iteration % 3 + 1}"}}},
  {"name": "Wait for order", "repeat": {"until": "order_status == \"ready\"", "max": 50},
   "steps": [
     {"name": "Order status", "request": {"method": "GET", "url": "https://shop.example.com/api/order"},
      "extract": [{"variable_name": "order_status", "selector": "$.status", "type": "json"}]}
   ]}
]
```

### Expressions

`skip_if`, `only_if`, `repeat.until` and an `assert` check's `expr` are
bare expressions, such as `count > 0`, written without `${...}`. The
engine compiles them with [expr-lang/expr](https://expr-lang.org) when
the test loads; the contract checks only that each is non-blank, holds no
NUL, does not start with `${` and has no `//` or `/*` outside a quoted
string (expr-lang reads them as comments). What an expression can read (the
variables, `status`, `body`, `json(path)`, `header(name)`,
`cookie(name)`, `duration_ms`) is defined by the engine.

- **`skip_if` / `only_if`.** Evaluated before the step's request is
  built. A step skipped by `skip_if` being true, or by `only_if` being
  false, sends nothing. Not both on one step.
- **`assert`.** A check `{"type": "assert", "name": ..., "expr": ...}`
  evaluated after the response. It requires `name` and `expr`, takes no
  selector and no other operator, and takes `on_fail` like any check
  (default `record`). An `assert` counts as reading the response body:
  `discard_body: true` is refused on its step, and `http.discard_bodies`
  keeps that step's body.
- **`repeat`.** `{"until": "<expression>", "max": N}` runs the step again
  until `until` is true, at most `max` times (1 to 1000, required).
  Reaching `max` with `until` still false fails the step.
- **`foreach`.** `{"var": "id", "in": "${ids}"}` runs the step once per
  element of the list `in` names, with the element in `${id}`. `in` is
  exactly one `${name}` reference; `var` is an identifier an expression
  can read (ASCII letters, digits and `_`, not starting with a digit, so
  no `-`), not `env` or `fake`, not a variable the scenario extracts, a
  setup global or a `vars` name, and not the variable `in` reads. Not
  with `repeat`. The engine runs at most 1000 passes per `foreach`
  (`definition.MaxForeach`).
- `repeat` and `foreach` are refused in a `run_once` or `teardown`
  scenario; `skip_if` and `only_if` are allowed there.

### Step groups

A step with `steps` is a step group: its steps run in order as one unit
under the group's `repeat`, which is required. A group takes only
`name`, `repeat` and `steps` (`Parse` refuses any other key on it,
`request` included, whatever its value), and its steps:

- are ordinary steps with a request, never groups: a group holds one
  level of steps;
- take no `repeat`, `foreach` or `once_per_vu`;
- keep their own names, unique across the scenario together with the
  groups, so results stay flat.

A group is not allowed in a `run_once` or `teardown` scenario. `"steps":
[]` is refused, not read as absent.

In Go, `Scenario.LeafSteps()` returns the steps that send requests, in
document order, each group replaced by its steps, as pointers into the
definition, so a change through them (redaction, for example) changes
the definition. Groups are the elements of `Scenario.Steps` whose
`IsGroup()` is true. A group marshals without `request`.

### Templates

A template value (a request's `url`, `body`, header values, the string
values of `body_json`, the values of `fields`, multipart file names,
cookie values, `vars` values and `target.base_url`) may hold
`${expr: <expression>}`, whose value the engine computes, e.g.
`${expr: qty * price}` or `${expr: type == "single" ? {"id": id} : {"value": 1}}`.
`definition.ScanTemplate` reads a template's references in source order,
the same way for every reader:

- A reference that starts with exactly `${expr:` is an expression. It
  ends at the `}` that balances its braces; braces inside `'...'`,
  `"..."` and `` `...` `` literals do not count, a backslash inside
  `'...'` and `"..."` escapes the next character, and a line feed inside
  them, escaped or not, leaves the literal unterminated, as in expr-lang.
  `${` inside an expression is plain text.
- Every other reference, `${exprx}` and `${ expr: x}` included, runs from
  `${` to the first `}`, as before; without a `}` the rest is literal.
  There is no escape for `${`.
- `Parse` refuses, in v2, an expression that is not closed, holds only
  white space, or has `//` or `/*` outside a quoted string (expr-lang
  reads them as comments, so `${expr: a // } + 1}` would end early), at
  the path of the value. v1 templates are never scanned for expressions.
- `${env.NAME}` is not looked for inside an expression; expressions
  cannot read environment variables.

Dynamic steps joined schema_version "2.0" while it is a preview.

## VoltTest Cloud

A definition that `Parse` accepts can still be refused by VoltTest Cloud.
`definition.CloudProblems(def, nodes)` lists the refusals for a run on
`nodes` nodes, so every reader refuses the same things with the same
paths and messages:

```go
if ps := definition.CloudProblems(def, nodes); len(ps) > 0 {
	return &definition.ValidationError{Problems: ps}
}
```

It reports, in this order:

1. **File paths in `tls`.** A `client_cert`, `client_key` or `ca_cert`
   that is not inline PEM (`IsInlinePEM`): a cloud node cannot read a
   file. The value is never repeated.
2. **Binary request content.** For each step, the request's
   `body_base64`, then each multipart file's `path` or `content_base64`,
   by field name: "is not available on VoltTest Cloud yet; run this test
   headless". VoltTest Cloud cannot ship file content to its nodes yet,
   and a path names a file that is not on the node. A multipart body with
   `fields` only is allowed.
3. **`${env.NAME}` references.** A cloud node's environment holds its own
   settings, not yours. Each distinct reference is reported as written,
   filters included, at each path where it appears: `target.base_url`,
   each test-level `cookies` value and then each `vars` value (each by
   sorted name), then for each scenario its `cookies` and `vars` values,
   and for each of its steps the request `url`, `body`, every string value in
   `body_json`, every value in `fields`, each multipart file's
   `filename` by field name, every header value, by sorted
   name (`headers.X-A` for one value, `headers.X-A[1]` for the second of
   several), and the step's `cookies` values.
   A reference runs from `${` to the first `}`; an unterminated `${env.X`
   is literal. A `${expr: ...}` expression is not scanned: expressions
   cannot read environment variables (see [Templates](#templates)). Keys
   in `body_json`, names in `fields` and cookie and variable names are
   not refused.
   Steps inside a [step group](#step-groups) are scanned at their own
   paths, such as `scenarios[0].steps[2].steps[0].request.url`, here and
   for binary content.
4. **Setup and teardown.** Each `run_once` and each `teardown` scenario,
   at that flag.

No current refusal depends on `nodes`; it is kept for callers. A
scenario's own `load` and `start_at` are not refused here (see [Scenario
load](#scenario-load)).

Paths name keys the way `Parse` does: `headers.Accept`, or
`headers["X Y"]` for a key that is not letters, digits, `_` and `-`, cut
to about 128 bytes. It returns nil for a v1 definition. The list is not
capped; `ValidationError.Error` shows the first 100. Thresholds are not
part of it: whether a metric is measured depends on the engine release
that runs the test.

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
