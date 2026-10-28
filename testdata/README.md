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
  with the wrong JSON type or case.

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
  The schema rejects all of them except the cases listed, with reasons, in
  `knownSchemaLooser` in `definition/schema_test.go`.

## Private keys

No fixture, v1 or v2, carries a private key, not even a placeholder:
secret scanners match the PEM block type whatever the body. `client_key`
is always a file path in fixtures, and `TestFixturesCarryNoSecrets` in
`definition/fixtures_test.go` fails if any fixture contains the words of a
private-key PEM block type, in any case. Inline certificates are
placeholder `CERTIFICATE` blocks with the body `AAAA`. Go tests that need
an inline key build its block type at run time.
