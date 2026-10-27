package definition_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

// loadFixtureProblems maps each invalid fixture for the load model to the
// problems Parse reports, in order.
var loadFixtureProblems = map[string][]string{
	"structure_load_type_missing.json":                  {`load.type: is required (one of "rate", "ramping_rate")`},
	"structure_load_type_unknown.json":                  {`load.type: must be one of "rate", "ramping_rate", got "constant"`},
	"structure_load_time_unit_zero.json":                {`load.time_unit: must be greater than zero`},
	"structure_load_preallocated_missing.json":          {`load.preallocated_vus: is required (at least 1)`},
	"structure_load_max_vus_below_preallocated.json":    {`load.max_vus: must be at least preallocated_vus (200), got 100`},
	"structure_load_rate_missing.json":                  {`load.rate: is required (at least 1) for type rate`},
	"structure_load_rate_duration_missing.json":         {`load.duration: is required for type rate`},
	"structure_load_rate_duration_zero.json":            {`load.duration: must be greater than zero`},
	"structure_load_rate_stages.json":                   {`load.stages: does not apply to type rate`},
	"structure_load_ramping_no_stages.json":             {`load.stages: is required for type ramping_rate (at least one stage)`},
	"structure_load_ramping_stage_zero_duration.json":   {`load.stages[1].duration: must be greater than zero`},
	"structure_load_ramping_stage_negative_target.json": {`load.stages[0].target: must not be negative`},
	"structure_load_ramping_rate.json":                  {`load.rate: does not apply to type ramping_rate; set each stage's target`},
	"structure_load_ramping_duration.json":              {`load.duration: does not apply to type ramping_rate; the stages set the length`},
	"structure_load_with_virtual_users.json":            {`load: cannot be combined with virtual_users, duration`},
	"structure_load_with_stages.json":                   {`load: cannot be combined with stages`},
	"structure_load_none.json":                          {`virtual_users: is required (at least 1) unless stages or load are set`},
	"structure_load_both.json": {
		"virtual_users: cannot be combined with stages",
		"duration: cannot be combined with stages",
	},
	"type_load_string.json":      {`load: must be an object`},
	"type_load_null.json":        {`load: must not be null`},
	"type_load_rate_string.json": {`load.rate: must be an integer`},
}

// loadFixturePrefixes are the fixture name prefixes this file owns.
var loadFixturePrefixes = []string{"structure_load_", "type_load_"}

// TestLoadFixtures checks that each invalid load fixture fails with exactly
// the problems listed for it, and that every such fixture is listed.
func TestLoadFixtures(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures(t, "v2/invalid") {
		name := filepath.Base(f)
		want, ok := loadFixtureProblems[name]
		if !ok {
			for _, p := range loadFixturePrefixes {
				if strings.HasPrefix(name, p) {
					t.Errorf("%s: no expected problem in loadFixtureProblems", name)
				}
			}
			continue
		}
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			if got := problemStrings(t, readFile(t, f)); !reflect.DeepEqual(got, want) {
				t.Errorf("problems = %q, want %q", got, want)
			}
		})
	}
	for name := range loadFixtureProblems {
		if !seen[name] {
			t.Errorf("missing fixture testdata/v2/invalid/%s", name)
		}
	}
}

// loadDoc returns minimal.json with its constant load replaced by load.
func loadDoc(t *testing.T, load map[string]any, edit func(m map[string]any)) []byte {
	t.Helper()
	return v2doc(t, func(m map[string]any) {
		delete(m, "virtual_users")
		delete(m, "duration")
		m["load"] = load
		if edit != nil {
			edit(m)
		}
	})
}

// TestLoadRules covers the load rules that have no fixture of their own.
// Every definition Parse accepts must pass the schema too.
func TestLoadRules(t *testing.T) {
	schema := compileSchema(t)
	rate := func(kv ...any) map[string]any {
		m := map[string]any{"type": "rate", "rate": 10, "duration": "1m", "preallocated_vus": 5}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	ramping := func(stages ...any) map[string]any {
		return map[string]any{"type": "ramping_rate", "stages": stages, "preallocated_vus": 5}
	}
	stage := func(d string, target int) map[string]any { return map[string]any{"duration": d, "target": target} }
	cases := []struct {
		name string
		load map[string]any
		edit func(m map[string]any)
		want []string
	}{
		{"rate defaults", rate(), nil, nil},
		{"max_vus equal to preallocated_vus", rate("max_vus", 5), nil, nil},
		{"max_vus zero means preallocated_vus", rate("max_vus", 0), nil, nil},
		{"time_unit empty means 1s", rate("time_unit", ""), nil, nil},
		{"time_unit 1m", rate("time_unit", "1m"), nil, nil},
		{"rate with empty stages", rate("stages", []any{}), nil, nil},
		{"top-level keys with zero values", rate(), func(m map[string]any) {
			m["virtual_users"] = 0
			m["duration"] = ""
			m["ramp_up"] = ""
			m["stages"] = []any{}
		}, nil},
		{"ramping", ramping(stage("1m", 100), stage("30s", 0)), nil, nil},
		{"ramping with all-zero targets", ramping(stage("1m", 0), map[string]any{"duration": "1m"}), nil, nil},
		{"ramping with zero rate and empty duration", map[string]any{
			"type": "ramping_rate", "rate": 0, "duration": "", "stages": []any{stage("1m", 1)}, "preallocated_vus": 1,
		}, nil, nil},

		{"negative preallocated_vus", rate("preallocated_vus", -1), nil, []string{"load.preallocated_vus: must not be negative"}},
		{"negative max_vus", rate("max_vus", -1), nil, []string{"load.max_vus: must not be negative"}},
		{"negative rate", rate("rate", -1), nil, []string{"load.rate: must not be negative"}},
		{"bad time_unit", rate("time_unit", "second"), nil, []string{
			`load.time_unit: must be a duration such as "30s" or "1m30s", got "second"`}},
		{"negative time_unit", rate("time_unit", "-1s"), nil, []string{"load.time_unit: must be greater than zero"}},
		{"bad duration", rate("duration", "5"), nil, []string{
			`load.duration: must be a duration such as "30s" or "1m30s", got "5"`}},
		{"stage without duration", ramping(map[string]any{"target": 5}), nil, []string{"load.stages[0].duration: is required"}},
		{"unknown type skips the type rules", map[string]any{"type": "burst", "preallocated_vus": 1}, nil, []string{
			`load.type: must be one of "rate", "ramping_rate", got "burst"`}},
		{"load with ramp_up", rate(), func(m map[string]any) { m["ramp_up"] = "10s" }, []string{
			"load: cannot be combined with ramp_up"}},
		{"load with every load key", rate(), func(m map[string]any) {
			m["virtual_users"] = 1
			m["duration"] = "1m"
			m["ramp_up"] = "1s"
			m["stages"] = []any{stage("1m", 1)}
		}, []string{"load: cannot be combined with virtual_users, duration, ramp_up, stages"}},
		{"empty load", map[string]any{}, nil, []string{
			`load.type: is required (one of "rate", "ramping_rate")`,
			"load.preallocated_vus: is required (at least 1)",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := loadDoc(t, tc.load, tc.edit)
			if got := problemStrings(t, doc); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("problems = %q, want %q", got, tc.want)
			}
			if tc.want == nil {
				requireSchemaAccepts(t, schema, doc)
			} else if err := validateAgainstSchema(t, schema, doc); err == nil && !looserLoadCase(tc.name) {
				t.Error("schema accepts a definition Parse rejects")
			}
		})
	}
}

// looserLoadCase names the TestLoadRules cases the schema accepts: duration
// values are plain strings in the schema.
func looserLoadCase(name string) bool {
	switch name {
	case "bad time_unit", "negative time_unit", "bad duration":
		return true
	}
	return false
}

// TestLoadRoundTrip: both load fixtures survive Marshal and Parse.
func TestLoadRoundTrip(t *testing.T) {
	for _, name := range []string{"load_rate.json", "load_ramping_rate.json"} {
		d := mustParse(t, readFile(t, filepath.Join("..", "testdata", "v2", "valid", name)))
		if d.Load == nil || d.VirtualUsers != 0 || d.Duration != "" || d.Stages != nil {
			t.Fatalf("%s: load %+v, virtual_users %d, duration %q, stages %v", name, d.Load, d.VirtualUsers, d.Duration, d.Stages)
		}
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if d2 := mustParse(t, b); !reflect.DeepEqual(d, d2) {
			t.Errorf("%s: Parse(Marshal(d)) = %+v, want %+v", name, d2.Load, d.Load)
		}
	}
	d := mustParse(t, readFile(t, "../testdata/v2/valid/load_rate.json"))
	want := definition.Load{Type: definition.LoadRate, Rate: 500, TimeUnit: "1s", Duration: "5m", PreallocatedVUs: 200, MaxVUs: 1000}
	if !reflect.DeepEqual(*d.Load, want) {
		t.Errorf("load = %+v, want %+v", *d.Load, want)
	}
}
