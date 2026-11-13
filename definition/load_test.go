package definition_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

// loadFixtureProblems maps each invalid fixture for the load model to the
// problems Parse reports, in order.
var loadFixtureProblems = map[string][]string{
	"structure_load_type_missing.json":                         {`load.type: is required (one of "rate", "ramping_rate")`},
	"structure_load_type_unknown.json":                         {`load.type: must be one of "rate", "ramping_rate", got "burst"`},
	"structure_load_time_unit_zero.json":                       {`load.time_unit: must be greater than zero`},
	"structure_load_preallocated_missing.json":                 {`load.preallocated_virtual_users: is required (at least 1)`},
	"structure_load_max_virtual_users_below_preallocated.json": {`load.max_virtual_users: must be at least preallocated_virtual_users (200), got 100`},
	"structure_load_rate_missing.json":                         {`load.rate: is required (at least 1) for type rate`},
	"structure_load_rate_duration_missing.json":                {`load.duration: is required for type rate`},
	"structure_load_rate_duration_zero.json":                   {`load.duration: must be greater than zero`},
	"structure_load_rate_stages.json":                          {`load.stages: does not apply to type rate`},
	"structure_load_ramping_no_stages.json":                    {`load.stages: is required for type ramping_rate (at least one stage)`},
	"structure_load_ramping_stage_zero_duration.json":          {`load.stages[1].duration: must be greater than zero`},
	"structure_load_ramping_stage_negative_target.json":        {`load.stages[0].target: must not be negative`},
	"structure_load_ramping_all_zero.json":                     {`load: never exceeds a rate of 0, so the run would do nothing; set a stage target above 0`},
	"structure_load_ramping_rate.json":                         {`load.rate: does not apply to type ramping_rate; set each stage's target`},
	"structure_load_ramping_duration.json":                     {`load.duration: does not apply to type ramping_rate; the stages set the length`},
	"structure_load_with_virtual_users.json":                   {`load: cannot be combined with virtual_users, duration`},
	"structure_load_with_stages.json":                          {`load: cannot be combined with stages`},
	"structure_load_none.json":                                 {`virtual_users: is required (at least 1) unless stages or load are set`},
	"structure_load_both.json": {
		"virtual_users: cannot be combined with stages",
		"duration: cannot be combined with stages",
	},
	"structure_load_constant_top_level.json": {`load.type: "constant" is only valid on a scenario's load; ` +
		`at the top level use virtual_users and duration, or stages`},
	"structure_load_stages_top_level.json": {`load.type: "stages" is only valid on a scenario's load; ` +
		`at the top level use virtual_users and duration, or stages`},
	"structure_load_initial_virtual_users_constant.json": {`initial_virtual_users: needs stages`},
	"structure_load_initial_virtual_users_negative.json": {`initial_virtual_users: must not be negative`},
	"structure_load_ramp_down_grace_rate.json":           {`ramp_down_grace: needs stages`},
	"structure_load_stop_grace_negative.json":            {`stop_grace: must not be negative`},
	"structure_load_stop_grace_on_load.json":             {`load.stop_grace: does not apply to the test-level load; use the top-level stop_grace`},
	"type_load_string.json":                              {`load: must be an object`},
	"type_load_null.json":                                {`load: must not be null`},
	"type_load_rate_string.json":                         {`load.rate: must be an integer`},
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

// rampingZero is the problem for a ramping_rate load whose every stage
// target is 0.
const rampingZero = "load: never exceeds a rate of 0, so the run would do nothing; set a stage target above 0"

// TestLoadRules covers the load rules that have no fixture of their own.
// Every definition Parse accepts must pass the schema too.
func TestLoadRules(t *testing.T) {
	schema := compileSchema(t)
	rate := func(kv ...any) map[string]any {
		m := map[string]any{"type": "rate", "rate": 10, "duration": "1m", "preallocated_virtual_users": 5}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	ramping := func(stages ...any) map[string]any {
		return map[string]any{"type": "ramping_rate", "stages": stages, "preallocated_virtual_users": 5}
	}
	stage := func(d string, target int) map[string]any { return map[string]any{"duration": d, "target": target} }
	cases := []struct {
		name string
		load map[string]any
		edit func(m map[string]any)
		want []string
	}{
		{"rate defaults", rate(), nil, nil},
		{"max_virtual_users equal to preallocated_virtual_users", rate("max_virtual_users", 5), nil, nil},
		{"max_virtual_users zero means preallocated_virtual_users", rate("max_virtual_users", 0), nil, nil},
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
		{"ramping from 0 to 1", ramping(stage("1m", 0), stage("1m", 1)), nil, nil},
		{"ramping to 1", ramping(stage("1m", 1)), nil, nil},
		{"top-level stop_grace with rate", rate(), func(m map[string]any) { m["stop_grace"] = "45s" }, nil},
		{"top-level stop_grace 0s with ramping", ramping(stage("1m", 5)), func(m map[string]any) { m["stop_grace"] = "0s" }, nil},
		{"lifecycle keys with zero values", rate("stop_grace", ""), func(m map[string]any) {
			m["stop_grace"] = ""
			m["ramp_down_grace"] = ""
		}, nil},
		{"ramping with zero rate and empty duration", map[string]any{
			"type": "ramping_rate", "rate": 0, "duration": "", "stages": []any{stage("1m", 1)}, "preallocated_virtual_users": 1,
		}, nil, nil},

		{"negative preallocated_virtual_users", rate("preallocated_virtual_users", -1), nil, []string{"load.preallocated_virtual_users: must not be negative"}},
		{"negative max_virtual_users", rate("max_virtual_users", -1), nil, []string{"load.max_virtual_users: must not be negative"}},
		{"negative rate", rate("rate", -1), nil, []string{"load.rate: must not be negative"}},
		{"bad time_unit", rate("time_unit", "second"), nil, []string{
			`load.time_unit: must be a duration such as "30s" or "1m30s", got "second"`}},
		{"negative time_unit", rate("time_unit", "-1s"), nil, []string{"load.time_unit: must be greater than zero"}},
		{"bad duration", rate("duration", "5"), nil, []string{
			`load.duration: must be a duration such as "30s" or "1m30s", got "5"`}},
		{"stage without duration", ramping(map[string]any{"target": 5}), nil, []string{"load.stages[0].duration: is required"}},
		{"ramping to 0", ramping(stage("1m", 0)), nil, []string{rampingZero}},
		{"ramping with all-zero targets", ramping(stage("1m", 0), stage("1m", 0), stage("1m", 0)), nil, []string{rampingZero}},
		{"ramping with an absent target", ramping(stage("1m", 0), map[string]any{"duration": "1m"}), nil, []string{rampingZero}},
		{"ramping with a negative target", ramping(stage("1m", -5), stage("1m", 0)), nil, []string{
			"load.stages[0].target: must not be negative"}},
		{"unknown type skips the type rules", map[string]any{"type": "burst", "preallocated_virtual_users": 1}, nil, []string{
			`load.type: must be one of "rate", "ramping_rate", got "burst"`}},
		{"load with ramp_up", rate(), func(m map[string]any) { m["ramp_up"] = "10s" }, []string{
			"load: cannot be combined with ramp_up"}},
		{"load with every load key", rate(), func(m map[string]any) {
			m["virtual_users"] = 1
			m["duration"] = "1m"
			m["ramp_up"] = "1s"
			m["stages"] = []any{stage("1m", 1)}
		}, []string{"load: cannot be combined with virtual_users, duration, ramp_up, stages"}},
		{"initial_virtual_users 0 on the load", rate("initial_virtual_users", 0), nil, []string{
			"load.initial_virtual_users: does not apply to type rate"}},
		{"ramp_down_grace on a ramping load", ramping(stage("1m", 5)), func(m map[string]any) {
			m["load"].(map[string]any)["ramp_down_grace"] = "1s"
		}, []string{"load.ramp_down_grace: does not apply to type ramping_rate"}},
		{"stop_grace on the load", rate("stop_grace", "0s"), nil, []string{
			"load.stop_grace: does not apply to the test-level load; use the top-level stop_grace"}},
		{"top-level initial_virtual_users with load", rate(), func(m map[string]any) { m["initial_virtual_users"] = 0 }, []string{
			"initial_virtual_users: needs stages"}},
		{"top-level ramp_down_grace with load", ramping(stage("1m", 5)), func(m map[string]any) { m["ramp_down_grace"] = "0s" }, []string{
			"ramp_down_grace: needs stages"}},
		{"top-level bad stop_grace with load", rate(), func(m map[string]any) { m["stop_grace"] = "soon" }, []string{
			`stop_grace: must be a duration such as "30s" or "1m30s", got "soon"`}},
		{"empty load", map[string]any{}, nil, []string{
			`load.type: is required (one of "rate", "ramping_rate")`,
			"load.preallocated_virtual_users: is required (at least 1)",
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
	case "bad time_unit", "negative time_unit", "bad duration", "top-level bad stop_grace with load":
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
	want := definition.Load{Type: definition.LoadRate, Rate: 500, TimeUnit: "1s", Duration: "5m", PreallocatedVirtualUsers: 200, MaxVirtualUsers: 1000}
	if !reflect.DeepEqual(*d.Load, want) {
		t.Errorf("load = %+v, want %+v", *d.Load, want)
	}
}

// TestLoadLifecycleRoundTrip checks that an explicit initial_virtual_users
// of 0 is kept apart from an absent one through Parse and Marshal, at the
// test level and on a pool, and that the graces survive as written.
func TestLoadLifecycleRoundTrip(t *testing.T) {
	d := mustParse(t, readFile(t, "../testdata/v2/valid/load_lifecycle.json"))
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"initial_virtual_users":0,`, `"initial_virtual_users":10,`, `"stop_grace":"0s"`, `"ramp_down_grace":"45s"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("Marshal has no %s: %s", want, b)
		}
	}
	d2 := mustParse(t, b)
	if !reflect.DeepEqual(d, d2) {
		t.Errorf("Parse(Marshal(d)) = %+v, want %+v", d2, d)
	}
	for name, got := range map[string]*definition.Definition{"parsed": d, "round trip": d2} {
		if got.InitialVirtualUsers == nil || *got.InitialVirtualUsers != 0 || got.StopGrace != "0s" || got.RampDownGrace != "45s" {
			t.Errorf("%s: test level = initial %v, stop_grace %q, ramp_down_grace %q", name, got.InitialVirtualUsers, got.StopGrace, got.RampDownGrace)
		}
		spike := got.Scenarios[1].Load
		if spike.InitialVirtualUsers == nil || *spike.InitialVirtualUsers != 10 || spike.RampDownGrace != "10s" || spike.StopGrace != "5s" {
			t.Errorf("%s: Spike load = %+v", name, spike)
		}
		api := got.Scenarios[2].Load
		if api.InitialVirtualUsers != nil || api.StopGrace != "1m" || api.PreallocatedVirtualUsers != 2 || api.MaxVirtualUsers != 10 {
			t.Errorf("%s: API load = %+v", name, api)
		}
	}

	// Absent keys stay absent.
	plain := mustParse(t, readFile(t, "../testdata/v2/valid/staged.json"))
	if plain.InitialVirtualUsers != nil || plain.StopGrace != "" || plain.RampDownGrace != "" {
		t.Errorf("staged.json: initial %v, stop_grace %q, ramp_down_grace %q", plain.InitialVirtualUsers, plain.StopGrace, plain.RampDownGrace)
	}
	pb, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"initial_virtual_users", "stop_grace", "ramp_down_grace"} {
		if strings.Contains(string(pb), k) {
			t.Errorf("Marshal of staged.json writes %s: %s", k, pb)
		}
	}
}

// TestLoadLifecycleRepeatedKeys checks that a repeated initial_virtual_users,
// stop_grace or ramp_down_grace reads as its last occurrence, as
// encoding/json reads it, at the test level and on a stages pool, and
// that a null in last place is refused while one before a value is not.
func TestLoadLifecycleRepeatedKeys(t *testing.T) {
	schema := compileSchema(t)
	zero := 0
	doc := func(top, pool string) []byte {
		return []byte(`{"schema_version": "2.0", "name": "r", "stages": [{"duration": "1m", "target": 2}], ` + top +
			`"scenarios": [{"name": "a", "steps": [{"name": "s", "request": {"method": "GET", "url": "https://a.example.com/"}}]},
			{"name": "p", "load": {"type": "stages", ` + pool + `"stages": [{"duration": "1m", "target": 2}]},
			"steps": [{"name": "s", "request": {"method": "GET", "url": "https://a.example.com/"}}]}]}`)
	}
	cases := []struct {
		name, keys string
		initial    *int
		stopGrace  string
		rampDown   string
		want       []string // at the test level; the pool's are the same under scenarios[1].load
	}{
		{name: "initial 3 then 0", keys: `"initial_virtual_users": 3, "initial_virtual_users": 0, `, initial: &zero},
		{name: "initial 0 then null", keys: `"initial_virtual_users": 0, "initial_virtual_users": null, `,
			want: []string{"initial_virtual_users: must not be null"}},
		{name: "initial null then 0", keys: `"initial_virtual_users": null, "initial_virtual_users": 0, `, initial: &zero},
		{name: "stop_grace then empty", keys: `"stop_grace": "5s", "stop_grace": "", `},
		{name: "bad ramp_down_grace then good", keys: `"ramp_down_grace": "-1s", "ramp_down_grace": "2s", `, rampDown: "2s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, level := range []string{"test", "pool"} {
				data, prefix := doc(tc.keys, ""), ""
				if level == "pool" {
					data, prefix = doc("", tc.keys), "scenarios[1].load."
				}
				var want []string
				for _, p := range tc.want {
					want = append(want, prefix+p)
				}
				if got := problemStrings(t, data); !reflect.DeepEqual(got, want) {
					t.Fatalf("%s: problems = %q, want %q", level, got, want)
				}
				if want != nil {
					continue
				}
				requireSchemaAccepts(t, schema, data)
				d := mustParse(t, data)
				initial, stopGrace, rampDown := d.InitialVirtualUsers, d.StopGrace, d.RampDownGrace
				if level == "pool" {
					l := d.Scenarios[1].Load
					initial, stopGrace, rampDown = l.InitialVirtualUsers, l.StopGrace, l.RampDownGrace
				}
				if !reflect.DeepEqual(initial, tc.initial) || stopGrace != tc.stopGrace || rampDown != tc.rampDown {
					t.Errorf("%s: initial %v, stop_grace %q, ramp_down_grace %q; want %v, %q, %q",
						level, initial, stopGrace, rampDown, tc.initial, tc.stopGrace, tc.rampDown)
				}
			}
		})
	}
}

// TestOldLoadKeyNames checks that the load keys preallocated_vus and
// max_vus, renamed to preallocated_virtual_users and max_virtual_users,
// are unknown in v2 at the test level and on a scenario's load.
func TestOldLoadKeyNames(t *testing.T) {
	data := []byte(`{"schema_version": "2.0", "name": "old",
		"load": {"type": "rate", "rate": 5, "duration": "1m", "preallocated_vus": 1},
		"scenarios": [{"name": "a", "steps": [{"name": "s", "request": {"method": "GET", "url": "https://a.example.com/"}}]},
			{"name": "p", "load": {"type": "rate", "rate": 5, "duration": "1m", "preallocated_virtual_users": 1, "max_vus": 2},
			"steps": [{"name": "s", "request": {"method": "GET", "url": "https://a.example.com/"}}]}]}`)
	_, err := definition.Parse(data)
	var ufe *definition.UnknownFieldError
	if !errors.As(err, &ufe) {
		t.Fatalf("error = %v, want *UnknownFieldError", err)
	}
	if want := []string{"load.preallocated_vus", "scenarios[1].load.max_vus"}; !reflect.DeepEqual(ufe.Paths, want) {
		t.Errorf("Paths = %q, want %q", ufe.Paths, want)
	}
}
