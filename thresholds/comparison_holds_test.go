package thresholds

import (
	"math"
	"testing"
)

// Holds is the comparison a threshold of the same kind makes: for every
// operator (and unknown ones), kind and a grid of measured values around
// each bound, inside and outside the == tolerance.
func TestComparisonHoldsParity(t *testing.T) {
	ops := []Op{OpLT, OpLE, OpGT, OpGE, OpEQ, 0, Op(9)}
	kinds := []Kind{KindDuration, KindSize, KindCount}
	bounds := []float64{0, 1, 500, 200000, 1e9, 5e9, 1 << 53}
	for _, k := range kinds {
		for _, op := range ops {
			for _, b := range bounds {
				c := Comparison{Kind: k, Op: op, Value: b}
				actuals := []float64{
					b, b + 1, b - 1, b + 0.5, b - 0.5,
					b * (1 + 1e-13), b * (1 - 1e-13), // inside the duration tolerance
					b + 1e-10, b - 1e-10, // inside the absolute tolerance near zero
					b * (1 + 1e-6), b * (1 - 1e-6), // outside it
					0, 1e18, -1, math.Inf(1), math.Inf(-1), math.NaN(),
				}
				for _, a := range actuals {
					if got, want := c.Holds(a), holds(op, k, a, b); got != want {
						t.Errorf("%+v.Holds(%v) = %v, holds() = %v", c, a, got, want)
					}
				}
			}
		}
	}
}

// The == rule decides the edges: durations compare with a tolerance,
// counts and sizes exactly.
func TestComparisonHoldsEdges(t *testing.T) {
	cases := []struct {
		kind   Kind
		op     Op
		bound  float64
		actual float64
		want   bool
	}{
		// 199999.9999us is equal to 200000us for durations, so not below it.
		{KindDuration, OpLT, 200000, 199999.9999, false},
		{KindDuration, OpLE, 200000, 200000.0001, true},
		{KindDuration, OpEQ, 200000, 200000.0001, true},
		{KindDuration, OpGT, 200000, 200000.0001, false},
		{KindDuration, OpLT, 200000, 199999.9997, true},
		{KindDuration, OpLT, 500000, 499999, true},
		{KindDuration, OpLT, 500000, 500000, false},
		// Counts and sizes are exact at any scale.
		{KindCount, OpLT, 1e9, 1e9 - 1, true},
		{KindCount, OpEQ, 1e9, 1e9 + 1, false},
		{KindCount, OpGT, 0, 1, true},
		{KindCount, OpGT, 0, 0, false},
		{KindSize, OpLT, 50000, 49999, true},
		{KindSize, OpLT, 50000, 50000, false},
		{KindSize, OpGE, 5e9, 5e9 - 5, false},
		// NaN and unknown operators never hold.
		{KindDuration, OpLE, 1, math.NaN(), false},
		{KindCount, OpEQ, 0, math.NaN(), false},
		{KindCount, 0, 0, 0, false},
	}
	for _, tc := range cases {
		c := Comparison{Kind: tc.kind, Op: tc.op, Value: tc.bound}
		if got := c.Holds(tc.actual); got != tc.want {
			t.Errorf("%+v.Holds(%v) = %v, want %v", c, tc.actual, got, tc.want)
		}
	}
	if (Comparison{}).Holds(0) {
		t.Error("the zero Comparison holds")
	}
}

// A parsed comparison and the threshold with the same text after a
// metric of the same kind give the same verdict.
func TestComparisonHoldsLikeThreshold(t *testing.T) {
	cases := []struct {
		kind   Kind
		metric string
		s      string
	}{
		{KindDuration, "p95", "< 500ms"},
		{KindDuration, "p95", "== 200ms"},
		{KindDuration, "p95", ">= 1.5s"},
		{KindSize, "bytes_received", "< 50kb"},
		{KindSize, "bytes_received", "<= 1kib"},
		{KindCount, "requests", "> 0"},
		{KindCount, "requests", "== 3"},
	}
	for _, tc := range cases {
		c, err := ParseComparison(tc.kind, tc.s)
		if err != nil {
			t.Fatal(err)
		}
		th, err := Parse(tc.metric + " " + tc.s)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range []float64{0, 1, 3, 1024, 49999, 50000, 199999.9999, 200000, 500000, 1.5e6, 1e9} {
			if got, want := c.Holds(a), holds(th.Op, th.Metric.Kind, a, th.Value); got != want {
				t.Errorf("%q.Holds(%v) = %v, threshold %q gives %v", tc.s, a, got, th.Raw, want)
			}
		}
	}
}
