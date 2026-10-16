package thresholds

import (
	"math"
)

// Status is the outcome of one threshold.
type Status string

const (
	// StatusPassed: the measured value meets the threshold.
	StatusPassed Status = "passed"
	// StatusFailed: the measured value breaks the threshold.
	StatusFailed Status = "failed"
	// StatusNoData: there was nothing to measure, e.g. a distribution with
	// no values, a rate with a zero denominator, a counter the producer did
	// not measure, or a scope that never ran. It fails the final verdict
	// but never counts as a breach that aborts a run.
	StatusNoData Status = "no_data"
)

// Result is the outcome of one threshold, in the public wire form.
type Result struct {
	// Scope is where the threshold applies: "test", "scenario" or "step".
	Scope ScopeKind `json:"scope"`
	// Scenario and Step name the scenario and step, when the scope has them.
	Scenario string `json:"scenario,omitempty"`
	Step     string `json:"step,omitempty"`
	// Expr is the threshold expression as written.
	Expr string `json:"expr"`
	// Actual is the measured value in Unit, or nil for no_data.
	Actual *float64 `json:"actual"`
	// Unit of Actual: "ms" for durations, "%" for rates, "bytes" for sizes,
	// "req/s" for rps and "" for counts.
	Unit   string `json:"unit"`
	Status Status `json:"status"`
	// Passed is true exactly when Status is "passed".
	Passed bool `json:"passed"`
}

// Unit names used in Result.Unit.
const (
	UnitMilliseconds = "ms"
	UnitPercent      = "%"
	UnitBytes        = "bytes"
	UnitRequestsPerS = "req/s"
	UnitCount        = ""
)

func unitOf(k Kind) string {
	switch k {
	case KindDuration:
		return UnitMilliseconds
	case KindRate:
		return UnitPercent
	case KindSize:
		return UnitBytes
	case KindThroughput:
		return UnitRequestsPerS
	}
	return UnitCount
}

// Evaluate checks each bound against the snapshot at its scope and returns
// one Result per bound, in order. It is a pure function of its inputs and
// does not modify them. A nil snapshot, or a scope the snapshot does not
// have (a scenario or step that never ran), is evaluated as empty. A
// bound whose threshold was not built by Parse gives no_data.
//
// Values are compared in the canonical units of Threshold.Value
// (microseconds, fractions, bytes) and converted for display only.
func Evaluate(s *Snapshot, bounds []Bound) []Result {
	if s == nil {
		s = &Snapshot{}
	}
	out := make([]Result, len(bounds))
	actuals := make([]float64, len(bounds))
	var empty Scope
	for i, b := range bounds {
		th := b.Threshold
		r := Result{
			Scope:    b.Ref.Scope(),
			Scenario: b.Ref.Scenario,
			Step:     b.Ref.Step,
			Expr:     th.Raw,
			Unit:     unitOf(th.Metric.Kind),
			Status:   StatusNoData,
		}
		if r.Expr == "" {
			r.Expr = th.String()
		}
		sc := s.scope(b.Ref)
		if sc == nil {
			sc = &empty
		}
		if c, ok := lookupMetric(th.Metric.Name); ok && c == th.Metric {
			if v, display, ok := measure(s, sc, th.Metric); ok {
				actuals[i] = display
				r.Actual = &actuals[i]
				r.Status = StatusFailed
				if holds(th.Op, th.Metric.Kind, v, th.Value) {
					r.Status = StatusPassed
				}
			}
		}
		r.Passed = r.Status == StatusPassed
		out[i] = r
	}
	return out
}

// scope finds the scope a Ref names, or nil.
func (s *Snapshot) scope(r Ref) *Scope {
	switch r.Scope() {
	case ScopeTest:
		return &s.Test
	case ScopeScenario:
		if sc := s.Scenarios[r.Scenario]; sc != nil {
			return &sc.Scope
		}
	case ScopeStep:
		if sc := s.Scenarios[r.Scenario]; sc != nil {
			return sc.Steps[r.Step]
		}
	}
	return nil
}

// measure derives a metric from a scope: v in the canonical unit of the
// metric's kind and display in the unit of Result.Actual. ok is false for
// no_data.
func measure(s *Snapshot, sc *Scope, m Metric) (v, display float64, ok bool) {
	switch m.family {
	case famRequestDuration:
		return distribution(&sc.Duration, m)
	case famTTFB:
		return distribution(&sc.TTFB, m)
	case famIterationDuration:
		return distribution(&sc.IterationDuration, m)
	case famErrorRate:
		return rate(sc.Failed, sc.Requests)
	case famCheckRate:
		if sc.Checks == nil || sc.ChecksPassed == nil {
			return 0, 0, false
		}
		return rate(*sc.ChecksPassed, *sc.Checks)
	case famRPS:
		span, ok := s.span()
		if !ok {
			return 0, 0, false
		}
		v := float64(sc.Requests) / span
		return v, v, true
	case famRequests:
		return count(sc.Requests)
	case famFailed:
		return count(sc.Failed)
	case famBytesSent:
		return count(sc.BytesSent)
	case famBytesReceived:
		return count(sc.BytesReceived)
	case famIterations:
		return optionalCount(sc.Iterations)
	case famDroppedIterations:
		return optionalCount(sc.DroppedIterations)
	}
	return 0, 0, false
}

func count(n int64) (float64, float64, bool) { return float64(n), float64(n), true }

func optionalCount(p *int64) (float64, float64, bool) {
	if p == nil {
		return 0, 0, false
	}
	return count(*p)
}

// rate returns num/den as a fraction, and as a percentage computed from the
// integers so that 2 of 1000 shows as 0.2, not 0.20000000000000004.
func rate(num, den int64) (float64, float64, bool) {
	if den <= 0 {
		return 0, 0, false
	}
	return float64(num) / float64(den), float64(num) * 100 / float64(den), true
}

// distribution reads a duration metric in microseconds, and in
// milliseconds rounded to the microsecond for display.
func distribution(d *Distribution, m Metric) (float64, float64, bool) {
	if d.Count <= 0 {
		return 0, 0, false
	}
	var us float64
	switch m.agg {
	case aggAvg:
		us = float64(d.SumUs) / float64(d.Count)
	case aggMin:
		us = float64(d.MinUs)
	case aggMax:
		us = float64(d.MaxUs)
	case aggPercentile:
		v, ok := d.percentile(m.percentile)
		if !ok {
			return 0, 0, false
		}
		us = v
	default:
		return 0, 0, false
	}
	return us, math.Round(us) / 1000, true
}

// holds reports whether actual op bound is true. For durations, rates and
// rps, values within the == tolerance are equal, so < and > need a value
// outside it and <= and >= accept one inside it; counts and sizes compare
// exactly.
func holds(op Op, k Kind, actual, bound float64) bool {
	eq := equal(k, actual, bound)
	switch op {
	case OpEQ:
		return eq
	case OpLT:
		return actual < bound && !eq
	case OpLE:
		return actual <= bound || eq
	case OpGT:
		return actual > bound && !eq
	case OpGE:
		return actual >= bound || eq
	}
	return false
}

// AllPassed reports whether every result passed: false when any failed or
// had no data. It is true for no results. This is the final verdict.
func AllPassed(rs []Result) bool {
	for _, r := range rs {
		if r.Status != StatusPassed {
			return false
		}
	}
	return true
}

// Breached returns the results that failed, for callers that evaluate
// while the run is going (a rolling window) and abort on a breach.
// no_data is never a breach: a scope may simply not have run yet.
func Breached(rs []Result) []Result {
	var out []Result
	for _, r := range rs {
		if r.Status == StatusFailed {
			out = append(out, r)
		}
	}
	return out
}
