package thresholds

import (
	"math"
	"testing"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"
)

// countsLen matches the counter array hdrhistogram allocates for the
// snapshot configuration.
func TestCountsLen(t *testing.T) {
	if got := len(NewHistogram().Export().Counts); got != countsLen || countsLen != 23552 {
		t.Errorf("hdrhistogram has %d counters, countsLen is %d", got, countsLen)
	}
	if got := len(hdrhistogram.New(HistogramLowestUs, HistogramHighestUs, HistogramSigFigs).Export().Counts); got != countsLen {
		t.Errorf("hdrhistogram.New has %d counters", got)
	}
}

// The == comparison and the examples in the package documentation.
func TestEqual(t *testing.T) {
	cases := []struct {
		kind Kind
		a, b float64
		want bool
	}{
		// "dropped_iterations == 0": counts compare exactly.
		{KindCount, 0, 0, true},
		{KindCount, 1, 0, false},
		{KindCount, 3, 3, true},
		// Exact at any scale, where a relative tolerance would reach 1.
		{KindCount, 1e9, 1e9 + 1, false},
		{KindCount, 5e9, 5e9 + 5, false},
		{KindSize, 1e9, 1e9 + 1, false},
		{KindSize, 5e9, 5e9 + 5, false},
		{KindSize, 5e9, 5e9, true},
		// "p95 == 200ms" (200000us): within 0.0002us of 200000us.
		{KindDuration, 200000, 200000, true},
		{KindDuration, 200000.0001, 200000, true},
		{KindDuration, 199999.9999, 200000, true},
		{KindDuration, 200000.0003, 200000, false},
		{KindDuration, 199999.9997, 200000, false},
		// Near zero the tolerance is absolute, 1e-9.
		{KindRate, 1e-10, 0, true},
		{KindRate, 0, 5e-10, true},
		{KindRate, 2e-9, 0, false},
		{KindThroughput, 1e-10, 0, true},
		{KindThroughput, 100, 100.000001, false},
		// 0.07% parsed and measured the same way.
		{KindRate, 0.07 / 100, 0.0007, true},
		// An infinite value equals only the same infinity, never a
		// finite bound, however large.
		{KindDuration, math.Inf(1), 500000, false},
		{KindDuration, math.Inf(1), 1e300, false},
		{KindDuration, math.Inf(-1), 0, false},
		{KindDuration, math.Inf(1), math.Inf(1), true},
		{KindDuration, math.Inf(-1), math.Inf(-1), true},
		{KindDuration, math.Inf(1), math.Inf(-1), false},
		{KindRate, math.Inf(1), 0.5, false},
		{KindThroughput, math.Inf(1), 100, false},
		{KindCount, math.Inf(1), 1e9, false},
		{KindSize, math.Inf(1), math.Inf(1), true},
	}
	for _, tc := range cases {
		if got := equal(tc.kind, tc.a, tc.b); got != tc.want {
			t.Errorf("equal(%s, %v, %v) = %v, want %v", tc.kind, tc.a, tc.b, got, tc.want)
		}
		if got := equal(tc.kind, tc.b, tc.a); got != tc.want {
			t.Errorf("equal(%s, %v, %v) is not symmetric", tc.kind, tc.b, tc.a)
		}
	}

	th, err := Parse("p95 == 200ms")
	if err != nil || th.Op != OpEQ || th.Value != 200000 {
		t.Fatalf("Parse(p95 == 200ms) = %+v, %v", th, err)
	}
}

// The catalog's internal fields: plain ttfb and iteration_duration are the
// average, and percentiles carry their percent.
func TestCatalogFields(t *testing.T) {
	for _, n := range []string{"ttfb", "ttfb_avg", "iteration_duration", "iteration_duration_avg", "avg"} {
		if m, _ := lookupMetric(n); m.agg != aggAvg {
			t.Errorf("%s: agg %d, want avg", n, m.agg)
		}
	}
	if catalog["ttfb"].family != catalog["ttfb_avg"].family || catalog["ttfb"].family != famTTFB {
		t.Error("ttfb and ttfb_avg read different data")
	}
	want := map[string]float64{"p50": 50, "p75": 75, "p90": 90, "p95": 95, "p99": 99, "p99.9": 99.9, "ttfb_p99.9": 99.9, "iteration_duration_p50": 50, "max": 0}
	for n, p := range want {
		if catalog[n].percentile != p {
			t.Errorf("%s: percentile %v, want %v", n, catalog[n].percentile, p)
		}
	}
	for n, m := range catalog {
		if m.Name != n || m.Kind == 0 || m.family == 0 {
			t.Errorf("catalog[%q] = %+v", n, m)
		}
		if (m.Kind == KindDuration) != (m.agg != aggNone) {
			t.Errorf("%s: kind %s with agg %d", n, m.Kind, m.agg)
		}
	}
}
