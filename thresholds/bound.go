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
type Bound struct {
	Ref       Ref
	Threshold Threshold
	// AbortOnFail asks the engine to stop the test as soon as the threshold
	// fails. Whether an engine acts on it depends on the engine release.
	AbortOnFail bool
	// DelayAbort is a grace period from the start of the test during which
	// a failing threshold does not abort it; 0 means none. It only applies
	// with AbortOnFail.
	DelayAbort time.Duration
}
