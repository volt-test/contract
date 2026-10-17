package thresholds_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/volt-test/contract/thresholds"
)

// benchSnapshot builds a snapshot with 5 scenarios of 4 steps each, every
// scope with a duration histogram of n values.
func benchSnapshot(b *testing.B, scenarios, steps, n int, seed uint64) *thresholds.Snapshot {
	b.Helper()
	s := &thresholds.Snapshot{Start: t0, End: t0.Add(time.Minute), Scenarios: map[string]*thresholds.ScenarioScope{}}
	first := true
	for i := 0; i < scenarios; i++ {
		sc := &thresholds.ScenarioScope{Steps: map[string]*thresholds.Scope{}}
		firstStep := true
		for j := 0; j < steps; j++ {
			st := &thresholds.Scope{Requests: int64(n), Failed: 1, Checks: ptr(int64(n)), ChecksPassed: ptr(int64(n - 1)),
				Duration: dist(b, values(seed+uint64(i*steps+j), n)...)}
			sc.Steps[fmt.Sprint("step", j)] = st
			if firstStep {
				sc.Scope = *st.Clone()
				firstStep = false
			} else if err := sc.Scope.Add(st); err != nil {
				b.Fatal(err)
			}
		}
		sc.Iterations, sc.DroppedIterations = ptr(int64(n/steps)), ptr(0)
		sc.IterationDuration = dist(b, values(seed+999, n/steps)...)
		s.Scenarios[fmt.Sprint("scenario", i)] = sc
		if first {
			s.Test = *sc.Scope.Clone()
			first = false
		} else if err := s.Test.Add(&sc.Scope); err != nil {
			b.Fatal(err)
		}
	}
	return s
}

// benchBounds returns 20 thresholds over the test, the 5 scenarios and
// the 20 steps, most of them percentiles.
func benchBounds(b *testing.B) []thresholds.Bound {
	b.Helper()
	var out []thresholds.Bound
	add := func(ref thresholds.Ref, expr string) {
		th, err := thresholds.Parse(expr)
		if err != nil {
			b.Fatal(err)
		}
		out = append(out, thresholds.Bound{Ref: ref, Threshold: th})
	}
	add(thresholds.Ref{}, "p95 < 500ms")
	add(thresholds.Ref{}, "p99.9 < 2s")
	add(thresholds.Ref{}, "error_rate < 1%")
	add(thresholds.Ref{}, "rps > 10")
	add(thresholds.Ref{}, "dropped_iterations == 0")
	for i := 0; i < 5; i++ {
		add(thresholds.Ref{Scenario: fmt.Sprint("scenario", i)}, "p99 < 1s")
	}
	for i := 0; i < 5; i++ {
		add(thresholds.Ref{Scenario: fmt.Sprint("scenario", i)}, "iteration_duration_p95 < 5s")
	}
	for i := 0; i < 5; i++ {
		add(thresholds.Ref{Scenario: fmt.Sprint("scenario", i), Step: fmt.Sprint("step", i%4)}, "p99.9 < 2s")
	}
	if len(out) != 20 {
		b.Fatalf("%d bounds", len(out))
	}
	return out
}

func BenchmarkEvaluate(b *testing.B) {
	s := benchSnapshot(b, 5, 4, 2000, 1)
	bounds := benchBounds(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		thresholds.Evaluate(s, bounds)
	}
}

// BenchmarkPercentile measures one percentile read, which must not
// allocate.
func BenchmarkPercentile(b *testing.B) {
	s := benchSnapshot(b, 1, 1, 2000, 1)
	bounds := benchBounds(b)[:1]
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		thresholds.Evaluate(s, bounds)
	}
}

// BenchmarkMerge merges two node snapshots of 50 steps each (5 scenarios
// of 10 steps). Merge allocates a histogram per distribution with values.
func BenchmarkMerge(b *testing.B) {
	a := benchSnapshot(b, 5, 10, 500, 1)
	c := benchSnapshot(b, 5, 10, 500, 2)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := thresholds.Merge(a, c); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSnapshotBuild is the producer path: record step durations into
// histograms, then sum steps into scenarios and scenarios into the test
// with Clone and Add (5 scenarios of 4 steps, 1000 values per step).
func BenchmarkSnapshotBuild(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchSnapshot(b, 5, 4, 1000, uint64(i))
	}
}

// BenchmarkScopeAdd is the rolling-window path: add one interval's step
// scope into a cumulative scope.
func BenchmarkScopeAdd(b *testing.B) {
	cum := &thresholds.Scope{Requests: 1, Checks: ptr(1), ChecksPassed: ptr(1), Duration: dist(b, values(1, 1000)...)}
	tick := &thresholds.Scope{Requests: 1, Checks: ptr(1), ChecksPassed: ptr(1), Duration: dist(b, values(2, 100)...)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := cum.Add(tick); err != nil {
			b.Fatal(err)
		}
	}
}
