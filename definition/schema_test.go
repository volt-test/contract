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
	// Load model: non-empty stages, or at least one virtual user.
	one := uint64(1)
	staged := jsonschema.NewProperties()
	staged.Set("stages", &jsonschema.Schema{MinItems: &one})
	constant := jsonschema.NewProperties()
	constant.Set("virtual_users", &jsonschema.Schema{Minimum: json.Number("1")})
	s.AnyOf = []*jsonschema.Schema{
		{Required: []string{"stages"}, Properties: staged},
		{Required: []string{"virtual_users"}, Properties: constant},
	}
	s.Definitions["DataConfig"].AnyOf = []*jsonschema.Schema{
		{Required: []string{"data_source"}},
		{Required: []string{"data_content"}},
	}
	checkSchemaRules(t, s)

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
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
	extract.DependentSchemas = map[string]*jsonschema.Schema{
		"on_fail": {AnyOf: []*jsonschema.Schema{
			{Properties: emptyOnFail},
			{Required: []string{"required"}, Properties: needRequired},
		}},
	}
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

	checkShape(t, props, defs)
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
	if contains(extract["required"], "required") || contains(extract["required"], "on_fail") {
		t.Errorf("extract required keys = %v; required and on_fail are optional", extract["required"])
	}
	wantDep := map[string]any{"anyOf": []any{
		map[string]any{"properties": map[string]any{"on_fail": map[string]any{"maxLength": 0.0}}},
		map[string]any{"required": []any{"required"}, "properties": map[string]any{"required": map[string]any{"const": true}}},
	}}
	if got := obj(obj(extract["dependentSchemas"])["on_fail"]); !reflect.DeepEqual(got, wantDep) {
		t.Errorf("extract dependentSchemas.on_fail = %v, want %v", got, wantDep)
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
