package definition_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

func TestV2ValidFixtures(t *testing.T) {
	for _, f := range fixtures(t, "v2/valid") {
		t.Run(filepath.Base(f), func(t *testing.T) {
			d, err := definition.Parse(readFile(t, f))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if d.IsV1() || d.Version() != definition.V2_0 {
				t.Fatalf("Version() = %v, want 2.0", d.Version())
			}
		})
	}
}

// unknownFixtures maps each testdata/v2/invalid/unknown_* file to the paths
// Parse must report.
var unknownFixtures = map[string][]string{
	"unknown_top.json":                    {"retries"},
	"unknown_target.json":                 {"target.timeout"},
	"unknown_scenario.json":               {"scenarios[1].iterations"},
	"unknown_step.json":                   {"scenarios[0].steps[2].checks"},
	"unknown_request.json":                {"scenarios[1].steps[0].request.retries"},
	"unknown_extract.json":                {"scenarios[0].steps[1].extract[1].default"},
	"unknown_data_config.json":            {"scenarios[0].data_config.delimiter"},
	"unknown_stage.json":                  {"stages[1].ramp"},
	"unknown_region_config.json":          {"region_config[0].zone"},
	"unknown_case_name.json":              {"Name"},
	"unknown_case_headers.json":           {"scenarios[0].steps[0].request.Headers"},
	"unknown_v1_validate.json":            {"scenarios[0].steps[0].validate"},
	"unknown_v1_header.json":              {"scenarios[0].steps[1].request.header"},
	"unknown_v1_weights_report_type.json": {"report_type", "weights"},
	"unknown_v1_cli_keys.json":            {"api_key", "cloud", "cloud_timeout", "existing_test_id", "skip_lookup"},
	"unknown_multiple.json":               {"alpha", "scenarios[0].steps[0].think", "scenarios[1].steps[0].request.retries", "target.proxy", "zeta"},
	"unknown_threshold_key.json":          {"thresholds[1].abort_on_fial"},
}

func TestV2UnknownFields(t *testing.T) {
	files := fixtures(t, "v2/invalid")
	covered := map[string]bool{}
	for _, f := range files {
		name := filepath.Base(f)
		if !strings.HasPrefix(name, "unknown_") {
			continue
		}
		want, ok := unknownFixtures[name]
		if !ok {
			t.Errorf("%s: no expected paths in unknownFixtures", name)
			continue
		}
		covered[name] = true
		t.Run(name, func(t *testing.T) {
			_, err := definition.Parse(readFile(t, f))
			var ufe *definition.UnknownFieldError
			if !errors.As(err, &ufe) {
				t.Fatalf("error = %v, want *UnknownFieldError", err)
			}
			if !reflect.DeepEqual(ufe.Paths, want) {
				t.Errorf("Paths = %q, want %q", ufe.Paths, want)
			}
			if ufe.Version != definition.V2_0 {
				t.Errorf("Version = %v", ufe.Version)
			}
			if !errors.Is(err, definition.ErrInvalidDefinition) {
				t.Error("errors.Is(ErrInvalidDefinition) = false")
			}
		})
	}
	for name := range unknownFixtures {
		if !covered[name] {
			t.Errorf("missing fixture testdata/v2/invalid/%s", name)
		}
	}
}

func TestUnknownFieldErrorText(t *testing.T) {
	_, err := definition.Parse(readFile(t, "../testdata/v2/invalid/unknown_multiple.json"))
	want := `definition: unknown fields for schema_version "2.0": alpha, scenarios[0].steps[0].think, scenarios[1].steps[0].request.retries, target.proxy, zeta (check spelling and case; v1-only fields are not allowed in v2; a field from a newer schema_version needs a newer reader, see https://github.com/volt-test/contract#compatibility)`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v\nwant    %s", err, want)
	}
	_, err = definition.Parse(readFile(t, "../testdata/v2/invalid/unknown_request.json"))
	want = `definition: unknown field for schema_version "2.0": scenarios[1].steps[0].request.retries (check spelling and case; v1-only fields are not allowed in v2; a field from a newer schema_version needs a newer reader, see https://github.com/volt-test/contract#compatibility)`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v\nwant    %s", err, want)
	}
}

func TestUnknownFieldPathQuoting(t *testing.T) {
	_, err := definition.Parse([]byte(`{"schema_version": "2.0", "name": "x", "a.b": 1, "": 2, "target": {"sp ace": 3}}`))
	var ufe *definition.UnknownFieldError
	if !errors.As(err, &ufe) {
		t.Fatalf("error = %v", err)
	}
	want := []string{`[""]`, `["a.b"]`, `target["sp ace"]`}
	if !reflect.DeepEqual(ufe.Paths, want) {
		t.Errorf("Paths = %q, want %q", ufe.Paths, want)
	}
}

// v2doc returns testdata/v2/valid/minimal.json changed by edit.
func v2doc(t *testing.T, edit func(m map[string]any)) []byte {
	t.Helper()
	m := decodeMap(t, readFile(t, "../testdata/v2/valid/minimal.json"))
	if edit != nil {
		edit(m)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func scenario0(m map[string]any) map[string]any {
	return m["scenarios"].([]any)[0].(map[string]any)
}

func step0(m map[string]any) map[string]any {
	return scenario0(m)["steps"].([]any)[0].(map[string]any)
}

func request0(m map[string]any) map[string]any {
	return step0(m)["request"].(map[string]any)
}

func validScenario(name string) map[string]any {
	return map[string]any{"name": name, "steps": []any{map[string]any{
		"name": "a", "request": map[string]any{"method": "GET", "url": "https://example.com/"},
	}}}
}

func TestV2Problems(t *testing.T) {
	schema := compileSchema(t)
	cases := []struct {
		name string
		edit func(m map[string]any)
		want []string
	}{
		// JSON types, checked before decoding.
		{"string as number", func(m map[string]any) { m["virtual_users"] = "10" }, []string{"virtual_users: must be an integer"}},
		{"fraction", func(m map[string]any) { m["virtual_users"] = 1.5 }, []string{"virtual_users: must be an integer without a decimal point or exponent"}},
		{"int overflow", func(m map[string]any) { m["virtual_users"] = 1e30 }, []string{"virtual_users: must be an integer without a decimal point or exponent"}},
		{"int out of range", func(m map[string]any) { m["virtual_users"] = json.Number("99999999999999999999") }, []string{"virtual_users: is out of range"}},
		{"bool as string", func(m map[string]any) { m["http_debug"] = "true" }, []string{"http_debug: must be true or false"}},
		{"number as string", func(m map[string]any) { m["name"] = 5 }, []string{"name: must be a string"}},
		{"null leaf", func(m map[string]any) { m["description"] = nil }, []string{"description: must not be null"}},
		{"null object", func(m map[string]any) { m["target"] = nil }, []string{"target: must not be null"}},
		{"null array", func(m map[string]any) { m["stages"] = nil }, []string{"stages: must not be null"}},
		{"null array element", func(m map[string]any) { m["scenarios"] = []any{nil} }, []string{"scenarios[0]: must not be null"}},
		{"null header value", func(m map[string]any) { request0(m)["headers"] = map[string]any{"A": nil} }, []string{"scenarios[0].steps[0].request.headers.A: must not be null"}},
		{"header value not string", func(m map[string]any) { request0(m)["headers"] = map[string]any{"A": 1} }, []string{"scenarios[0].steps[0].request.headers.A: must be a string"}},
		{"string at object", func(m map[string]any) { m["target"] = "https://a" }, []string{"target: must be an object"}},
		{"array at object", func(m map[string]any) { scenario0(m)["data_config"] = []any{} }, []string{"scenarios[0].data_config: must be an object"}},
		{"object at array", func(m map[string]any) { m["scenarios"] = map[string]any{} }, []string{"scenarios: must be an array"}},
		{"string in array of objects", func(m map[string]any) { scenario0(m)["steps"] = []any{"GET /"} }, []string{"scenarios[0].steps[0]: must be an object"}},
		{"several type problems", func(m map[string]any) { m["virtual_users"] = true; m["duration"] = 30 },
			[]string{"duration: must be a string", "virtual_users: must be an integer"}},

		// Structure.
		{"missing name", func(m map[string]any) { delete(m, "name") }, []string{"name: is required"}},
		{"blank name", func(m map[string]any) { m["name"] = "  " }, []string{"name: is required"}},
		{"no scenarios", func(m map[string]any) { delete(m, "scenarios") }, []string{"scenarios: must have at least one scenario"}},
		{"empty scenarios", func(m map[string]any) { m["scenarios"] = []any{} }, []string{"scenarios: must have at least one scenario"}},
		{"no steps", func(m map[string]any) { scenario0(m)["steps"] = []any{} }, []string{"scenarios[0].steps: must have at least one step"}},
		{"scenario name", func(m map[string]any) { scenario0(m)["name"] = "" }, []string{"scenarios[0].name: is required"}},
		{"step name", func(m map[string]any) { step0(m)["name"] = "" }, []string{"scenarios[0].steps[0].name: is required"}},
		{"duplicate scenario", func(m map[string]any) {
			m["scenarios"] = []any{validScenario("A"), validScenario("B"), validScenario("A")}
		}, []string{`scenarios[2].name: "A" is already used by scenarios[0]`}},
		{"duplicate step", func(m map[string]any) {
			s := scenario0(m)
			s["steps"] = append(s["steps"].([]any), validScenario("x")["steps"].([]any)[0], map[string]any{
				"name": "Get home", "request": map[string]any{"method": "GET", "url": "https://example.com/"},
			})
		}, []string{`scenarios[0].steps[2].name: "Get home" is already used by scenarios[0].steps[0]`}},
		{"same step name in two scenarios", func(m map[string]any) {
			m["scenarios"] = []any{validScenario("A"), validScenario("B")}
		}, nil},
		{"method", func(m map[string]any) { delete(request0(m), "method") }, []string{"scenarios[0].steps[0].request.method: is required"}},
		{"url", func(m map[string]any) { request0(m)["url"] = "" }, []string{"scenarios[0].steps[0].request.url: is required"}},
		{"durations", func(m map[string]any) {
			m["duration"] = "10"
			m["ramp_up"] = "-1s"
			m["http_timeout"] = "fast"
			m["target"] = map[string]any{"idle_timeout": "1 m"}
			scenario0(m)["think_time"] = "1x"
			step0(m)["think_time"] = "0.5s"
		}, []string{
			`duration: must be a duration such as "30s" or "1m30s", got "10"`,
			"ramp_up: must not be negative",
			`http_timeout: must be a duration such as "30s" or "1m30s", got "fast"`,
			`target.idle_timeout: must be a duration such as "30s" or "1m30s", got "1 m"`,
			`scenarios[0].think_time: must be a duration such as "30s" or "1m30s", got "1x"`,
		}},
		{"stages with constant load", func(m map[string]any) {
			m["stages"] = []any{map[string]any{"duration": "10s", "target": 5}}
			m["virtual_users"] = 5
			m["duration"] = "10s"
			m["ramp_up"] = "1s"
		}, []string{
			"virtual_users: cannot be combined with stages",
			"duration: cannot be combined with stages",
			"ramp_up: cannot be combined with stages",
		}},
		{"no load model", func(m map[string]any) { delete(m, "virtual_users"); delete(m, "duration") }, []string{
			"virtual_users: is required (at least 1) unless stages are set",
		}},
		{"zero virtual users", func(m map[string]any) { m["virtual_users"] = 0 }, []string{
			"virtual_users: is required (at least 1) unless stages are set",
		}},
		{"run once: no duration", func(m map[string]any) { delete(m, "duration") }, nil},
		{"run once with ramp_up", func(m map[string]any) { delete(m, "duration"); m["ramp_up"] = "5s" }, nil},
		{"constant load with ramp_up", func(m map[string]any) { m["ramp_up"] = "5s" }, nil},
		{"zero duration", func(m map[string]any) { m["duration"] = "0s" }, []string{
			"duration: must be greater than zero; leave it out to run the scenario once per virtual user",
		}},
		{"negative duration", func(m map[string]any) { m["duration"] = "-1m" }, []string{
			"duration: must be greater than zero; leave it out to run the scenario once per virtual user",
		}},
		{"staged load alone", func(m map[string]any) {
			delete(m, "virtual_users")
			delete(m, "duration")
			m["stages"] = []any{map[string]any{"duration": "10s", "target": 5}}
		}, nil},
		{"staged load with explicit zero values", func(m map[string]any) {
			m["virtual_users"] = 0
			m["duration"] = ""
			m["ramp_up"] = ""
			m["stages"] = []any{map[string]any{"duration": "10s", "target": 5}}
		}, nil},
		{"empty stages with constant load", func(m map[string]any) { m["stages"] = []any{} }, nil},
		{"empty stages alone", func(m map[string]any) {
			delete(m, "virtual_users")
			delete(m, "duration")
			m["stages"] = []any{}
		}, []string{"virtual_users: is required (at least 1) unless stages are set"}},
		{"stage zero duration", func(m map[string]any) {
			delete(m, "virtual_users")
			delete(m, "duration")
			m["stages"] = []any{map[string]any{"duration": "10s", "target": 5}, map[string]any{"duration": "0s"}, map[string]any{"duration": "-5s", "target": 1}}
		}, []string{"stages[1].duration: must be greater than zero", "stages[2].duration: must be greater than zero"}},
		{"zero is fine for think_time, http_timeout, ramp_up and idle_timeout", func(m map[string]any) {
			m["ramp_up"] = "0s"
			m["http_timeout"] = "0s"
			m["target"] = map[string]any{"idle_timeout": "0s"}
			scenario0(m)["think_time"] = "0s"
			step0(m)["think_time"] = "0ms"
		}, nil},
		{"stage fields", func(m map[string]any) {
			delete(m, "virtual_users")
			delete(m, "duration")
			m["stages"] = []any{map[string]any{"target": 5}, map[string]any{"duration": "1h", "target": -1}}
		}, []string{"stages[0].duration: is required", "stages[1].target: must not be negative"}},
		{"negative numbers", func(m map[string]any) {
			m["virtual_users"] = -1
			scenario0(m)["weight"] = -5
			m["region_config"] = []any{map[string]any{"region": "us", "weight": -1}, map[string]any{"weight": 1}}
		}, []string{
			"virtual_users: must not be negative",
			"region_config[0].weight: must not be negative",
			"region_config[1].region: is required",
			"scenarios[0].weight: must not be negative",
		}},
		{"extract", func(m map[string]any) {
			step0(m)["extract"] = []any{
				map[string]any{"variable_name": "a", "selector": "$.a", "type": "xml"},
				map[string]any{"variable_name": " ", "selector": "\t", "type": ""},
			}
		}, []string{
			`scenarios[0].steps[0].extract[0].type: must be one of "json", "regex", "header", "cookie", "html", got "xml"`,
			"scenarios[0].steps[0].extract[1].variable_name: is required",
			"scenarios[0].steps[0].extract[1].selector: is required",
			`scenarios[0].steps[0].extract[1].type: is required (one of "json", "regex", "header", "cookie", "html")`,
		}},
		{"data_config", func(m map[string]any) {
			scenario0(m)["data_config"] = map[string]any{"data_format": "xlsx", "mode": "shuffle"}
		}, []string{
			`scenarios[0].data_config.data_format: must be one of "csv", got "xlsx"`,
			`scenarios[0].data_config.mode: must be one of "sequential", "random", "unique", got "shuffle"`,
			"scenarios[0].data_config.data_source: is required",
		}},
		{"empty data_config", func(m map[string]any) { scenario0(m)["data_config"] = map[string]any{} }, []string{
			`scenarios[0].data_config.data_format: is required (one of "csv")`,
			`scenarios[0].data_config.mode: is required (one of "sequential", "random", "unique")`,
			"scenarios[0].data_config.data_source: is required",
		}},
		// Thresholds.
		{"threshold not an array", func(m map[string]any) { m["thresholds"] = "p95 < 1s" }, []string{"thresholds: must be an array"}},
		{"null threshold", func(m map[string]any) { m["thresholds"] = []any{"p95 < 1s", nil} }, []string{"thresholds[1]: must not be null"}},
		{"number threshold", func(m map[string]any) { scenario0(m)["thresholds"] = []any{5} },
			[]string{"scenarios[0].thresholds[0]: must be a string or an object"}},
		{"threshold object types", func(m map[string]any) {
			step0(m)["thresholds"] = []any{map[string]any{"expr": 5, "abort_on_fail": "yes", "delay_abort": 10}}
		}, []string{
			"scenarios[0].steps[0].thresholds[0].abort_on_fail: must be true or false",
			"scenarios[0].steps[0].thresholds[0].delay_abort: must be a string",
			"scenarios[0].steps[0].thresholds[0].expr: must be a string",
		}},
		{"threshold expressions at every scope", func(m map[string]any) {
			m["thresholds"] = []any{"p95 < 500"}
			scenario0(m)["thresholds"] = []any{"p95 < 1s", map[string]any{"expr": "error_rate < 5", "abort_on_fail": true}}
			step0(m)["thresholds"] = []any{"p95 < 1s", "iterations > 1", "iteration_duration_p95 < 1s", "p99.99 < 1s"}
		}, []string{
			`thresholds[0]: invalid threshold "p95 < 500": p95 is a duration and needs a unit (us, ms, s or m); did you mean "500ms"? (at offset 6)`,
			`scenarios[0].thresholds[1]: invalid threshold "error_rate < 5": error_rate is a rate and needs "%"; did you mean "5%"? (at offset 13)`,
			"scenarios[0].steps[0].thresholds[1]: iterations is not available on a step; use it on the test or a scenario",
			"scenarios[0].steps[0].thresholds[2]: iteration_duration_p95 is not available on a step; use it on the test or a scenario",
			`scenarios[0].steps[0].thresholds[3]: invalid threshold "p99.99 < 1s": unknown metric "p99.99"; the percentiles are p50, p75, p90, p95, p99 and p99.9 (at offset 0)`,
		}},
		{"scope by position, not by name", func(m map[string]any) {
			step0(m)["name"] = ""
			step0(m)["thresholds"] = []any{"iterations > 1"}
		}, []string{
			"scenarios[0].steps[0].name: is required",
			"scenarios[0].steps[0].thresholds[0]: iterations is not available on a step; use it on the test or a scenario",
		}},
		{"threshold without expr", func(m map[string]any) { m["thresholds"] = []any{map[string]any{"abort_on_fail": true}, ""} }, []string{
			`thresholds[0]: invalid threshold "": the expression is empty; write <metric> <operator> <value>, e.g. "p95 < 500ms" (at offset 0)`,
			`thresholds[1]: invalid threshold "": the expression is empty; write <metric> <operator> <value>, e.g. "p95 < 500ms" (at offset 0)`,
		}},
		{"delay_abort", func(m map[string]any) {
			m["thresholds"] = []any{
				map[string]any{"expr": "p95 < 1s", "abort_on_fail": true, "delay_abort": "soon"},
				map[string]any{"expr": "p95 < 1s", "abort_on_fail": true, "delay_abort": "-1s"},
				map[string]any{"expr": "p95 < 1s", "delay_abort": "1s"},
				map[string]any{"expr": "p95 < 1s", "abort_on_fail": false, "delay_abort": "0s"},
			}
		}, []string{
			`thresholds[0].delay_abort: must be a duration such as "30s" or "1m30s", got "soon"`,
			"thresholds[1].delay_abort: must not be negative",
			"thresholds[2].delay_abort: only applies with abort_on_fail: true",
			"thresholds[3].delay_abort: only applies with abort_on_fail: true",
		}},
		{"thresholds valid at their scopes", func(m map[string]any) {
			m["thresholds"] = []any{"iterations > 1", "dropped_iterations == 0", "iteration_duration < 2s", map[string]any{"expr": "p95<1s", "abort_on_fail": true, "delay_abort": "0s"}}
			scenario0(m)["thresholds"] = []any{"iteration_duration_p99.9 < 2s", map[string]any{"expr": "failed == 0"},
				map[string]any{"expr": "p95<1s", "delay_abort": ""}, map[string]any{"expr": "p95<1s", "abort_on_fail": false, "delay_abort": ""}}
			step0(m)["thresholds"] = []any{"ttfb < 100ms", "bytes_sent<=1kib", "rps>1", "check_rate == 100%"}
		}, nil},

		{"data_download_url alone is a source", func(m map[string]any) {
			scenario0(m)["data_config"] = map[string]any{"data_format": "csv", "mode": "random", "data_download_url": "https://storage.example.com/x.csv"}
		}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := v2doc(t, tc.edit)
			d, err := definition.Parse(doc)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Parse: %v", err)
				}
				requireSchemaAccepts(t, schema, doc)
				if d.IsV1() {
					t.Fatal("parsed as v1")
				}
				return
			}
			var ve *definition.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error = %v, want *ValidationError", err)
			}
			var got []string
			for _, p := range ve.Problems {
				got = append(got, p.String())
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("problems:\n got %q\nwant %q", got, tc.want)
			}
			if !errors.Is(err, definition.ErrInvalidDefinition) {
				t.Error("errors.Is(ErrInvalidDefinition) = false")
			}
		})
	}
}

func TestValidationErrorText(t *testing.T) {
	_, err := definition.Parse(v2doc(t, func(m map[string]any) {
		delete(m, "name")
		request0(m)["url"] = "api/items"
	}))
	want := `definition: name: is required; scenarios[0].steps[0].request.url: must be an absolute http(s) URL, start with ${...}, or be a path starting with "/"`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v\nwant    %s", err, want)
	}
}

func TestV2URLRules(t *testing.T) {
	schema := compileSchema(t)
	const (
		absolute = `must be an absolute http(s) URL, start with ${...}, or be a path starting with "/"`
		needBase = "is relative, so target.base_url is required"
		double   = `must not start with "//"; use an absolute URL or a path starting with a single "/"`
		badBase  = "must be an absolute http(s) URL or start with ${...}"
		queryFrg = "must not contain a query string or fragment"
	)
	cases := []struct {
		base, url string
		wantURL   string
		wantBase  string
	}{
		{"", "https://example.com/a", "", ""},
		{"", "http://example.com", "", ""},
		{"", "HTTPS://EXAMPLE.COM/A", "", ""},
		{"", "https://${host}/path", "", ""},
		{"", "${base}/path", "", ""},
		{"", "${url}", "", ""},
		{"", "/items", needBase, ""},
		{"https://api.example.com", "/items?q=${q}", "", ""},
		{"https://api.example.com/", "/items", "", ""},
		{"${base}", "/items", "", ""},
		{"http://localhost:8080/v1", "/", "", ""},
		{"", "items", absolute, ""},
		{"https://api.example.com", "items", absolute, ""},
		{"", "ftp://example.com/a", absolute, ""},
		{"", "https://", absolute, ""},
		{"", "example.com/a", absolute, ""},
		{"", "{host}/a", absolute, ""},
		{"https://api.example.com", "//cdn.example.com/a", double, ""},
		{"api.example.com", "/items", "", badBase},
		{"ftp://api.example.com", "https://a/b", "", badBase},
		{"https://api.example.com?x=1", "/items", "", queryFrg},
		{"https://api.example.com#top", "/items", "", queryFrg},
	}
	for _, tc := range cases {
		t.Run(tc.base+" "+tc.url, func(t *testing.T) {
			doc := v2doc(t, func(m map[string]any) {
				if tc.base != "" {
					m["target"] = map[string]any{"base_url": tc.base}
				}
				request0(m)["url"] = tc.url
			})
			_, err := definition.Parse(doc)
			var want []string
			if tc.wantBase != "" {
				want = append(want, "target.base_url: "+tc.wantBase)
			}
			if tc.wantURL != "" {
				want = append(want, "scenarios[0].steps[0].request.url: "+tc.wantURL)
			}
			if want == nil {
				if err != nil {
					t.Fatalf("Parse: %v", err)
				}
				requireSchemaAccepts(t, schema, doc)
				return
			}
			var ve *definition.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error = %v, want *ValidationError", err)
			}
			var got []string
			for _, p := range ve.Problems {
				got = append(got, p.String())
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("problems:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestV2ParseKeepsURLsAsWritten(t *testing.T) {
	d, err := definition.Parse(readFile(t, "../testdata/v2/valid/full.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Scenarios[0].Steps[0].Request.URL; got != "/forms/login" {
		t.Errorf("URL = %q, want it unresolved", got)
	}
	if d.Target.BaseURL != "https://shop.example.com/api/" || d.Target.URL != "https://shop.example.com" {
		t.Errorf("Target = %+v", d.Target)
	}
}

func TestV2DataDownloadURLAccepted(t *testing.T) {
	d, err := definition.Parse(v2doc(t, func(m map[string]any) {
		scenario0(m)["data_config"] = map[string]any{
			"data_source": "users.csv", "data_format": "csv", "mode": "sequential", "has_header": true,
			"data_download_url": "https://storage.example.com/users.csv?sig=x",
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Scenarios[0].DataConfig.DataDownloadURL; got != "https://storage.example.com/users.csv?sig=x" {
		t.Errorf("DataDownloadURL = %q", got)
	}
}

// TestV2RoundTrip checks that marshalling a parsed v2 definition yields a
// v2 document that parses to the same definition.
func TestV2RoundTrip(t *testing.T) {
	schema := compileSchema(t)
	for _, f := range fixtures(t, "v2/valid") {
		t.Run(filepath.Base(f), func(t *testing.T) {
			d1, err := definition.Parse(readFile(t, f))
			if err != nil {
				t.Fatal(err)
			}
			b1, err := json.Marshal(d1)
			if err != nil {
				t.Fatal(err)
			}
			requireSchemaAccepts(t, schema, b1)
			d2, err := definition.Parse(b1)
			if err != nil {
				t.Fatalf("re-parse: %v\n%s", err, b1)
			}
			b2, _ := json.Marshal(d2)
			if !bytes.Equal(b1, b2) {
				t.Errorf("not a fixed point:\n%s\n%s", b1, b2)
			}
			d3, _ := definition.Parse(b2)
			if !reflect.DeepEqual(d2, d3) {
				t.Errorf("Parse(Marshal(d)) != d")
			}
			for _, c := range changesBetween(decodeMap(t, readFile(t, f)), decodeMap(t, b1)) {
				t.Error(c)
			}
		})
	}

	// A definition built in Go, with no empty collections, survives exactly.
	v := definition.V2_0
	want := &definition.Definition{
		SchemaVersion: &v,
		Name:          "built",
		Description:   "d",
		Stages:        []definition.Stage{{Duration: "10s", Target: 5}, {Duration: "5s"}},
		HTTPTimeout:   "5s",
		HTTPDebug:     true,
		Target:        &definition.Target{URL: "https://a", IdleTimeout: "1m", BaseURL: "${base}"},
		RegionConfig:  []definition.RegionWeight{{Region: "us", Weight: 2}},
		Thresholds:    []definition.Threshold{{Expr: "p95 < 1s"}, {Expr: "error_rate < 1%", AbortOnFail: true, DelayAbort: "10s"}},
		Scenarios: []definition.Scenario{{
			Name: "s", Description: "x", Weight: 3, ThinkTime: "1s", AutoHandleCookies: true,
			Thresholds: []definition.Threshold{{Expr: "iterations > 1", AbortOnFail: true}},
			DataConfig: &definition.DataConfig{DataSource: "a.csv", DataContent: "YQ==", DataFormat: "csv", HasHeader: true, Mode: "unique"},
			Steps: []definition.Step{{
				Name: "a", ThinkTime: "1ms", Thresholds: []definition.Threshold{{Expr: "ttfb_p99.9 <= 50ms"}},
				Request: definition.Request{Method: "POST", URL: "/a", Body: "{}", Headers: map[string]string{"A": "b"}},
				Extract: []definition.Extract{{VariableName: "v", Selector: "p", Attribute: "href", Type: "html"}},
			}},
		}},
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	requireSchemaAccepts(t, schema, b)
	got, err := definition.Parse(b)
	if err != nil {
		t.Fatalf("Parse(Marshal(d)): %v\n%s", err, b)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(Marshal(d)) != d\n got %+v\nwant %+v", got, want)
	}
}

func TestParseSyntaxErrors(t *testing.T) {
	cases := map[string]string{
		"empty":         ``,
		"spaces":        `   `,
		"truncated":     `{"name": `,
		"trailing data": `{"name": "x"} {}`,
		"array":         `[{"name": "x"}]`,
		"string":        `"x"`,
		"null":          `null`,
		"number":        `2`,
		"true":          `true`,
		"false":         `false`,
		"BOM prefix":    "\xef\xbb\xbf{\"name\": \"x\"}",
		"bad utf8 key":  "{\"a\xff\": 1",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := definition.Parse([]byte(in))
			var se *definition.SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("error = %v, want *SyntaxError", err)
			}
			if !errors.Is(err, definition.ErrInvalidDefinition) || se.Unwrap() == nil {
				t.Error("SyntaxError must match ErrInvalidDefinition and wrap the cause")
			}
		})
	}
	_, err := definition.Parse([]byte(`[]`))
	if want := "definition: invalid JSON: a test definition must be a JSON object"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %s", err, want)
	}
}

func TestErrorTypes(t *testing.T) {
	errs := []error{
		&definition.SyntaxError{Err: errors.New("x")},
		&definition.InvalidVersionError{Value: `"x"`},
		&definition.UnsupportedVersionError{Version: definition.SchemaVersion{Major: 9}},
		&definition.UnknownFieldError{Paths: []string{"x"}, Version: definition.V2_0},
		&definition.ValidationError{Problems: []definition.Problem{{Path: "x", Message: "y"}}},
	}
	for _, err := range errs {
		if !errors.Is(err, definition.ErrInvalidDefinition) {
			t.Errorf("%T: errors.Is(ErrInvalidDefinition) = false", err)
		}
		if errors.Is(err, errors.New("invalid test definition")) {
			t.Errorf("%T matches an unrelated error", err)
		}
		if !strings.HasPrefix(err.Error(), "definition: ") {
			t.Errorf("%T: %q lacks the package prefix", err, err.Error())
		}
	}
	if got := (definition.Problem{Message: "m"}).String(); got != "m" {
		t.Errorf("Problem without path = %q", got)
	}
}
