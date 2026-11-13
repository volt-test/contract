package definition

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Struct tags used in this file:
//
//   - json: the wire name. Parse matches v2 keys against it exactly.
//   - contract: comma-separated flags.
//     "v1": the field exists only in v1; Parse rejects it as unknown in v2.
//     "v2": the field exists only in v2; Parse drops it in v1 without
//     looking at its value.
//     "loose": in v1, a value of the wrong JSON type is not an error,
//     because at least one v1 reader ignores the field. The field is
//     decoded as if the mistyped value were absent, and the problem is
//     recorded in Definition.V1TypeProblems.
//     v1 fields without "loose" are core: every v1 reader rejects a value
//     of the wrong type, and so does Parse.
//   - jsonschema: input for the published JSON Schema. "-" keeps a field out
//     of the schema (every v1-only field, plus dispatch-only fields).
//
// Field comments become descriptions in the published schema, so they are
// written for people who write definitions.
//
// Every optional field carries omitempty so that marshalling a v2
// Definition produces a document that parses as v2 again.

// Definition is a VoltTest test definition.
//
// Durations are Go duration strings ("30s", "1m30s"). Parse checks them in
// v2 and leaves them unchecked in v1.
type Definition struct {
	// Format version of this definition: "2.0".
	SchemaVersion *SchemaVersion `json:"schema_version,omitempty" jsonschema:"required"`

	// Name of the test.
	Name string `json:"name" jsonschema:"required,pattern=\\S"`
	// Free-text description of the test.
	Description string `json:"description,omitempty" contract:"loose"`

	// Constant load: number of virtual users (at least 1). Use instead of
	// stages.
	VirtualUsers int `json:"virtual_users,omitempty" jsonschema:"minimum=0"`
	// Constant load: how long the test runs, e.g. "5m". Leave it out to have
	// each virtual user run its scenario once.
	Duration string `json:"duration,omitempty"`
	// Constant load: time to start all virtual users, e.g. "30s".
	RampUp string `json:"ramp_up,omitempty"`
	// Staged load: the number of virtual users moves linearly from
	// initial_virtual_users to each stage's target over that stage's
	// duration. Use instead of virtual_users, duration and ramp_up.
	Stages []Stage `json:"stages,omitempty"`
	// Staged load: virtual users running when the load starts (after
	// setup); the first stage moves linearly from this number to its target
	// (default 1; 0 ramps from zero). Needs stages.
	InitialVirtualUsers *int `json:"initial_virtual_users,omitempty" contract:"v2" jsonschema:"minimum=0"`
	// How long each virtual user may finish its current iteration once the
	// test-level load ends, e.g. "30s" (default "30s"; "0s" cuts at once).
	// No new iteration starts; one still running is then cancelled and
	// counted in interrupted_iterations. Applies to the scenarios without
	// their own load.
	StopGrace string `json:"stop_grace,omitempty" contract:"v2"`
	// Staged load: how long a virtual user removed by a stage that lowers
	// the target may finish its current iteration before it exits (default
	// "30s"; "0s" cuts at once). One still running is then cancelled and
	// counted in interrupted_iterations. Needs stages.
	RampDownGrace string `json:"ramp_down_grace,omitempty" contract:"v2"`
	// Arrival-rate load: iterations start at a set rate, whatever the
	// response times. Use instead of virtual_users, duration, ramp_up and
	// stages. Only types "rate" and "ramping_rate" apply here. Leave the
	// whole test-level load model out when every load scenario has its own
	// load.
	Load *Load `json:"load,omitempty" contract:"v2"`

	// Timeout for each HTTP request, e.g. "30s".
	HTTPTimeout string `json:"http_timeout,omitempty"`
	// Log request and response details.
	HTTPDebug bool `json:"http_debug,omitempty"`

	// The system under test.
	Target *Target `json:"target,omitempty"`
	// Scenarios that virtual users run, picked at random by weight. A
	// run_once or teardown scenario is never picked; it runs once.
	Scenarios []Scenario `json:"scenarios" jsonschema:"required,minItems=1"`
	// Percent of the virtual users per VoltTest Cloud region.
	RegionConfig []RegionWeight `json:"region_config,omitempty" contract:"loose"`
	// Pass/fail rules for the whole test, e.g. "p95 < 500ms" or
	// "error_rate < 1%".
	Thresholds []ThresholdConfig `json:"thresholds,omitempty" contract:"v2"`
	// HTTP status codes (100-599) that count as a successful request, for
	// steps without a status check whose on_fail is fail or abort; such a
	// check decides for its own step instead. Leave it out (or write []) to
	// use the engine's default.
	ExpectedStatuses []int `json:"expected_statuses,omitempty" contract:"v2"`
	// HTTP options for every request: redirects and the HTTP version.
	HTTP *HTTPOptions `json:"http,omitempty" contract:"v2"`
	// TLS options for every request to an https URL.
	TLS *TLSOptions `json:"tls,omitempty" contract:"v2"`
	// Preset cookies, by name. A name is an HTTP token (no spaces, "=", ";"
	// or other separators) and is sent as written. A value is cookie-octets
	// (no space, '"', ",", ";", "\", control character or non-ASCII) outside
	// ${variable} references; the engine checks the resolved value when it
	// sends the request. CR, LF and NUL are refused anywhere. A scenario's or a step's
	// cookie of the same name replaces it. How preset cookies interact with
	// the cookie jar, a step's Cookie header and clear_cookies, and which
	// hosts receive them, is defined by the engine's HTTP options
	// documentation.
	Cookies map[string]string `json:"cookies,omitempty" contract:"v2"`
	// Variables every virtual user starts with, readable as ${name}, at the
	// lowest precedence: a CSV column, an extracted value (setup globals
	// included) or a built-in variable of the same name wins. A name is
	// ASCII letters, digits, "_" and "-", and not "env" or "fake". A value
	// may contain ${...} references; when they resolve is defined by the
	// engine. A scenario's vars of the same name replace these.
	Vars map[string]string `json:"vars,omitempty" contract:"v2"`

	// Weights repeats the scenario weights (v1 only).
	Weights []int `json:"weights,omitempty" contract:"v1,loose" jsonschema:"-"`
	// ReportType selects the CLI report format (v1 only).
	ReportType string `json:"report_type,omitempty" contract:"v1,loose" jsonschema:"-"`
	// Cloud asks the CLI to run the test in VoltTest Cloud (v1 only).
	Cloud bool `json:"cloud,omitempty" contract:"v1,loose" jsonschema:"-"`
	// APIKey is the VoltTest Cloud API key the v1 CLI reads from api_key.
	// Parse fills it on the v1 path. It is not marshalled, and the fmt
	// package prints it as "[redacted]". Read it with d.APIKey.Reveal().
	APIKey Secret `json:"-" contract:"v1" jsonschema:"-"`
	// CloudTimeout is the CLI's cloud wait, in seconds (v1 only).
	CloudTimeout int `json:"cloud_timeout,omitempty" contract:"v1,loose" jsonschema:"-"`
	// ExistingTestID makes the CLI reuse a cloud test (v1 only).
	ExistingTestID string `json:"existing_test_id,omitempty" contract:"v1,loose" jsonschema:"-"`
	// SkipLookup makes the CLI skip the cloud test lookup (v1 only).
	SkipLookup bool `json:"skip_lookup,omitempty" contract:"v1,loose" jsonschema:"-"`

	// V1TypeProblems lists values of a v1 definition that have the wrong
	// JSON type in fields that some v1 reader ignores (the "loose"
	// fields). Instead of failing, Parse decodes as if each mistyped value
	// were absent. Each reader should fail when a problem falls under a
	// field it reads; see ReadSet and V1TypeProblemsIn. Paths use the canonical key
	// names and array indexes, e.g. "scenarios[0].steps[1].request.headers".
	// One problem is kept per field and message: Path names the first
	// mistyped value and Count how many there were. Always empty for v2.
	V1TypeProblems []Problem `json:"-"`
}

// Secret holds a credential such as an API key. Reveal returns the value.
// The fmt package prints a Secret as "[redacted]" under every verb (the
// value sits behind a pointer, so even %p shows only an address), and
// encoding/json writes it as "[redacted]".
//
// MarshalJSON is one-way: Secret has no UnmarshalJSON, so a struct that
// carries a Secret without json:"-" writes the literal "[redacted]" and
// cannot read a value back.
type Secret struct {
	value *string
}

const redacted = "[redacted]"

// NewSecret wraps s. NewSecret("") is the zero Secret.
func NewSecret(s string) Secret {
	if s == "" {
		return Secret{}
	}
	return Secret{value: &s}
}

// Reveal returns the secret value, or "" for the zero Secret.
func (s Secret) Reveal() string {
	if s.value == nil {
		return ""
	}
	return *s.value
}

// Equal reports whether two Secrets hold the same value. go-cmp uses it.
func (s Secret) Equal(o Secret) bool { return s.Reveal() == o.Reveal() }

// IsZero reports whether the Secret is empty.
func (s Secret) IsZero() bool { return s.Reveal() == "" }

// Format writes "[redacted]" for every fmt verb.
func (Secret) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, redacted) }

// String returns "[redacted]".
func (Secret) String() string { return redacted }

// GoString returns "[redacted]".
func (Secret) GoString() string { return redacted }

// MarshalJSON writes "[redacted]".
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// HTTPOptions are the test-level HTTP options. A step can override the
// redirect options with its own follow_redirects and max_redirects, and
// discard_bodies with its own discard_body.
type HTTPOptions struct {
	// Follow redirects (default true). With false, the 3xx response and
	// its Location header are what checks and extractors see. Set it to
	// false to stop following redirects.
	FollowRedirects *bool `json:"follow_redirects,omitempty"`
	// Most redirects followed for one request (default 10; 0 or absent
	// means the default). A request that needs more fails. There is no
	// upper bound; http_timeout still bounds the whole request, redirects
	// included.
	MaxRedirects int `json:"max_redirects,omitempty" jsonschema:"minimum=0"`
	// HTTP version: "1.1" forces HTTP/1.1. Absent, HTTP/2 is used over TLS
	// when the server offers it, else HTTP/1.1.
	Version string `json:"version,omitempty"`
	// Most bytes of a response body kept in memory for checks and
	// extractors (default "1mib", at most "1gib"), as a size such as
	// "512kb" or "4mib". The rest of a longer body is read and dropped so
	// the connection can be reused.
	MaxBodySize string `json:"max_body_size,omitempty"`
	// Read and drop response bodies without keeping them (default false),
	// except on a step with a check or extractor that reads the body (see
	// a step's discard_body, which overrides this for that step).
	DiscardBodies bool `json:"discard_bodies,omitempty"`
	// Most time to open a TCP connection, e.g. "5s" (default "10s").
	ConnectTimeout string `json:"connect_timeout,omitempty"`
	// Most time for the TLS handshake, e.g. "5s" (default "10s").
	TLSHandshakeTimeout string `json:"tls_handshake_timeout,omitempty"`
	// Most time to wait for the response headers once the request is sent,
	// e.g. "30s" (default: no limit of its own; the request timeout still
	// applies).
	ResponseHeaderTimeout string `json:"response_header_timeout,omitempty"`
}

// TLSOptions are the TLS options for requests to https URLs.
//
// client_cert, client_key and ca_cert each take a file path on the machine
// that runs the test, or inline PEM: a value that contains a -----BEGIN
// line. A relative path resolves against the working
// directory of the process that runs the test (a definition may arrive on
// standard input, so there is no definition directory). VoltTest Cloud
// runs need the inline form.
type TLSOptions struct {
	// Skip verification of the target's certificate, e.g. for a staging
	// server with a self-signed certificate (default false). When true,
	// ca_cert is accepted but has no effect: verification is skipped.
	InsecureSkipVerify bool `json:"insecure_skip_verify,omitempty"`
	// Lowest TLS version to accept: "1.0", "1.1", "1.2" or "1.3"
	// (default "1.2").
	MinVersion string `json:"min_version,omitempty"`
	// Client certificate for mutual TLS: a file path, or inline PEM with
	// one or more CERTIFICATE blocks. A relative path resolves against the
	// working directory of the process that runs the test; VoltTest Cloud
	// runs need the inline form. Requires client_key.
	ClientCert string `json:"client_cert,omitempty"`
	// Private key of client_cert: a file path, or inline PEM with an
	// unencrypted private key block. A relative path resolves against the
	// working directory of the process that runs the test; VoltTest Cloud
	// runs need the inline form. An inline key is a private key stored
	// with the definition as written; prefer a path for local and CI runs.
	// Requires client_cert.
	ClientKey string `json:"client_key,omitempty"`
	// Certificates trusted for the target, in addition to the system
	// roots: a file path, or inline PEM with one or more CERTIFICATE
	// blocks. A relative path resolves against the working directory of
	// the process that runs the test; VoltTest Cloud runs need the inline
	// form.
	CACert string `json:"ca_cert,omitempty"`
}

// Stage is one step of a staged load profile.
type Stage struct {
	// How long the stage lasts, e.g. "1m".
	Duration string `json:"duration" jsonschema:"required,minLength=1"`
	// Number of virtual users at the end of the stage.
	Target int `json:"target,omitempty" jsonschema:"minimum=0"`
}

// Load is a load profile: an arrival rate for the test or a scenario
// (types rate and ramping_rate), or a scenario's own pool of virtual users
// (types constant and stages).
type Load struct {
	// "rate" starts iterations at a constant rate for duration;
	// "ramping_rate" moves the rate through stages. On a scenario only,
	// "constant" runs virtual_users for duration and "stages" moves the
	// number of virtual users through stages.
	Type LoadType `json:"type" jsonschema:"required,enum=rate,enum=ramping_rate,enum=constant,enum=stages"`
	// Type rate: iterations started per time_unit (at least 1).
	Rate int `json:"rate,omitempty" jsonschema:"minimum=0"`
	// Types rate and ramping_rate: the period rate and stage targets count
	// over, e.g. "1m" (default "1s").
	TimeUnit string `json:"time_unit,omitempty"`
	// Types rate and constant: how long the load runs, e.g. "5m".
	Duration string `json:"duration,omitempty"`
	// Types ramping_rate and stages: the value moves linearly to each
	// stage's target over that stage's duration. The rate starts at 0; the
	// number of virtual users starts at initial_virtual_users.
	Stages []RateStage `json:"stages,omitempty"`
	// Type constant: the scenario's virtual users (at least 1).
	VirtualUsers int `json:"virtual_users,omitempty" jsonschema:"minimum=0"`
	// Types rate and ramping_rate: virtual users started before the load
	// begins (at least 1).
	PreallocatedVirtualUsers int `json:"preallocated_virtual_users,omitempty" jsonschema:"minimum=0"`
	// Types rate and ramping_rate: most virtual users the load may run
	// (default preallocated_virtual_users). When all are busy, an iteration
	// that is due is dropped and counted.
	MaxVirtualUsers int `json:"max_virtual_users,omitempty" jsonschema:"minimum=0"`
	// Type stages: virtual users running when the scenario's load starts
	// (at start_at); the first stage moves linearly from this number to its
	// target (default 1; 0 ramps from zero).
	InitialVirtualUsers *int `json:"initial_virtual_users,omitempty" jsonschema:"minimum=0"`
	// Any type, on a scenario's load: how long each virtual user may finish
	// its current iteration when the load's duration or last stage ends,
	// e.g. "30s" (default "30s"; "0s" cuts at once). No new iteration
	// starts; one still running is then cancelled and counted in
	// interrupted_iterations. The test-level stop_grace does not apply here.
	StopGrace string `json:"stop_grace,omitempty"`
	// Type stages: how long a virtual user removed by a stage that lowers
	// the target may finish its current iteration before it exits (default
	// "30s"; "0s" cuts at once). One still running is then cancelled and
	// counted in interrupted_iterations.
	RampDownGrace string `json:"ramp_down_grace,omitempty"`
}

// RateStage is one stage of a ramping_rate or stages load profile. The
// value moves linearly from the previous stage's target to Target; the
// first stage starts from 0 (ramping_rate) or initial_virtual_users
// (stages).
type RateStage struct {
	// How long the stage lasts, e.g. "1m".
	Duration string `json:"duration" jsonschema:"required,minLength=1"`
	// Type ramping_rate: iterations started per time_unit at the end of the
	// stage. Type stages: virtual users at the end of the stage.
	Target int `json:"target,omitempty" jsonschema:"minimum=0"`
}

// RegionWeight is one region's percent of the load.
type RegionWeight struct {
	// VoltTest Cloud region code, e.g. us-east-1.
	Region string `json:"region" jsonschema:"required,pattern=\\S"`
	// Percent of the virtual users; on VoltTest Cloud the weights sum to 100.
	Weight int `json:"weight,omitempty" jsonschema:"minimum=0"`
}

// Target describes the system under test.
type Target struct {
	// URL of the system under test, for reports. Requests go to the step URLs.
	URL string `json:"url,omitempty"`
	// How long an idle keep-alive connection stays open, e.g. "30s".
	IdleTimeout string `json:"idle_timeout,omitempty" contract:"loose"`
	// Base URL that step URLs starting with "/" are appended to. An absolute
	// http(s) URL or a ${variable}, without a query string or fragment.
	BaseURL string `json:"base_url,omitempty" contract:"v2"`
}

// Scenario is a sequence of steps a virtual user runs.
type Scenario struct {
	// Scenario name, unique within the test.
	Name string `json:"name" jsonschema:"required,pattern=\\S"`
	// Free-text description of the scenario.
	Description string `json:"description,omitempty"`
	// Relative chance that a virtual user runs this scenario. Not allowed
	// on a run_once or teardown scenario, or with load.
	Weight int `json:"weight,omitempty" jsonschema:"minimum=0"`
	// Default think_time for each step that sets none: a duration or a
	// range such as "1s-3s".
	ThinkTime string `json:"think_time,omitempty"`
	// Keep cookies between the steps of a run. On by default for
	// schema_version 2.x, where false opts out; off by default in v1. An
	// explicit false is kept when the definition is written back.
	AutoHandleCookies *bool `json:"auto_handle_cookies,omitempty"`
	// Steps, run in order. A step may be a step group.
	Steps []Step `json:"steps" jsonschema:"required,minItems=1"`
	// CSV or JSON data whose columns or keys fill ${name} variables.
	DataConfig *DataConfig `json:"data_config,omitempty"`
	// Pass/fail rules for this scenario, e.g. "iteration_duration_p95 < 5s".
	Thresholds []ThresholdConfig `json:"thresholds,omitempty" contract:"v2"`
	// Setup: run the scenario once, by one virtual user, before the load
	// starts. The variables it extracts are readable by every virtual user
	// as ${variable}. Takes no weight and is never picked for the load.
	RunOnce bool `json:"run_once,omitempty" contract:"v2"`
	// Run the scenario once after every virtual user has stopped, with the
	// setup variables readable, e.g. to delete seeded data. Takes no weight
	// and is never picked for the load.
	Teardown bool `json:"teardown,omitempty" contract:"v2"`
	// The scenario's own load, run alongside the test-level load model and
	// independent of it: its own virtual users ("constant" or "stages") or
	// arrival rate ("rate" or "ramping_rate"). The scenario takes no weight
	// and is never picked by the test-level virtual users. Not allowed on a
	// run_once or teardown scenario. On VoltTest Cloud the backend refuses,
	// before the run starts, a pool whose peak in a region is below that
	// region's node count, the peak being virtual_users for constant, the
	// larger of initial_virtual_users and the highest stage target for
	// stages, and max_virtual_users (or preallocated_virtual_users when
	// max_virtual_users is absent) for rate and ramping_rate.
	Load *Load `json:"load,omitempty" contract:"v2"`
	// How long after the load starts the scenario's own load starts, e.g.
	// "30s" (greater than zero, no upper bound; absent starts it with the
	// load). Requires load. A run lasts until every scenario ends: the
	// latest start_at plus pool length over the pools, and the test-level
	// model's length for the scenarios that share it.
	StartAt string `json:"start_at,omitempty" contract:"v2"`
	// Preset cookies for the scenario, by name, as for the test-level
	// cookies; one of the same name replaces the test's.
	Cookies map[string]string `json:"cookies,omitempty" contract:"v2"`
	// Variables for the virtual users running the scenario, as for the
	// test-level vars; one of the same name replaces the test's.
	Vars map[string]string `json:"vars,omitempty" contract:"v2"`
}

// IsLoad reports whether the scenario takes part in the load: it is
// neither a run_once nor a teardown scenario. A scenario with its own Load
// is a load scenario.
func (s Scenario) IsLoad() bool { return !s.RunOnce && !s.Teardown }

// DataConfig is a scenario's data source.
type DataConfig struct {
	// Name or path of the data file.
	DataSource string `json:"data_source,omitempty"`
	// Base64-encoded content of the data file.
	DataContent string `json:"data_content,omitempty"`
	// DataDownloadURL is dispatch-only: the VoltTest backend sets it when it
	// sends a run to the engines. It counts as a data source in v2. The
	// backend must strip any user-supplied value before calling Parse on a
	// submitted definition. The published schema leaves it out.
	DataDownloadURL string `json:"data_download_url,omitempty" contract:"loose" jsonschema:"-"`
	// File format: "csv", or "json" (an array of objects, one per row,
	// whose keys name the variables).
	DataFormat string `json:"data_format,omitempty" jsonschema:"required,enum=csv,enum=json"`
	// The first row holds column names. CSV only.
	HasHeader bool `json:"has_header,omitempty"`
	// How virtual users take rows: in order, at random, or each row once.
	Mode string `json:"mode,omitempty" jsonschema:"required,enum=sequential,enum=random,enum=unique"`
}

// Step is one HTTP request in a scenario, or a step group: a step with
// Steps, which has no request of its own.
type Step struct {
	// Step name, unique within the scenario, counting step groups and the
	// steps inside them.
	Name string `json:"name" jsonschema:"required,pattern=\\S"`
	// The HTTP request to send. A step group has none.
	Request Request `json:"request" jsonschema:"required"`
	// Values to capture from the response into ${variable_name}.
	Extract []Extract `json:"extract,omitempty"`
	// Assertions on the response. Each check is reported separately. When
	// no response is received (a transport error or a timeout), the
	// step's checks are not evaluated and not counted.
	Checks []Check `json:"checks,omitempty" contract:"v2"`
	// Validate holds the v1 response checks.
	Validate []LegacyValidator `json:"validate,omitempty" contract:"v1" jsonschema:"-"`
	// Pause after the step: a duration such as "500ms", or a range such
	// as "1s-3s" for a uniformly random pause between the two, bounds
	// included.
	ThinkTime string `json:"think_time,omitempty"`
	// Pass/fail rules for this step, e.g. "p99 < 1s".
	Thresholds []ThresholdConfig `json:"thresholds,omitempty" contract:"v2"`
	// Overrides http.follow_redirects for this step.
	FollowRedirects *bool `json:"follow_redirects,omitempty" contract:"v2"`
	// Overrides http.max_redirects for this step (0 or absent means the
	// test-level value). Only applies when the step follows redirects.
	MaxRedirects int `json:"max_redirects,omitempty" contract:"v2" jsonschema:"minimum=0"`
	// Run the step only on a virtual user's first iteration of this
	// scenario, and again on the next iteration while it fails or any of
	// its extractors misses. The variables it extracts stay set for later
	// iterations, which skip the step. Not allowed in a run_once or
	// teardown scenario.
	OncePerVU bool `json:"once_per_vu,omitempty" contract:"v2"`
	// Overrides http.discard_bodies for this step: true reads and drops
	// the response body without keeping it, false keeps it. true is not
	// allowed when the step reads the body: a body check with equals,
	// contains, not_contains or matches, a json or html check, or a json,
	// regex or html extractor.
	DiscardBody *bool `json:"discard_body,omitempty" contract:"v2"`
	// How the response body is read: "text" (the default) or "binary".
	// With "binary" regex extractors and body checks read the raw bytes,
	// and json and html checks and extractors are not allowed.
	ResponseType ResponseType `json:"response_type,omitempty" contract:"v2"`
	// Preset cookies for this step, by name, as for the test-level cookies;
	// one of the same name replaces the scenario's or the test's.
	Cookies map[string]string `json:"cookies,omitempty" contract:"v2"`
	// Drop the cookies the virtual user kept from earlier responses before
	// this step's request (default false). Not allowed when the scenario's
	// auto_handle_cookies is false, where it has no effect.
	ClearCookies bool `json:"clear_cookies,omitempty" contract:"v2"`
	// Skip the step when this expression is true, e.g. "cart_count == 0".
	// It is evaluated before the request is built; a skipped step sends
	// nothing. Not with only_if.
	SkipIf string `json:"skip_if,omitempty" contract:"v2"`
	// Run the step only when this expression is true; otherwise it is
	// skipped as with skip_if. Not with skip_if.
	OnlyIf string `json:"only_if,omitempty" contract:"v2"`
	// Run the step, or a step group's steps, again until an expression is
	// true, at most max times. Not with foreach, not on a step inside a
	// step group, and not in a run_once or teardown scenario.
	Repeat *Repeat `json:"repeat,omitempty" contract:"v2"`
	// Run the step once per element of a list. Not with repeat, not on a
	// step inside a step group, and not in a run_once or teardown scenario.
	Foreach *Foreach `json:"foreach,omitempty" contract:"v2"`
	// Makes the step a step group: these steps run in order, as one unit,
	// under the group's repeat. A group takes only name, repeat and steps.
	// Its steps cannot be groups and take no repeat, foreach or
	// once_per_vu. Not in a run_once or teardown scenario.
	Steps []Step `json:"steps,omitempty" contract:"v2"`
}

// Repeat runs a step or a step group again until an expression is true.
type Repeat struct {
	// Expression that ends the repeat when true, evaluated after each run.
	Until string `json:"until" jsonschema:"required,pattern=\\S"`
	// Most runs, from 1 to 1000. Reaching it while until is still false
	// fails the step.
	Max int `json:"max" jsonschema:"required,minimum=1,maximum=1000"`
}

// Foreach runs a step once per element of a list variable.
type Foreach struct {
	// Variable that holds the current element, readable as ${var} and in
	// expressions: ASCII letters, digits and "_", not starting with a
	// digit, not "env" or "fake", and not a variable the scenario extracts,
	// a setup global or a vars name.
	Var string `json:"var" jsonschema:"required,pattern=^[A-Za-z_][A-Za-z0-9_]*$"`
	// The list: exactly one ${name} reference to a variable that holds a
	// JSON array, such as one extracted with pick "all".
	In string `json:"in" jsonschema:"required,pattern=^\\$\\{[A-Za-z0-9_-]+\\}$"`
}

// IsGroup reports whether the step is a step group: it has steps.
func (s Step) IsGroup() bool { return len(s.Steps) > 0 }

// MarshalJSON leaves request out of a step group, which has none.
func (s Step) MarshalJSON() ([]byte, error) {
	type plain Step
	var v any = plain(s)
	if s.IsGroup() {
		// Declared before plain, Request shadows plain's request without
		// moving the other keys.
		v = struct {
			Request *Request `json:"request,omitempty"`
			plain
		}{plain: plain(s)}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // encoding/json escapes again when asked to
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// LeafSteps returns the steps of the scenario that send a request, in
// document order, with each step group replaced by its steps. The
// pointers point into s.Steps, so a change through them changes the
// definition. The groups themselves are the elements of s.Steps whose
// IsGroup is true.
func (s Scenario) LeafSteps() []*Step {
	return appendLeaves(nil, s.Steps)
}

func appendLeaves(out []*Step, steps []Step) []*Step {
	for i := range steps {
		if steps[i].IsGroup() {
			out = appendLeaves(out, steps[i].Steps)
			continue
		}
		out = append(out, &steps[i])
	}
	return out
}

// forEachLeaf calls fn with each leaf step of steps, in document order,
// and its path: steps sit under prefix+".steps".
func forEachLeaf(steps []Step, prefix string, fn func(path string, st *Step)) {
	for i := range steps {
		p := indexPath(prefix+".steps", i)
		if steps[i].IsGroup() {
			forEachLeaf(steps[i].Steps, p, fn)
			continue
		}
		fn(p, &steps[i])
	}
}

// ResponseType is how a step reads its response body.
type ResponseType string

// The response types. The zero ResponseType means ResponseText.
const (
	ResponseText   ResponseType = "text"
	ResponseBinary ResponseType = "binary"
)

// Request is an HTTP request template. URL, Body, header values, the
// string leaves of BodyJSON, the values of Fields and the file names of
// Files may contain ${variable} references; object keys, field names and
// file contents are sent as written.
type Request struct {
	// HTTP method, e.g. "GET".
	Method string `json:"method" jsonschema:"required,minLength=1"`
	// An absolute http(s) URL, a URL starting with ${variable}, or a path
	// starting with "/" that is appended to target.base_url.
	URL string `json:"url" jsonschema:"required,minLength=1"`
	// Request body, sent as written after ${variable} substitution. Only
	// allowed with body_type "raw" (the default).
	Body string `json:"body,omitempty"`
	// How the body is built: "raw" (the default) sends body (or
	// body_base64) as written, "json" sends body_json, "form" sends fields
	// URL-encoded and "multipart" sends fields and files as
	// multipart/form-data. With "json" and "form" the Content-Type header
	// is set for you unless headers sets it; with "multipart" it is always
	// set for you, with the part boundary, and headers must not set it.
	BodyType BodyType `json:"body_type,omitempty" contract:"v2"`
	// The body for body_type "json": a JSON object or array, sent with its
	// keys, key order and number literals as written. Each string value
	// is substituted on its own. A string that is exactly one ${...}
	// reference without a filter takes the type of its value: a number,
	// true/false, an array or an object is sent unquoted, and so is an
	// integer built-in such as ${random_int(1,3)}. Any other string value
	// is sent as a string. Keys are sent as written, never substituted.
	BodyJSON JSONBody `json:"body_json,omitempty" contract:"v2"`
	// The fields for body_type "form" or "multipart", in the order
	// written. Each value is substituted; names are sent as written, never
	// substituted. Names must be unique. With "form" they are sent
	// URL-encoded, and absent or {} sends an empty body. With "multipart"
	// each field is one part, before the files.
	Fields Fields `json:"fields,omitempty" contract:"v2"`
	// The files for body_type "multipart", by form field name, which must
	// not also be a name in fields. Each file is one part, after the
	// fields, in field name order. Not available on VoltTest Cloud yet.
	Files map[string]FilePart `json:"files,omitempty" contract:"v2"`
	// A binary body for body_type "raw", as standard base64 with padding
	// and no line breaks. It is decoded once and sent as is, without
	// substitution. Not allowed with body. Not available on VoltTest Cloud
	// yet.
	BodyBase64 string `json:"body_base64,omitempty" contract:"v2"`
	// Most time for the whole request, redirects and the body included,
	// e.g. "2s" (default http_timeout). A request that takes longer fails.
	Timeout string `json:"timeout,omitempty" contract:"v2"`
	// Header holds request headers under the v1 key "header". v1 readers
	// differ in how they treat header and headers together, so Parse
	// exposes both as sent.
	Header map[string]string `json:"header,omitempty" contract:"v1" jsonschema:"-"`
	// Request headers: a value is a string, or an array of strings to send
	// the header once per value, in order. A Host header sets the host the
	// request is sent as (one value only); the request still goes to the
	// URL's host. A User-Agent header replaces the default one.
	Headers map[string]HeaderValues `json:"headers,omitempty" contract:"loose"`
}

// FilePart is one file of a multipart request body.
type FilePart struct {
	// Path of the file on the machine that runs the test. A relative path
	// resolves against the working directory of the process that runs the
	// test. Use instead of content_base64. Not blank, and no line break or
	// NUL.
	Path string `json:"path,omitempty"`
	// The file's content as standard base64 with padding and no line
	// breaks. Use instead of path.
	ContentBase64 string `json:"content_base64,omitempty"`
	// File name sent in the part's Content-Disposition header, after
	// ${variable} substitution (default: the last element of path, or the
	// field name with content_base64).
	Filename string `json:"filename,omitempty"`
	// Media type sent in the part's Content-Type header, e.g. "image/png"
	// (default "application/octet-stream").
	ContentType string `json:"content_type,omitempty"`
}

// MarshalJSON keeps an empty but non-nil header map ("header": {}) in the
// output: in v1 an empty header map can shadow headers.
func (r Request) MarshalJSON() ([]byte, error) {
	type plain Request
	out := struct {
		plain
		Header  *map[string]string       `json:"header,omitempty"`
		Headers *map[string]HeaderValues `json:"headers,omitempty"`
	}{plain: plain(r)}
	if r.Header != nil {
		out.Header = &r.Header
	}
	if r.Headers != nil {
		out.Headers = &r.Headers
	}
	return json.Marshal(out)
}

// BodyType is how a request body is built.
type BodyType string

// The body types. The zero BodyType means BodyTypeRaw.
const (
	BodyTypeRaw       BodyType = "raw"
	BodyTypeJSON      BodyType = "json"
	BodyTypeForm      BodyType = "form"
	BodyTypeMultipart BodyType = "multipart"
)

// BodyKind returns the request's body type: BodyTypeRaw when body_type
// is absent or "".
func (r Request) BodyKind() BodyType {
	if r.BodyType == "" {
		return BodyTypeRaw
	}
	return r.BodyType
}

// Extract captures a value from a response.
type Extract struct {
	// Name of the variable that receives the value.
	VariableName string `json:"variable_name" jsonschema:"required,pattern=\\S"`
	// What to read, by type: a JSON path, a regex, a header name, a cookie
	// name or a CSS selector. When redirects are followed, header, json,
	// regex and html read the final response, and cookie reads the
	// Set-Cookie headers of the final response and of every redirect
	// followed to reach it, a later one winning for the same name.
	Selector string `json:"selector" jsonschema:"required,pattern=\\S"`
	// HTML attribute to read, for type html.
	Attribute string `json:"attribute,omitempty"`
	// Where to read the value from: the body as JSON ("json"), the body
	// with a regex ("regex"), a response header ("header"), a cookie set
	// by the response or by any redirect followed to reach it ("cookie"),
	// or the body as HTML ("html").
	Type string `json:"type" jsonschema:"required,enum=json,enum=regex,enum=header,enum=cookie,enum=html"`
	// Treat a miss (nothing found, or a JSON null) as a failed check
	// labelled "extract <variable_name>". Required or not, an extractor
	// always assigns its variable; a miss assigns "".
	Required bool `json:"required,omitempty" contract:"v2"`
	// What a miss does when required is true: "record", "fail" or "abort"
	// (the default). Only applies with required: true.
	OnFail FailMode `json:"on_fail,omitempty" contract:"v2"`
	// Which value to take when the selector finds several or an array, for
	// types json, regex and html: "random", "first", "last" or "all" (every
	// value as a JSON array).
	Pick Pick `json:"pick,omitempty" contract:"v2"`
}

// Pick is which value an extractor takes when its selector matches more
// than one.
type Pick string

// The picks.
const (
	PickRandom Pick = "random"
	PickFirst  Pick = "first"
	PickLast   Pick = "last"
	PickAll    Pick = "all"
)

// Check is a v2 assertion on a step's response: a type, an optional
// selector and exactly one operator. See Label for how a check is named in
// results and FailMode for what a failure does.
//
// A key with its zero value ("" or []) counts as absent, except equals and
// contains, which are values whenever written ("equals": "" checks for an
// empty value), and exists, missing and empty, which must be true when
// written. Values and selectors are literal: a ${variable} in them is
// compared as written, not replaced.
type Check struct {
	// Name of the check in results, unique within the step; required for
	// type assert. Without one, the check is named after its type,
	// selector, operator and value, and for every type except header that
	// label contains the expected value as written; give a check whose
	// value is sensitive a name.
	Name string `json:"name,omitempty"`
	// What the check reads: "status" (the status code), "header", "body",
	// "json" (the body as JSON), "html" (the body as HTML), "timing", "url"
	// (the final URL after redirects; the request URL when redirects are
	// not followed) or "cookie". When redirects are followed, status,
	// header, body, json and html read the final response, and cookie
	// reads the Set-Cookie headers of the final response and of every
	// redirect followed to reach it, a later one winning for the same name.
	// "assert" evaluates expr against the response.
	Type CheckType `json:"type" jsonschema:"required,enum=status,enum=header,enum=body,enum=json,enum=html,enum=timing,enum=url,enum=cookie,enum=assert"`
	// What to read, by type: the header name for header (case-insensitive;
	// when the header repeats, any of its values may match), a JSON path
	// such as "$.data.items[0].id" for json, a CSS selector for html, the
	// cookie name for cookie. Other types take no selector.
	Selector string `json:"selector,omitempty"`

	// status: the status code is one of these.
	In []int `json:"in,omitempty"`
	// status: the status code is none of these.
	NotIn []int `json:"not_in,omitempty"`
	// status: the status code is within [low, high], both included.
	Between []int `json:"between,omitempty"`
	// header, body, html, url (a string) and json (a string, number or
	// true/false): the value is exactly this. For html, the value is the
	// text content of the first matching element (its text and its
	// descendants' text, without tags) with leading and trailing white
	// space trimmed. For json, equality is between scalars only; an object
	// or array never equals. A string equals a string with the same text,
	// true/false the same value, and a number a number with the same exact
	// decimal value (42 equals 42.0 and 4.2e1, with no float64 rounding);
	// a string never equals a number or true/false.
	Equals *Scalar `json:"equals,omitempty"`
	// header, body, url (a non-empty string): the value contains this
	// text. json (a string, number or true/false): the array contains
	// this element, compared as equals compares; any other JSON kind
	// fails the check.
	Contains *Scalar `json:"contains,omitempty"`
	// body: the body does not contain this text.
	NotContains string `json:"not_contains,omitempty"`
	// header, body, url and json: the value matches this regular
	// expression (RE2 syntax). For json, it applies to string values; any
	// other JSON kind fails the check.
	Matches string `json:"matches,omitempty"`
	// header, html, cookie and json: the header, element, cookie or path
	// is present. For header, it reads the final response. For cookie, it
	// reads the Set-Cookie headers of the response and of every redirect
	// followed to reach it, a later one winning for the same name. Must be
	// true.
	Exists *bool `json:"exists,omitempty"`
	// json: the path is absent. Must be true.
	Missing *bool `json:"missing,omitempty"`
	// body: the body is empty. Must be true.
	Empty *bool `json:"empty,omitempty"`
	// json: the value at the path is of this JSON kind: "array",
	// "object", "string", "number", "bool" or "null".
	Is string `json:"is,omitempty"`
	// json: the array at the path has this many elements, e.g. "> 0". A
	// value that is not an array fails the check.
	Count string `json:"count,omitempty"`
	// body: the size of the decoded response body in bytes, e.g. "< 50kb".
	Size string `json:"size,omitempty"`
	// timing: the total request time, the same measurement the duration
	// thresholds use, e.g. "< 500ms".
	Duration string `json:"duration,omitempty"`
	// assert: an expression over the response that must be true, e.g.
	// "status == 200 && duration_ms < 500". It takes no selector.
	Expr string `json:"expr,omitempty"`

	// What a failure does: "record" counts it toward the check rate only,
	// "fail" also fails the run, and "abort" fails the run and ends the
	// virtual user's current iteration after the step's other checks are
	// evaluated (the test goes on). Defaults: "fail" for status, "record"
	// for every other type.
	OnFail FailMode `json:"on_fail,omitempty"`
}

// LegacyValidator is a v1 response check (a step's "validate" entry).
type LegacyValidator struct {
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
	Selector string `json:"selector,omitempty"`
	// Expected is decoded with encoding/json defaults: numbers are float64.
	Expected any `json:"expected,omitempty"`
}

// v1Wrapper is what the v1 path decodes into. Its own fields sit one level
// above the embedded Definition, so they win for these names, including
// case variants: schema_version comes from the exact-key peek only, and
// api_key is read into a field that is never marshalled.
type v1Wrapper struct {
	*Definition
	SchemaVersion json.RawMessage `json:"schema_version"`
	APIKey        string          `json:"api_key" contract:"v1,loose"`
}
