package definition_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

// knownDropped lists leaf paths that Marshal(Parse(v1)) intentionally does
// not reproduce. api_key is read but never written back out.
var knownDropped = map[string]bool{
	"api_key": true,
}

// knownRetyped lists leaf paths whose JSON type Marshal(Parse(v1))
// intentionally changes: schema_version is always written as a string.
var knownRetyped = map[string]bool{
	"schema_version": true,
}

func TestV1FixturesParse(t *testing.T) {
	for _, f := range fixtures(t, "v1") {
		t.Run(filepath.ToSlash(f), func(t *testing.T) {
			d, err := definition.Parse(readFile(t, f))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !d.IsV1() || d.Version() != definition.V1 {
				t.Fatalf("IsV1() = %v, Version() = %v; want v1", d.IsV1(), d.Version())
			}
		})
	}
}

// TestV1RoundTrip checks that every leaf of every v1 fixture survives
// Parse and Marshal: nothing the engines read is lost by the typed model.
func TestV1RoundTrip(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures(t, "v1") {
		t.Run(filepath.ToSlash(f), func(t *testing.T) {
			in := readFile(t, f)
			d, err := definition.Parse(in)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			out, err := json.Marshal(d)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			want, got := decodeMap(t, in), decodeMap(t, out)
			for p := range leaves(want) {
				seen[pattern(p)] = true
			}
			for _, c := range changesBetween(want, got) {
				switch {
				case !c.InAfter && knownDropped[c.Path]:
				case c.InBefore && c.InAfter && knownRetyped[c.Path]:
				default:
					t.Error(c)
				}
			}
		})
	}

	// The fixtures must exercise the fields most at risk of being lost.
	mustCover := []string{
		"schema_version",
		"name", "description", "virtual_users", "duration", "ramp_up", "http_timeout", "http_debug",
		"target.url", "target.idle_timeout",
		"stages[].duration", "stages[].target",
		"weights[]", "report_type",
		"region_config[].region", "region_config[].weight",
		"cloud", "api_key", "cloud_timeout", "existing_test_id", "skip_lookup",
		"scenarios[].name", "scenarios[].description", "scenarios[].weight", "scenarios[].think_time",
		"scenarios[].auto_handle_cookies",
		"scenarios[].data_config.data_source", "scenarios[].data_config.data_content",
		"scenarios[].data_config.data_download_url", "scenarios[].data_config.data_format",
		"scenarios[].data_config.has_header", "scenarios[].data_config.mode",
		"scenarios[].steps[].name", "scenarios[].steps[].think_time",
		"scenarios[].steps[].request.method", "scenarios[].steps[].request.url",
		"scenarios[].steps[].request.body",
		"scenarios[].steps[].request.header.Content-Type",
		"scenarios[].steps[].request.headers.Content-Type",
		"scenarios[].steps[].request.header",
		"scenarios[].steps[].extract[].variable_name", "scenarios[].steps[].extract[].selector",
		"scenarios[].steps[].extract[].attribute", "scenarios[].steps[].extract[].type",
		"scenarios[].steps[].validate[].name", "scenarios[].steps[].validate[].type",
		"scenarios[].steps[].validate[].selector", "scenarios[].steps[].validate[].expected",
		"scenarios[].steps[].validate[].expected.page", "scenarios[].steps[].validate[].expected[]",
	}
	for _, p := range mustCover {
		if !seen[p] {
			t.Errorf("no v1 fixture has a non-empty %s", p)
		}
	}
}

func TestV1APIKeyReadNeverWritten(t *testing.T) {
	d, err := definition.Parse(readFile(t, "../testdata/v1/cli/cli_cloud_keys.json"))
	if err != nil {
		t.Fatal(err)
	}
	if d.APIKey.Reveal() != "vt_example_0000000000000000" {
		t.Errorf("APIKey = %q", d.APIKey)
	}
	if !d.Cloud || d.CloudTimeout != 900 || d.ExistingTestID == "" || !d.SkipLookup {
		t.Errorf("CLI keys not decoded: %+v", d)
	}
	out, _ := json.Marshal(d)
	if strings.Contains(string(out), "api_key") || strings.Contains(string(out), "vt_example") {
		t.Errorf("Marshal output carries the API key: %s", out)
	}

	// encoding/json matches keys case-insensitively; so does the v1 path.
	d, err = definition.Parse([]byte(`{"API_KEY": "k", "name": "x"}`))
	if err != nil || d.APIKey.Reveal() != "k" {
		t.Errorf("API_KEY: %q, %v", d.APIKey.Reveal(), err)
	}
}

func TestV1HeaderMaps(t *testing.T) {
	d, err := definition.Parse(readFile(t, "../testdata/v1/cli/header_shadow.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := d.Scenarios[0].Steps[0].Request
	if r.Header == nil || len(r.Header) != 0 {
		t.Errorf("Header = %#v, want empty non-nil map", r.Header)
	}
	if r.Headers["Accept"] != "application/json" {
		t.Errorf("Headers = %#v", r.Headers)
	}
	r = d.Scenarios[0].Steps[1].Request
	if r.Header["X-Old"] != "1" || r.Headers["X-New"] != "2" {
		t.Errorf("both maps: Header = %v, Headers = %v", r.Header, r.Headers)
	}
	out, _ := json.Marshal(d.Scenarios[0].Steps[0].Request)
	if want := `{"method":"GET","url":"https://api.example.com/a","header":{},"headers":{"Accept":"application/json"}}`; string(out) != want {
		t.Errorf("Marshal(Request) = %s\nwant                %s", out, want)
	}
	out, _ = json.Marshal(definition.Request{Method: "GET", URL: "/x"})
	if want := `{"method":"GET","url":"/x"}`; string(out) != want {
		t.Errorf("Marshal(Request without headers) = %s, want %s", out, want)
	}

	// "header": null decodes to a nil map, as the engines see it.
	d, err = definition.Parse(readFile(t, "../testdata/v1/cli/cloud_upload.json"))
	if err != nil {
		t.Fatal(err)
	}
	if h := d.Scenarios[0].Steps[1].Request.Header; h != nil {
		t.Errorf("header null: Header = %#v, want nil", h)
	}
}

func TestV1ExpectedTypes(t *testing.T) {
	d, err := definition.Parse(readFile(t, "../testdata/v1/cli/expected_types.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"number":   float64(200),
		"fraction": 0.25,
		"string":   "application/json",
		"bool":     true,
		"false":    false,
		"null":     nil,
		"object":   map[string]any{"page": float64(1), "tags": []any{"a"}},
		"array":    []any{float64(1), "two", true},
	}
	vs := d.Scenarios[0].Steps[0].Validate
	if len(vs) != len(want) {
		t.Fatalf("got %d validators, want %d", len(vs), len(want))
	}
	for _, v := range vs {
		if !reflect.DeepEqual(v.Expected, want[v.Name]) {
			t.Errorf("%s: Expected = %#v (%T), want %#v", v.Name, v.Expected, v.Expected, want[v.Name])
		}
	}
	// Engines type-assert status codes to float64.
	if _, ok := vs[0].Expected.(float64); !ok {
		t.Errorf("status_code expected is %T, want float64", vs[0].Expected)
	}
}

// injectUnknown adds an unknown key to every object that decodes into a
// struct (header maps and validator values are data, not structure).
func injectUnknown(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if k == "header" || k == "headers" || k == "expected" {
				continue
			}
			injectUnknown(val)
		}
		x["x_unknown_key"] = map[string]any{"nested": []any{1, "two"}}
	case []any:
		for _, val := range x {
			injectUnknown(val)
		}
	}
}

func TestV1IgnoresUnknownKeysAtEveryLevel(t *testing.T) {
	for _, f := range fixtures(t, "v1") {
		t.Run(filepath.ToSlash(f), func(t *testing.T) {
			in := readFile(t, f)
			base, err := definition.Parse(in)
			if err != nil {
				t.Fatal(err)
			}
			m := decodeMap(t, in)
			injectUnknown(m)
			injected, _ := json.Marshal(m)
			if !strings.Contains(string(injected), `"scenarios":[{`) {
				t.Fatal("fixture has no scenario objects to inject into")
			}
			d, err := definition.Parse(injected)
			if err != nil {
				t.Fatalf("Parse with unknown keys: %v", err)
			}
			if !reflect.DeepEqual(d, base) {
				t.Errorf("unknown keys changed the result:\n got %+v\nwant %+v", d, base)
			}
		})
	}
}

func TestV1CaseInsensitiveKeys(t *testing.T) {
	d, err := definition.Parse([]byte(`{"NAME": "x", "Virtual_Users": 7, "Target": {"URL": "https://a"},
		"scenarios": [{"Name": "s", "Steps": [{"name": "a", "Request": {"Method": "GET", "url": "https://a/"}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "x" || d.VirtualUsers != 7 || d.Target.URL != "https://a" ||
		d.Scenarios[0].Name != "s" || d.Scenarios[0].Steps[0].Request.Method != "GET" {
		t.Errorf("case-insensitive keys not decoded: %+v", d)
	}
}

func TestV1SchemaVersionCaseVariantsIgnored(t *testing.T) {
	cases := []struct {
		doc      string
		declared bool
	}{
		{`{"Schema_Version": "garbage", "name": "x"}`, false},
		{`{"SCHEMA_VERSION": "2.0", "name": "x"}`, false},
		{`{"Schema_Version": "", "name": "x"}`, false},
		{`{"schema_version": "1", "Schema_Version": "2.0", "name": "x"}`, true},
		{`{"Schema_Version": "2.0", "schema_version": 1, "name": "x"}`, true},
	}
	for _, tc := range cases {
		d, err := definition.Parse([]byte(tc.doc))
		if err != nil {
			t.Errorf("%s: %v", tc.doc, err)
			continue
		}
		if !d.IsV1() || (d.SchemaVersion != nil) != tc.declared {
			t.Errorf("%s: IsV1() = %v, SchemaVersion = %v", tc.doc, d.IsV1(), d.SchemaVersion)
		}
		if tc.declared && *d.SchemaVersion != definition.V1 {
			t.Errorf("%s: SchemaVersion = %v", tc.doc, *d.SchemaVersion)
		}
	}
}

func TestV1ClearsBaseURL(t *testing.T) {
	d, err := definition.Parse([]byte(`{"name": "x", "target": {"url": "https://a", "base_url": "https://b"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.Target.BaseURL != "" || d.Target.URL != "https://a" {
		t.Errorf("Target = %+v, want base_url cleared", d.Target)
	}
}

func TestV1NoSemanticValidation(t *testing.T) {
	docs := []string{
		`{}`,
		`{"name": "", "scenarios": []}`,
		`{"duration": "forever", "virtual_users": -5, "stages": [{"duration": "x", "target": -1}], "duration": "1m"}`,
		`{"scenarios": [{"weight": -3, "steps": [{"request": {"url": "relative/path"}, "extract": [{"type": "xml"}]}],
			"data_config": {"mode": "shuffle", "data_format": "xlsx"}}]}`,
		`{"scenarios": [{"name": "a"}, {"name": "a"}], "target": {"idle_timeout": ""}}`,
		`{"header": null, "scenarios": null, "target": null, "stages": null}`,
	}
	for _, doc := range docs {
		d, err := definition.Parse([]byte(doc))
		if err != nil {
			t.Errorf("%s: %v", doc, err)
			continue
		}
		if !d.IsV1() {
			t.Errorf("%s: not v1", doc)
		}
	}
}

func TestV1KeepsDataDownloadURL(t *testing.T) {
	d, err := definition.Parse(readFile(t, "../testdata/v1/compat/constant_csv.node-0.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Scenarios[0].DataConfig.DataDownloadURL; !strings.HasPrefix(got, "https://storage.example.com/") {
		t.Errorf("DataDownloadURL = %q", got)
	}
}

// TestV1TypeDifferential pins how Parse treats v1 values of the wrong JSON
// type, against what the v1 readers (headless CLI, cloud engine, backend)
// do today: a field every reader decodes fails; a field some reader ignores
// is accepted, decoded as if absent and recorded in V1TypeProblems; a v2-only field is
// dropped unread. TestV1WrongTypeEveryField covers every field generically.
func TestV1TypeDifferential(t *testing.T) {
	const scen = `"scenarios": [{"name": "s", "steps": [{"name": "a", "request": {"method": "GET", "url": "https://a/"}}, %s]}]`
	step := func(req string) string {
		return fmt.Sprintf(scen, `{"name": "b", "request": {"method": "GET", "url": "https://a/b"`+req+`}}`)
	}
	cases := []struct {
		name  string
		doc   string
		core  []string // problems that make Parse fail
		loose []string // problems recorded in V1TypeProblems
		check func(t *testing.T, d *definition.Definition)
	}{
		{"v2-only base_url with a number", `{"target": {"url": "https://a", "base_url": 5}}`, nil, nil,
			func(t *testing.T, d *definition.Definition) {
				if d.Target.BaseURL != "" || d.Target.URL != "https://a" {
					t.Errorf("Target = %+v", d.Target)
				}
			}},
		{"v2-only base_url, case variant", `{"target": {"Base_URL": {"x": 1}}}`, nil, nil, nil},
		{"description", `{"description": 5}`, nil, []string{"description: must be a string"}, nil},
		{"weights not an array", `{"weights": "70,30"}`, nil, []string{"weights: must be an array"},
			func(t *testing.T, d *definition.Definition) {
				if d.Weights != nil {
					t.Errorf("Weights = %v", d.Weights)
				}
			}},
		{"weights element", `{"weights": [70, "30"]}`, nil, []string{"weights[1]: must be an integer"},
			func(t *testing.T, d *definition.Definition) {
				if !reflect.DeepEqual(d.Weights, []int{70, 0}) {
					t.Errorf("Weights = %v", d.Weights)
				}
			}},
		{"report_type", `{"report_type": true}`, nil, []string{"report_type: must be a string"}, nil},
		{"region_config", `{"region_config": {"us": 1}}`, nil, []string{"region_config: must be an array"}, nil},
		{"region_config weight", `{"region_config": [{"region": "us", "weight": "1"}]}`, nil,
			[]string{"region_config[0].weight: must be an integer"},
			func(t *testing.T, d *definition.Definition) {
				if !reflect.DeepEqual(d.RegionConfig, []definition.RegionWeight{{Region: "us"}}) {
					t.Errorf("RegionConfig = %+v", d.RegionConfig)
				}
			}},
		{"cloud", `{"cloud": "yes"}`, nil, []string{"cloud: must be true or false"}, nil},
		{"api_key", `{"api_key": 12345}`, nil, []string{"api_key: must be a string"},
			func(t *testing.T, d *definition.Definition) {
				if !d.APIKey.IsZero() {
					t.Error("APIKey set")
				}
			}},
		{"cloud_timeout", `{"cloud_timeout": "60"}`, nil, []string{"cloud_timeout: must be an integer"}, nil},
		{"existing_test_id", `{"existing_test_id": 7}`, nil, []string{"existing_test_id: must be a string"}, nil},
		{"skip_lookup", `{"skip_lookup": 1}`, nil, []string{"skip_lookup: must be true or false"}, nil},
		{"idle_timeout", `{"target": {"url": "https://a", "idle_timeout": 30}}`, nil, []string{"target.idle_timeout: must be a string"}, nil},
		{"headers not an object", `{` + step(`, "headers": ["A: 1"]`) + `}`, nil,
			[]string{"scenarios[0].steps[1].request.headers: must be an object"}, nil},
		{"headers value", `{` + step(`, "headers": {"A": 1, "B": "2"}`) + `}`, nil,
			[]string{"scenarios[0].steps[1].request.headers.A: must be a string"},
			func(t *testing.T, d *definition.Definition) {
				if h := d.Scenarios[0].Steps[1].Request.Headers; !reflect.DeepEqual(h, map[string]string{"B": "2"}) {
					t.Errorf("Headers = %v", h)
				}
			}},
		{"data_download_url", `{"scenarios": [{"name": "s", "data_config": {"data_download_url": 1, "mode": "random"}}]}`, nil,
			[]string{"scenarios[0].data_config.data_download_url: must be a string"},
			func(t *testing.T, d *definition.Definition) {
				if d.Scenarios[0].DataConfig.Mode != "random" {
					t.Errorf("DataConfig = %+v", d.Scenarios[0].DataConfig)
				}
			}},

		{"duplicate key, good then bad", `{"weights": [70, 30], "weights": "x", "Description": "kept", "description": 5}`, nil,
			[]string{"weights: must be an array", "description: must be a string"},
			func(t *testing.T, d *definition.Definition) {
				if !reflect.DeepEqual(d.Weights, []int{70, 30}) || d.Description != "kept" {
					t.Errorf("earlier good values lost: Weights = %v, Description = %q", d.Weights, d.Description)
				}
			}},
		{"expected out of range", `{` + fmt.Sprintf(scen, `{"name": "b", "validate": [{"expected": 1e999}]}`) + `}`,
			[]string{"scenarios[0].steps[1].validate[0].expected: is out of range"}, nil, nil},
		{"expected nested out of range", `{` + fmt.Sprintf(scen, `{"name": "b", "validate": [{"expected": {"a": [1, -1e400]}}]}`) + `}`,
			[]string{"scenarios[0].steps[1].validate[0].expected.a[1]: is out of range"}, nil, nil},
		{"null after target keeps the earlier object", `{"target": {"url": "https://u"}, "target": null}`, nil, nil,
			func(t *testing.T, d *definition.Definition) {
				if d.Target == nil || d.Target.URL != "https://u" {
					t.Errorf("Target = %+v", d.Target)
				}
			}},
		{"null after target, case variant and escaped key", `{"target": {"url": "https://u"}, "TARGET": null, "\u0074arget": null}`, nil, nil,
			func(t *testing.T, d *definition.Definition) {
				if d.Target == nil || d.Target.URL != "https://u" {
					t.Errorf("Target = %+v", d.Target)
				}
			}},
		{"null after data_config keeps the earlier object",
			`{"scenarios": [{"name": "s", "data_config": {"data_source": "a.csv", "mode": "random"}, "Data_Config": null}]}`, nil, nil,
			func(t *testing.T, d *definition.Definition) {
				if dc := d.Scenarios[0].DataConfig; dc == nil || dc.DataSource != "a.csv" || dc.Mode != "random" {
					t.Errorf("DataConfig = %+v", dc)
				}
			}},
		{"object after null target", `{"target": null, "target": {"url": "https://u"}}`, nil, nil,
			func(t *testing.T, d *definition.Definition) {
				if d.Target == nil || d.Target.URL != "https://u" {
					t.Errorf("Target = %+v", d.Target)
				}
			}},
		{"virtual_users", `{"virtual_users": "10"}`, []string{"virtual_users: must be an integer"}, nil, nil},
		{"virtual_users case variant", `{"Virtual_Users": "10"}`, []string{"virtual_users: must be an integer"}, nil, nil},
		{"duplicate key, first occurrence wrong", `{"name": 1, "name": "x"}`, []string{"name: must be a string"}, nil, nil},
		{"header not an object", `{` + step(`, "header": "A: 1"`) + `}`,
			[]string{"scenarios[0].steps[1].request.header: must be an object"}, nil, nil},
		{"header value", `{` + step(`, "header": {"A": 1}`) + `}`,
			[]string{"scenarios[0].steps[1].request.header.A: must be a string"}, nil, nil},
		{"target", `{"target": "https://a"}`, []string{"target: must be an object"}, nil, nil},
		{"stage target", `{"stages": [{"duration": "1s", "target": "5"}]}`, []string{"stages[0].target: must be an integer"}, nil, nil},
		{"validate type", `{` + fmt.Sprintf(scen, `{"name": "b", "validate": [{"type": 1}]}`) + `}`,
			[]string{"scenarios[0].steps[1].validate[0].type: must be a string"}, nil, nil},
		{"several, core and loose", `{"http_debug": "true", "weights": "x", "scenarios": {"a": 1}, "description": 1}`,
			[]string{"http_debug: must be true or false", "scenarios: must be an array"}, nil, nil},
		{"core and loose inside the same objects", `{"target": {"url": 5, "idle_timeout": 7}, ` + step(`, "body": 1, "headers": "x"`) + `}`,
			[]string{"target.url: must be a string", "scenarios[0].steps[1].request.body: must be a string"}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := definition.Parse([]byte(tc.doc))
			if tc.core != nil {
				var ve *definition.ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("error = %v, want *ValidationError", err)
				}
				var got []string
				for _, p := range ve.Problems {
					got = append(got, p.String())
				}
				if !reflect.DeepEqual(got, tc.core) {
					t.Errorf("problems = %q, want %q", got, tc.core)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			var got []string
			for _, p := range d.V1TypeProblems {
				got = append(got, p.String())
			}
			if !reflect.DeepEqual(got, tc.loose) {
				t.Errorf("V1TypeProblems = %q, want %q", got, tc.loose)
			}
			if tc.check != nil {
				tc.check(t, d)
			}
			out, _ := json.Marshal(d)
			if strings.Contains(string(out), "V1TypeProblems") || strings.Contains(string(out), "must be") {
				t.Errorf("V1TypeProblems marshalled: %s", out)
			}
		})
	}
}

func TestAPIKeyNeverPrinted(t *testing.T) {
	d, err := definition.Parse([]byte(`{"name": "x", "api_key": "vt_live_secret_value"}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.APIKey.Reveal() != "vt_live_secret_value" {
		t.Fatalf("APIKey = %q", d.APIKey.Reveal())
	}
	verbs := []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%t", "%b", "%c",
		"%o", "%O", "%e", "%E", "%f", "%F", "%g", "%G", "%U", "%p", "%T", "%10.3s", "%-20v", "%08d"}
	for _, verb := range verbs {
		for _, v := range []any{d, *d, d.APIKey, &d.APIKey, []any{d.APIKey}, map[string]any{"k": d.APIKey}} {
			if out := fmt.Sprintf(verb, v); strings.Contains(out, "secret_value") {
				t.Errorf("fmt %s prints the key: %s", verb, out)
			}
		}
	}
	if out := fmt.Sprint(d.APIKey); out != "[redacted]" {
		t.Errorf("Sprint = %q", out)
	}
	if b, _ := json.Marshal(d); strings.Contains(string(b), "secret_value") {
		t.Errorf("Marshal(d) carries the key: %s", b)
	}
	if !d.APIKey.Equal(definition.NewSecret("vt_live_secret_value")) || d.APIKey.Equal(definition.NewSecret("other")) ||
		!(definition.Secret{}).Equal(definition.NewSecret("")) {
		t.Error("Secret.Equal compares values")
	}
	if b, _ := json.Marshal(d.APIKey); string(b) != `"[redacted]"` {
		t.Errorf("Marshal(Secret) = %s", b)
	}
}
