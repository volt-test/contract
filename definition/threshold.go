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

// ThresholdConfig is a threshold as a definition carries it: a pass/fail
// rule for the test, a scenario or a step. In JSON it is either the
// expression alone or an object with abort settings:
//
//	"p95 < 500ms"
//	{"expr": "error_rate < 1%", "abort_on_fail": true, "delay_abort": "30s"}
//
// The expression grammar, metrics and units are those of the thresholds
// package; Bounds turns a definition's ThresholdConfigs into
// thresholds.Bound values. Parse checks each expression in v2 and ignores
// thresholds in v1. A ThresholdConfig without abort settings marshals as
// the plain string.
type ThresholdConfig struct {
	// Threshold expression: a metric, an operator and a value, e.g.
	// "p95 < 500ms" or "error_rate < 1%".
	Expr string `json:"expr" jsonschema:"required,minLength=1"`
	// Stop the test when the threshold fails. Accepted by the format;
	// whether an engine acts on it depends on the engine release.
	AbortOnFail bool `json:"abort_on_fail,omitempty"`
	// Defers the abort: abort checks for this threshold start this long
	// after the run starts, e.g. "30s" (Go duration syntax, like the other
	// durations). The threshold is still evaluated during the delay and
	// still decides the final verdict; only the abort is deferred. The
	// engine defines the exact run-start clock. Requires abort_on_fail.
	DelayAbort string `json:"delay_abort,omitempty"`
}

// thresholdConfigObject is the object form of a ThresholdConfig: the same
// fields without the methods, so it decodes and encodes as a plain object.
type thresholdConfigObject ThresholdConfig

// objectForm lets the walkers check the keys of the object form.
func (t *ThresholdConfig) objectForm() any { return &thresholdConfigObject{} }

// UnmarshalJSON reads a threshold string or object. null leaves t
// unchanged, as encoding/json does for other types; Parse rejects null in
// v2 before decoding.
func (t *ThresholdConfig) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case string(b) == "null":
		return nil
	case len(b) > 0 && b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*t = ThresholdConfig{Expr: s}
		return nil
	case len(b) > 0 && b[0] == '{':
		var o thresholdConfigObject
		if err := json.Unmarshal(b, &o); err != nil {
			return err
		}
		*t = ThresholdConfig(o)
		return nil
	}
	return errors.New("definition: must be a string or an object")
}

// usesObjectForm reports whether t marshals as an object.
func (t ThresholdConfig) usesObjectForm() bool { return t.AbortOnFail || t.DelayAbort != "" }

// MarshalJSON writes the plain expression string when AbortOnFail and
// DelayAbort are zero, and the object form otherwise. Characters such as
// "<" are written as they are; encoding/json escapes them again when it
// is asked to.
func (t ThresholdConfig) MarshalJSON() ([]byte, error) {
	var v any = t.Expr
	if t.usesObjectForm() {
		v = thresholdConfigObject(t)
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
// and delay_abort), and that every scenario and step carrying thresholds
// has a name, so that each Ref's Scope matches where its threshold sits.
// It returns a *ValidationError when a check fails, so a definition from
// Parse never fails here. It does not check the rest of the definition
// (for a definition built in Go, unique names are the caller's concern).
// Expression problems are reported at ".expr" for a threshold that
// marshals as an object (one with abort settings).
//
// A v1 definition from Parse has no thresholds: v1 readers ignore them,
// and so does Parse.
func Bounds(d *Definition) ([]thresholds.Bound, error) {
	if d == nil {
		return nil, nil
	}
	var ps problems
	out := collectBounds(d, &ps, true, true, func(_ string, t ThresholdConfig) bool { return t.usesObjectForm() })
	if err := ps.err(); err != nil {
		return nil, err
	}
	return out, nil
}

// collectBounds checks every threshold of d, records the problems in ps
// and, when keep is set, returns the bounds of the valid ones (validateV2
// only checks, so it does not build the slice). checkNames requires a name
// on every scenario and step that carries thresholds (validateV2 checks
// names itself). objectForm reports whether the threshold at a path was
// written as an object.
func collectBounds(d *Definition, ps *problems, keep, checkNames bool, objectForm func(path string, t ThresholdConfig) bool) []thresholds.Bound {
	var out []thresholds.Bound
	// The scope comes from where a threshold sits, not from the names in
	// its Ref, so a blank name does not hide a scope problem.
	add := func(path string, list []ThresholdConfig, scope thresholds.ScopeKind, ref thresholds.Ref) {
		for i, th := range list {
			p := indexPath(path, i)
			if b, ok := checkThreshold(ps, p, th, objectForm(p, th), scope, ref); ok && keep {
				out = append(out, b)
			}
		}
	}
	blank := func(name string) bool { return strings.TrimSpace(name) == "" }
	add("thresholds", d.Thresholds, thresholds.ScopeTest, thresholds.Ref{})
	for i, sc := range d.Scenarios {
		p := "scenarios[" + strconv.Itoa(i) + "]"
		if checkNames && blank(sc.Name) && carriesThresholds(sc) {
			ps.add(p+".name", "is required")
		}
		add(p+".thresholds", sc.Thresholds, thresholds.ScopeScenario, thresholds.Ref{Scenario: sc.Name})
		for j, st := range sc.Steps {
			sp := p + ".steps[" + strconv.Itoa(j) + "]"
			if checkNames && blank(st.Name) && len(st.Thresholds) > 0 {
				ps.add(sp+".name", "is required")
			}
			add(sp+".thresholds", st.Thresholds, thresholds.ScopeStep, thresholds.Ref{Scenario: sc.Name, Step: st.Name})
		}
	}
	return out
}

// carriesThresholds reports whether a scenario or any of its steps has
// thresholds.
func carriesThresholds(sc Scenario) bool {
	if len(sc.Thresholds) > 0 {
		return true
	}
	for _, st := range sc.Steps {
		if len(st.Thresholds) > 0 {
			return true
		}
	}
	return false
}

// checkThreshold checks one threshold at path. Problems with the
// expression are reported at path for the string form and at path.expr
// for the object form; problems with delay_abort at path.delay_abort.
func checkThreshold(ps *problems, path string, th ThresholdConfig, object bool, scope thresholds.ScopeKind, ref thresholds.Ref) (thresholds.Bound, bool) {
	ok := true
	exprPath := path
	if object {
		exprPath = path + ".expr"
	}
	var parsed thresholds.Threshold
	if object && strings.TrimSpace(th.Expr) == "" {
		ps.add(exprPath, "is required")
		ok = false
	} else {
		var err error
		parsed, err = thresholds.Parse(th.Expr)
		if err == nil {
			err = parsed.ValidFor(scope)
		}
		if err != nil {
			ps.add(exprPath, strings.TrimPrefix(err.Error(), "thresholds: "))
			ok = false
		}
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
