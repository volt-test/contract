package thresholds

import "time"

// Ref names the part of a test a threshold applies to, by scenario and
// step name. Both empty is the whole test; Step empty is the scenario
// Scenario; both set is one step of that scenario.
type Ref struct {
	Scenario string
	Step     string
}

// Scope returns the scope kind r names: ScopeStep when Step is set,
// ScopeScenario when only Scenario is set, ScopeTest otherwise.
func (r Ref) Scope() ScopeKind {
	switch {
	case r.Step != "":
		return ScopeStep
	case r.Scenario != "":
		return ScopeScenario
	}
	return ScopeTest
}

// Bound is a threshold attached to a part of a test, with its abort
// settings. definition.Bounds builds the bounds of a test definition.
//
// A Bound encodes to JSON with its Go field names, the metric as its name
// ("Metric":"p95") and the enums by name, and decodes back to an equal
// Bound. That form is for passing bounds between Go programs that use this
// package; it is not the public wire format of threshold results. A reader
// must know every metric the writer used: an unknown metric name fails to
// decode.
type Bound struct {
	Ref       Ref
	Threshold Threshold
	// AbortOnFail asks the engine to stop the test when the threshold
	// fails. Whether an engine acts on it depends on the engine release.
	AbortOnFail bool
	// DelayAbort defers the abort: abort checks for this threshold start
	// DelayAbort after the run starts. The threshold is still evaluated
	// during the delay and still decides the final verdict; only the abort
	// is deferred. The runner defines the exact run-start clock. 0 means
	// no delay. It only applies with AbortOnFail.
	DelayAbort time.Duration
}
