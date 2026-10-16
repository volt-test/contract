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

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
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
