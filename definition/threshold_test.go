package definition_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/volt-test/contract/definition"
	"github.com/volt-test/contract/thresholds"
)

func TestThresholdJSON(t *testing.T) {
	decode := []struct {
		in   string
		want definition.ThresholdConfig
		err  bool
	}{
		{`"p95 < 500ms"`, definition.ThresholdConfig{Expr: "p95 < 500ms"}, false},
		{`{"expr": "p95 < 1s"}`, definition.ThresholdConfig{Expr: "p95 < 1s"}, false},
		{`{"expr": "p95 < 1s", "abort_on_fail": true, "delay_abort": "5s"}`, definition.ThresholdConfig{Expr: "p95 < 1s", AbortOnFail: true, DelayAbort: "5s"}, false},
		{`null`, definition.ThresholdConfig{Expr: "unchanged"}, false},
		{`5`, definition.ThresholdConfig{Expr: "unchanged"}, true},
		{`[]`, definition.ThresholdConfig{Expr: "unchanged"}, true},
		{`true`, definition.ThresholdConfig{Expr: "unchanged"}, true},
	}
	for _, tc := range decode {
		got := definition.ThresholdConfig{Expr: "unchanged"}
		err := json.Unmarshal([]byte(tc.in), &got)
		if (err != nil) != tc.err || got != tc.want {
			t.Errorf("Unmarshal(%s) = %+v, %v; want %+v, error %v", tc.in, got, err, tc.want, tc.err)
		}
	}

	encode := map[definition.ThresholdConfig]string{
		{Expr: "p95 < 500ms"}:                            `"p95 < 500ms"`,
		{Expr: "p95 < 1s", AbortOnFail: true}:            `{"expr":"p95 < 1s","abort_on_fail":true}`,
		{Expr: "p95 < 1s", DelayAbort: "5s"}:             `{"expr":"p95 < 1s","delay_abort":"5s"}`,
		{Expr: "a&b", AbortOnFail: true}:                 `{"expr":"a&b","abort_on_fail":true}`,
		{}:                                               `""`,
		{Expr: "p95 > 1s", AbortOnFail: false}:           `"p95 > 1s"`,
		{Expr: "x", AbortOnFail: true, DelayAbort: "1m"}: `{"expr":"x","abort_on_fail":true,"delay_abort":"1m"}`,
	}
	for th, want := range encode {
		b, err := th.MarshalJSON()
		if err != nil || string(b) != want {
			t.Errorf("MarshalJSON(%+v) = %s, %v; want %s", th, b, err, want)
		}
		var back definition.ThresholdConfig
		if err := json.Unmarshal(b, &back); err != nil || back != th {
			t.Errorf("round trip of %+v = %+v, %v", th, back, err)
		}
	}
	// Inside a document, encoding/json applies its usual escaping.
	b, err := json.Marshal([]definition.ThresholdConfig{{Expr: "p95 < 1s"}})
	if want := `["p95 ` + `\` + `u003c 1s"]`; err != nil || string(b) != want {
		t.Errorf("json.Marshal = %s, %v", b, err)
	}
}

func TestBounds(t *testing.T) {
	d, err := definition.Parse(readFile(t, "../testdata/v2/valid/thresholds.json"))
	if err != nil {
		t.Fatal(err)
	}
	bounds, err := definition.Bounds(d)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Ref    thresholds.Ref
		Metric string
		Op     thresholds.Op
		Value  float64
		Abort  bool
		Delay  time.Duration
	}
	var got []row
	for _, b := range bounds {
		got = append(got, row{b.Ref, b.Threshold.Metric.Name, b.Threshold.Op, b.Threshold.Value, b.AbortOnFail, b.DelayAbort})
	}
	test := thresholds.Ref{}
	browse := thresholds.Ref{Scenario: "Browse"}
	home := thresholds.Ref{Scenario: "Browse", Step: "Home"}
	search := thresholds.Ref{Scenario: "Browse", Step: "Search"}
	want := []row{
		{test, "p95", thresholds.OpLT, 500000, false, 0},
		{test, "error_rate", thresholds.OpLT, 0.01, false, 0},
		{test, "dropped_iterations", thresholds.OpEQ, 0, false, 0},
		{test, "rps", thresholds.OpGE, 50, false, 0},
		{test, "p99.9", thresholds.OpLE, 2e6, true, 30 * time.Second},
		{browse, "iteration_duration_p95", thresholds.OpLT, 5e6, false, 0},
		{browse, "iterations", thresholds.OpGT, 100, false, 0},
		{browse, "check_rate", thresholds.OpGE, 0.995, true, 0},
		{home, "ttfb_p99", thresholds.OpLT, 300000, false, 0},
		{home, "bytes_received", thresholds.OpLT, 2 << 20, false, 0},
		{search, "failed", thresholds.OpEQ, 0, true, 10 * time.Second},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Bounds:\n got %+v\nwant %+v", got, want)
	}
	if bounds[0].Threshold.Raw != "p95 < 500ms" || bounds[3].Threshold.Raw != "rps>=50" {
		t.Errorf("Raw = %q, %q", bounds[0].Threshold.Raw, bounds[3].Threshold.Raw)
	}
	for _, b := range bounds {
		if err := b.Threshold.ValidFor(b.Ref.Scope()); err != nil {
			t.Errorf("%+v: %v", b, err)
		}
	}
}

func TestBoundsNone(t *testing.T) {
	for _, f := range []string{"../testdata/v2/valid/minimal.json", "../testdata/v1/cli/thresholds_ignored.json"} {
		d, err := definition.Parse(readFile(t, f))
		if err != nil {
			t.Fatal(err)
		}
		bounds, err := definition.Bounds(d)
		if bounds != nil || err != nil {
			t.Errorf("%s: Bounds = %v, %v", f, bounds, err)
		}
	}
	if bounds, err := definition.Bounds(nil); bounds != nil || err != nil {
		t.Errorf("Bounds(nil) = %v, %v", bounds, err)
	}
}

// Bounds checks a definition built in Go the way Parse checks one read
// from JSON.
func TestBoundsChecksBuiltDefinitions(t *testing.T) {
	d := &definition.Definition{
		Thresholds: []definition.ThresholdConfig{{Expr: "p95 < 1s"}, {Expr: "p95 = 1s"}, {Expr: "p95 = 1s", AbortOnFail: true}, {AbortOnFail: true}},
		Scenarios: []definition.Scenario{{
			Name:  "s",
			Steps: []definition.Step{{Name: "a", Thresholds: []definition.ThresholdConfig{{Expr: "iterations > 1"}, {Expr: "p95 < 1s", DelayAbort: "1s"}}}},
		}},
	}
	_, err := definition.Bounds(d)
	var ve *definition.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %v", err)
	}
	var got []string
	for _, p := range ve.Problems {
		got = append(got, p.Path)
	}
	want := []string{"thresholds[1]", "thresholds[2].expr", "thresholds[3].expr", "scenarios[0].steps[0].thresholds[0]",
		"scenarios[0].steps[0].thresholds[1].delay_abort"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("problem paths = %q, want %q", got, want)
	}
}

// Bounds never returns a Ref whose Scope differs from where its threshold
// sits: a scenario or step that carries thresholds needs a name.
func TestBoundsRequiresNames(t *testing.T) {
	step := func(name string, th ...string) definition.Step {
		s := definition.Step{Name: name}
		for _, e := range th {
			s.Thresholds = append(s.Thresholds, definition.ThresholdConfig{Expr: e})
		}
		return s
	}
	d := &definition.Definition{Scenarios: []definition.Scenario{
		{Name: "", Thresholds: []definition.ThresholdConfig{{Expr: "p95 < 1s"}}, Steps: []definition.Step{step("a")}},
		{Name: " ", Steps: []definition.Step{step("a", "p95 < 1s")}},
		{Name: "named", Steps: []definition.Step{step("", "p95 < 1s"), step("\t"), step("ok", "p95 < 1s")}},
		{Name: "", Steps: []definition.Step{step("")}}, // no thresholds: not Bounds' concern
	}}
	_, err := definition.Bounds(d)
	var ve *definition.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %v", err)
	}
	var got []string
	for _, p := range ve.Problems {
		got = append(got, p.String())
	}
	want := []string{"scenarios[0].name: is required", "scenarios[1].name: is required", "scenarios[2].steps[0].name: is required"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("problems = %q, want %q", got, want)
	}

	// With names, every Ref's Scope matches the position.
	d.Scenarios = []definition.Scenario{{Name: "s", Thresholds: []definition.ThresholdConfig{{Expr: "p95 < 1s"}}, Steps: []definition.Step{step("a", "p95 < 2s")}}}
	d.Thresholds = []definition.ThresholdConfig{{Expr: "p95 < 3s"}}
	bounds, err := definition.Bounds(d)
	if err != nil {
		t.Fatal(err)
	}
	scopes := []thresholds.ScopeKind{thresholds.ScopeTest, thresholds.ScopeScenario, thresholds.ScopeStep}
	for i, b := range bounds {
		if b.Ref.Scope() != scopes[i] {
			t.Errorf("bound %d: Ref %+v has scope %s, want %s", i, b.Ref, b.Ref.Scope(), scopes[i])
		}
	}
}

// v1 readers ignore thresholds, so Parse drops them unread at every level,
// whatever their type.
func TestV1IgnoresThresholds(t *testing.T) {
	for _, doc := range []string{
		`{"name": "x", "thresholds": ["p95 < 1s"], "scenarios": [{"name": "s", "thresholds": ["iterations > 1"], "steps": [{"name": "a", "thresholds": [{"expr": "p95 < 1"}]}]}]}`,
		`{"name": "x", "thresholds": 5, "scenarios": [{"name": "s", "Thresholds": {"a": 1}, "steps": [{"name": "a", "THRESHOLDS": [null, 1, "x"]}]}]}`,
	} {
		d, err := definition.Parse([]byte(doc))
		if err != nil {
			t.Fatalf("%s: %v", doc, err)
		}
		if !d.IsV1() || d.Thresholds != nil || d.Scenarios[0].Thresholds != nil || d.Scenarios[0].Steps[0].Thresholds != nil {
			t.Errorf("%s: thresholds decoded in v1: %+v", doc, d)
		}
		if len(d.V1TypeProblems) != 0 {
			t.Errorf("%s: V1TypeProblems %v", doc, d.V1TypeProblems)
		}
	}
}
