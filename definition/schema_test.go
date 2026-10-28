package definition_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/invopop/jsonschema"
	validator "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/volt-test/contract/definition"
)

var update = flag.Bool("update", false, "regenerate schema/test-definition.schema.json")

const (
	schemaPath = "../schema/test-definition.schema.json"
	schemaID   = "https://raw.githubusercontent.com/volt-test/contract/main/schema/test-definition.schema.json"
)

// generateSchema builds the published JSON Schema for v2 definitions from
// the Go types. Fields tagged jsonschema:"-" (v1-only and dispatch-only)
// are left out; every object rejects unknown keys.
func generateSchema(t *testing.T) []byte {
	t.Helper()
	var v2 []any
	for _, s := range definition.SupportedSchemaVersions() {
		if v, err := definition.ParseSchemaVersion(s); err == nil && v.Major >= 2 {
			v2 = append(v2, s)
		}
	}
	thresholdType := reflect.TypeOf(definition.ThresholdConfig{})
	r := &jsonschema.Reflector{
		RequiredFromJSONSchemaTags: true,
		ExpandedStruct:             true,
		Mapper: func(t reflect.Type) *jsonschema.Schema {
			switch t {
			case reflect.TypeOf(definition.SchemaVersion{}):
				return &jsonschema.Schema{Type: "string", Enum: v2}
			case thresholdType:
				return &jsonschema.Schema{Ref: "#/$defs/Threshold"}
			case reflect.TypeOf(definition.Scalar{}):
				return &jsonschema.Schema{AnyOf: []*jsonschema.Schema{
					{Type: "string"}, {Type: "number"}, {Type: "boolean"},
				}}
			}
			return nil
		},
	}
	// Field descriptions come from the field comments in types.go.
	if err := r.AddGoComments("github.com/volt-test/contract/definition", "./"); err != nil {
		t.Fatal(err)
	}
	s := r.Reflect(&definition.Definition{})
	s.Definitions["Threshold"] = thresholdSchema(t, thresholdType)
	s.ID = schemaID
	s.Title = "VoltTest test definition"

	// Type descriptions are written here rather than taken from the Go
	// doc comments, which follow Go conventions ("Definition is ...").
	s.Description = "A VoltTest load test: the load profile, the system under test and the scenarios that virtual users run."
	typeDescriptions := map[string]string{
		"Stage":        "One stage of a staged load profile.",
		"RegionWeight": "One cloud region's share of the virtual users.",
		"Target":       "The system under test.",
		"Scenario":     "A sequence of steps that a virtual user runs.",
		"DataConfig":   "A CSV data source. Each virtual user takes rows according to mode.",
		"Step":         "One HTTP request in a scenario.",
		"Request":      "An HTTP request. The URL, body and header values may contain ${variable} references.",
		"Extract":      "A value to capture from the response.",
		"Check":        "An assertion on the response: a type, a selector where the type needs one, and exactly one operator.",
		"Threshold":    "A pass/fail rule: an expression such as \"p95 < 500ms\", or an object with the expression and abort settings.",
		"HTTPOptions":  "HTTP client options for every request: redirects and the HTTP version.",
		"TLSOptions":   "TLS options for HTTPS targets: verification, minimum version and client certificates.",
		"Load":         "An arrival-rate load profile: iterations start at a set rate, picked up by idle virtual users.",
		"RateStage":    "One stage of a ramping_rate load profile. The rate starts at 0 and moves linearly to each stage's target over that stage's duration.",
	}
	for name, def := range s.Definitions {
		desc, ok := typeDescriptions[name]
		if !ok {
			t.Fatalf("no description for $defs.%s", name)
		}
		def.Description = desc
	}

	// Rules Parse applies that the reflector cannot derive from the types.
	// They may only make the schema stricter where Parse is stricter still:
	// the schema must never reject a definition Parse accepts.
	// Load model: non-empty stages, at least one virtual user, or load
	// alone. The other keys may be written with their zero value.
	loadModels(s)
	s.Definitions["DataConfig"].AnyOf = []*jsonschema.Schema{
		{Required: []string{"data_source"}},
		{Required: []string{"data_content"}},
	}
	checkSchemaRules(t, s)
	setupTeardownRules(t, s)

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

// loadModels adds the load model rules: the top-level choice between
// stages, virtual_users and load, and the keys each load type requires or
// forbids. Zero values read as absent, so a forbidden key may still be
// written with its zero value.
func loadModels(s *jsonschema.Schema) {
	zero, one := uint64(0), uint64(1)
	noLoad := &jsonschema.Schema{Required: []string{"load"}}
	staged := jsonschema.NewProperties()
	staged.Set("stages", &jsonschema.Schema{MinItems: &one})
	constant := jsonschema.NewProperties()
	constant.Set("virtual_users", &jsonschema.Schema{Minimum: json.Number("1")})
	loadOnly := jsonschema.NewProperties()
	loadOnly.Set("virtual_users", &jsonschema.Schema{Maximum: json.Number("0")})
	loadOnly.Set("duration", &jsonschema.Schema{MaxLength: &zero})
	loadOnly.Set("ramp_up", &jsonschema.Schema{MaxLength: &zero})
	loadOnly.Set("stages", &jsonschema.Schema{MaxItems: &zero})
	s.AnyOf = []*jsonschema.Schema{
		{Required: []string{"stages"}, Properties: staged, Not: noLoad},
		{Required: []string{"virtual_users"}, Properties: constant, Not: noLoad},
		{Required: []string{"load"}, Properties: loadOnly},
	}

	rate := jsonschema.NewProperties()
	rate.Set("type", &jsonschema.Schema{Const: string(definition.LoadRate)})
	rate.Set("rate", &jsonschema.Schema{Minimum: json.Number("1")})
	rate.Set("duration", &jsonschema.Schema{MinLength: &one})
	rate.Set("stages", &jsonschema.Schema{MaxItems: &zero})
	ramping := jsonschema.NewProperties()
	ramping.Set("type", &jsonschema.Schema{Const: string(definition.LoadRampingRate)})
	ramping.Set("rate", &jsonschema.Schema{Maximum: json.Number("0")})
	ramping.Set("duration", &jsonschema.Schema{MaxLength: &zero})
	ramping.Set("stages", &jsonschema.Schema{MinItems: &one})
	s.Definitions["Load"].AnyOf = []*jsonschema.Schema{
		{Required: []string{"rate", "duration"}, Properties: rate},
		{Required: []string{"stages"}, Properties: ramping},
	}
}

// checkSchemaRules adds the check, required-extractor and expected_statuses rules
// that a JSON Schema can state. Every optional key reads its zero value
// ("" or []) as absent, so no optional key gets a pattern, minLength or
// minItems, and enums also accept "". The rules Parse alone applies (one
// operator per check, which operators and selectors a type takes, value
// kinds, regular expressions, comparisons, unique labels) are listed in
// knownSchemaLooser.
func checkSchemaRules(t *testing.T, s *jsonschema.Schema) {
	t.Helper()
	prop := func(def *jsonschema.Schema, name string) *jsonschema.Schema {
		p, ok := def.Properties.Get(name)
		if !ok {
			t.Fatalf("schema has no property %s", name)
		}
		return p
	}
	zero, two := uint64(0), uint64(2)
	// An enum that also takes "" (absent), keeping the string type.
	optionalEnum := func(p *jsonschema.Schema, values []string) {
		enum := make([]any, len(values))
		for i, v := range values {
			enum[i] = v
		}
		p.AnyOf = []*jsonschema.Schema{{Enum: enum}, {MaxLength: &zero}}
	}
	// HTTP status codes, on the items of a list.
	statusCodes := func(p *jsonschema.Schema) {
		if p.Items == nil {
			t.Fatal("status code list has no items schema")
		}
		p.Items.Minimum, p.Items.Maximum = json.Number("100"), json.Number("599")
	}

	check := s.Definitions["Check"]
	optionalEnum(prop(check, "on_fail"), definition.FailModesForTest)
	optionalEnum(prop(check, "is"), definition.JSONKindsForTest)
	for _, k := range []string{"in", "not_in", "between"} {
		statusCodes(prop(check, k))
	}
	prop(check, "between").AnyOf = []*jsonschema.Schema{{MaxItems: &zero}, {MinItems: &two, MaxItems: &two}}
	for _, k := range []string{"exists", "missing", "empty"} {
		prop(check, k).Const = true
	}
	statusCodes(prop(s, "expected_statuses"))

	// A non-empty on_fail on an extractor needs required: true, as in
	// Parse, which reads an empty one as absent.
	extract := s.Definitions["Extract"]
	optionalEnum(prop(extract, "on_fail"), definition.FailModesForTest)
	emptyOnFail := jsonschema.NewProperties()
	emptyOnFail.Set("on_fail", &jsonschema.Schema{MaxLength: &zero})
	needRequired := jsonschema.NewProperties()
	needRequired.Set("required", &jsonschema.Schema{Const: true})
	// A non-empty pick needs type json, regex or html, as in Parse.
	optionalEnum(prop(extract, "pick"), definition.PicksForTest)
	emptyPick := jsonschema.NewProperties()
	emptyPick.Set("pick", &jsonschema.Schema{MaxLength: &zero})
	pickTypes := jsonschema.NewProperties()
	pickTypes.Set("type", &jsonschema.Schema{Enum: []any{"json", "regex", "html"}})
	extract.DependentSchemas = map[string]*jsonschema.Schema{
		"on_fail": {AnyOf: []*jsonschema.Schema{
			{Properties: emptyOnFail},
			{Required: []string{"required"}, Properties: needRequired},
		}},
		"pick": {AnyOf: []*jsonschema.Schema{
			{Properties: emptyPick},
			{Properties: pickTypes},
		}},
	}
	// http.version and tls.min_version read "" as absent. The other rules
	// on http, tls and the step redirect keys (cert and key together, PEM
	// contents, max_redirects only where redirects are followed) are
	// Parse's alone; see knownSchemaLooser.
	optionalEnum(prop(s.Definitions["HTTPOptions"], "version"), definition.HTTPVersionsForTest)
	optionalEnum(prop(s.Definitions["TLSOptions"], "min_version"), definition.TLSVersionsForTest)
}

// setupTeardownRules adds the run_once and teardown rules a JSON Schema can
// state: not both on one scenario, no weight on either, and at least one
// load scenario. false reads as absent. The rule that no other scenario
// extracts a run_once global is Parse's alone; see knownSchemaLooser.
func setupTeardownRules(t *testing.T, s *jsonschema.Schema) {
	t.Helper()
	scenario := s.Definitions["Scenario"]
	for _, k := range []string{"run_once", "teardown"} {
		if _, ok := scenario.Properties.Get(k); !ok {
			t.Fatalf("schema has no property Scenario.%s", k)
		}
	}
	props := func(kv ...any) *jsonschema.Schema {
		p := jsonschema.NewProperties()
		for i := 0; i < len(kv); i += 2 {
			p.Set(kv[i].(string), kv[i+1].(*jsonschema.Schema))
		}
		return &jsonschema.Schema{Properties: p}
	}
	off := func() *jsonschema.Schema { return &jsonschema.Schema{Const: false} }
	noWeight := func() *jsonschema.Schema { return &jsonschema.Schema{Maximum: json.Number("0")} }
	scenario.DependentSchemas = map[string]*jsonschema.Schema{
		"run_once": {AnyOf: []*jsonschema.Schema{
			props("run_once", off()),
			props("teardown", off(), "weight", noWeight()),
		}},
		"teardown": {AnyOf: []*jsonschema.Schema{
			props("teardown", off()),
			props("weight", noWeight()),
		}},
	}
	list, ok := s.Properties.Get("scenarios")
	if !ok {
		t.Fatal("schema has no property scenarios")
	}
	list.Contains = props("run_once", off(), "teardown", off())
}

// thresholdSchema describes a threshold: the expression string, or the
// object form reflected from the ThresholdConfig struct (with its field
// comments).
func thresholdSchema(t *testing.T, typ reflect.Type) *jsonschema.Schema {
	t.Helper()
	r := &jsonschema.Reflector{RequiredFromJSONSchemaTags: true, ExpandedStruct: true}
	if err := r.AddGoComments("github.com/volt-test/contract/definition", "./"); err != nil {
		t.Fatal(err)
	}
	obj := r.ReflectFromType(typ)
	obj.Version, obj.ID, obj.Definitions = "", "", nil
	obj.Description = "The expression with abort settings."
	// A non-empty delay_abort needs abort_on_fail: true, as in Parse,
	// which reads an empty one as absent.
	zero, one := uint64(0), uint64(1)
	emptyDelay := jsonschema.NewProperties()
	emptyDelay.Set("delay_abort", &jsonschema.Schema{MaxLength: &zero})
	needAbort := jsonschema.NewProperties()
	needAbort.Set("abort_on_fail", &jsonschema.Schema{Const: true})
	obj.DependentSchemas = map[string]*jsonschema.Schema{
		"delay_abort": {AnyOf: []*jsonschema.Schema{
			{Properties: emptyDelay},
			{Required: []string{"abort_on_fail"}, Properties: needAbort},
		}},
	}
	expr := &jsonschema.Schema{
		Type:        "string",
		MinLength:   &one,
		Description: "The expression alone, e.g. \"p95 < 500ms\".",
	}
	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{expr, obj}}
}

func TestSchemaUpToDate(t *testing.T) {
	got := generateSchema(t)
	if *update {
		if err := os.MkdirAll(filepath.Dir(schemaPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(schemaPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("%v (run: go test ./definition -run TestSchema -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale; run: go test ./definition -run TestSchema -update", schemaPath)
	}
}

func compileSchema(t testing.TB) *validator.Schema {
	t.Helper()
	f, err := os.Open(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := validator.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	c := validator.NewCompiler()
	if err := c.AddResource(schemaID, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(schemaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func validateAgainstSchema(t testing.TB, s *validator.Schema, data []byte) error {
	t.Helper()
	inst, err := validator.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return s.Validate(inst)
}

// knownSchemaLooser lists invalid fixtures that the schema accepts although
// Parse rejects them: rules JSON Schema cannot express (or that are not
// worth expressing). Any other disagreement fails the test.
var knownSchemaLooser = map[string]string{
	"type_integer_with_decimal_point.json":     "JSON Schema integer accepts 1.0; Parse wants an integer literal",
	"structure_load_both.json":                 "the schema does not forbid virtual_users/duration next to stages",
	"structure_duplicate_scenario.json":        "names must be unique",
	"structure_duplicate_step.json":            "names must be unique",
	"structure_relative_url_no_base.json":      "a path URL needs target.base_url",
	"structure_bad_url.json":                   "URL forms",
	"structure_bad_base_url.json":              "base_url forms",
	"structure_bad_duration.json":              "durations are plain strings in the schema",
	"structure_zero_duration.json":             "durations are plain strings in the schema",
	"structure_stage_zero_duration.json":       "durations are plain strings in the schema",
	"structure_whitespace_name.json":           "the schema's \\S is ASCII-only; Parse trims Unicode spaces",
	"structure_threshold_bad_expr.json":        "threshold expressions are plain strings in the schema",
	"structure_threshold_step_iterations.json": "the schema does not know which metrics a step allows",
	"structure_threshold_negative_delay.json":  "durations are plain strings in the schema",

	"structure_check_no_operator.json":                "exactly one operator per check, counting only non-empty keys",
	"structure_check_two_operators.json":              "exactly one operator per check, counting only non-empty keys",
	"structure_check_wrong_operator.json":             "the schema does not know which operators a check type takes",
	"structure_check_missing_selector.json":           "the schema does not know which check types need a selector",
	"structure_check_blank_selector.json":             "the schema does not know which check types need a selector",
	"structure_check_selector_not_applicable.json":    "the schema does not know which check types take no selector",
	"structure_check_header_number_equals.json":       "equals and contains take any scalar in the schema; the kind depends on the check type",
	"structure_check_empty_contains.json":             "an empty contains is valid for json; the schema does not know the check type",
	"structure_check_bad_regex.json":                  "regular expressions are plain strings in the schema",
	"structure_check_bad_comparison.json":             "count, size and duration comparisons are plain strings in the schema",
	"structure_check_between_reversed.json":           "between [low, high] needs low <= high",
	"structure_check_duplicate_label.json":            "check labels must be unique in a step",
	"structure_check_duplicate_name.json":             "check labels must be unique in a step",
	"structure_check_header_label_collision.json":     "check labels must be unique in a step; header labels omit the value",
	"structure_check_label_cut_collision.json":        "check labels must be unique in a step, after the cut to 128 bytes",
	"structure_check_duplicate_required_extract.json": "a required extractor's label, extract <variable_name>, must be unique in a step",
	"type_check_equals_out_of_range.json":             "JSON Schema numbers have no range; Parse rejects equals and contains outside float64 range",

	"structure_step_max_redirects_not_followed.json": "max_redirects needs redirects followed for the step, a rule across the test and step levels",
	"structure_step_max_redirects_inherits_off.json": "max_redirects needs redirects followed for the step, a rule across the test and step levels",
	"structure_tls_key_without_cert.json":            "client_cert and client_key go together, and \"\" counts as absent",
	"structure_tls_cert_without_key.json":            "client_cert and client_key go together, and \"\" counts as absent",
	"structure_tls_cert_not_pem.json":                "PEM contents are plain strings in the schema",
	"structure_tls_ca_wrong_block.json":              "PEM contents are plain strings in the schema",
	"structure_tls_blank_path.json":                  "optional keys take no pattern; a blank path is a Parse rule",
	"structure_tls_path_with_newline.json":           "optional keys take no pattern; a line break in a path is a Parse rule",

	"structure_load_time_unit_zero.json":              "durations are plain strings in the schema",
	"structure_load_rate_duration_zero.json":          "durations are plain strings in the schema",
	"structure_load_ramping_stage_zero_duration.json": "durations are plain strings in the schema",
	"structure_load_max_vus_below_preallocated.json":  "max_vus >= preallocated_vus compares two keys",

	"structure_run_once_global_extracted.json":   "a run_once global extracted by another scenario compares extractors across scenarios",
	"structure_run_once_global_by_run_once.json": "a run_once global extracted by another scenario compares extractors across scenarios",
}

// TestSchemaAgreesWithParse checks that the schema and Parse agree on
// every v2 fixture: both accept valid/*, both reject invalid/* except the
// known cases where the schema is looser.
func TestSchemaAgreesWithParse(t *testing.T) {
	s := compileSchema(t)
	for _, f := range fixtures(t, "v2/valid") {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data := readFile(t, f)
			if err := validateAgainstSchema(t, s, data); err != nil {
				t.Errorf("schema rejects a valid definition: %v", err)
			}
			if _, err := definition.Parse(data); err != nil {
				t.Errorf("Parse rejects a valid definition: %v", err)
			}
		})
	}
	seen := map[string]bool{}
	kinds := map[string]int{}
	for _, f := range fixtures(t, "v2/invalid") {
		name := filepath.Base(f)
		kind, _, _ := strings.Cut(name, "_")
		kinds[kind]++
		t.Run(name, func(t *testing.T) {
			data := readFile(t, f)
			_, err := definition.Parse(data)
			switch kind {
			case "unknown":
				var ufe *definition.UnknownFieldError
				if !errors.As(err, &ufe) {
					t.Errorf("Parse error = %v, want *UnknownFieldError", err)
				}
			case "type", "structure":
				var ve *definition.ValidationError
				if !errors.As(err, &ve) {
					t.Errorf("Parse error = %v, want *ValidationError", err)
				}
			default:
				t.Fatalf("invalid fixture %s must start with unknown_, type_ or structure_", name)
			}
			schemaErr := validateAgainstSchema(t, s, data)
			if _, looser := knownSchemaLooser[name]; looser {
				seen[name] = true
				if schemaErr != nil {
					t.Errorf("listed in knownSchemaLooser but the schema rejects it; remove it from the list: %v", schemaErr)
				}
				return
			}
			if schemaErr == nil {
				t.Error("schema accepts a definition Parse rejects (add a schema rule, or list it in knownSchemaLooser with a reason)")
			}
		})
	}
	for name := range knownSchemaLooser {
		if !seen[name] {
			t.Errorf("knownSchemaLooser lists missing fixture %s", name)
		}
	}
	for _, k := range []string{"unknown", "type", "structure"} {
		if kinds[k] == 0 {
			t.Errorf("no %s_* fixtures", k)
		}
	}
}

// requireSchemaAccepts fails when the schema rejects a document that Parse
// accepted as v2. The one documented exception is data_download_url, which
// Parse accepts (the backend sets it at dispatch) and the schema leaves out.
func requireSchemaAccepts(t testing.TB, s *validator.Schema, data []byte) {
	t.Helper()
	if bytes.Contains(data, []byte("data_download_url")) {
		return
	}
	inst, err := validator.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(inst); err != nil {
		t.Fatalf("schema rejects a definition Parse accepts as v2: %v\n%s", err, data)
	}
}

// TestSchemaShape pins the properties of the schema that consumers rely on.
func TestSchemaShape(t *testing.T) {
	var s map[string]any
	if err := json.Unmarshal(readFile(t, schemaPath), &s); err != nil {
		t.Fatal(err)
	}
	if s["$id"] != schemaID {
		t.Errorf("$id = %v", s["$id"])
	}
	if s["additionalProperties"] != false {
		t.Error("top level allows additional properties")
	}
	if !contains(s["required"], "schema_version") || !contains(s["required"], "name") || !contains(s["required"], "scenarios") {
		t.Errorf("required = %v", s["required"])
	}
	if _, ok := s["anyOf"]; !ok {
		t.Error("top level has no anyOf for the load model")
	}
	props := s["properties"].(map[string]any)
	sv := props["schema_version"].(map[string]any)
	if sv["type"] != "string" || !reflect.DeepEqual(sv["enum"], []any{"2.0"}) {
		t.Errorf("schema_version = %v", sv)
	}
	defs := s["$defs"].(map[string]any)
	for name, d := range defs {
		def := d.(map[string]any)
		if name == "Threshold" {
			continue // a union, checked below
		}
		if def["additionalProperties"] != false {
			t.Errorf("$defs.%s allows additional properties", name)
		}
	}
	// A threshold is a string or a closed object with a required expr.
	th := defs["Threshold"].(map[string]any)
	branches, _ := th["oneOf"].([]any)
	if len(branches) != 2 {
		t.Fatalf("$defs.Threshold = %v, want a oneOf of two", th)
	}
	str, obj := branches[0].(map[string]any), branches[1].(map[string]any)
	if str["type"] != "string" || obj["type"] != "object" || obj["additionalProperties"] != false ||
		!contains(obj["required"], "expr") {
		t.Errorf("$defs.Threshold branches = %v, %v", str, obj)
	}
	var keys []string
	for k := range obj["properties"].(map[string]any) {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if want := []string{"abort_on_fail", "delay_abort", "expr"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("threshold object keys = %q, want %q", keys, want)
	}
	for _, at := range []map[string]any{props, defs["Scenario"].(map[string]any)["properties"].(map[string]any),
		defs["Step"].(map[string]any)["properties"].(map[string]any)} {
		items := at["thresholds"].(map[string]any)["items"].(map[string]any)
		if items["$ref"] != "#/$defs/Threshold" {
			t.Errorf("thresholds items = %v", items)
		}
	}
	// No v1-only or dispatch-only key appears anywhere in the schema.
	raw := string(readFile(t, schemaPath))
	for _, k := range []string{"weights", "report_type", "cloud", "api_key", "cloud_timeout",
		"existing_test_id", "skip_lookup", "validate", "\"header\"", "data_download_url", "expected"} {
		if strings.Contains(raw, "\""+strings.Trim(k, "\"")+"\":") {
			t.Errorf("schema contains %s", k)
		}
	}

	enum := func(def, prop string) []string {
		p := defs[def].(map[string]any)["properties"].(map[string]any)[prop].(map[string]any)
		var out []string
		for _, v := range p["enum"].([]any) {
			out = append(out, v.(string))
		}
		sort.Strings(out)
		return out
	}
	sorted := func(in []string) []string {
		out := append([]string(nil), in...)
		sort.Strings(out)
		return out
	}
	if got, want := enum("Extract", "type"), sorted(definition.ExtractTypesForTest); !reflect.DeepEqual(got, want) {
		t.Errorf("schema extract types %q != Parse %q", got, want)
	}
	if got, want := enum("DataConfig", "mode"), sorted(definition.DataModesForTest); !reflect.DeepEqual(got, want) {
		t.Errorf("schema data modes %q != Parse %q", got, want)
	}
	if got, want := enum("DataConfig", "data_format"), sorted(definition.DataFormatsForTest); !reflect.DeepEqual(got, want) {
		t.Errorf("schema data formats %q != Parse %q", got, want)
	}
	if got, want := enum("Check", "type"), sorted(definition.CheckTypesForTest); !reflect.DeepEqual(got, want) {
		t.Errorf("schema check types %q != Parse %q", got, want)
	}
	// Optional enums also take "" (absent): anyOf [{enum}, {maxLength: 0}].
	optionalEnum := func(def, prop string) []string {
		p := defs[def].(map[string]any)["properties"].(map[string]any)[prop].(map[string]any)
		if p["type"] != "string" {
			t.Errorf("$defs.%s.%s type = %v, want string", def, prop, p["type"])
		}
		branches, _ := p["anyOf"].([]any)
		if len(branches) != 2 || !reflect.DeepEqual(branches[1], map[string]any{"maxLength": 0.0}) {
			t.Errorf("$defs.%s.%s anyOf = %v, want [{enum}, {maxLength: 0}]", def, prop, p["anyOf"])
			return nil
		}
		var out []string
		for _, v := range branches[0].(map[string]any)["enum"].([]any) {
			out = append(out, v.(string))
		}
		sort.Strings(out)
		return out
	}
	if got, want := optionalEnum("Check", "on_fail"), sorted(definition.FailModesForTest); !reflect.DeepEqual(got, want) {
		t.Errorf("schema check on_fail %q != Parse %q", got, want)
	}
	if got, want := optionalEnum("Check", "is"), sorted(definition.JSONKindsForTest); !reflect.DeepEqual(got, want) {
		t.Errorf("schema check is %q != Parse %q", got, want)
	}
	if got, want := optionalEnum("Extract", "on_fail"), sorted(definition.FailModesForTest); !reflect.DeepEqual(got, want) {
		t.Errorf("schema extract on_fail %q != Parse %q", got, want)
	}
	if got, want := optionalEnum("Extract", "pick"), sorted(definition.PicksForTest); !reflect.DeepEqual(got, want) {
		t.Errorf("schema extract pick %q != Parse %q", got, want)
	}
	if got, want := optionalEnum("HTTPOptions", "version"), sorted(definition.HTTPVersionsForTest); !reflect.DeepEqual(got, want) {
		t.Errorf("schema http version %q != Parse %q", got, want)
	}
	if got, want := optionalEnum("TLSOptions", "min_version"), sorted(definition.TLSVersionsForTest); !reflect.DeepEqual(got, want) {
		t.Errorf("schema tls min_version %q != Parse %q", got, want)
	}

	if got, want := enum("Load", "type"), sorted(definition.LoadTypesForTest); !reflect.DeepEqual(got, want) {
		t.Errorf("schema load types %q != Parse %q", got, want)
	}

	checkShape(t, props, defs)
	httpTLSShape(t, props, defs)
	loadShape(t, s, defs)
}

// loadShape pins the schema of the load object.
func loadShape(t *testing.T, s, defs map[string]any) {
	t.Helper()
	obj := func(v any) map[string]any { m, _ := v.(map[string]any); return m }
	keys := func(m map[string]any) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	if got := obj(obj(s["properties"])["load"])["$ref"]; got != "#/$defs/Load" {
		t.Errorf("load $ref = %v", got)
	}
	load := obj(defs["Load"])
	want := []string{"duration", "max_vus", "preallocated_vus", "rate", "stages", "time_unit", "type"}
	if got := keys(obj(load["properties"])); !reflect.DeepEqual(got, want) {
		t.Errorf("load keys = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(load["required"], []any{"type", "preallocated_vus"}) {
		t.Errorf("load required = %v", load["required"])
	}
	if got := keys(obj(obj(defs["RateStage"])["properties"])); !reflect.DeepEqual(got, []string{"duration", "target"}) {
		t.Errorf("rate stage keys = %q", got)
	}
	if branches, _ := load["anyOf"].([]any); len(branches) != 2 {
		t.Errorf("load anyOf = %v, want one branch per type", load["anyOf"])
	}
	if branches, _ := s["anyOf"].([]any); len(branches) != 3 {
		t.Errorf("top-level anyOf = %v, want stages, virtual_users and load", s["anyOf"])
	}
}

// httpTLSShape pins the schema of the http and tls objects and the step
// redirect keys.
func httpTLSShape(t *testing.T, props, defs map[string]any) {
	t.Helper()
	obj := func(v any) map[string]any { m, _ := v.(map[string]any); return m }
	keys := func(m map[string]any) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	for k, ref := range map[string]string{"http": "#/$defs/HTTPOptions", "tls": "#/$defs/TLSOptions"} {
		if got := obj(props[k])["$ref"]; got != ref {
			t.Errorf("%s $ref = %v, want %s", k, got, ref)
		}
	}
	httpDef, tlsDef := obj(defs["HTTPOptions"]), obj(defs["TLSOptions"])
	hp, tp := obj(httpDef["properties"]), obj(tlsDef["properties"])
	if got, want := keys(hp), []string{"follow_redirects", "max_redirects", "version"}; !reflect.DeepEqual(got, want) {
		t.Errorf("http keys = %q, want %q", got, want)
	}
	if got, want := keys(tp), []string{"ca_cert", "client_cert", "client_key", "insecure_skip_verify", "min_version"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tls keys = %q, want %q", got, want)
	}
	for name, def := range map[string]map[string]any{"HTTPOptions": httpDef, "TLSOptions": tlsDef} {
		if _, ok := def["required"]; ok {
			t.Errorf("$defs.%s has required keys; every key is optional", name)
		}
		if _, ok := def["dependentSchemas"]; ok {
			t.Errorf("$defs.%s has dependentSchemas; \"\" reads as absent, so pairing is Parse's rule", name)
		}
	}
	for _, k := range []string{"client_cert", "client_key", "ca_cert"} {
		if p := obj(tp[k]); p["type"] != "string" || len(p) != 2 {
			t.Errorf("tls %s = %v, want a plain string with a description", k, p)
		}
	}
	if p := obj(tp["insecure_skip_verify"]); p["type"] != "boolean" {
		t.Errorf("tls insecure_skip_verify = %v", p)
	}
	sp := obj(obj(defs["Step"])["properties"])
	for at, p := range map[string]map[string]any{"http": hp, "step": sp} {
		if f := obj(p["follow_redirects"]); f["type"] != "boolean" || f["const"] != nil {
			t.Errorf("%s follow_redirects = %v, want a boolean (false is meaningful)", at, f)
		}
		if m := obj(p["max_redirects"]); m["type"] != "integer" || m["minimum"] != 0.0 || m["maximum"] != nil {
			t.Errorf("%s max_redirects = %v, want an integer, minimum 0, no maximum", at, m)
		}
	}
	// Optional keys read "" as absent, so none may demand content.
	opt := map[string]any{"step.follow_redirects": sp["follow_redirects"], "step.max_redirects": sp["max_redirects"]}
	for k, v := range hp {
		opt["http."+k] = v
	}
	for k, v := range tp {
		opt["tls."+k] = v
	}
	for k, v := range opt {
		for _, rule := range []string{"pattern", "minLength", "minItems"} {
			if _, ok := obj(v)[rule]; ok {
				t.Errorf("%s has %s; a zero value must stay valid", k, rule)
			}
		}
	}
	if sc := obj(obj(defs["Scenario"])["properties"]); sc["follow_redirects"] != nil || sc["max_redirects"] != nil {
		t.Error("scenarios have redirect keys; there is no scenario level")
	}
	if req := obj(obj(defs["Request"])["properties"]); req["follow_redirects"] != nil || req["max_redirects"] != nil {
		t.Error("requests have redirect keys; they belong on the step")
	}
}

// checkShape pins the schema of checks, required extractors and
// expected_statuses.
func checkShape(t *testing.T, props, defs map[string]any) {
	t.Helper()
	obj := func(v any) map[string]any { m, _ := v.(map[string]any); return m }
	check := obj(defs["Check"])
	cp := obj(check["properties"])
	var keys []string
	for k := range cp {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"between", "contains", "count", "duration", "empty", "equals", "exists", "in", "is", "matches",
		"missing", "name", "not_contains", "not_in", "on_fail", "selector", "size", "type"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("check keys = %q, want %q", keys, want)
	}
	if !reflect.DeepEqual(check["required"], []any{"type"}) {
		t.Errorf("check required = %v, want only type", check["required"])
	}
	// Optional keys read "" and [] as absent, so none may demand content.
	for k, v := range cp {
		for _, rule := range []string{"pattern", "minLength", "minItems"} {
			if _, ok := obj(v)[rule]; ok {
				t.Errorf("check %s has %s; a zero value must stay valid", k, rule)
			}
		}
	}
	for _, k := range []string{"exists", "missing", "empty"} {
		if p := obj(cp[k]); p["type"] != "boolean" || p["const"] != true {
			t.Errorf("check %s = %v, want boolean const true", k, p)
		}
	}
	for _, k := range []string{"equals", "contains"} {
		var kinds []string
		branches, _ := obj(cp[k])["anyOf"].([]any)
		for _, b := range branches {
			kinds = append(kinds, obj(b)["type"].(string))
		}
		if want := []string{"string", "number", "boolean"}; !reflect.DeepEqual(kinds, want) {
			t.Errorf("check %s kinds = %q, want %q", k, kinds, want)
		}
	}
	codes := map[string]map[string]any{"expected_statuses": obj(props["expected_statuses"])}
	for _, k := range []string{"in", "not_in", "between"} {
		codes[k] = obj(cp[k])
	}
	for k, p := range codes {
		items := obj(p["items"])
		if p["type"] != "array" || items["type"] != "integer" || items["minimum"] != 100.0 || items["maximum"] != 599.0 {
			t.Errorf("%s = %v, want an array of integers from 100 to 599", k, p)
		}
		if _, ok := p["minimum"]; ok {
			t.Errorf("%s has minimum on the array, not on its items", k)
		}
		if _, ok := p["minItems"]; ok {
			t.Errorf("%s has minItems; [] must stay valid", k)
		}
	}
	wantBetween := []any{map[string]any{"maxItems": 0.0}, map[string]any{"minItems": 2.0, "maxItems": 2.0}}
	if got := obj(cp["between"])["anyOf"]; !reflect.DeepEqual(got, wantBetween) {
		t.Errorf("between anyOf = %v, want %v", got, wantBetween)
	}

	extract := obj(defs["Extract"])
	ep := obj(extract["properties"])
	if p := obj(ep["required"]); p["type"] != "boolean" {
		t.Errorf("extract required = %v", p)
	}
	if contains(extract["required"], "required") || contains(extract["required"], "on_fail") || contains(extract["required"], "pick") {
		t.Errorf("extract required keys = %v; required, on_fail and pick are optional", extract["required"])
	}
	wantDep := map[string]any{"anyOf": []any{
		map[string]any{"properties": map[string]any{"on_fail": map[string]any{"maxLength": 0.0}}},
		map[string]any{"required": []any{"required"}, "properties": map[string]any{"required": map[string]any{"const": true}}},
	}}
	if got := obj(obj(extract["dependentSchemas"])["on_fail"]); !reflect.DeepEqual(got, wantDep) {
		t.Errorf("extract dependentSchemas.on_fail = %v, want %v", got, wantDep)
	}
	wantPickDep := map[string]any{"anyOf": []any{
		map[string]any{"properties": map[string]any{"pick": map[string]any{"maxLength": 0.0}}},
		map[string]any{"properties": map[string]any{"type": map[string]any{"enum": []any{"json", "regex", "html"}}}},
	}}
	if got := obj(obj(extract["dependentSchemas"])["pick"]); !reflect.DeepEqual(got, wantPickDep) {
		t.Errorf("extract dependentSchemas.pick = %v, want %v", got, wantPickDep)
	}
	steps := obj(obj(defs["Step"])["properties"])
	if items := obj(obj(steps["checks"])["items"]); items["$ref"] != "#/$defs/Check" {
		t.Errorf("step checks items = %v", items)
	}
}

func contains(list any, s string) bool {
	l, _ := list.([]any)
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}
