package definition_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

const globalExtracted = `"admin_token" is a global set by the run_once scenario scenarios[%d]; no other scenario may extract it`

// setupTeardownFixtureProblems maps each invalid fixture for run_once and
// teardown scenarios to the problems Parse reports, in order.
var setupTeardownFixtureProblems = map[string][]string{
	"structure_run_once_and_teardown.json": {`scenarios[0].teardown: cannot be combined with run_once`},
	"structure_run_once_weight.json": {
		`scenarios[0].weight: does not apply to a run_once scenario, which is never picked for the load`,
	},
	"structure_teardown_weight.json": {
		`scenarios[1].weight: does not apply to a teardown scenario, which is never picked for the load`,
	},
	"structure_run_once_no_load.json": {`scenarios: must have at least one load scenario, one without run_once or teardown`},
	"structure_run_once_global_extracted.json": {
		`scenarios[0].steps[0].extract[0].variable_name: ` + strings.Replace(globalExtracted, "%d", "1", 1),
	},
	"structure_run_once_global_by_run_once.json": {
		`scenarios[1].steps[0].extract[0].variable_name: ` + strings.Replace(globalExtracted, "%d", "0", 1),
	},
	"type_run_once_string.json": {`scenarios[0].run_once: must be true or false`},
	"type_teardown_number.json": {`scenarios[1].teardown: must be true or false`},
}

// setupTeardownFixturePrefixes are the fixture name prefixes this file owns.
var setupTeardownFixturePrefixes = []string{"structure_run_once_", "structure_teardown_", "type_run_once_", "type_teardown_"}

// TestSetupTeardownFixtures checks that each invalid run_once or teardown
// fixture fails with exactly the problems listed for it, and that every
// such fixture is listed.
func TestSetupTeardownFixtures(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures(t, "v2/invalid") {
		name := filepath.Base(f)
		want, ok := setupTeardownFixtureProblems[name]
		if !ok {
			for _, p := range setupTeardownFixturePrefixes {
				if strings.HasPrefix(name, p) {
					t.Errorf("%s: no expected problem in setupTeardownFixtureProblems", name)
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
	for name := range setupTeardownFixtureProblems {
		if !seen[name] {
			t.Errorf("missing fixture testdata/v2/invalid/%s", name)
		}
	}
}

// TestSetupTeardownValidFixture checks the flags and the load scenarios
// Parse reads from setup_teardown.json.
func TestSetupTeardownValidFixture(t *testing.T) {
	d := mustParse(t, readFile(t, "../testdata/v2/valid/setup_teardown.json"))
	type flags struct {
		name              string
		runOnce, teardown bool
		load              bool
	}
	want := []flags{
		{"Log in", true, false, false},
		{"Seed", true, false, false},
		{"Browse", false, false, true},
		{"Browse again", false, false, true},
		{"Clean up", false, true, false},
	}
	var got []flags
	for _, sc := range d.Scenarios {
		got = append(got, flags{sc.Name, sc.RunOnce, sc.Teardown, sc.IsLoad()})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scenarios:\n got %+v\nwant %+v", got, want)
	}
}

// sc builds a one-step scenario with the given keys and extracted
// variables.
func sc(name string, keys map[string]any, extract ...string) map[string]any {
	s := validScenario(name)
	for k, v := range keys {
		s[k] = v
	}
	if len(extract) > 0 {
		var exs []any
		for _, v := range extract {
			exs = append(exs, map[string]any{"variable_name": v, "selector": "$." + v, "type": "json"})
		}
		s["steps"].([]any)[0].(map[string]any)["extract"] = exs
	}
	return s
}

func TestSetupTeardownRules(t *testing.T) {
	schema := compileSchema(t)
	setup := map[string]any{"run_once": true}
	teardown := map[string]any{"teardown": true}
	load := map[string]any(nil)
	global := func(owner string) string {
		return strings.Replace(globalExtracted, "%d", owner, 1)
	}
	cases := []struct {
		name      string
		scenarios []any
		want      []string
		// schemaToo: the schema rejects it as well. The global rule is
		// Parse's alone (knownSchemaLooser).
		schemaToo bool
	}{
		{name: "setup and load", scenarios: []any{sc("s", setup, "admin_token"), sc("l", load)}},
		{name: "load and teardown", scenarios: []any{sc("l", load), sc("t", teardown)}},
		{name: "teardown first", scenarios: []any{sc("t", teardown), sc("l", load), sc("s", setup)}},
		{name: "false reads as absent", scenarios: []any{
			sc("l", map[string]any{"run_once": false, "teardown": false, "weight": 4}),
			sc("s", map[string]any{"run_once": true, "teardown": false}),
			sc("t", map[string]any{"teardown": true, "run_once": false}),
		}},
		{name: "zero weight on setup and teardown", scenarios: []any{
			sc("s", map[string]any{"run_once": true, "weight": 0}),
			sc("t", map[string]any{"teardown": true, "weight": 0}),
			sc("l", load),
		}},
		{name: "several setups and teardowns", scenarios: []any{
			sc("s1", setup, "admin_token"), sc("s2", setup, "product_id"), sc("l1", load), sc("l2", load),
			sc("t1", teardown), sc("t2", teardown),
		}},
		{name: "one setup extracts a global twice", scenarios: []any{
			func() map[string]any {
				s := sc("s", setup, "admin_token")
				steps := s["steps"].([]any)
				second := map[string]any{
					"name": "b", "request": map[string]any{"method": "GET", "url": "https://example.com/"},
					"extract": []any{map[string]any{"variable_name": "admin_token", "selector": "$.t", "type": "json"}},
				}
				s["steps"] = append(steps, second)
				return s
			}(),
			sc("l", load),
		}},
		{name: "load scenarios share a variable", scenarios: []any{
			sc("s", setup, "admin_token"), sc("l1", load, "cart_id"), sc("l2", load, "cart_id"),
		}},
		{name: "teardown extracts its own variable", scenarios: []any{
			sc("s", setup, "admin_token"), sc("l", load), sc("t", teardown, "deleted"),
		}},

		{name: "both flags", scenarios: []any{sc("s", map[string]any{"run_once": true, "teardown": true}), sc("l", load)},
			want: []string{"scenarios[0].teardown: cannot be combined with run_once"}, schemaToo: true},
		{name: "both flags and a weight",
			scenarios: []any{sc("l", load), sc("s", map[string]any{"run_once": true, "teardown": true, "weight": 2})},
			want: []string{
				"scenarios[1].teardown: cannot be combined with run_once",
				"scenarios[1].weight: does not apply to a run_once scenario, which is never picked for the load",
			}, schemaToo: true},
		{name: "weight on setup", scenarios: []any{sc("s", map[string]any{"run_once": true, "weight": 1}), sc("l", load)},
			want:      []string{"scenarios[0].weight: does not apply to a run_once scenario, which is never picked for the load"},
			schemaToo: true},
		{name: "weight on teardown", scenarios: []any{sc("l", load), sc("t", map[string]any{"teardown": true, "weight": 5})},
			want:      []string{"scenarios[1].weight: does not apply to a teardown scenario, which is never picked for the load"},
			schemaToo: true},
		{name: "negative weight on setup", scenarios: []any{sc("s", map[string]any{"run_once": true, "weight": -1}), sc("l", load)},
			want: []string{"scenarios[0].weight: must not be negative"}, schemaToo: true},
		{name: "only a setup", scenarios: []any{sc("s", setup)},
			want: []string{"scenarios: must have at least one load scenario, one without run_once or teardown"}, schemaToo: true},
		{name: "only a teardown", scenarios: []any{sc("t", teardown)},
			want: []string{"scenarios: must have at least one load scenario, one without run_once or teardown"}, schemaToo: true},
		{name: "setups and teardowns only", scenarios: []any{sc("s", setup), sc("t1", teardown), sc("t2", teardown)},
			want: []string{"scenarios: must have at least one load scenario, one without run_once or teardown"}, schemaToo: true},
		{name: "no scenarios", scenarios: []any{},
			want: []string{"scenarios: must have at least one scenario"}, schemaToo: true},

		{name: "load extracts a global, defined first",
			scenarios: []any{sc("l", load, "admin_token"), sc("s", setup, "admin_token")},
			want:      []string{"scenarios[0].steps[0].extract[0].variable_name: " + global("1")}},
		{name: "teardown extracts a global",
			scenarios: []any{sc("s", setup, "admin_token"), sc("l", load), sc("t", teardown, "admin_token")},
			want:      []string{"scenarios[2].steps[0].extract[0].variable_name: " + global("0")}},
		{name: "a later setup extracts a global",
			scenarios: []any{sc("l", load), sc("s1", setup, "admin_token"), sc("s2", setup, "admin_token")},
			want:      []string{"scenarios[2].steps[0].extract[0].variable_name: " + global("1")}},
		{name: "every other extractor is reported",
			scenarios: []any{sc("s", setup, "admin_token"), sc("l1", load, "admin_token"), sc("l2", load, "admin_token")},
			want: []string{
				"scenarios[1].steps[0].extract[0].variable_name: " + global("0"),
				"scenarios[2].steps[0].extract[0].variable_name: " + global("0"),
			}},
		{name: "names compare exactly",
			scenarios: []any{sc("s", setup, "admin_token"), sc("l", load, "Admin_Token")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := v2doc(t, func(m map[string]any) { m["scenarios"] = tc.scenarios })
			if got := problemStrings(t, doc); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("problems:\n got %q\nwant %q", got, tc.want)
			}
			schemaErr := validateAgainstSchema(t, schema, doc)
			switch {
			case tc.want == nil && schemaErr != nil:
				t.Errorf("schema rejects a definition Parse accepts: %v", schemaErr)
			case tc.schemaToo && schemaErr == nil:
				t.Error("schema accepts a definition Parse rejects")
			}
		})
	}
}

// TestSetupTeardownMarshal checks that both flags survive a marshal and
// re-parse, and that false is left out.
func TestSetupTeardownMarshal(t *testing.T) {
	d := mustParse(t, readFile(t, "../testdata/v2/valid/setup_teardown.json"))
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(out, []byte(`"run_once":true`)); n != 2 {
		t.Errorf("Marshal wrote run_once %d times, want 2: %s", n, out)
	}
	if n := bytes.Count(out, []byte(`"teardown":true`)); n != 1 {
		t.Errorf("Marshal wrote teardown %d times, want 1: %s", n, out)
	}
	if bytes.Contains(out, []byte(`"run_once":false`)) || bytes.Contains(out, []byte(`"teardown":false`)) {
		t.Errorf("Marshal kept a false flag: %s", out)
	}
	again := mustParse(t, out)
	if !reflect.DeepEqual(again.Scenarios, d.Scenarios) {
		t.Errorf("re-parse changed the scenarios:\n got %+v\nwant %+v", again.Scenarios, d.Scenarios)
	}
}

func TestScenarioIsLoad(t *testing.T) {
	for _, tc := range []struct {
		s    definition.Scenario
		want bool
	}{
		{definition.Scenario{}, true},
		{definition.Scenario{RunOnce: true}, false},
		{definition.Scenario{Teardown: true}, false},
		{definition.Scenario{RunOnce: true, Teardown: true}, false},
	} {
		if got := tc.s.IsLoad(); got != tc.want {
			t.Errorf("%+v.IsLoad() = %v, want %v", tc.s, got, tc.want)
		}
	}
}
