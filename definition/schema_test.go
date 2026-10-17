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
	r := &jsonschema.Reflector{
		RequiredFromJSONSchemaTags: true,
		ExpandedStruct:             true,
		Mapper: func(t reflect.Type) *jsonschema.Schema {
			if t == reflect.TypeOf(definition.SchemaVersion{}) {
				return &jsonschema.Schema{Type: "string", Enum: v2}
			}
			return nil
		},
	}
	// Field descriptions come from the field comments in types.go.
	if err := r.AddGoComments("github.com/volt-test/contract/definition", "./"); err != nil {
		t.Fatal(err)
	}
	s := r.Reflect(&definition.Definition{})
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
	"type_integer_with_decimal_point.json": "JSON Schema integer accepts 1.0; Parse wants an integer literal",
	"structure_load_both.json":             "the schema does not forbid virtual_users/duration next to stages",
	"structure_duplicate_scenario.json":    "names must be unique",
	"structure_duplicate_step.json":        "names must be unique",
	"structure_relative_url_no_base.json":  "a path URL needs target.base_url",
	"structure_bad_url.json":               "URL forms",
	"structure_bad_base_url.json":          "base_url forms",
	"structure_bad_duration.json":          "durations are plain strings in the schema",
	"structure_zero_duration.json":         "durations are plain strings in the schema",
	"structure_stage_zero_duration.json":   "durations are plain strings in the schema",
	"structure_whitespace_name.json":       "the schema's \\S is ASCII-only; Parse trims Unicode spaces",
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
		if d.(map[string]any)["additionalProperties"] != false {
			t.Errorf("$defs.%s allows additional properties", name)
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
