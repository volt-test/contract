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
  minor. [Checks](#checks), `expected_statuses`, the extractor keys
  `required`, `on_fail` and [`pick`](#extracting-from-arrays), and the
  [HTTP and TLS options](#http-and-tls-options) (`http`, `tls` and a
  step's `follow_redirects` and `max_redirects`) and the [arrival-rate
  load](#arrival-rate-load) (`load`) joined `"2.0"` under this rule.
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
  - a load model: non-empty `stages`, `virtual_users` (at least 1) with
    optional `duration` and `ramp_up`, or a `load` object (see
    [Arrival-rate load](#arrival-rate-load)), exactly one of them. Without
    `duration` each virtual user runs its scenario once; a `duration` that
    is present must be greater than zero, and so must every stage's
    `duration`;
  - `run_once` and `teardown` not both on one scenario, no `weight` on
    either, at least one load scenario, and no other scenario extracting a
    variable a `run_once` scenario sets (see [Setup and
    teardown](#setup-and-teardown));
  - known extractor types, data formats and data modes, and an
    extractor's `pick` from its list, on `json`, `regex` and `html`
    extractors only (see [Extracting from arrays](#extracting-from-arrays));
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
    options](#http-and-tls-options)).
- **v1** is read the way the v1 readers (the CLI, the cloud engine and the
  backend) read it. Unknown keys are ignored, keys match case-insensitively,
  and v2-only keys (`target.base_url`, `thresholds` at every level,
  `expected_statuses`, `http`, `tls`, `load`, a scenario's `run_once` and
  `teardown`, a step's `checks`,
  `follow_redirects` and `max_redirects`, and an extractor's `required`,
  `on_fail` and `pick`) are dropped unread, whatever their value. Values are
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
labels. For the HTTP and TLS options it also checks the rules that span
keys or levels (a step's `max_redirects` against the `follow_redirects`
in effect for it, `client_cert` and `client_key` together), the PEM
contents of `client_cert`, `client_key` and `ca_cert`, and blank or
multi-line file paths. For `load` it also checks that durations are
greater than zero and that `max_vus` is at least `preallocated_vus`.

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
| `tls.insecure_skip_verify` | `false` | Skip verification of the target's certificate, e.g. a staging server with a self-signed certificate. |
| `tls.min_version` | `"1.2"` | Lowest TLS version accepted: `"1.0"`, `"1.1"`, `"1.2"` or `"1.3"`. The old versions are there to test legacy targets. |
| `tls.client_cert` | none | Client certificate for mutual TLS: one or more `CERTIFICATE` blocks. Requires `client_key`. |
| `tls.client_key` | none | The client certificate's private key, unencrypted. Requires `client_cert`. |
| `tls.ca_cert` | none | Certificates trusted for the target in addition to the system roots: one or more `CERTIFICATE` blocks. |

`http_timeout` and `http_debug` stay at the top level; there is no
`http.timeout` or `http.debug`.

These keys joined `"2.0"` while it is a preview (see
[schema_version](#schema_version)). v1 definitions drop them unread.

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
  "preallocated_vus": 200,
  "max_vus": 1000
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
  "preallocated_vus": 50
}
```

| Key | Applies to | Meaning |
|---|---|---|
| `type` | both, required | `"rate"` or `"ramping_rate"`. |
| `rate` | `rate`, required | Iterations started per `time_unit`, at least 1. |
| `duration` | `rate`, required | How long the test runs, greater than zero. |
| `stages` | `ramping_rate`, required | At least one `{duration, target}`. The rate starts at 0 and moves linearly to each stage's `target` (0 or more) over that stage's `duration` (greater than zero). |
| `time_unit` | both | The period `rate` and `target` count over. Default `"1s"`; must be greater than zero. |
| `preallocated_vus` | both, required | Virtual users started before the test begins, at least 1. |
| `max_vus` | both | Most virtual users the test may run, at least `preallocated_vus`. `0` or absent means `preallocated_vus`. |

- `"rate"` takes no `stages`, and `"ramping_rate"` takes no `rate` or
  `duration`; each may still be written with its zero value.
- An iteration that is due starts on an idle virtual user. When none is
  idle and fewer than `max_vus` run, a new one starts; otherwise the
  iteration is dropped and counted.
- There is no `start_rate`.
- `Parse` leaves the defaults to the reader: `time_unit` stays `""` and
  `max_vus` stays `0`.

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
- **Precedence.** A CSV column, then a global, then a built-in variable.
  A CSV column named like a global shadows it, in the virtual user that
  reads that row only. A setup extractor named like a built-in (such as
  `vu_id`, `iteration` or `scenario`) overrides that built-in in every
  load virtual user, since variables win over built-ins; this is allowed
  but best avoided.
- **Setup failure.** When a setup step fails (a `fail` or `abort` check
  that fails, a required extractor that misses, a transport error or an
  unexpected status), the run fails before the load starts, and neither
  the load nor the teardown runs. `on_fail: "record"` makes a check
  advisory. Stopping the run during setup ends it as stopped, never as
  failed, and the teardown does not run either.
- **Teardown.** Teardown scenarios run once each, in definition order,
  after every load virtual user has stopped, whether the load completed,
  was stopped or was aborted by a threshold, with the globals readable.
  They do not run after a failed or stopped setup. A teardown is not
  cancelled by a stop: its `think_time` runs in full and only
  `http_timeout` bounds each request. A stop during the teardown keeps
  the load's status, and a failing teardown never changes the status;
  its checks count like any other (use `on_fail: "record"` to make them
  advisory).
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
  and rejects a definition that has them.

`run_once` and `teardown` joined schema_version "2.0" while it is a
preview.

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
