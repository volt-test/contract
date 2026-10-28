package definition_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	onceInRunOnce  = "does not apply to a run_once scenario, which runs once"
	onceInTeardown = "does not apply to a teardown scenario, which runs once"
	onceEveryStep  = "must have at least one step without once_per_vu"
)

// oncePerVUFixtureProblems maps each invalid fixture for once_per_vu to the
// problems Parse reports, in order.
var oncePerVUFixtureProblems = map[string][]string{
	"structure_once_per_vu_run_once.json":  {`scenarios[0].steps[0].once_per_vu: ` + onceInRunOnce},
	"structure_once_per_vu_teardown.json":  {`scenarios[1].steps[1].once_per_vu: ` + onceInTeardown},
	"structure_once_per_vu_all_steps.json": {`scenarios[1].steps: ` + onceEveryStep},
	"type_once_per_vu_string.json":         {`scenarios[0].steps[0].once_per_vu: must be true or false`},
}

// oncePerVUFixturePrefixes are the fixture name prefixes this file owns.
var oncePerVUFixturePrefixes = []string{"structure_once_per_vu_", "type_once_per_vu_"}

// TestOncePerVUFixtures checks that each invalid once_per_vu fixture fails
// with exactly the problems listed for it, and that every such fixture is
// listed.
func TestOncePerVUFixtures(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures(t, "v2/invalid") {
		name := filepath.Base(f)
		want, ok := oncePerVUFixtureProblems[name]
		if !ok {
			for _, p := range oncePerVUFixturePrefixes {
				if strings.HasPrefix(name, p) {
					t.Errorf("%s: no expected problem in oncePerVUFixtureProblems", name)
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
	for name := range oncePerVUFixtureProblems {
		if !seen[name] {
			t.Errorf("missing fixture testdata/v2/invalid/%s", name)
		}
	}
}

// TestOncePerVUValidFixture checks the flags Parse reads from
// once_per_vu.json, false included.
func TestOncePerVUValidFixture(t *testing.T) {
	d := mustParse(t, readFile(t, "../testdata/v2/valid/once_per_vu.json"))
	want := map[string][]bool{
		"Seed":  {false},
		"Shop":  {true, false},
		"Admin": {true, false},
	}
	got := map[string][]bool{}
	for _, sc := range d.Scenarios {
		for _, st := range sc.Steps {
			got[sc.Name] = append(got[sc.Name], st.OncePerVU)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("once_per_vu per step:\n got %v\nwant %v", got, want)
	}
}

// onceSteps builds a scenario with one step per flag: true sets
// once_per_vu, false writes it as false, and nil leaves it out.
func onceSteps(name string, keys map[string]any, flags ...any) map[string]any {
	s := map[string]any{"name": name}
	for k, v := range keys {
		s[k] = v
	}
	steps := []any{}
	for i, f := range flags {
		st := map[string]any{
			"name":    "s" + string(rune('a'+i)),
			"request": map[string]any{"method": "GET", "url": "https://example.com/"},
		}
		if f != nil {
			st["once_per_vu"] = f
		}
		steps = append(steps, st)
	}
	s["steps"] = steps
	return s
}

func TestOncePerVURules(t *testing.T) {
	schema := compileSchema(t)
	setup := map[string]any{"run_once": true}
	teardown := map[string]any{"teardown": true}
	both := map[string]any{"run_once": true, "teardown": true}
	load := map[string]any(nil)
	cases := []struct {
		name      string
		scenarios []any
		want      []string
	}{
		{name: "once step then a plain step", scenarios: []any{onceSteps("l", load, true, nil)}},
		{name: "plain step then a once step", scenarios: []any{onceSteps("l", load, nil, true)}},
		{name: "several once steps and one plain", scenarios: []any{onceSteps("l", load, true, true, false)}},
		{name: "false on every step", scenarios: []any{onceSteps("l", load, false, false)}},
		{name: "false on a load scenario's only step", scenarios: []any{onceSteps("l", load, false)}},
		{name: "false in run_once and teardown", scenarios: []any{
			onceSteps("s", setup, false), onceSteps("l", load, true, nil), onceSteps("t", teardown, false, nil),
		}},
		{name: "run_once and teardown false read as load", scenarios: []any{
			onceSteps("l", map[string]any{"run_once": false, "teardown": false}, true, nil),
		}},
		{name: "two weighted load scenarios", scenarios: []any{
			onceSteps("a", map[string]any{"weight": 2}, true, nil),
			onceSteps("b", map[string]any{"weight": 1}, true, nil),
		}},

		{name: "in a run_once scenario", scenarios: []any{onceSteps("s", setup, nil, true), onceSteps("l", load, nil)},
			want: []string{"scenarios[0].steps[1].once_per_vu: " + onceInRunOnce}},
		{name: "in a teardown scenario", scenarios: []any{onceSteps("l", load, nil), onceSteps("t", teardown, true)},
			want: []string{"scenarios[1].steps[0].once_per_vu: " + onceInTeardown}},
		{name: "every step of a run_once scenario", scenarios: []any{onceSteps("s", setup, true, true), onceSteps("l", load, nil)},
			want: []string{
				"scenarios[0].steps[0].once_per_vu: " + onceInRunOnce,
				"scenarios[0].steps[1].once_per_vu: " + onceInRunOnce,
			}},
		{name: "both flags: the run_once wording", scenarios: []any{onceSteps("x", both, true), onceSteps("l", load, nil)},
			want: []string{
				"scenarios[0].teardown: cannot be combined with run_once",
				"scenarios[0].steps[0].once_per_vu: " + onceInRunOnce,
			}},
		{name: "a load scenario's only step", scenarios: []any{onceSteps("l", load, true)},
			want: []string{"scenarios[0].steps: " + onceEveryStep}},
		{name: "every step of a load scenario", scenarios: []any{onceSteps("a", load, nil), onceSteps("b", load, true, true)},
			want: []string{"scenarios[1].steps: " + onceEveryStep}},
		{name: "every step, with run_once and teardown false", scenarios: []any{
			onceSteps("l", map[string]any{"run_once": false, "teardown": false}, true),
		}, want: []string{"scenarios[0].steps: " + onceEveryStep}},
		{name: "no steps: reported once", scenarios: []any{onceSteps("l", load)},
			want: []string{"scenarios[0].steps: must have at least one step"}},
		{name: "string", scenarios: []any{onceSteps("l", load, "true", nil)},
			want: []string{"scenarios[0].steps[0].once_per_vu: must be true or false"}},
		{name: "number", scenarios: []any{onceSteps("l", load, 1, nil)},
			want: []string{"scenarios[0].steps[0].once_per_vu: must be true or false"}},
		{name: "null", scenarios: []any{onceSteps("l", load, json.RawMessage("null"), nil)},
			want: []string{"scenarios[0].steps[0].once_per_vu: must not be null"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := v2doc(t, func(m map[string]any) { m["scenarios"] = tc.scenarios })
			if got := problemStrings(t, doc); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("problems:\n got %q\nwant %q", got, tc.want)
			}
			// The schema states both rules, so it agrees with Parse on
			// every case.
			schemaErr := validateAgainstSchema(t, schema, doc)
			switch {
			case tc.want == nil && schemaErr != nil:
				t.Errorf("schema rejects a definition Parse accepts: %v", schemaErr)
			case tc.want != nil && schemaErr == nil:
				t.Error("schema accepts a definition Parse rejects")
			}
		})
	}
}

// TestOncePerVUMarshal checks that once_per_vu survives a marshal and
// re-parse, and that false is left out.
func TestOncePerVUMarshal(t *testing.T) {
	d := mustParse(t, readFile(t, "../testdata/v2/valid/once_per_vu.json"))
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(out, []byte(`"once_per_vu":true`)); n != 2 {
		t.Errorf("Marshal wrote once_per_vu %d times, want 2: %s", n, out)
	}
	if bytes.Contains(out, []byte(`"once_per_vu":false`)) {
		t.Errorf("Marshal kept once_per_vu: false: %s", out)
	}
	again := mustParse(t, out)
	if !reflect.DeepEqual(again.Scenarios, d.Scenarios) {
		t.Errorf("re-parse changed the scenarios:\n got %+v\nwant %+v", again.Scenarios, d.Scenarios)
	}
}
