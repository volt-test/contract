# Test fixtures

## v1

Real v1 definitions, used to check that `definition.Parse` reads every field
the v1 engines read and loses nothing on a round trip.

- `compat/`: node configurations from the engine's v1 compatibility runs
  (one file per node).
- `golden/`: definitions behind the engine's golden-output tests.
- `perf/`: definitions behind the engine's performance gate. The gate's
  recorded baseline is not a definition and is not copied.
- `backend/`: definitions covering every field SDK 1.x and the CLI emit, as
  stored by the VoltTest backend.
- `cli/`: hand-written. `cli_cloud_keys.json` has the CLI-only cloud keys
  (the API key is fake); `cloud_upload.json` has the shape the CLI uploads
  for a cloud run (`"header": null`, `"weights"`, empty `"report_type"`);
  `expected_types.json` has validator `expected` values of every JSON type;
  `header_shadow.json` has `"header": {}` next to `"headers"`;
  `schema_version_number.json` declares `"schema_version": 1`;
  `thresholds_ignored.json` has `thresholds` keys at every level, which
  v1 readers ignore and the v1 path drops unread; `checks_ignored.json`
  does the same for `checks`, `expected_statuses` and the extractor keys
  `required`, `on_fail` and `pick`, some of them with values v2 rejects
  (including `pick` on a header extractor);
  `http_tls_ignored.json` does the same for `http`, `tls` and a step's
  `follow_redirects` and `max_redirects`, with values v2 rejects (a
  negative `max_redirects`, unknown versions, a key without a
  certificate, a blank path, a step maximum with following off).
  `TestV1DropsHTTPTLSKeys` in `definition/v1walk_test.go` checks that it
  parses the same with those keys removed. `load_ignored.json` does the
  same for `load`, with values v2 rejects; `TestV1DropsLoadKey` checks
  it, and a `load` of the wrong JSON type. `setup_teardown_ignored.json`
  does the same for a scenario's `run_once` and `teardown`, with values v2
  rejects (both on one scenario, weights, no load scenario, a global
  extracted again); `TestV1DropsSetupTeardownKeys` checks it, and the
  keys with the wrong JSON type or case. `once_per_vu_ignored.json` does
  the same for a step's `once_per_vu`, set on every step of a scenario
  (which v2 rejects); `TestV1DropsOncePerVUKey` checks it, and the key
  with the wrong JSON type or case. `typed_body_ignored.json` does the same for a
  request's `body_type`, `body_json` and `fields`, with combinations v2
  rejects (`body` next to `body_type: "json"`, an unknown `body_type`,
  `body_json` and `fields` without their `body_type`);
  `TestV1DropsTypedBodyKeys` checks it, and the keys with the wrong JSON
  type, a repeated field name, and other cases.
  `http_tier2_ignored.json` does the same for the `http` body and timeout
  keys, a step's `discard_body` and `response_type`, and a request's
  `timeout`, `files` and `body_base64`, with values and combinations v2
  rejects (a fractional `max_body_size`, zero and negative timeouts, a
  file with both sources, `discard_body` next to a `json` extractor),
  next to string `Host` and `Content-Type` headers that v1 keeps;
  `TestV1DropsHTTPTier2Keys` checks it, and the keys with the wrong JSON
  type and other cases.
  `scenario_load_cookies_ignored.json` does the same for a scenario's
  `load` and `start_at`, `cookies` at every level, a step's
  `clear_cookies` and `vars` at both levels, with values v2 rejects (a
  `constant` pool of 0 virtual users, an all-zero `stages` pool, a weight
  next to a `load`, a negative `start_at`, bad cookie and variable names
  and values), next to `data_format: "json"` with `has_header`, which v1
  keeps as written; `TestV1DropsScenarioLoadCookieKeys` checks it, and the
  keys with the wrong JSON type and other cases. It sets
  `auto_handle_cookies: true`, not `false`, so its v0.4.0 recording also
  holds for the current code.

Every `*_ignored.json` fixture keeps the right JSON type for each value:
`TestV1RewriteMatchesDirectDecode` decodes every v1 fixture with plain
`encoding/json`, which fails on a mistyped value. Mistyped v2-only keys are
covered by Go tests instead (`TestV1IgnoresHTTPTLS` in
`definition/http_tls_fixtures_test.go`).

Redactions: the presigned storage URL in `compat/constant_csv.node-0.json`
is replaced with a placeholder URL without a query string (no credential,
signature or run id); sample login credentials, both in request bodies
and in the base64 CSVs of `data_content`, are replaced with
`example.com` addresses and `example-password` (`example-password-1`,
`example-password-2`, ...); and internal issue references are removed
from descriptions. Hosts are local demo targets or `example.com`.
`definition/fixtures_test.go` fails if a fixture, or a CSV embedded in
one, contains "secret".

## v2

- `valid/`: definitions that both `Parse` and the published schema accept.
  `checks.json` has every check type and operator, every `on_fail` value
  and the `""` default, required and optional extractors, every `pick`
  value on `json` extractors, `pick` on `regex` and `html` extractors,
  `"pick": ""` on a header extractor,
  `expected_statuses`, keys written with their zero value (`""`, `[]`),
  `equals: ""`, and label edge cases: `<`, `>`, `&`, `\u` escapes
  (including U+2028), a 19-digit number, `1.50`, `-1e3`, and two header
  checks that differ only in value and so need names.
  `definition/check_fixtures_test.go` checks that every label in it is the
  same after a marshal and re-parse.
  `http_tls.json` has every `http` and `tls` key: `follow_redirects: false`
  with `max_redirects` at the test level, `version: "1.1"`,
  `insecure_skip_verify`, `min_version: "1.3"`, `client_cert` and `client_key` as file paths, `ca_cert` as
  an inline placeholder `CERTIFICATE` block, and steps that turn following
  back on with their own maximum, turn it off, or override nothing.
  `http_tls_zero.json` writes every `http` and `tls` key that has a zero
  value meaning absent or default (`""`, `0`, `insecure_skip_verify:
  false`), and steps with `max_redirects: 0`, one of them next to
  `follow_redirects: false`. Empty `"http": {}` and
  `"tls": {}` objects are covered by Go tests (`TestHTTPTLSEmptyObjects`)
  and a fuzz seed.
  `load_rate.json` and `load_ramping_rate.json` have one `load` of each
  type with every key; zero values and the other accepted cases are in
  `TestLoadRules` in `definition/load_test.go`.
  `setup_teardown.json` has two `run_once` scenarios (the second reads the
  first's global), two load scenarios and a `teardown` scenario reading
  both globals, with `run_once: false`, `teardown: false` and `weight: 0`
  written out. `run_once.json` is older and unrelated: a constant load
  without `duration`, so each virtual user runs its scenario once. The
  other accepted cases are in `TestSetupTeardownRules` in
  `definition/setup_teardown_test.go`.
  `once_per_vu.json` has two weighted load scenarios, each with a login
  step with `once_per_vu: true` and a required token extractor followed
  by a step that reads the token, and a `run_once` scenario, with
  `once_per_vu: false` written out in both kinds of scenario. The other
  accepted cases are in `TestOncePerVURules` in
  `definition/once_per_vu_test.go`.
  `think_time_range.json` has a `think_time` range on a scenario and on a
  step, a fixed step value and a range from a bare `0`. The other accepted
  and rejected forms are in `TestParseThinkTime` in
  `definition/think_time_test.go`.
  `body_json.json` has `body_type: "json"` bodies: an object with typed
  and mixed placeholders, `19.90`, a 20-digit integer, `<`, `>` and `&`,
  `null`, nested empty containers and keys out of alphabetical order; an
  array body with a `Content-Type` override; an explicit `raw` body; and
  `{}`. `body_form.json` has `body_type: "form"` fields out of
  alphabetical order, with a value holding a space, `&` and `=`, names
  that need encoding (`a b`, `ü`), an empty value, and a `Content-Type`
  override in upper case next to `body: ""`. `body_form_empty.json` has
  a form without `fields`, one with `fields: {}`, and a request with
  every body key at its zero value. The other accepted cases are in
  `TestBodyRules` in `definition/body_test.go`.
  `http_tier2.json` has every new `http` key, a request to an IP with a
  `Host` header, headers given as arrays of two and three values (one of
  them repeated), a request `timeout`, a `discard_body` step with the
  checks and extractors that do not read the body, a `binary` step with a
  `body` check and a `regex` extractor, and an explicit `text` step with
  `json` readers. A header written as a one-element array is written back
  as a string, so that case is in Go tests (`TestHeaderValuesRoundTrip`),
  not in a fixture `TestV2RoundTrip` compares leaf by leaf.
  `multipart.json` has a multipart request with fields and two files, one
  by path with a substituted file name and one as base64, and a `raw`
  request with `body_base64`. `cloud_multipart_path.json`,
  `cloud_multipart_content.json` and `cloud_body_base64.json` each hold
  one value VoltTest Cloud refuses, and `cloud_multipart_fields.json` a
  multipart body with fields only, which it allows;
  `TestCloudProblemsInlineBinary` in `definition/cloud_test.go` checks
  them. The other accepted and rejected cases are in
  `definition/http_tier2_test.go`.
  `scenario_load.json` has a test-level constant load for two weighted
  scenarios next to one scenario `load` of each type, two with
  `start_at` and one with `weight: 0`; `scenario_load_only.json` has no
  test-level load model, since every load scenario has its own `load`,
  next to a `run_once` scenario. `cookies_vars.json` has `cookies` at
  every level (a templated value, one with literal text around a
  reference, one whose reference holds a space, a comma and quotes, an
  empty one, `{}`), `vars` at both levels (`{}` too), `clear_cookies` true
  and false, `auto_handle_cookies` true and false (the latter in a
  scenario without `clear_cookies`), and a JSON and a CSV data file. The other accepted cases are in `TestScenarioLoadRules` and
  `TestCookieVarRules` in `definition/scenario_load_test.go`.
- `invalid/`: definitions `Parse` rejects, run through the schema as well.
  - `unknown_*`: unknown or v1-only keys added to a valid definition. The
    expected paths are listed in `definition/parse_v2_test.go`.
  - `type_*`: a value of the wrong JSON type (or, in
    `type_check_equals_out_of_range.json`, a number out of float64
    range).
  - `structure_*`: a structural rule, such as a missing name or load model.
  - Checks, required extractors and `expected_statuses` have their own
    families: `structure_check_*`, `type_check_*`, `structure_extract_*`
    (`on_fail` without `required: true`, a bad `on_fail` or `pick`, or
    `pick` on a header extractor),
    `type_extract_*`, `structure_expected_statuses_*` and
    `type_expected_statuses_*`. Each is a minimal definition with
    exactly one thing wrong; `checkFixtureProblems` in
    `definition/check_fixtures_test.go` lists the one problem (path and
    message) each must produce, and the test fails on a fixture of these
    families that it does not list. `unknown_check.json` (a check with an
    unknown key) is an `unknown_*` fixture like the others.
  - The HTTP and TLS options and a step's redirect keys have the families
    `structure_http_*`, `type_http_*`, `structure_tls_*`, `type_tls_*`,
    `structure_step_*` and `type_step_*`, built the same way:
    `httpTLSFixtureProblems` in `definition/http_tls_fixtures_test.go`
    lists the one problem each must produce, and the test fails on an
    unlisted fixture of these families. They cover a bad `http.version`
    or `tls.min_version`, a negative `max_redirects` at either level, a
    step `max_redirects` with following off on the step or inherited from
    the test, a key without a certificate and the reverse, inline PEM that
    is not PEM or holds the wrong block, a blank or multi-line path, and
    values of the wrong JSON type (`type_http_debug_string.json`, for the
    top-level `http_debug`, is listed there too).
    `unknown_http.json` and `unknown_tls.json` (a misspelled key in each
    object), `unknown_scenario_follow_redirects.json` (no scenario-level
    setting) and `unknown_request_follow_redirects.json` (the key belongs
    on the step, not the request) are `unknown_*` fixtures.
  - The load model has the families `structure_load_*` and
    `type_load_*`: `loadFixtureProblems` in `definition/load_test.go`
    lists the problems each must produce, and the test fails on an
    unlisted fixture of these families. They cover each `load` rule, a
    `load` next to the other load keys, and the older
    `structure_load_none.json` and `structure_load_both.json`.
    `unknown_load_start_rate.json` (no `start_rate`) and
    `unknown_load_stage.json` are `unknown_*` fixtures.
    `structure_load_constant_top_level.json` and
    `structure_load_stages_top_level.json` use a scenario-only type at the
    top level.
  - A scenario's own load, `start_at`, cookies, variables and data
    formats have the families `structure_pool_*`, `type_pool_*`,
    `structure_start_at_*`, `type_start_at_*`, `structure_cookies_*`,
    `type_cookies_*`, `type_clear_cookies_*`, `structure_vars_*`,
    `type_vars_*` and `structure_data_format_*`:
    `scenarioLoadFixtureProblems` in `definition/scenario_load_test.go`
    lists the problems each must produce, and the test fails on an
    unlisted fixture of these families. They cover a `constant` pool of 0
    virtual users (written and absent), an all-zero `stages` pool, keys of
    another load type, an arrival-rate pool without `preallocated_vus`, an
    unknown type, a pool with a weight, on a `run_once` and on a
    `teardown` scenario, a test-level model no scenario uses and one
    missing where a scenario needs it, `start_at` without a `load`, zero
    and malformed, bad cookie names and values at each level, bad variable
    names, `has_header` with `"json"`, an unknown `data_format`, and values
    of the wrong JSON type. `structure_cookies_value_*` hold a space, a
    comma, a quote, a backslash and non-ASCII in a value's literal text;
    `structure_clear_cookies_without_auto.json` has `clear_cookies` in a
    scenario with `auto_handle_cookies: false`; `structure_vars_env.json`
    and `structure_vars_fake.json` use the reserved names. `unknown_pool_ramp_up.json` (a scenario load
    takes no `ramp_up`) is an `unknown_*` fixture.
  - Setup and teardown scenarios have the families `structure_run_once_*`,
    `structure_teardown_*`, `type_run_once_*` and `type_teardown_*`:
    `setupTeardownFixtureProblems` in `definition/setup_teardown_test.go`
    lists the problems each must produce, and the test fails on an
    unlisted fixture of these families. They cover both flags on one
    scenario, a weight on either, no load scenario, a global extracted
    again by a load scenario defined before the setup scenario or by a
    second setup scenario, and flags of the wrong JSON type.
  - Once-per-VU steps have the families `structure_once_per_vu_*` and
    `type_once_per_vu_*`: `oncePerVUFixtureProblems` in
    `definition/once_per_vu_test.go` lists the problems each must
    produce, and the test fails on an unlisted fixture of these families.
    They cover `once_per_vu` in a `run_once` scenario and in a
    `teardown` scenario, on every step of a load scenario, and of the
    wrong JSON type.
  - Think time ranges have the family `structure_think_time_*`:
    `thinkTimeFixtureProblems` in `definition/think_time_test.go` lists
    the problem each must produce, and the test fails on an unlisted
    fixture of the family. They cover a reversed range, spaces around the
    hyphen, a bound without a unit, a negative minimum and a missing
    maximum.
  - Request bodies have the families `structure_body_*`, `type_body_*`,
    `structure_fields_*` and `type_fields_*`: `bodyFixtureProblems` in
    `definition/body_test.go` lists the problem each must produce, and the
    test fails on an unlisted fixture of these families. They cover an
    unknown `body_type`, `json` without `body_json`, `body_json` without
    `json`, `body` next to `json` and next to `form`, `fields` with
    `raw`, a field name used twice (written once as an escape), and
    values of the wrong JSON type (a string `body_json`, a number
    `body_type`, an array `fields`, a number field value).
  - Headers, multipart and binary bodies, response reading and the request
    timeout have the families `structure_headers_*`, `type_headers_*`,
    `structure_multipart_*`, `structure_files_*`, `type_files_*`,
    `structure_binary_*` (`body_base64`), `structure_discard_body_*`,
    `type_discard_body_*`, `structure_response_type_*`,
    `type_response_type_*`, `structure_request_*` and `type_request_*`:
    `tier2FixtureProblems` in `definition/http_tier2_test.go` lists the
    problems each must produce, and the test fails on an unlisted fixture
    of these families. The new `http` keys (`max_body_size`,
    `discard_bodies` and the timeouts) are `structure_http_*` and
    `type_http_*` fixtures, listed in `httpTLSFixtureProblems`.
    `unknown_file_part.json` (an unknown key in a file) and
    `unknown_step_timeout.json` (`timeout` belongs on the request) are
    `unknown_*` fixtures. The 10 MiB base64 cap is checked by
    `TestInlineBinaryCap`, not by a fixture.
  The schema rejects all of them except the cases listed, with reasons, in
  `knownSchemaLooser` in `definition/schema_test.go`.

## Recorded output

`recorded/` holds output written by the v0.3.0 code, before header values
could be arrays, so that later type changes can be checked against it:

- `marshal/` mirrors `v1/` and `v2/valid/`: the bytes `json.Marshal`
  writes for each fixture after `Parse`. `TestMarshalUnchanged` in
  `definition/marshal_test.go` fails if any of them changes, and if a v1
  fixture has no recording (older code reads every v1 fixture). v2
  fixtures added later have none; `TestV2RoundTrip` covers them.
- `v1_header_array.json` is a v1 definition whose header values are
  arrays, an empty array, numbers, an object, `true` and `null`, next to
  string values. `v1_header_array.problems.json` and
  `v1_header_array.marshal.json` are its `V1TypeProblems` and its
  `json.Marshal` output. `TestV1HeaderArrayStaysTypeProblem` compares
  against them.
- `v1_auto_handle_cookies.json` holds v1 definitions with
  `auto_handle_cookies` absent, `true`, `false`, `null`, of every wrong
  JSON type, in other cases and repeated. `v1_auto_handle_cookies.results.json`
  is what the v0.4.0 code made of each, when the field was a `bool`: the
  problems of the error, or the `V1TypeProblems` and `json.Marshal`
  output. `TestV1AutoHandleCookiesRecorded` compares against it.

The field became a `*bool` after v0.4.0, so an explicit
`"auto_handle_cookies":false` now survives `json.Marshal`: the v1
recordings of the seven fixtures that write one gained it, and that is
their only change. `TestV1AutoHandleCookiesRecorded` compares marshal
output without it, and `TestV1RecordedFalseReadsTheSame` checks that a v1
reader decodes each of those recordings the same with and without it.

Re-record (`go test ./definition -run 'TestMarshalUnchanged|TestV1HeaderArray' -update-recorded`)
only for an intended change, and say so in the pull request.

## Private keys

No fixture, v1 or v2, carries a private key, not even a placeholder:
secret scanners match the PEM block type whatever the body. `client_key`
is always a file path in fixtures, and `TestFixturesCarryNoSecrets` in
`definition/fixtures_test.go` fails if any fixture contains the words of a
private-key PEM block type, in any case. Inline certificates are
placeholder `CERTIFICATE` blocks with the body `AAAA`. Go tests that need
an inline key build its block type at run time.
