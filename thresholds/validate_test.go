package thresholds_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"

	"github.com/volt-test/contract/thresholds"
)

// Validate reports exactly what Add, Merge and Evaluate check on a single
// scope: every scope case the Add, Merge and Evaluate tests use, plus
// valid ones. Overflow is not a property of one scope, so those cases are
// valid.
func TestScopeValidate(t *testing.T) {
	cases := map[string]struct {
		scope thresholds.Scope
		want  error // nil: valid
	}{
		"zero":                      {thresholds.Scope{}, nil},
		"measured":                  {thresholds.Scope{Requests: 2, Failed: 1, BytesSent: 10, BytesReceived: 20, Checks: ptr(3), ChecksPassed: ptr(3), Iterations: ptr(1), DroppedIterations: ptr(0), Duration: dist(t, 10, 20), TTFB: dist(t, 5)}, nil},
		"empty histograms":          {thresholds.Scope{Duration: thresholds.Distribution{Hist: thresholds.NewHistogram()}, IterationDuration: thresholds.Distribution{Hist: thresholds.NewHistogram()}}, nil},
		"values left out of hist":   {thresholds.Scope{Requests: 2, Duration: thresholds.Distribution{Count: 2, SumUs: 30, MinUs: 10, MaxUs: 20, Hist: dist(t, 10).Hist}}, nil},
		"values without hist":       {thresholds.Scope{Requests: 1, Duration: thresholds.Distribution{Count: 1, SumUs: 5, MinUs: 5, MaxUs: 5}}, nil},
		"passed without checks":     {thresholds.Scope{ChecksPassed: ptr(5)}, nil},
		"overflow":                  {thresholds.Scope{Requests: 1<<63 - 1}, nil},
		"sum overflow in a summary": {thresholds.Scope{Duration: thresholds.Distribution{Count: 1, SumUs: 1<<63 - 1, MinUs: 1, MaxUs: 1}}, nil},

		"negative counter":          {thresholds.Scope{Requests: -1}, thresholds.ErrInvalidScope},
		"negative failed":           {thresholds.Scope{Failed: -1}, thresholds.ErrInvalidScope},
		"negative bytes":            {thresholds.Scope{BytesReceived: -1}, thresholds.ErrInvalidScope},
		"negative optional":         {thresholds.Scope{Checks: ptr(-1)}, thresholds.ErrInvalidScope},
		"negative dropped":          {thresholds.Scope{DroppedIterations: ptr(-1)}, thresholds.ErrInvalidScope},
		"negative count":            {thresholds.Scope{Duration: thresholds.Distribution{Count: -1}}, thresholds.ErrInvalidScope},
		"negative sum":              {thresholds.Scope{TTFB: thresholds.Distribution{SumUs: -1}}, thresholds.ErrInvalidScope},
		"negative min":              {thresholds.Scope{TTFB: thresholds.Distribution{Count: 1, MinUs: -1, MaxUs: 1}}, thresholds.ErrInvalidScope},
		"max below min":             {thresholds.Scope{Requests: 1, Duration: thresholds.Distribution{Count: 1, SumUs: 5, MinUs: 5, MaxUs: 4}}, thresholds.ErrInvalidScope},
		"more failed than requests": {thresholds.Scope{Requests: 1, Failed: 2}, thresholds.ErrInvalidScope},
		"more passed than checks":   {thresholds.Scope{Requests: 1, Checks: ptr(1), ChecksPassed: ptr(3)}, thresholds.ErrInvalidScope},
		"histogram above count":     {thresholds.Scope{Requests: 1, Duration: thresholds.Distribution{Count: 1, SumUs: 30, MinUs: 10, MaxUs: 20, Hist: dist(t, 10, 20).Hist}}, thresholds.ErrInvalidScope},
		"histogram without count":   {thresholds.Scope{Duration: thresholds.Distribution{Hist: dist(t, 5).Hist}}, thresholds.ErrInvalidScope},
		"other sig figs":            {thresholds.Scope{Duration: thresholds.Distribution{Hist: hdrhistogram.New(1, 3_600_000_000, 2)}}, thresholds.ErrHistogramConfig},
		"other highest value":       {thresholds.Scope{TTFB: thresholds.Distribution{Hist: hdrhistogram.New(1, 1_000_000, 3)}}, thresholds.ErrHistogramConfig},
		"other lowest value":        {thresholds.Scope{IterationDuration: thresholds.Distribution{Hist: hdrhistogram.New(1000, 3_600_000_000, 3)}}, thresholds.ErrHistogramConfig},
		"other config with values":  {thresholds.Scope{Requests: 1, Duration: thresholds.Distribution{Count: 1, SumUs: 5, MinUs: 5, MaxUs: 5, Hist: recorded(t, hdrhistogram.New(1, 3_600_000_000, 2), 5)}}, thresholds.ErrHistogramConfig},
	}
	sentinels := []error{thresholds.ErrInvalidScope, thresholds.ErrHistogramConfig, thresholds.ErrOverflow}
	for name, tc := range cases {
		sc := tc.scope
		before, hists := sc, histCounts(&sc)
		err := sc.Validate()
		if tc.want == nil {
			if err != nil {
				t.Errorf("%s: Validate = %v, want nil", name, err)
			}
		} else {
			if !errors.Is(err, tc.want) {
				t.Errorf("%s: Validate = %v, want %v", name, err, tc.want)
			}
			if err != nil && (!strings.HasPrefix(err.Error(), "thresholds: ") || strings.Count(err.Error(), "thresholds: ") != 1) {
				t.Errorf("%s: error text %q", name, err)
			}
		}
		if sc != before || histCounts(&sc) != hists {
			t.Errorf("%s: Validate changed the scope", name)
		}

		// Add checks both sides with the same rules, receiver first. A
		// shallow copy is enough: adding an empty scope records nothing
		// into the receiver's histograms, and Add never modifies its
		// argument.
		r := sc
		sides := map[string]error{"receiver": r.Add(&thresholds.Scope{})}
		if !strings.Contains(name, "overflow") {
			sides["argument"] = (&thresholds.Scope{Requests: 1, Duration: dist(t, 10)}).Add(&sc)
		}
		for side, got := range sides {
			if errText(got) != errText(err) {
				t.Errorf("%s: Add with the scope as %s = %v, Validate = %v", name, side, got, err)
			}
		}

		// Merge rejects the scope at any position, wrapping the same error.
		for where, snap := range map[string]*thresholds.Snapshot{
			"test": node(t, 0, time.Second, sc, nil),
			"step": node(t, 0, time.Second, thresholds.Scope{}, map[string]*thresholds.ScenarioScope{
				"s": {Steps: map[string]*thresholds.Scope{"a": &sc}},
			}),
		} {
			_, merr := thresholds.Merge(snap)
			if (merr == nil) != (err == nil) {
				t.Errorf("%s, in %s: Merge = %v, Validate = %v", name, where, merr, err)
				continue
			}
			for _, s := range sentinels {
				if errors.Is(merr, s) != errors.Is(err, s) {
					t.Errorf("%s, in %s: Merge = %v, Validate = %v", name, where, merr, err)
				}
			}
		}

		// Evaluate gives no_data on exactly the scopes Validate rejects.
		s := &thresholds.Snapshot{Start: t0, End: t0.Add(time.Second), Test: thresholds.Scope{Requests: 1},
			Scenarios: map[string]*thresholds.ScenarioScope{"s": {Scope: sc}}}
		res := evaluate(t, s, thresholds.Ref{Scenario: "s"}, "requests >= 0")
		if (res.Status == thresholds.StatusNoData) != (err != nil) {
			t.Errorf("%s: Evaluate status %s, Validate = %v", name, res.Status, err)
		}
	}
}

func TestScopeValidateNil(t *testing.T) {
	var s *thresholds.Scope
	err := s.Validate()
	if !errors.Is(err, thresholds.ErrInvalidScope) || err.Error() != "thresholds: invalid scope: nil scope" {
		t.Errorf("nil scope: Validate = %v", err)
	}
}

func recorded(t testing.TB, h *hdrhistogram.Histogram, us ...int64) *hdrhistogram.Histogram {
	t.Helper()
	for _, v := range us {
		if err := h.RecordValue(v); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

// histCounts is the number of values in each of s's histograms (-1 for
// none), to tell whether anything recorded into them.
func histCounts(s *thresholds.Scope) [3]int64 {
	var n [3]int64
	for i, d := range []thresholds.Distribution{s.Duration, s.TTFB, s.IterationDuration} {
		n[i] = -1
		if d.Hist != nil {
			n[i] = d.Hist.TotalCount()
		}
	}
	return n
}

func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
