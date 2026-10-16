package thresholds_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/volt-test/contract/thresholds"
)

// encode writes v as JSON without HTML escaping, so expectations read
// naturally.
func encode(t *testing.T, v any) string {
	t.Helper()
	var b strings.Builder
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(b.String())
}

// The enums encode as their names, so an encoded Bound is readable.
func TestEnumText(t *testing.T) {
	type enums struct {
		Kinds  []thresholds.Kind
		Ops    []thresholds.Op
		Scopes []thresholds.ScopeKind
	}
	in := enums{
		Kinds:  []thresholds.Kind{0, thresholds.KindDuration, thresholds.KindRate, thresholds.KindCount, thresholds.KindSize, thresholds.KindThroughput},
		Ops:    []thresholds.Op{0, thresholds.OpLT, thresholds.OpLE, thresholds.OpGT, thresholds.OpGE, thresholds.OpEQ},
		Scopes: []thresholds.ScopeKind{0, thresholds.ScopeTest, thresholds.ScopeScenario, thresholds.ScopeStep},
	}
	want := `{"Kinds":["","duration","rate","count","size","throughput"],"Ops":["","<","<=",">",">=","=="],"Scopes":["","test","scenario","step"]}`
	if got := encode(t, in); got != want {
		t.Errorf("encoded %s\nwant    %s", got, want)
	}
	var back enums
	if err := json.Unmarshal([]byte(want), &back); err != nil {
		t.Fatal(err)
	}
	if encode(t, back) != want {
		t.Errorf("decoded %+v", back)
	}

	// Values outside the constants, other than zero, do not encode.
	for _, v := range []any{thresholds.Kind(6), thresholds.Op(9), thresholds.ScopeKind(-1)} {
		if _, err := json.Marshal(v); err == nil {
			t.Errorf("%v encoded", v)
		}
	}
	// Unknown names do not decode and leave the value unchanged.
	op := thresholds.OpLT
	if err := json.Unmarshal([]byte(`"="`), &op); err == nil || op != thresholds.OpLT {
		t.Errorf("Unmarshal(\"=\") = %v, op %v", err, op)
	}
	k := thresholds.KindRate
	if err := json.Unmarshal([]byte(`"Duration"`), &k); err == nil || k != thresholds.KindRate {
		t.Errorf("Unmarshal(\"Duration\") = %v, kind %v", err, k)
	}
	s := thresholds.ScopeStep
	if err := json.Unmarshal([]byte(`"suite"`), &s); err == nil || s != thresholds.ScopeStep {
		t.Errorf("Unmarshal(\"suite\") = %v, scope %v", err, s)
	}
}

// A Metric encodes as its name and decodes to the catalog entry.
func TestMetricText(t *testing.T) {
	for _, name := range thresholds.Metrics() {
		th, err := thresholds.Parse(name + " > " + map[thresholds.Kind]string{
			thresholds.KindDuration: "1s", thresholds.KindRate: "1%", thresholds.KindCount: "1",
			thresholds.KindSize: "1", thresholds.KindThroughput: "1",
		}[kindOf(name)])
		if err != nil {
			t.Fatal(err)
		}
		if got := encode(t, th.Metric); got != `"`+name+`"` {
			t.Errorf("%s encodes as %s", name, got)
		}
		var m thresholds.Metric
		if err := json.Unmarshal([]byte(`"`+name+`"`), &m); err != nil || m != th.Metric {
			t.Errorf("%s decodes to %+v, %v", name, m, err)
		}
	}

	if got := encode(t, thresholds.Metric{}); got != `""` {
		t.Errorf("zero Metric encodes as %s", got)
	}
	m := thresholds.Metric{Name: "x"}
	if err := json.Unmarshal([]byte(`""`), &m); err != nil || m != (thresholds.Metric{}) {
		t.Errorf(`"" decodes to %+v, %v`, m, err)
	}
	p95, _ := thresholds.Parse("p95 < 1s")
	m = p95.Metric
	if err := json.Unmarshal([]byte(`"p98"`), &m); err == nil || m != p95.Metric {
		t.Errorf(`"p98" decodes: %v, metric %+v`, err, m)
	}
	// A Metric built by hand is not a catalog entry.
	forged := thresholds.Metric{Name: "iterations", Kind: thresholds.KindCount}
	if _, err := json.Marshal(forged); err == nil || !strings.Contains(err.Error(), "build it with Parse") {
		t.Errorf("hand-built Metric encodes: %v", err)
	}
}

func TestBoundJSON(t *testing.T) {
	th, err := thresholds.Parse("iterations>=10")
	if err != nil {
		t.Fatal(err)
	}
	b := thresholds.Bound{Ref: thresholds.Ref{Scenario: "s"}, Threshold: th, AbortOnFail: true, DelayAbort: 30 * time.Second}
	want := `{"Ref":{"Scenario":"s","Step":""},"Threshold":{"Raw":"iterations>=10","Metric":"iterations","Op":">=","Value":10},"AbortOnFail":true,"DelayAbort":30000000000}`
	if got := encode(t, b); got != want {
		t.Errorf("Bound encodes as %s\nwant             %s", got, want)
	}

	// A Bound decoded from JSON equals the original and passes ValidFor.
	for _, c := range []struct {
		expr string
		ref  thresholds.Ref
	}{
		{"p99.9 <= 1.5s", thresholds.Ref{}},
		{"error_rate < 0.07%", thresholds.Ref{Scenario: "s"}},
		{"iteration_duration_p95 < 2m", thresholds.Ref{Scenario: "s"}},
		{"ttfb_p99 < 300ms", thresholds.Ref{Scenario: "s", Step: "a"}},
		{"bytes_received < 2gib", thresholds.Ref{Scenario: "s", Step: "a"}},
		{"rps > 0.1", thresholds.Ref{Scenario: "s", Step: "a"}},
	} {
		th, err := thresholds.Parse(c.expr)
		if err != nil {
			t.Fatal(err)
		}
		orig := thresholds.Bound{Ref: c.ref, Threshold: th, AbortOnFail: true, DelayAbort: time.Second}
		data, err := json.Marshal(orig)
		if err != nil {
			t.Fatal(err)
		}
		var back thresholds.Bound
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("%s: %v", data, err)
		}
		if back != orig {
			t.Errorf("round trip of %q:\n got %+v\nwant %+v", c.expr, back, orig)
		}
		if err := back.Threshold.ValidFor(back.Ref.Scope()); err != nil {
			t.Errorf("decoded %q: %v", c.expr, err)
		}
	}

	// An unset Bound and Threshold encode and decode as zero.
	for _, v := range []any{thresholds.Bound{}, thresholds.Threshold{}} {
		data, err := json.Marshal(v)
		if err != nil {
			t.Errorf("json.Marshal(%T{}): %v", v, err)
		}
		switch v.(type) {
		case thresholds.Bound:
			var back thresholds.Bound
			if err := json.Unmarshal(data, &back); err != nil || back != (thresholds.Bound{}) {
				t.Errorf("zero Bound: %s -> %+v, %v", data, back, err)
			}
		case thresholds.Threshold:
			var back thresholds.Threshold
			if err := json.Unmarshal(data, &back); err != nil || back != (thresholds.Threshold{}) {
				t.Errorf("zero Threshold: %s -> %+v, %v", data, back, err)
			}
		}
	}
	// A reader that does not know a metric cannot decode the bound.
	var back thresholds.Bound
	if err := json.Unmarshal([]byte(`{"Threshold":{"Metric":"p42"}}`), &back); err == nil {
		t.Error("unknown metric decoded")
	}
}
