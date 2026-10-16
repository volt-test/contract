package thresholds

import "testing"

// T12: the == tolerance and the examples in the package documentation.
func TestEqualWithin(t *testing.T) {
	cases := []struct {
		a, b float64
		want bool
	}{
		// "dropped_iterations == 0": counts compare exactly.
		{0, 0, true},
		{1, 0, false},
		{3, 3, true},
		// "p95 == 200ms" (200000us): within 0.0002us of 200000us.
		{200000, 200000, true},
		{200000.0001, 200000, true},
		{199999.9999, 200000, true},
		{200000.0003, 200000, false},
		{199999.9997, 200000, false},
		// Near zero the tolerance is absolute, 1e-9.
		{1e-10, 0, true},
		{0, 5e-10, true},
		{2e-9, 0, false},
		// 0.07% parsed and measured the same way.
		{0.07 / 100, 0.0007, true},
	}
	for _, tc := range cases {
		if got := equalWithin(tc.a, tc.b); got != tc.want {
			t.Errorf("equalWithin(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
		if got := equalWithin(tc.b, tc.a); got != tc.want {
			t.Errorf("equalWithin(%v, %v) is not symmetric", tc.b, tc.a)
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
