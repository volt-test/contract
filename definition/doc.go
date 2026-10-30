// Package definition is the public VoltTest test-definition format.
//
// Parse reads a definition and returns a *Definition. The top-level
// schema_version selects the format. Every error Parse returns matches
// ErrInvalidDefinition.
//
// # v2
//
// A v2 definition ("schema_version": "2.0") is strict: every key must be
// defined for its schema_version with exact spelling and case
// (*UnknownFieldError), every value must have the right JSON type, and the
// structure is validated (*ValidationError).
//
// # v1
//
// A v1 definition (no schema_version, or 1, "1", "1.0" or null) is read the
// way the v1 readers read it: unknown keys are ignored, keys match
// case-insensitively, and v2-only keys (contract:"v2", such as
// target.base_url, thresholds, expected_statuses, http, tls, load, a
// scenario's run_once and teardown, a step's checks, follow_redirects,
// max_redirects and once_per_vu, a request's body_type, body_json and
// fields, and an extractor's required, on_fail and pick) are dropped
// unread. Values are checked for their
// JSON type only, with two classes of field:
//
//   - Core fields are decoded by every v1 reader (the headless CLI, the
//     cloud engine and the backend), all of which reject a value of the
//     wrong JSON type. Parse rejects it too, with a *ValidationError that
//     lists every such value by path.
//   - Loose fields (contract:"loose") are ignored by at least one v1
//     reader: the top-level description, weights, report_type,
//     region_config, cloud, api_key, cloud_timeout, existing_test_id and
//     skip_lookup, target.idle_timeout, a request's headers and a data
//     source's data_download_url. A value of the wrong JSON type does not
//     fail Parse: it is decoded as if the mistyped value were absent (with
//     a duplicate key, an earlier good value survives), and the problem is
//     recorded in Definition.V1TypeProblems, once per field with a Count.
//
// Each reader then fails on its own read set, as it does today. A reader
// declares the loose fields it reads as a ReadSet, with "[]" for every
// array level a pattern passes through and the pattern ending at a field
// name ("weights", not "weights[]"), and fails when V1TypeProblemsIn
// returns anything:
//
//	// Headless CLI: the CLI-only keys, description and target.idle_timeout.
//	var headlessReads = definition.MustReadSet("description", "weights",
//		"report_type", "region_config", "cloud", "api_key", "cloud_timeout",
//		"existing_test_id", "skip_lookup", "target.idle_timeout")
//
//	// Cloud engine: request headers and the dispatch download URL.
//	var cloudReads = definition.MustReadSet(
//		"scenarios[].steps[].request.headers",
//		"scenarios[].data_config.data_download_url")
//
//	if ps := def.V1TypeProblemsIn(headlessReads); len(ps) > 0 {
//		// reject the definition, as the headless engine does today
//	}
//
// A problem falls under a pattern when its path equals the pattern or
// continues it with "." or "[": "weights[1]" falls under "weights", and
// "weightsx" does not. Patterns stop at a map field (headers) and never
// name a literal index or a map key, because only the first mistyped value
// of each field keeps its path. NewReadSet rejects a pattern that breaks
// these rules (including a trailing "[]" or ".") or names an unknown or
// v2-only field; MustReadSet panics
// instead, so a bad package-level read set fails at init.
//
// # Dispatch-only fields
//
// data_config.data_download_url is set by the VoltTest backend when it
// sends a run to the engines, and counts as a data source in v2. The
// backend must strip any user-supplied value before calling Parse on a
// definition a user submits. The published JSON Schema leaves it out.
//
// # JSON Schema
//
// schema/test-definition.schema.json describes v2 definitions. It is
// necessary, not sufficient: every definition Parse accepts as v2 passes
// the schema (data_download_url aside), but Parse is stricter. It also
// checks durations, URL forms, unique names, parts of the load model,
// threshold expressions and their scopes, and the check rules the schema
// cannot express: one operator per check and only those its type takes,
// selectors, the value kind per type, regular expressions, comparisons,
// between order and unique check labels. For the HTTP and TLS options it
// also checks a step's max_redirects against the follow_redirects in
// effect for it, that client_cert and client_key come together, the PEM
// contents of client_cert, client_key and ca_cert, and blank or
// multi-line file paths.
//
// # Thresholds (v2)
//
// The test, each scenario and each step can carry thresholds
// (ThresholdConfig): expression strings such as "p95 < 500ms", or objects
// with abort settings. Parse checks each expression with thresholds.Parse
// and its scope with ValidFor, reporting problems at "thresholds[i]" for a
// string and at "thresholds[i].expr" for an object. Bounds returns them as
// thresholds.Bound values with the scenario and step each applies to. v1
// readers ignore thresholds, and so does Parse on the v1 path.
//
// Through the thresholds package, which evaluates thresholds on HDR
// histograms, this package depends on github.com/HdrHistogram/hdrhistogram-go.
//
// # Checks (v2)
//
// A step's Checks assert on its response. A Check has a type, a selector
// when the type needs one, and exactly one operator:
//
//	type    selector         operators
//	status  none             in, not_in, between
//	header  header name      equals, contains, exists, matches
//	body    none             equals, contains, not_contains, matches, size, empty
//	json    JSON path        equals, exists, missing, is, count, matches, contains
//	html    CSS selector     exists, equals
//	timing  none             duration
//	url     none             equals, contains, matches
//	cookie  cookie name      exists
//
// equals and contains hold a *Scalar: a string for header, body, html and
// url, and a string, number or true/false for json, where equals compares
// scalars only: a string equals a string with the same text, true/false
// the same value, and a number a number with the same exact decimal value
// (42 equals 42.0 and 4.2e1, with no float64 rounding); a string never
// equals a number or true/false. json contains compares each array
// element the same way. Status codes run from 100 to 599. count, size and
// duration are comparisons such as "< 500ms", read by
// thresholds.ParseComparison with the threshold grammar and units. A
// number outside float64 range, such as 1e999, is rejected. url is the
// final URL after redirects (the request URL when redirects are not
// followed), and cookie reads the Set-Cookie headers of the response and
// of every redirect followed to reach it, a later one winning for the same
// name. html equals compares the text content of the
// first matching element, trimmed of leading and trailing white space, as
// an html extractor without an attribute reads it. Check values and
// selectors are literal: ${variable} is not replaced. When no response is
// received, the step's checks are not evaluated and not counted.
//
// A key with its zero value ("" or []) counts as absent, with two
// exceptions: equals and contains are values whenever written
// ("equals": "" checks for an empty value), and exists, missing and empty
// must be true.
//
// FailMode says what a failure does: FailRecord counts it toward the
// check_rate metric only and does not fail the run; FailFail also fails
// the run; FailAbort fails the run and ends the virtual user's current
// iteration once the step's other checks are evaluated (the test goes on,
// unlike a threshold's abort_on_fail). The default is FailFail for status
// checks and FailRecord for the others.
//
// An Extract with Required set turns a miss (nothing found, or a JSON
// null) into a failed check labelled "extract <variable_name>", FailAbort
// by default; on_fail is rejected without required: true. Every extractor
// assigns its variable; a miss assigns "". A required extractor counts as one check on every response
// its step evaluates (a hit passes, a miss fails); an optional one is
// never counted. When an abort check or required extractor fails, the
// step's other checks and extractors are still evaluated, and every
// extractor assigns its variable, before the iteration ends.
//
// Definition.ExpectedStatuses lists the status codes that count as a
// successful request, for steps without a status check whose on_fail is
// fail or abort; such a check decides for its own step, and a record-mode
// status check changes nothing. Left out, the engine decides: currently a
// status from 200 to 499 succeeds and any other fails. This may change
// when the v2 default is decided.
//
// Label is a check's identity in results: its name, or a label derived
// from the decoded check and cut to MaxLabelBytes, such as
// "json $.data.items[0].id equals 42". Header labels leave the value out;
// every other derived label contains the expected value, so a check whose
// value is sensitive needs a name. Parse rejects two checks of a step with
// the same label, comparing cut labels and counting required extractors as
// "extract <variable_name>". In results a check is identified by its
// scenario name, step name and label together, each unique at its level;
// results also carry its type, which is not part of the key.
//
// Extract.Pick (PickRandom, PickFirst, PickLast, PickAll) chooses among
// several values for json, regex and html extractors; the README section
// "Extracting from arrays" gives the rules.
//
// Checks, expected_statuses and the extractor keys required, on_fail and
// pick joined schema_version "2.0" while it is a preview.
//
// # HTTP and TLS options (v2)
//
// Definition.HTTP (HTTPOptions) and Definition.TLS (TLSOptions) are
// optional. "" and 0 count as absent; follow_redirects is a *bool, so an
// explicit false is a value and is kept:
//
//	key                       default     meaning
//	http.follow_redirects     true        follow 3xx redirects
//	http.max_redirects        10          most redirects per request; 0 means the default
//	http.version              negotiated  "1.1" forces HTTP/1.1; absent, HTTP/2 over TLS when offered
//	tls.insecure_skip_verify  false       skip verification of the target's certificate
//	tls.min_version           "1.2"       "1.0", "1.1", "1.2" or "1.3"
//	tls.client_cert           none        client certificate; requires client_key
//	tls.client_key            none        its unencrypted private key; requires client_cert
//	tls.ca_cert               none        certificates trusted in addition to the system roots
//
// A Step overrides the redirect options with FollowRedirects and
// MaxRedirects; there is no scenario-level setting. Following is turned off
// only with follow_redirects: false; max_redirects 0 means absent (10 at
// the test level, the test-level maximum on a step), and a negative value
// is rejected. A step's max_redirects above 0 is rejected
// when the step does not follow redirects (its own follow_redirects, else
// http.follow_redirects, else true). max_redirects has no upper limit:
// http_timeout bounds the whole request, redirects included. http_timeout
// and http_debug stay at the top level.
//
// With redirects followed, checks and extractors read the final response
// and url is the final URL; cookie checks and extractors read the
// Set-Cookie headers of every redirect followed as well, a later one
// winning for the same name. With redirects not followed, they see the 3xx
// response, its Location header included, and url is the request URL.
//
// client_cert, client_key and ca_cert each take a file path or inline PEM.
// IsInlinePEM is the rule: a value that contains "-----BEGIN" anywhere is
// inline PEM, and any other value is a file path, which must not be blank
// or contain a line break. Inline PEM must hold complete blocks of the
// right kind: CERTIFICATE blocks and no private key for client_cert and
// ca_cert, an unencrypted private key block for client_key (encrypted keys
// are not supported). Parse checks the PEM structure only. A relative path
// resolves against the working directory of the process that runs the
// test, since a definition may arrive on standard input; VoltTest Cloud
// runs need the inline form. An inline client_key is stored with the
// definition as written. With insecure_skip_verify true, ca_cert has no
// effect. Parse problems for these three keys never repeat the value.
//
// The http and tls objects and the step keys follow_redirects and
// max_redirects joined schema_version "2.0" while it is a preview.
//
// # Load model (v2)
//
// One of: non-empty stages; virtual_users (at least 1) with optional
// duration and ramp_up; or load alone. Without duration each virtual user
// runs its scenario once. A duration that is present must be greater than
// zero, and so must every stage duration.
//
// Load is an arrival-rate (open) model. Type LoadRate needs rate and
// duration; LoadRampingRate needs stages and takes neither. Rates count
// iterations per time_unit; "" means 1s. preallocated_vus is required, and
// max_vus 0 means preallocated_vus. Parse leaves both defaults to the
// reader. load joined schema_version "2.0" while it is a preview.
//
// # Setup and teardown (v2)
//
// A Scenario with RunOnce is a setup scenario and one with Teardown a
// teardown scenario; IsLoad reports the others. Neither kind is picked for
// the load. Setup scenarios run before the load, one at a time in
// definition order, each by one virtual user; the variables they extract
// become read-only globals that every load virtual user and every teardown
// scenario reads as ${name}, and a later setup scenario sees the globals of
// the earlier ones. A CSV column wins over a global (in the virtual user
// that reads the row only), and a global wins over a built-in variable. A
// failed setup step fails the run before the load starts; a stop during
// setup ends it as stopped. Teardown scenarios run once each, in definition
// order, after the load ends (completed, stopped or aborted), but not
// after a failed or stopped setup. The engine does not cancel them on a
// stop, so only http_timeout bounds each request, but the headless CLI
// waits at most 10 seconds after a signal before it exits and reports
// a completed load as stopped. Parse rejects both flags on one
// scenario, a weight above 0 on either, a definition with no load scenario,
// and any other scenario extracting a variable a setup scenario extracts.
// VoltTest Cloud does not run them yet. The README section "Setup and
// teardown" gives the full rules. run_once and teardown joined
// schema_version "2.0" while it is a preview.
//
// # Once-per-VU steps (v2)
//
// A Step with OncePerVU runs on a virtual user's first iteration of its
// scenario only; the virtual user tracks each such step per scenario, and
// a reused arrival-rate virtual user does not run it again. The step is
// done only when it succeeded and every extractor on it hit; otherwise it
// runs again on the virtual user's next iteration of the scenario. Its
// variables are never reset by later iterations, which skip it, though a
// CSV column or another extractor of the same name still overwrites them.
// Its requests count under its own name in every total, check and
// threshold, and iteration_duration leaves out its time, think_time
// included. Parse rejects OncePerVU on a step of a run_once or teardown
// scenario, and on every step of a load scenario. The README section
// "Once-per-VU steps" gives the full rules. once_per_vu joined
// schema_version "2.0" while it is a preview.
//
// # Think time (v2)
//
// A Step's ThinkTime is the pause after it, and a Scenario's ThinkTime is
// the default for each step that sets none (a zero value counts as none).
// In v2 it is a duration such as "500ms", or a range "MIN-MAX" such as
// "1s-3s": two durations with units (a bare "0" aside) joined by one
// hyphen, no spaces and no sign, with 0 <= MIN <= MAX. The engine draws
// a uniformly random pause between the bounds, both included, each time
// the step runs. ParseThinkTime reads a value as Parse validates it. v1
// never validates think_time: Parse keeps a range or a negative value as
// written. Ranges joined schema_version "2.0" while it is a preview.
//
// # Request bodies (v2)
//
// A Request's BodyType says how its body is built: BodyTypeRaw (the
// default; "" means the same) sends Body, BodyTypeJSON sends BodyJSON and
// BodyTypeForm sends Fields URL-encoded; BodyKind returns the effective
// type. BodyJSON is a JSONBody, a JSON object or array held as compacted
// JSON text with its key order and number literals as written. Fields
// keeps the fields in the order written; on the wire it is a JSON object
// of strings, and a name may appear only once. Each string leaf of
// BodyJSON and each field value is substituted on its own; a leaf that is
// exactly one reference without a filter takes the type of its value, so
// a JSON number, true/false, array or object, or an integer built-in, is
// sent unquoted. Keys and field names are sent as written. Parse rejects
// an unknown body_type, a non-empty body with json or form, body_json
// without json (which requires it), fields without form, a body_json that
// is not an object or array, a field value that is not a string and a
// field name used twice; {} fields read as absent. The README section
// "Request bodies" gives the full rules. body_type, body_json and fields
// joined schema_version "2.0" while it is a preview.
package definition
