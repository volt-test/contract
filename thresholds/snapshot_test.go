package thresholds_test

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"

	"github.com/volt-test/contract/thresholds"
)

// lcg is a tiny deterministic generator, so generated fixtures do not
// depend on math/rand's algorithm.
type lcg uint64

func (g *lcg) next() uint64 {
	*g = *g*6364136223846793005 + 1442695040888963407
	return uint64(*g >> 33)
}

// values returns n durations in microseconds: mostly 1..200ms, with a tail
// of one in 200 between 1s and 4s.
func values(seed uint64, n int) []int64 {
	g := lcg(seed)
	out := make([]int64, n)
	for i := range out {
		if g.next()%200 == 0 {
			out[i] = 1_000_000 + int64(g.next()%3_000_000)
		} else {
			out[i] = 1_000 + int64(g.next()%199_000)
		}
	}
	return out
}

// dist builds a Distribution with a histogram from durations in µs.
func dist(t testing.TB, us ...int64) thresholds.Distribution {
	t.Helper()
	d := thresholds.Distribution{Hist: thresholds.NewHistogram()}
	for i, v := range us {
		if err := d.Hist.RecordValue(v); err != nil {
			t.Fatal(err)
		}
		d.Count++
		d.SumUs += v
		if i == 0 || v < d.MinUs {
			d.MinUs = v
		}
		if i == 0 || v > d.MaxUs {
			d.MaxUs = v
		}
	}
	return d
}

func ptr(v int64) *int64 { return &v }

var t0 = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

// equalScopes compares scopes by value, histograms by content.
func equalScopes(a, b *thresholds.Scope) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Requests != b.Requests || a.Failed != b.Failed || a.BytesSent != b.BytesSent || a.BytesReceived != b.BytesReceived {
		return false
	}
	for _, p := range [][2]*int64{{a.Checks, b.Checks}, {a.ChecksPassed, b.ChecksPassed}, {a.Iterations, b.Iterations}, {a.DroppedIterations, b.DroppedIterations}} {
		if (p[0] == nil) != (p[1] == nil) || (p[0] != nil && *p[0] != *p[1]) {
			return false
		}
	}
	for _, d := range [][2]*thresholds.Distribution{{&a.Duration, &b.Duration}, {&a.TTFB, &b.TTFB}, {&a.IterationDuration, &b.IterationDuration}} {
		x, y := d[0], d[1]
		if x.Count != y.Count || x.SumUs != y.SumUs {
			return false
		}
		if x.Count > 0 && (x.MinUs != y.MinUs || x.MaxUs != y.MaxUs) {
			return false
		}
		if !equalHists(canonHist(*x), canonHist(*y)) {
			return false
		}
	}
	return true
}

// canonHist is a distribution's histogram by value: with Count 0, an
// empty histogram is the same as none. With Count > 0 they differ: no
// histogram means the percentiles are unknown.
func canonHist(d thresholds.Distribution) *hdrhistogram.Histogram {
	if d.Hist != nil && d.Count == 0 && d.Hist.TotalCount() == 0 {
		return nil
	}
	return d.Hist
}

// equalHists treats nil and an empty histogram as different: nil means
// "no histogram", which matters when Count > 0.
func equalHists(a, b *hdrhistogram.Histogram) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equals(b)
}

func equalSnapshots(a, b *thresholds.Snapshot) bool {
	if !a.Start.Equal(b.Start) || !a.End.Equal(b.End) || !equalScopes(&a.Test, &b.Test) {
		return false
	}
	nonNil := func(m map[string]*thresholds.ScenarioScope) int {
		n := 0
		for _, v := range m {
			if v != nil {
				n++
			}
		}
		return n
	}
	if nonNil(a.Scenarios) != nonNil(b.Scenarios) {
		return false
	}
	for name, x := range a.Scenarios {
		if x == nil {
			continue
		}
		y := b.Scenarios[name]
		if y == nil || !equalScopes(&x.Scope, &y.Scope) {
			return false
		}
		n := 0
		for step, s := range x.Steps {
			if s == nil {
				continue
			}
			n++
			if !equalScopes(s, y.Steps[step]) {
				return false
			}
		}
		for _, s := range y.Steps {
			if s != nil {
				n--
			}
		}
		if n != 0 {
			return false
		}
	}
	return true
}

// deepCopy copies a snapshot so that a test can check an input was not
// modified.
func deepCopy(s *thresholds.Snapshot) *thresholds.Snapshot {
	c := &thresholds.Snapshot{Start: s.Start, End: s.End, Test: *s.Test.Clone()}
	if s.Scenarios != nil {
		c.Scenarios = map[string]*thresholds.ScenarioScope{}
	}
	for name, sc := range s.Scenarios {
		if sc == nil {
			c.Scenarios[name] = nil
			continue
		}
		cs := &thresholds.ScenarioScope{Scope: *sc.Scope.Clone()}
		if sc.Steps != nil {
			cs.Steps = map[string]*thresholds.Scope{}
		}
		for step, st := range sc.Steps {
			if st == nil {
				cs.Steps[step] = nil
				continue
			}
			cs.Steps[step] = st.Clone()
		}
		c.Scenarios[name] = cs
	}
	return c
}

func TestNewHistogram(t *testing.T) {
	h := thresholds.NewHistogram()
	if h.LowestTrackableValue() != 1 || h.HighestTrackableValue() != 3_600_000_000 || h.SignificantFigures() != 3 {
		t.Errorf("config %d..%d/%d", h.LowestTrackableValue(), h.HighestTrackableValue(), h.SignificantFigures())
	}
	if thresholds.HistogramLowestUs != 1 || thresholds.HistogramHighestUs != 3_600_000_000 || thresholds.HistogramSigFigs != 3 {
		t.Error("constants changed")
	}
}

func TestScopeAdd(t *testing.T) {
	a := thresholds.Scope{Requests: 10, Failed: 1, BytesSent: 100, BytesReceived: 1000,
		Checks: ptr(4), ChecksPassed: ptr(3), Iterations: ptr(2), DroppedIterations: nil,
		Duration: dist(t, 5000, 7000), TTFB: thresholds.Distribution{Count: 2, SumUs: 30, MinUs: 10, MaxUs: 20}}
	b := thresholds.Scope{Requests: 5, Failed: 2, BytesSent: 50, BytesReceived: 500,
		Checks: ptr(6), ChecksPassed: ptr(6), Iterations: ptr(1), DroppedIterations: ptr(0),
		Duration: dist(t, 1000, 9000), TTFB: dist(t, 15)}
	bCopy := b.Clone()

	sum := a.Clone()
	if err := sum.Add(&b); err != nil {
		t.Fatal(err)
	}
	if sum.Requests != 15 || sum.Failed != 3 || sum.BytesSent != 150 || sum.BytesReceived != 1500 {
		t.Errorf("counters %+v", sum)
	}
	if *sum.Checks != 10 || *sum.ChecksPassed != 9 || *sum.Iterations != 3 || sum.DroppedIterations != nil {
		t.Errorf("optional counters %v %v %v %v", sum.Checks, sum.ChecksPassed, sum.Iterations, sum.DroppedIterations)
	}
	if d := sum.Duration; d.Count != 4 || d.SumUs != 22000 || d.MinUs != 1000 || d.MaxUs != 9000 || d.Hist.TotalCount() != 4 {
		t.Errorf("duration %+v", d)
	}
	// a's TTFB has values but no histogram: the merged percentiles are
	// unknown, while the exact fields still add.
	if d := sum.TTFB; d.Count != 3 || d.SumUs != 45 || d.MinUs != 10 || d.MaxUs != 20 || d.Hist != nil {
		t.Errorf("ttfb %+v", d)
	}
	if !equalScopes(&b, bCopy) {
		t.Error("Add modified its argument")
	}
	if sum.Checks == a.Checks || sum.Checks == b.Checks {
		t.Error("Add shares a counter pointer")
	}

	// A zero Scope has nil optional counters, so they stay nil.
	var zero thresholds.Scope
	if err := zero.Add(&b); err != nil {
		t.Fatal(err)
	}
	if zero.Checks != nil || zero.Requests != 5 || zero.Duration.Count != 2 || zero.Duration.Hist.TotalCount() != 2 {
		t.Errorf("zero.Add = %+v", zero)
	}
	if zero.Duration.Hist == b.Duration.Hist {
		t.Error("Add shares the argument's histogram")
	}

	// Adding a scope to itself doubles it.
	self := a.Clone()
	if err := self.Add(self); err != nil {
		t.Fatal(err)
	}
	if self.Requests != 20 || self.Duration.Hist.TotalCount() != 4 || self.Duration.Count != 4 {
		t.Errorf("self add %+v", self)
	}

	// nil is a no-op.
	n := a.Clone()
	if err := n.Add(nil); err != nil || !equalScopes(n, &a) {
		t.Errorf("Add(nil): %v", err)
	}
}

func TestScopeAddErrors(t *testing.T) {
	good := func() *thresholds.Scope { return &thresholds.Scope{Requests: 1, Duration: dist(t, 10)} }
	cases := map[string]*thresholds.Scope{
		"negative counter":          {Requests: -1},
		"negative optional":         {Checks: ptr(-1)},
		"negative count":            {Duration: thresholds.Distribution{Count: -1}},
		"max below min":             {Duration: thresholds.Distribution{Count: 1, SumUs: 5, MinUs: 5, MaxUs: 4}},
		"overflow":                  {Requests: 1<<63 - 1},
		"other sig figs":            {Duration: thresholds.Distribution{Hist: hdrhistogram.New(1, 3_600_000_000, 2)}},
		"other highest value":       {TTFB: thresholds.Distribution{Hist: hdrhistogram.New(1, 1_000_000, 3)}},
		"other lowest value":        {IterationDuration: thresholds.Distribution{Hist: hdrhistogram.New(1000, 3_600_000_000, 3)}},
		"sum overflow in a summary": {Duration: thresholds.Distribution{Count: 1, SumUs: 1<<63 - 1, MinUs: 1, MaxUs: 1}},
		"more failed than requests": {Requests: 1, Failed: 2},
		"more passed than checks":   {Checks: ptr(1), ChecksPassed: ptr(2)},
		"negative sum":              {TTFB: thresholds.Distribution{SumUs: -1}},
		"negative min":              {TTFB: thresholds.Distribution{Count: 1, MinUs: -1, MaxUs: 1}},
	}
	want := func(name string) error {
		switch {
		case strings.HasPrefix(name, "other"):
			return thresholds.ErrHistogramConfig
		case strings.Contains(name, "overflow"):
			return thresholds.ErrOverflow
		}
		return thresholds.ErrInvalidScope
	}
	for name, o := range cases {
		s := good()
		before := s.Clone()
		if err := s.Add(o); !errors.Is(err, want(name)) {
			t.Errorf("%s: Add = %v, want %v", name, err, want(name))
		}
		if !equalScopes(s, before) {
			t.Errorf("%s: Add changed s on error", name)
		}
	}
}

// node builds a node snapshot for the merge tests.
func node(t testing.TB, start, end time.Duration, test thresholds.Scope, scenarios map[string]*thresholds.ScenarioScope) *thresholds.Snapshot {
	t.Helper()
	return &thresholds.Snapshot{Start: t0.Add(start), End: t0.Add(end), Test: test, Scenarios: scenarios}
}

func evaluate(t testing.TB, s *thresholds.Snapshot, ref thresholds.Ref, expr string) thresholds.Result {
	t.Helper()
	th, err := thresholds.Parse(expr)
	if err != nil {
		t.Fatal(err)
	}
	return thresholds.Evaluate(s, []thresholds.Bound{{Ref: ref, Threshold: th}})[0]
}

func actual(t testing.TB, r thresholds.Result) float64 {
	t.Helper()
	if r.Actual == nil {
		t.Fatalf("%s: no actual (status %s)", r.Expr, r.Status)
	}
	return *r.Actual
}

// Two nodes with different amounts of traffic merge into the run's
// numbers, not averages of the nodes' numbers.
func TestMergeTwoNodes(t *testing.T) {
	va, vb := values(11, 100), values(22, 900)
	stepA := thresholds.Scope{Requests: 60, Duration: dist(t, 5000, 9000), Checks: ptr(10), ChecksPassed: ptr(10)}
	// The step never ran on node B: no requests, no durations, and
	// checks measured as zero (a producer that measures checks reports 0,
	// never nil, for a scope where nothing happened).
	stepB := thresholds.Scope{Checks: ptr(0), ChecksPassed: ptr(0)}
	a := node(t, 0, 10*time.Second,
		thresholds.Scope{Requests: 100, Failed: 2, Duration: dist(t, va...), Checks: ptr(10), ChecksPassed: ptr(9), DroppedIterations: ptr(0)},
		map[string]*thresholds.ScenarioScope{"browse": {Scope: thresholds.Scope{Requests: 60}, Steps: map[string]*thresholds.Scope{"home": &stepA}}})
	b := node(t, 5*time.Second, 20*time.Second,
		thresholds.Scope{Requests: 900, Failed: 0, Duration: dist(t, vb...), Checks: nil, ChecksPassed: nil, DroppedIterations: ptr(3)},
		map[string]*thresholds.ScenarioScope{
			"browse": {Scope: thresholds.Scope{Requests: 0}, Steps: map[string]*thresholds.Scope{"home": &stepB}},
			"buy":    {Scope: thresholds.Scope{Requests: 900}},
		})
	aBefore, bBefore := deepCopy(a), deepCopy(b)

	m, err := thresholds.Merge(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !equalSnapshots(a, aBefore) || !equalSnapshots(b, bBefore) {
		t.Error("Merge modified its inputs")
	}
	if !m.Start.Equal(t0) || !m.End.Equal(t0.Add(20*time.Second)) {
		t.Errorf("window %v..%v", m.Start, m.End)
	}
	test := thresholds.Ref{}

	// error_rate is 2 failed of 1000 requests, not the 1% mean of 2% and 0%.
	if got := actual(t, evaluate(t, m, test, "error_rate < 1%")); got != 0.2 {
		t.Errorf("merged error_rate = %v%%, want 0.2%%", got)
	}
	// rps is 1000 requests over the 20s window, not 100/10 + 900/15 = 70.
	if got := actual(t, evaluate(t, m, test, "rps > 1")); got != 50 {
		t.Errorf("merged rps = %v, want 50", got)
	}
	// The merged p99 is the p99 of all the values together.
	union := dist(t, append(append([]int64{}, va...), vb...)...)
	if got, want := actual(t, evaluate(t, m, test, "p99 < 10s")), float64(union.Hist.ValueAtQuantile(99))/1000; got != want {
		t.Errorf("merged p99 = %vms, want %vms", got, want)
	}
	if got, want := actual(t, evaluate(t, m, test, "avg < 10s")), math.Round(float64(union.SumUs)/float64(union.Count))/1000; got != want {
		t.Errorf("merged avg = %v, want %v", got, want)
	}
	// min and max ignore the step that did not run on node B.
	home := thresholds.Ref{Scenario: "browse", Step: "home"}
	if got := actual(t, evaluate(t, m, home, "min > 1ms")); got != 5 {
		t.Errorf("merged step min = %vms, want 5ms", got)
	}
	if got := actual(t, evaluate(t, m, home, "max < 1s")); got != 9 {
		t.Errorf("merged step max = %vms, want 9ms", got)
	}
	// Node B did not measure checks: the merged check_rate is unknown.
	if r := evaluate(t, m, test, "check_rate > 50%"); r.Status != thresholds.StatusNoData || r.Actual != nil {
		t.Errorf("check_rate with a node not measuring checks: %+v", r)
	}
	// The step's checks were measured on both nodes; B had none.
	if got := actual(t, evaluate(t, m, home, "check_rate == 100%")); got != 100 {
		t.Errorf("step check_rate = %v", got)
	}
	if got := actual(t, evaluate(t, m, test, "dropped_iterations == 0")); got != 3 {
		t.Errorf("dropped_iterations = %v", got)
	}
	// A scenario on one node only is kept.
	if got := actual(t, evaluate(t, m, thresholds.Ref{Scenario: "buy"}, "requests > 1")); got != 900 {
		t.Errorf("buy requests = %v", got)
	}
}

func TestMergeErrors(t *testing.T) {
	ok := node(t, 0, time.Second, thresholds.Scope{Requests: 1}, nil)
	mismatch := node(t, 0, time.Second, thresholds.Scope{Duration: thresholds.Distribution{Count: 1, SumUs: 5, MinUs: 5, MaxUs: 5, Hist: hdrhistogram.New(1, 3_600_000_000, 2)}}, nil)
	stepMismatch := node(t, 0, time.Second, thresholds.Scope{}, map[string]*thresholds.ScenarioScope{
		"s": {Steps: map[string]*thresholds.Scope{"a": {TTFB: thresholds.Distribution{Hist: hdrhistogram.New(1, 60_000_000, 3)}}}},
	})
	cases := map[string][]*thresholds.Snapshot{
		"no snapshots":              nil,
		"nil snapshot":              {ok, nil},
		"zero start":                {ok, {End: t0}},
		"zero end":                  {ok, {Start: t0}},
		"histogram config":          {ok, mismatch},
		"histogram config, first":   {mismatch, ok},
		"histogram config, in step": {ok, stepMismatch},
		"negative counter":          {ok, node(t, 0, time.Second, thresholds.Scope{Failed: -1}, nil)},
		"end before start":          {ok, node(t, time.Second, 0, thresholds.Scope{}, nil)},
		"failed above requests":     {ok, node(t, 0, time.Second, thresholds.Scope{Requests: 1, Failed: 2}, nil)},
		"overflow":                  {ok, node(t, 0, time.Second, thresholds.Scope{Requests: 1<<63 - 1}, nil)},
	}
	want := map[string]error{
		"zero start": thresholds.ErrInvalidScope, "zero end": thresholds.ErrInvalidScope,
		"histogram config": thresholds.ErrHistogramConfig, "histogram config, first": thresholds.ErrHistogramConfig,
		"histogram config, in step": thresholds.ErrHistogramConfig, "negative counter": thresholds.ErrInvalidScope,
		"end before start": thresholds.ErrInvalidScope, "failed above requests": thresholds.ErrInvalidScope,
		"overflow": thresholds.ErrOverflow,
	}
	for name, nodes := range cases {
		m, err := thresholds.Merge(nodes...)
		if err == nil || m != nil {
			t.Errorf("%s: Merge = %v, %v", name, m, err)
			continue
		}
		if w := want[name]; w != nil && !errors.Is(err, w) {
			t.Errorf("%s: error %v, want %v", name, err, w)
		}
		if !strings.HasPrefix(err.Error(), "thresholds: ") || strings.Count(err.Error(), "thresholds: ") != 1 {
			t.Errorf("%s: error text %q", name, err)
		}
	}
	// An End equal to the Start is allowed: rps over it is no_data.
	if _, err := thresholds.Merge(node(t, time.Second, time.Second, thresholds.Scope{}, nil)); err != nil {
		t.Errorf("empty window: %v", err)
	}
	// Long scenario and step names are cut in the error.
	long := strings.Repeat("x", 5000)
	_, err := thresholds.Merge(ok, node(t, 0, time.Second, thresholds.Scope{}, map[string]*thresholds.ScenarioScope{
		long: {Steps: map[string]*thresholds.Scope{long: {Failed: 1}}},
	}))
	if !errors.Is(err, thresholds.ErrInvalidScope) || len(err.Error()) > 400 {
		t.Errorf("long names: %d bytes: %v", len(fmt.Sprint(err)), err)
	}
}

// Merging is order-independent, and merging one snapshot copies it.
func TestMergeProperties(t *testing.T) {
	a := node(t, time.Second, 9*time.Second, thresholds.Scope{Requests: 7, Checks: ptr(1), ChecksPassed: ptr(1), Duration: dist(t, values(3, 50)...)},
		map[string]*thresholds.ScenarioScope{"x": {Scope: thresholds.Scope{Requests: 7, Iterations: ptr(2)}, Steps: map[string]*thresholds.Scope{"1": {Requests: 7, Duration: dist(t, 100, 200)}}}})
	b := node(t, 0, 5*time.Second, thresholds.Scope{Requests: 3, Checks: ptr(4), ChecksPassed: ptr(2), Duration: dist(t, values(4, 30)...)},
		map[string]*thresholds.ScenarioScope{"x": {Scope: thresholds.Scope{Requests: 3, Iterations: ptr(1)}, Steps: map[string]*thresholds.Scope{"2": {Requests: 3}}},
			"y": {Scope: thresholds.Scope{TTFB: thresholds.Distribution{Count: 1, SumUs: 9, MinUs: 9, MaxUs: 9}}}})
	requireMergeProperties(t, a, b)

	// An empty histogram on one node and none on the other merge to the
	// same thing in either order; values without a histogram on either
	// node make the percentiles unknown in either order.
	empty := node(t, 0, time.Second, thresholds.Scope{Duration: thresholds.Distribution{Hist: thresholds.NewHistogram()}}, nil)
	none := node(t, 0, time.Second, thresholds.Scope{}, nil)
	unknown := node(t, 0, time.Second, thresholds.Scope{Duration: thresholds.Distribution{Count: 2, SumUs: 4, MinUs: 1, MaxUs: 3}}, nil)
	withValues := node(t, 0, time.Second, thresholds.Scope{Duration: dist(t, 10, 20)}, nil)
	requireMergeProperties(t, empty, none)
	requireMergeProperties(t, unknown, withValues)
	m, err := thresholds.Merge(withValues, unknown)
	if err != nil {
		t.Fatal(err)
	}
	if m.Test.Duration.Hist != nil || m.Test.Duration.Count != 4 {
		t.Errorf("values without a histogram did not make the percentiles unknown: %+v", m.Test.Duration)
	}
	if r := evaluate(t, m, thresholds.Ref{}, "p50 < 1s"); r.Status != thresholds.StatusNoData {
		t.Errorf("p50 = %+v", r)
	}
	if got := actual(t, evaluate(t, m, thresholds.Ref{}, "max < 1s")); got != 0.02 {
		t.Errorf("max = %v", got)
	}
}

func requireMergeProperties(t *testing.T, a, b *thresholds.Snapshot) {
	t.Helper()
	ab, err1 := thresholds.Merge(a, b)
	ba, err2 := thresholds.Merge(b, a)
	if (err1 == nil) != (err2 == nil) {
		t.Fatalf("Merge(a, b) error %v, Merge(b, a) error %v", err1, err2)
	}
	if err1 == nil && !equalSnapshots(ab, ba) {
		t.Fatal("Merge(a, b) != Merge(b, a)")
	}
	for _, s := range []*thresholds.Snapshot{a, b} {
		one, err := thresholds.Merge(s)
		if err != nil {
			if err1 == nil {
				t.Fatalf("Merge(s) failed but Merge(a, b) did not: %v", err)
			}
			continue
		}
		if !equalSnapshots(one, s) {
			t.Fatal("Merge(s) != s")
		}
		if one.Test.Duration.Hist != nil && one.Test.Duration.Hist == s.Test.Duration.Hist {
			t.Fatal("Merge(s) shares a histogram with s")
		}
	}
}

// FuzzMerge checks the merge properties on snapshots shaped by the input:
// a scenario with up to two steps, counters, optional counters and small
// distributions. Histograms are about 188 KB each, so the shape is kept
// small for the fuzzer to stay fast.
func FuzzMerge(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20})
	f.Add([]byte{0xff, 0, 0x80, 7, 0, 0, 0, 3, 9, 200, 100, 50})
	f.Add(make([]byte, 64))
	f.Fuzz(func(t *testing.T, data []byte) {
		r := reader(data)
		a, b := r.snapshot(t), r.snapshot(t)
		requireMergeProperties(t, a, b)
	})
}

type reader []byte

func (r *reader) byte() byte {
	if len(*r) == 0 {
		return 0
	}
	c := (*r)[0]
	*r = (*r)[1:]
	return c
}

func (r *reader) scope(t *testing.T) thresholds.Scope {
	s := thresholds.Scope{Requests: int64(r.byte()), BytesSent: int64(r.byte()) << 10}
	s.Failed = min(int64(r.byte()%8), s.Requests)
	opt := func() *int64 {
		if c := r.byte(); c%3 != 0 {
			return ptr(int64(c))
		}
		return nil
	}
	s.Checks, s.ChecksPassed, s.Iterations, s.DroppedIterations = opt(), opt(), opt(), opt()
	if s.Checks != nil && s.ChecksPassed != nil && *s.ChecksPassed > *s.Checks {
		s.ChecksPassed = ptr(*s.Checks)
	}
	switch mode := r.byte() % 8; mode {
	case 1, 2:
		n := int(r.byte() % 6)
		vs := make([]int64, n)
		for i := range vs {
			vs[i] = 1 + int64(r.byte())*int64(r.byte()+1)*37
		}
		s.Duration = dist(t, vs...)
		if mode == 2 && n > 0 {
			s.Duration.Hist = nil // values without a histogram
		}
	case 3:
		s.Duration = thresholds.Distribution{Hist: thresholds.NewHistogram()} // empty
	}
	if c := r.byte(); c > 0 {
		s.TTFB = thresholds.Distribution{Count: int64(c % 5), SumUs: int64(c), MinUs: int64(c % 7), MaxUs: int64(c)}
	}
	return s
}

func (r *reader) snapshot(t *testing.T) *thresholds.Snapshot {
	s := &thresholds.Snapshot{Start: t0.Add(time.Duration(r.byte()) * time.Second)}
	s.End = s.Start.Add(time.Duration(r.byte()) * time.Second)
	if s.End.Equal(s.Start) {
		s.End = s.End.Add(time.Millisecond)
	}
	s.Test = r.scope(t)
	for _, name := range []string{"a"} {
		if r.byte()%2 == 0 {
			continue
		}
		if s.Scenarios == nil {
			s.Scenarios = map[string]*thresholds.ScenarioScope{}
		}
		sc := &thresholds.ScenarioScope{Scope: r.scope(t)}
		for _, step := range []string{"1", "2"} {
			if r.byte()%2 == 0 {
				continue
			}
			if sc.Steps == nil {
				sc.Steps = map[string]*thresholds.Scope{}
			}
			st := r.scope(t)
			sc.Steps[step] = &st
		}
		s.Scenarios[name] = sc
	}
	return s
}
