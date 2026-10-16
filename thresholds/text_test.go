package thresholds_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/volt-test/contract/thresholds"
)

// The enums encode as their names, so an encoded Bound is readable.
func TestEnumText(t *testing.T) {
	type enums struct {
		Kinds  []thresholds.Kind
		Ops    []thresholds.Op
		Scopes []thresholds.ScopeKind
	}
	in := enums{
		Kinds:  []thresholds.Kind{thresholds.KindDuration, thresholds.KindRate, thresholds.KindCount, thresholds.KindSize, thresholds.KindThroughput},
		Ops:    []thresholds.Op{thresholds.OpLT, thresholds.OpLE, thresholds.OpGT, thresholds.OpGE, thresholds.OpEQ},
		Scopes: []thresholds.ScopeKind{thresholds.ScopeTest, thresholds.ScopeScenario, thresholds.ScopeStep},
	}
	enc := func(v any) string {
		var b strings.Builder
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		if err := e.Encode(v); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(b.String())
	}
	want := `{"Kinds":["duration","rate","count","size","throughput"],"Ops":["<","<=",">",">=","=="],"Scopes":["test","scenario","step"]}`
	if got := enc(in); got != want {
		t.Errorf("encoded %s\nwant    %s", got, want)
	}
	var back enums
	if err := json.Unmarshal([]byte(want), &back); err != nil {
		t.Fatal(err)
	}
	if enc(back) != want {
		t.Errorf("decoded %+v", back)
	}

	// Values outside the constants do not encode; unknown names do not decode
	// and leave the value unchanged.
	for _, v := range []any{thresholds.Kind(0), thresholds.Op(9), thresholds.ScopeKind(-1)} {
		if _, err := json.Marshal(v); err == nil {
			t.Errorf("%v encoded", v)
		}
	}
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

func TestBoundJSON(t *testing.T) {
	th, err := thresholds.Parse("iterations>=10")
	if err != nil {
		t.Fatal(err)
	}
	b := thresholds.Bound{Ref: thresholds.Ref{Scenario: "s"}, Threshold: th, AbortOnFail: true, DelayAbort: 30 * time.Second}
	var buf strings.Builder
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	if err := e.Encode(b); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(buf.String())
	want := `{"Ref":{"Scenario":"s","Step":""},"Threshold":{"Raw":"iterations>=10","Metric":{"Name":"iterations","Kind":"count"},"Op":">=","Value":10},"AbortOnFail":true,"DelayAbort":30000000000}`
	if got != want {
		t.Errorf("json.Marshal(Bound) = %s\nwant                  %s", got, want)
	}
}
