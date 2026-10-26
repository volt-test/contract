package definition

import (
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
	// Staged load: each stage moves to its target number of virtual users
	// over its duration. Use instead of virtual_users, duration and ramp_up.
	Stages []Stage `json:"stages,omitempty"`

	// Timeout for each HTTP request, e.g. "30s".
	HTTPTimeout string `json:"http_timeout,omitempty"`
	// Log request and response details.
	HTTPDebug bool `json:"http_debug,omitempty"`

	// The system under test.
	Target *Target `json:"target,omitempty"`
	// Scenarios that virtual users run, picked at random by weight.
	Scenarios []Scenario `json:"scenarios" jsonschema:"required,minItems=1"`
	// Share of the virtual users per cloud region.
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
// redirect options with its own follow_redirects and max_redirects.
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

// RegionWeight is one region's share of the load.
type RegionWeight struct {
	// Cloud region name.
	Region string `json:"region" jsonschema:"required,pattern=\\S"`
	// Relative share of the virtual users.
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
	// Relative chance that a virtual user runs this scenario.
	Weight int `json:"weight,omitempty" jsonschema:"minimum=0"`
	// Pause after each run of the scenario, e.g. "1s".
	ThinkTime string `json:"think_time,omitempty"`
	// Keep cookies between the steps of a run.
	AutoHandleCookies bool `json:"auto_handle_cookies,omitempty"`
	// Steps, run in order.
	Steps []Step `json:"steps" jsonschema:"required,minItems=1"`
	// CSV data whose columns fill ${column} variables.
	DataConfig *DataConfig `json:"data_config,omitempty"`
	// Pass/fail rules for this scenario, e.g. "iteration_duration_p95 < 5s".
	Thresholds []ThresholdConfig `json:"thresholds,omitempty" contract:"v2"`
}

// DataConfig is a scenario's data source.
type DataConfig struct {
	// Name or path of the CSV file.
	DataSource string `json:"data_source,omitempty"`
	// Base64-encoded content of the CSV file.
	DataContent string `json:"data_content,omitempty"`
	// DataDownloadURL is dispatch-only: the VoltTest backend sets it when it
	// sends a run to the engines. It counts as a data source in v2. The
	// backend must strip any user-supplied value before calling Parse on a
	// submitted definition. The published schema leaves it out.
	DataDownloadURL string `json:"data_download_url,omitempty" contract:"loose" jsonschema:"-"`
	// File format.
	DataFormat string `json:"data_format,omitempty" jsonschema:"required,enum=csv"`
	// The first row holds column names.
	HasHeader bool `json:"has_header,omitempty"`
	// How virtual users take rows: in order, at random, or each row once.
	Mode string `json:"mode,omitempty" jsonschema:"required,enum=sequential,enum=random,enum=unique"`
}

// Step is one HTTP request in a scenario.
type Step struct {
	// Step name, unique within the scenario.
	Name string `json:"name" jsonschema:"required,pattern=\\S"`
	// The HTTP request to send.
	Request Request `json:"request" jsonschema:"required"`
	// Values to capture from the response into ${variable_name}.
	Extract []Extract `json:"extract,omitempty"`
	// Assertions on the response. Each check is reported separately. When
	// no response is received (a transport error or a timeout), the
	// step's checks are not evaluated and not counted.
	Checks []Check `json:"checks,omitempty" contract:"v2"`
	// Validate holds the v1 response checks.
	Validate []LegacyValidator `json:"validate,omitempty" contract:"v1" jsonschema:"-"`
	// Pause after the step, e.g. "500ms".
	ThinkTime string `json:"think_time,omitempty"`
	// Pass/fail rules for this step, e.g. "p99 < 1s".
	Thresholds []ThresholdConfig `json:"thresholds,omitempty" contract:"v2"`
	// Overrides http.follow_redirects for this step.
	FollowRedirects *bool `json:"follow_redirects,omitempty" contract:"v2"`
	// Overrides http.max_redirects for this step (0 or absent means the
	// test-level value). Only applies when the step follows redirects.
	MaxRedirects int `json:"max_redirects,omitempty" contract:"v2" jsonschema:"minimum=0"`
}

// Request is an HTTP request template. URL, Body and header values may
// contain ${variable} references.
type Request struct {
	// HTTP method, e.g. "GET".
	Method string `json:"method" jsonschema:"required,minLength=1"`
	// An absolute http(s) URL, a URL starting with ${variable}, or a path
	// starting with "/" that is appended to target.base_url.
	URL string `json:"url" jsonschema:"required,minLength=1"`
	// Request body.
	Body string `json:"body,omitempty"`
	// Header holds request headers under the v1 key "header". v1 readers
	// differ in how they treat header and headers together, so Parse
	// exposes both as sent.
	Header map[string]string `json:"header,omitempty" contract:"v1" jsonschema:"-"`
	// Request headers.
	Headers map[string]string `json:"headers,omitempty" contract:"loose"`
}

// MarshalJSON keeps an empty but non-nil header map ("header": {}) in the
// output: in v1 an empty header map can shadow headers.
func (r Request) MarshalJSON() ([]byte, error) {
	type plain Request
	out := struct {
		plain
		Header  *map[string]string `json:"header,omitempty"`
		Headers *map[string]string `json:"headers,omitempty"`
	}{plain: plain(r)}
	if r.Header != nil {
		out.Header = &r.Header
	}
	if r.Headers != nil {
		out.Headers = &r.Headers
	}
	return json.Marshal(out)
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
	// Treat a miss (nothing found) as a failed check labelled
	// "extract <variable_name>". Required or not, an extractor always
	// assigns its variable; a miss assigns "".
	Required bool `json:"required,omitempty" contract:"v2"`
	// What a miss does when required is true: "record", "fail" or "abort"
	// (the default). Only applies with required: true.
	OnFail FailMode `json:"on_fail,omitempty" contract:"v2"`
}

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
	// Name of the check in results, unique within the step. Without one,
	// the check is named after its type, selector, operator and value, and
	// for every type except header that label contains the expected value
	// as written; give a check whose value is sensitive a name.
	Name string `json:"name,omitempty"`
	// What the check reads: "status" (the status code), "header", "body",
	// "json" (the body as JSON), "html" (the body as HTML), "timing", "url"
	// (the final URL after redirects; the request URL when redirects are
	// not followed) or "cookie". When redirects are followed, status,
	// header, body, json and html read the final response, and cookie
	// reads the Set-Cookie headers of the final response and of every
	// redirect followed to reach it, a later one winning for the same name.
	Type CheckType `json:"type" jsonschema:"required,enum=status,enum=header,enum=body,enum=json,enum=html,enum=timing,enum=url,enum=cookie"`
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
