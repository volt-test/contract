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
  `required` and `on_fail`, some of them with values v2 rejects.

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
  and the `""` default, required and optional extractors,
  `expected_statuses`, keys written with their zero value (`""`, `[]`),
  `equals: ""`, and label edge cases: `<`, `>`, `&`, `\u` escapes
  (including U+2028), a 19-digit number, `1.50`, `-1e3`, and two header
  checks that differ only in value and so need names.
  `definition/check_fixtures_test.go` checks that every label in it is the
  same after a marshal and re-parse.
- `invalid/`: definitions `Parse` rejects, run through the schema as well.
  - `unknown_*`: unknown or v1-only keys added to a valid definition. The
    expected paths are listed in `definition/parse_v2_test.go`.
  - `type_*`: a value of the wrong JSON type (or, in
    `type_check_equals_out_of_range.json`, a number out of float64
    range).
  - `structure_*`: a structural rule, such as a missing name or load model.
  - Checks, required extractors and `expected_statuses` have their own
    families: `structure_check_*`, `type_check_*`, `structure_extract_*`
    (`on_fail` without `required: true`, or a bad value),
    `type_extract_*`, `structure_expected_statuses_*` and
    `type_expected_statuses_*`. Each is a minimal definition with
    exactly one thing wrong; `checkFixtureProblems` in
    `definition/check_fixtures_test.go` lists the one problem (path and
    message) each must produce, and the test fails on a fixture of these
    families that it does not list. `unknown_check.json` (a check with an
    unknown key) is an `unknown_*` fixture like the others.
  The schema rejects all of them except the cases listed, with reasons, in
  `knownSchemaLooser` in `definition/schema_test.go`.
