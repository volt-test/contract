package thresholds_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"

	"github.com/volt-test/contract/thresholds"
)

var update = flag.Bool("update", false, "regenerate testdata/step_hdr.b64")

const stepHDRFixture = "testdata/step_hdr.b64"

// stepValues are the durations behind the step histogram fixture.
func stepValues() []int64 { return values(7, 20_000) }

// A step-scoped percentile threshold reads the step's own histogram, as
// carried in a node result (base64 V2 compressed HDR).
func TestStepPercentileFromEncodedHistogram(t *testing.T) {
	want := dist(t, stepValues()...)
	if *update {
		enc, err := thresholds.EncodeHistogram(want.Hist)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stepHDRFixture, append(enc, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(stepHDRFixture)
	if err != nil {
		t.Fatalf("%v (run: go test ./thresholds -run TestStepPercentile -update)", err)
	}
	h, err := thresholds.DecodeHistogram(bytes.TrimSpace(data))
	if err != nil {
		t.Fatal(err)
	}
	// Compare content, not bytes: zlib output may differ between Go releases.
	if !h.Equals(want.Hist) {
		t.Fatalf("%s does not hold the generated values; run with -update", stepHDRFixture)
	}

	step := want
	step.Hist = h
	s := &thresholds.Snapshot{Start: t0, End: t0.Add(time.Minute), Scenarios: map[string]*thresholds.ScenarioScope{
		"checkout": {Steps: map[string]*thresholds.Scope{"pay": {Requests: step.Count, Duration: step}}},
	}}
	ref := thresholds.Ref{Scenario: "checkout", Step: "pay"}
	r := evaluate(t, s, ref, "p99.9 < 2s")
	wantMs := float64(h.ValueAtQuantile(99.9)) / 1000
	if got := actual(t, r); got != wantMs {
		t.Errorf("p99.9 actual = %vms, want ValueAtQuantile(99.9)/1000 = %vms", got, wantMs)
	}
	if wantMs < 1000 {
		t.Fatalf("fixture p99.9 is %vms; the tail should put it above 1s", wantMs)
	}
	if r.Passed != (wantMs < 2000) || r.Unit != "ms" || r.Scope != thresholds.ScopeStep || r.Scenario != "checkout" || r.Step != "pay" {
		t.Errorf("result %+v", r)
	}
	// The scenario and test have no data of their own here.
	if r := evaluate(t, s, thresholds.Ref{Scenario: "checkout"}, "p99.9 < 2s"); r.Status != thresholds.StatusNoData {
		t.Errorf("scenario p99.9 = %+v", r)
	}
}

// dropped_iterations == 0 passes on 0, fails on 3, and has no data when
// the producer did not measure it, at test and scenario scope.
func TestDroppedIterationsZero(t *testing.T) {
	for _, tc := range []struct {
		dropped *int64
		status  thresholds.Status
	}{{ptr(0), thresholds.StatusPassed}, {ptr(3), thresholds.StatusFailed}, {nil, thresholds.StatusNoData}} {
		s := &thresholds.Snapshot{Start: t0, End: t0.Add(time.Second),
			Test:      thresholds.Scope{DroppedIterations: tc.dropped},
			Scenarios: map[string]*thresholds.ScenarioScope{"s": {Scope: thresholds.Scope{DroppedIterations: tc.dropped}}}}
		for _, ref := range []thresholds.Ref{{}, {Scenario: "s"}} {
			r := evaluate(t, s, ref, "dropped_iterations == 0")
			if r.Status != tc.status || r.Passed != (tc.status == thresholds.StatusPassed) {
				t.Errorf("dropped %v at %s: %+v", tc.dropped, ref.Scope(), r)
			}
			if tc.dropped == nil {
				if r.Actual != nil {
					t.Errorf("no_data with actual %v", *r.Actual)
				}
			} else if *r.Actual != float64(*tc.dropped) || r.Unit != "" {
				t.Errorf("actual %v %q", *r.Actual, r.Unit)
			}
		}
	}
}

// Missing data is no_data, never a 0ms pass, and fails the final verdict
// without counting as a breach. A scope the snapshot does not have is
// no_data for every metric.
func TestNoData(t *testing.T) {
	s := &thresholds.Snapshot{Start: t0, End: t0.Add(time.Second),
		Test: thresholds.Scope{
			Duration:          thresholds.Distribution{Hist: thresholds.NewHistogram()},                                          // nothing recorded
			TTFB:              thresholds.Distribution{Count: 3, SumUs: 30, MinUs: 5, MaxUs: 15},                                 // values, no histogram
			IterationDuration: thresholds.Distribution{Count: 2, SumUs: 10, MinUs: 5, MaxUs: 5, Hist: thresholds.NewHistogram()}, // dropped from the histogram
		},
		Scenarios: map[string]*thresholds.ScenarioScope{"s": {Steps: map[string]*thresholds.Scope{}}},
	}
	cases := []struct {
		ref    thresholds.Ref
		expr   string
		status thresholds.Status
	}{
		{thresholds.Ref{}, "p95 < 500ms", thresholds.StatusNoData},
		{thresholds.Ref{}, "avg < 500ms", thresholds.StatusNoData},
		{thresholds.Ref{}, "min < 500ms", thresholds.StatusNoData},
		{thresholds.Ref{}, "ttfb_p95 < 500ms", thresholds.StatusNoData},
		{thresholds.Ref{}, "ttfb < 500ms", thresholds.StatusPassed}, // the exact average is known
		{thresholds.Ref{}, "iteration_duration_p50 < 1s", thresholds.StatusNoData},
		{thresholds.Ref{}, "iteration_duration_max < 1s", thresholds.StatusPassed},
		{thresholds.Ref{}, "error_rate < 1%", thresholds.StatusNoData},  // no requests
		{thresholds.Ref{}, "check_rate > 99%", thresholds.StatusNoData}, // not measured
		{thresholds.Ref{}, "iterations > 0", thresholds.StatusNoData},
		{thresholds.Ref{}, "requests < 10", thresholds.StatusPassed}, // plain counters are always defined
		// A step that never ran, and a scenario that never ran.
		{thresholds.Ref{Scenario: "s", Step: "absent"}, "p99 < 1s", thresholds.StatusNoData},
		{thresholds.Ref{Scenario: "missing"}, "p99 < 1s", thresholds.StatusNoData},
		{thresholds.Ref{Scenario: "missing", Step: "x"}, "error_rate < 1%", thresholds.StatusNoData},
		// A scope the snapshot does not have is no_data for every kind:
		// counts, sizes and rps included, not a passing zero.
		{thresholds.Ref{Scenario: "missing", Step: "x"}, "failed == 0", thresholds.StatusNoData},
		{thresholds.Ref{Scenario: "missing"}, "requests > 10", thresholds.StatusNoData},
		{thresholds.Ref{Scenario: "missing"}, "rps > 1", thresholds.StatusNoData},
		{thresholds.Ref{Scenario: "s", Step: "absent"}, "bytes_sent < 1kb", thresholds.StatusNoData},
		{thresholds.Ref{Scenario: "s", Step: "absent"}, "dropped_iterations == 0", thresholds.StatusNoData},
		// A present scope with nothing in it is a measured zero.
		{thresholds.Ref{}, "failed == 0", thresholds.StatusPassed},
		{thresholds.Ref{}, "requests > 10", thresholds.StatusFailed},
	}
	var rs []thresholds.Result
	for _, tc := range cases {
		r := evaluate(t, s, tc.ref, tc.expr)
		rs = append(rs, r)
		if r.Status != tc.status {
			t.Errorf("%s at %+v: status %s, want %s", tc.expr, tc.ref, r.Status, tc.status)
		}
		if (r.Actual == nil) != (tc.status == thresholds.StatusNoData) {
			t.Errorf("%s: actual %v with status %s", tc.expr, r.Actual, r.Status)
		}
	}
	if thresholds.AllPassed(rs) {
		t.Error("AllPassed with no_data results")
	}
	if b := thresholds.Breached(rs); len(b) != 1 || b[0].Expr != "requests > 10" || b[0].Scope != thresholds.ScopeTest {
		t.Errorf("Breached = %+v, want only the measured test-scope failure", b)
	}

	// A nil snapshot fails closed: everything is no_data, nothing passes
	// and nothing is breached.
	var bounds []thresholds.Bound
	for _, c := range []struct {
		ref  thresholds.Ref
		expr string
	}{
		{thresholds.Ref{}, "failed == 0"},
		{thresholds.Ref{}, "bytes_sent < 1kb"},
		{thresholds.Ref{Scenario: "s"}, "requests > 10"},
		{thresholds.Ref{Scenario: "s", Step: "a"}, "rps > 1"},
		{thresholds.Ref{}, "error_rate < 1%"},
		{thresholds.Ref{}, "p95 < 1s"},
	} {
		th, err := thresholds.Parse(c.expr)
		if err != nil {
			t.Fatal(err)
		}
		bounds = append(bounds, thresholds.Bound{Ref: c.ref, Threshold: th})
	}
	nilResults := thresholds.Evaluate(nil, bounds)
	for _, r := range nilResults {
		if r.Status != thresholds.StatusNoData || r.Actual != nil || r.Passed {
			t.Errorf("nil snapshot, %s: %+v", r.Expr, r)
		}
	}
	if thresholds.AllPassed(nilResults) || len(thresholds.Breached(nilResults)) != 0 {
		t.Error("nil snapshot: AllPassed or Breached")
	}
	// The same bounds on a snapshot without those scenarios: the test
	// scope is measured, the missing scopes are no_data and not breached.
	empty := thresholds.Evaluate(&thresholds.Snapshot{Start: t0, End: t0.Add(time.Second)}, bounds)
	if b := thresholds.Breached(empty); len(b) != 0 {
		t.Errorf("never-run scopes breached: %+v", b)
	}

	// rps needs a window.
	for _, w := range [][2]time.Time{{t0, t0}, {t0.Add(time.Second), t0}, {{}, t0}, {t0, {}}} {
		s := &thresholds.Snapshot{Start: w[0], End: w[1], Test: thresholds.Scope{Requests: 10}}
		if r := evaluate(t, s, thresholds.Ref{}, "rps > 1"); r.Status != thresholds.StatusNoData {
			t.Errorf("rps over %v..%v: %+v", w[0], w[1], r)
		}
	}
	// An invalid threshold has no data, and is never breached.
	req, _ := thresholds.Parse("requests >= 1")
	for name, th := range map[string]thresholds.Threshold{
		"hand-built metric": {Metric: thresholds.Metric{Name: "requests", Kind: thresholds.KindCount}, Op: thresholds.OpGE},
		"unknown op":        {Metric: req.Metric, Op: thresholds.Op(42), Value: 1},
		"NaN":               {Metric: req.Metric, Op: thresholds.OpGE, Value: math.NaN()},
		"negative":          {Metric: req.Metric, Op: thresholds.OpGE, Value: -1},
		"fraction count":    {Metric: req.Metric, Op: thresholds.OpGE, Value: 0.5},
	} {
		r := thresholds.Evaluate(&thresholds.Snapshot{Test: thresholds.Scope{Requests: 0}}, []thresholds.Bound{{Threshold: th}})
		if r[0].Status != thresholds.StatusNoData || len(thresholds.Breached(r)) != 0 {
			t.Errorf("%s: %+v", name, r[0])
		}
	}
}

func TestVerdictHelpers(t *testing.T) {
	p := thresholds.Result{Status: thresholds.StatusPassed, Passed: true}
	f := thresholds.Result{Status: thresholds.StatusFailed, Expr: "f"}
	n := thresholds.Result{Status: thresholds.StatusNoData, Expr: "n"}
	if !thresholds.AllPassed(nil) || !thresholds.AllPassed([]thresholds.Result{p, p}) {
		t.Error("AllPassed false for passing results")
	}
	for _, rs := range [][]thresholds.Result{{p, f}, {n}, {p, n, p}} {
		if thresholds.AllPassed(rs) {
			t.Errorf("AllPassed(%+v)", rs)
		}
	}
	if got := thresholds.Breached([]thresholds.Result{p, f, n, f}); len(got) != 2 || got[0].Expr != "f" || got[1].Expr != "f" {
		t.Errorf("Breached = %+v", got)
	}
	if got := thresholds.Breached([]thresholds.Result{p, n}); got != nil {
		t.Errorf("Breached = %+v", got)
	}
}

// == and its neighbours at the tolerance boundary, through Evaluate, for
// every kind.
func TestEvaluateBoundaries(t *testing.T) {
	window := func(sc thresholds.Scope, d time.Duration) *thresholds.Snapshot {
		return &thresholds.Snapshot{Start: t0, End: t0.Add(d), Test: sc}
	}
	avg := func(sum, count int64) thresholds.Scope {
		return thresholds.Scope{Duration: thresholds.Distribution{Count: count, SumUs: sum, MinUs: 1, MaxUs: sum}}
	}
	cases := []struct {
		s    *thresholds.Snapshot
		expr string
		want thresholds.Status
	}{
		// Durations: 200000.0001us is within 0.0002us of 200ms.
		{window(avg(200_000*10_000+1, 10_000), time.Second), "avg == 200ms", thresholds.StatusPassed},
		{window(avg(200_000*10_000+1, 10_000), time.Second), "avg > 200ms", thresholds.StatusFailed},
		{window(avg(200_000*10_000+1, 10_000), time.Second), "avg <= 200ms", thresholds.StatusPassed},
		{window(avg(200_000*10_000+1, 10_000), time.Second), "avg < 200ms", thresholds.StatusFailed},
		{window(avg(2_000_001, 10), time.Second), "avg == 200ms", thresholds.StatusFailed}, // 0.1us off
		{window(avg(2_000_001, 10), time.Second), "avg > 200ms", thresholds.StatusPassed},
		// Rates: 1/3 against 33.333333333333%.
		{window(thresholds.Scope{Requests: 3, Failed: 1}, time.Second), "error_rate == 33.333333333333%", thresholds.StatusPassed},
		{window(thresholds.Scope{Requests: 3, Failed: 1}, time.Second), "error_rate > 33.333333333333%", thresholds.StatusFailed},
		{window(thresholds.Scope{Requests: 3, Failed: 1}, time.Second), "error_rate >= 33.333333333333%", thresholds.StatusPassed},
		{window(thresholds.Scope{Requests: 3, Failed: 1}, time.Second), "error_rate < 33.333333333333%", thresholds.StatusFailed},
		{window(thresholds.Scope{Requests: 3, Failed: 1}, time.Second), "error_rate == 33.3333%", thresholds.StatusFailed},
		{window(thresholds.Scope{Requests: 10_000, Failed: 7}, time.Second), "error_rate == 0.07%", thresholds.StatusPassed},
		{window(thresholds.Scope{Requests: 10_001, Failed: 7}, time.Second), "error_rate == 0.07%", thresholds.StatusFailed},
		// Throughput: 1000 requests over 3s.
		{window(thresholds.Scope{Requests: 1000}, 3*time.Second), "rps == 333.333333333333", thresholds.StatusPassed},
		{window(thresholds.Scope{Requests: 1000}, 3*time.Second), "rps < 333.333333333333", thresholds.StatusFailed},
		{window(thresholds.Scope{Requests: 1000}, 3*time.Second), "rps == 333.3333", thresholds.StatusFailed},
		// Counts and sizes are exact at any scale.
		{window(thresholds.Scope{Requests: 1_000_000_000}, time.Second), "requests == 1000000000", thresholds.StatusPassed},
		{window(thresholds.Scope{Requests: 1_000_000_001}, time.Second), "requests == 1000000000", thresholds.StatusFailed},
		{window(thresholds.Scope{Requests: 1_000_000_001}, time.Second), "requests > 1000000000", thresholds.StatusPassed},
		{window(thresholds.Scope{BytesSent: 5_000_000_005}, time.Second), "bytes_sent == 5gb", thresholds.StatusFailed},
		{window(thresholds.Scope{BytesSent: 5_000_000_000}, time.Second), "bytes_sent == 5gb", thresholds.StatusPassed},
		{window(thresholds.Scope{BytesSent: 5_000_000_005}, time.Second), "bytes_sent >= 5gb", thresholds.StatusPassed},
		{window(thresholds.Scope{BytesSent: 5_000_000_005}, time.Second), "bytes_sent <= 5gb", thresholds.StatusFailed},
	}
	for _, tc := range cases {
		if r := evaluate(t, tc.s, thresholds.Ref{}, tc.expr); r.Status != tc.want {
			t.Errorf("%s: %s (actual %v), want %s", tc.expr, r.Status, deref(r.Actual), tc.want)
		}
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// Every metric reads the right measurement, in the documented unit.
func TestEvaluateEveryMetric(t *testing.T) {
	sc := thresholds.Scope{
		Requests: 400, Failed: 10, BytesSent: 12_345, BytesReceived: 67_890,
		Checks: ptr(200), ChecksPassed: ptr(150), Iterations: ptr(40), DroppedIterations: ptr(2),
		Duration:          dist(t, 1000, 2000, 3000, 4000, 5500),
		TTFB:              dist(t, 100, 300),
		IterationDuration: dist(t, 1_000_000, 2_000_000, 4_000_000),
	}
	s := &thresholds.Snapshot{Start: t0, End: t0.Add(8 * time.Second), Test: sc}
	q := func(d thresholds.Distribution, p float64) float64 { return float64(d.Hist.ValueAtQuantile(p)) / 1000 }
	want := map[string][2]any{
		"avg": {3.1, "ms"}, "min": {1.0, "ms"}, "max": {5.5, "ms"},
		"p50": {q(sc.Duration, 50), "ms"}, "p75": {q(sc.Duration, 75), "ms"}, "p90": {q(sc.Duration, 90), "ms"},
		"p95": {q(sc.Duration, 95), "ms"}, "p99": {q(sc.Duration, 99), "ms"}, "p99.9": {q(sc.Duration, 99.9), "ms"},
		"ttfb": {0.2, "ms"}, "ttfb_avg": {0.2, "ms"}, "ttfb_min": {0.1, "ms"}, "ttfb_max": {0.3, "ms"},
		"iteration_duration": {7000.0 / 3, "ms"}, "iteration_duration_min": {1000.0, "ms"},
		"error_rate": {2.5, "%"}, "check_rate": {75.0, "%"},
		"rps": {50.0, "req/s"}, "requests": {400.0, ""}, "failed": {10.0, ""},
		"iterations": {40.0, ""}, "dropped_iterations": {2.0, ""},
		"bytes_sent": {12345.0, "bytes"}, "bytes_received": {67890.0, "bytes"},
	}
	for _, p := range []string{"p50", "p75", "p90", "p95", "p99", "p99.9"} {
		want["ttfb_"+p] = [2]any{q(sc.TTFB, mustPercent(p)), "ms"}
		want["iteration_duration_"+p] = [2]any{q(sc.IterationDuration, mustPercent(p)), "ms"}
	}
	want["iteration_duration_avg"] = want["iteration_duration"]
	want["iteration_duration_max"] = [2]any{4000.0, "ms"}
	sample := map[thresholds.Kind]string{
		thresholds.KindDuration: "1s", thresholds.KindRate: "1%", thresholds.KindCount: "1",
		thresholds.KindSize: "1", thresholds.KindThroughput: "1",
	}
	for _, name := range thresholds.Metrics() {
		w, ok := want[name]
		if !ok {
			t.Errorf("no expectation for %s", name)
			continue
		}
		r := evaluate(t, s, thresholds.Ref{}, name+" > "+sample[kindOf(name)])
		got := actual(t, r)
		wantV := w[0].(float64)
		if name == "iteration_duration" || name == "iteration_duration_avg" {
			wantV = math.Round(7_000_000.0/3) / 1000 // µs precision
		}
		if got != wantV || r.Unit != w[1] {
			t.Errorf("%s: actual %v %q, want %v %q", name, got, r.Unit, wantV, w[1])
		}
	}
}

func mustPercent(p string) float64 {
	switch p {
	case "p50":
		return 50
	case "p75":
		return 75
	case "p90":
		return 90
	case "p95":
		return 95
	case "p99":
		return 99
	}
	return 99.9
}

// The JSON of a result is the public wire form.
func TestResultJSON(t *testing.T) {
	s := &thresholds.Snapshot{Start: t0, End: t0.Add(10 * time.Second),
		Test: thresholds.Scope{Requests: 1000, Failed: 2, BytesReceived: 2048, Duration: dist(t, 1500, 2500)},
		Scenarios: map[string]*thresholds.ScenarioScope{"browse": {
			Scope: thresholds.Scope{Requests: 10},
			Steps: map[string]*thresholds.Scope{"home": {Requests: 10}},
		}},
	}
	var bounds []thresholds.Bound
	for _, b := range []struct {
		ref  thresholds.Ref
		expr string
	}{
		{thresholds.Ref{}, "avg<1.5ms"},
		{thresholds.Ref{}, "error_rate < 1%"},
		{thresholds.Ref{}, "bytes_received <= 2kib"},
		{thresholds.Ref{}, "rps>=100"},
		{thresholds.Ref{Scenario: "browse"}, "requests > 100"},
		{thresholds.Ref{Scenario: "browse", Step: "home"}, "p95 < 1s"},
	} {
		th, err := thresholds.Parse(b.expr)
		if err != nil {
			t.Fatal(err)
		}
		bounds = append(bounds, thresholds.Bound{Ref: b.ref, Threshold: th})
	}
	rs := thresholds.Evaluate(s, bounds)
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rs); err != nil {
		t.Fatal(err)
	}
	want := `[
  {
    "scope": "test",
    "expr": "avg<1.5ms",
    "actual": 2,
    "unit": "ms",
    "status": "failed",
    "passed": false
  },
  {
    "scope": "test",
    "expr": "error_rate < 1%",
    "actual": 0.2,
    "unit": "%",
    "status": "passed",
    "passed": true
  },
  {
    "scope": "test",
    "expr": "bytes_received <= 2kib",
    "actual": 2048,
    "unit": "bytes",
    "status": "passed",
    "passed": true
  },
  {
    "scope": "test",
    "expr": "rps>=100",
    "actual": 100,
    "unit": "req/s",
    "status": "passed",
    "passed": true
  },
  {
    "scope": "scenario",
    "scenario": "browse",
    "expr": "requests > 100",
    "actual": 10,
    "unit": "",
    "status": "failed",
    "passed": false
  },
  {
    "scope": "step",
    "scenario": "browse",
    "step": "home",
    "expr": "p95 < 1s",
    "actual": null,
    "unit": "ms",
    "status": "no_data",
    "passed": false
  }
]
`
	if buf.String() != want {
		t.Errorf("JSON:\n%s\nwant:\n%s", buf.String(), want)
	}
	var back []thresholds.Result
	if err := json.Unmarshal([]byte(want), &back); err != nil || !reflect.DeepEqual(back, rs) {
		t.Errorf("decoded %+v, %v", back, err)
	}
	// Results do not share actual values.
	if *rs[0].Actual = 99; *rs[1].Actual == 99 {
		t.Error("results share an actual")
	}
}

// Evaluate does not modify the snapshot or the bounds.
func TestEvaluateIsPure(t *testing.T) {
	s := &thresholds.Snapshot{Start: t0, End: t0.Add(time.Second), Test: thresholds.Scope{Requests: 5, Duration: dist(t, values(5, 100)...)}}
	before := deepCopy(s)
	th, _ := thresholds.Parse("p99 < 1s")
	bounds := []thresholds.Bound{{Threshold: th}, {Ref: thresholds.Ref{Scenario: "x", Step: "y"}, Threshold: th}}
	boundsBefore := append([]thresholds.Bound(nil), bounds...)
	r1 := thresholds.Evaluate(s, bounds)
	r2 := thresholds.Evaluate(s, bounds)
	if !equalSnapshots(s, before) || !reflect.DeepEqual(bounds, boundsBefore) || !reflect.DeepEqual(r1, r2) {
		t.Error("Evaluate is not pure")
	}
	if s.Scenarios != nil {
		t.Error("Evaluate created scopes")
	}
}

// A snapshot built directly is not checked by Merge or Add: a scope that
// breaks the invariants gives no_data for every threshold on it, while
// other scopes evaluate normally.
func TestEvaluateUncheckedScopes(t *testing.T) {
	bad := map[string]thresholds.Scope{
		"failed above requests":  {Requests: 1, Failed: 5},
		"passed above checks":    {Requests: 1, Checks: ptr(1), ChecksPassed: ptr(3)},
		"negative counter":       {Requests: -1},
		"histogram above count":  {Requests: 1, Duration: thresholds.Distribution{Count: 1, SumUs: 30, MinUs: 10, MaxUs: 20, Hist: dist(t, 10, 20).Hist}},
		"max below min":          {Requests: 1, Duration: thresholds.Distribution{Count: 1, SumUs: 5, MinUs: 5, MaxUs: 4}},
		"other histogram config": {Requests: 1, TTFB: thresholds.Distribution{Hist: hdrhistogram.New(1, 1_000_000, 3)}},
	}
	for name, sc := range bad {
		s := &thresholds.Snapshot{Start: t0, End: t0.Add(time.Second), Test: thresholds.Scope{Requests: 10, Failed: 1},
			Scenarios: map[string]*thresholds.ScenarioScope{"s": {Scope: sc}}}
		for _, expr := range []string{"error_rate < 100%", "requests > 0", "rps > 0", "check_rate > 0%", "max < 1s", "failed == 0"} {
			if r := evaluate(t, s, thresholds.Ref{Scenario: "s"}, expr); r.Status != thresholds.StatusNoData || r.Actual != nil {
				t.Errorf("%s, %s: %+v", name, expr, r)
			}
		}
		if got := actual(t, evaluate(t, s, thresholds.Ref{}, "error_rate < 50%")); got != 10 {
			t.Errorf("%s: the valid test scope reads error_rate %v", name, got)
		}
	}
}

// The example in the package documentation: a percentile reads the top of
// its HDR bucket, so values of exactly 500ms give a p95 of 500.223ms.
func TestPercentileReadsBucketTop(t *testing.T) {
	d := dist(t, 500_000, 500_000, 500_000)
	s := &thresholds.Snapshot{Start: t0, End: t0.Add(time.Second), Test: thresholds.Scope{Requests: 3, Duration: d}}
	want := map[string]thresholds.Status{
		"p95 == 500ms": thresholds.StatusFailed,
		"p95 <= 500ms": thresholds.StatusFailed,
		"p95 < 501ms":  thresholds.StatusPassed,
		"max == 500ms": thresholds.StatusPassed, // exact
	}
	for expr, status := range want {
		r := evaluate(t, s, thresholds.Ref{}, expr)
		if r.Status != status {
			t.Errorf("%s: %s (actual %v), want %s", expr, r.Status, deref(r.Actual), status)
		}
	}
	if got := actual(t, evaluate(t, s, thresholds.Ref{}, "p95 < 1s")); got != 500.223 {
		t.Errorf("p95 = %vms, want 500.223ms", got)
	}
}
