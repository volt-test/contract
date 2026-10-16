package definition

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/volt-test/contract/thresholds"
)

// Threshold is a pass/fail rule for a test, a scenario or a step. In JSON
// it is either the expression alone or an object with abort settings:
//
//	"p95 < 500ms"
//	{"expr": "error_rate < 1%", "abort_on_fail": true, "delay_abort": "30s"}
//
// The expression grammar, metrics and units are those of the thresholds
// package. Parse checks each expression in v2 and ignores thresholds in
// v1. A Threshold without abort settings marshals as the plain string.
type Threshold struct {
	// Threshold expression: a metric, an operator and a value, e.g.
	// "p95 < 500ms" or "error_rate < 1%".
	Expr string `json:"expr" jsonschema:"required,minLength=1"`
	// Stop the test as soon as the threshold fails. Accepted by the format;
	// whether an engine acts on it depends on the engine release.
	AbortOnFail bool `json:"abort_on_fail,omitempty"`
	// Grace period from the start of the test during which a failing
	// threshold does not abort it, e.g. "30s". Requires abort_on_fail.
	DelayAbort string `json:"delay_abort,omitempty"`
}

// thresholdObject is the object form of a Threshold: the same fields
// without the methods, so it decodes and encodes as a plain object.
type thresholdObject Threshold

// objectForm lets the walkers check the keys of the object form.
func (t *Threshold) objectForm() any { return &thresholdObject{} }

// UnmarshalJSON reads a threshold string or object. null leaves t
// unchanged, as encoding/json does for other types; Parse rejects null in
// v2 before decoding.
func (t *Threshold) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case string(b) == "null":
		return nil
	case len(b) > 0 && b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*t = Threshold{Expr: s}
		return nil
	case len(b) > 0 && b[0] == '{':
		var o thresholdObject
		if err := json.Unmarshal(b, &o); err != nil {
			return err
		}
		*t = Threshold(o)
		return nil
	}
	return errors.New("definition: must be a string or an object")
}

// MarshalJSON writes the plain expression string when AbortOnFail and
// DelayAbort are zero, and the object form otherwise. Characters such as
// "<" are written as they are; encoding/json escapes them again when it
// is asked to.
func (t Threshold) MarshalJSON() ([]byte, error) {
	var v any = thresholdObject(t)
	if !t.AbortOnFail && t.DelayAbort == "" {
		v = t.Expr
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Bounds returns the thresholds of d with the part of the test each one
// applies to: the test's own thresholds first, then each scenario's
// followed by the thresholds of its steps, in document order. A Ref names
// scenarios and steps by name, which v2 makes unique.
//
// Bounds checks each threshold as Parse does (the expression, its scope
// and delay_abort) and returns a *ValidationError when one is invalid, so
// a definition from Parse never fails here. It does not check the rest of
// the definition. A v1 definition from Parse has no thresholds: v1 readers
// ignore them, and so does Parse.
func Bounds(d *Definition) ([]thresholds.Bound, error) {
	if d == nil {
		return nil, nil
	}
	var ps problems
	out := collectBounds(d, &ps)
	if err := ps.err(); err != nil {
		return nil, err
	}
	return out, nil
}

// collectBounds checks every threshold of d, records the problems in ps
// and returns the bounds of the valid ones.
func collectBounds(d *Definition, ps *problems) []thresholds.Bound {
	var out []thresholds.Bound
	// The scope comes from where a threshold sits, not from the names in
	// its Ref, so a blank name does not hide a scope problem.
	add := func(path string, list []Threshold, scope thresholds.ScopeKind, ref thresholds.Ref) {
		for i, th := range list {
			if b, ok := checkThreshold(ps, indexPath(path, i), th, scope, ref); ok {
				out = append(out, b)
			}
		}
	}
	add("thresholds", d.Thresholds, thresholds.ScopeTest, thresholds.Ref{})
	for i, sc := range d.Scenarios {
		p := "scenarios[" + strconv.Itoa(i) + "]"
		add(p+".thresholds", sc.Thresholds, thresholds.ScopeScenario, thresholds.Ref{Scenario: sc.Name})
		for j, st := range sc.Steps {
			add(p+".steps["+strconv.Itoa(j)+"].thresholds", st.Thresholds, thresholds.ScopeStep,
				thresholds.Ref{Scenario: sc.Name, Step: st.Name})
		}
	}
	return out
}

// checkThreshold checks one threshold at path. Problems with the
// expression are reported at path itself, whichever form the threshold
// was written in; problems with delay_abort at path.delay_abort.
func checkThreshold(ps *problems, path string, th Threshold, scope thresholds.ScopeKind, ref thresholds.Ref) (thresholds.Bound, bool) {
	ok := true
	parsed, err := thresholds.Parse(th.Expr)
	if err == nil {
		err = parsed.ValidFor(scope)
	}
	if err != nil {
		ps.add(path, strings.TrimPrefix(err.Error(), "thresholds: "))
		ok = false
	}
	var delay time.Duration
	if th.DelayAbort != "" {
		d, err := time.ParseDuration(th.DelayAbort)
		switch {
		case err != nil:
			ps.add(path+".delay_abort", durationMessage(th.DelayAbort))
			ok = false
		case d < 0:
			ps.add(path+".delay_abort", "must not be negative")
			ok = false
		case !th.AbortOnFail:
			ps.add(path+".delay_abort", "only applies with abort_on_fail: true")
			ok = false
		}
		delay = d
	}
	return thresholds.Bound{Ref: ref, Threshold: parsed, AbortOnFail: th.AbortOnFail, DelayAbort: delay}, ok
}
