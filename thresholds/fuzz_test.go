package thresholds_test

import (
	"errors"
	"testing"

	"github.com/volt-test/contract/thresholds"
)

// T11: Parse never panics; whatever it accepts prints in a canonical form
// that parses back to the same threshold.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", " ", "p95 < 500ms", "p99<=1s", "p99.9 > 0.5us", "ttfb_p99.9 == 1µs", "iteration_duration >= 2m",
		"error_rate < 0.07%", "check_rate > 99.99%", "requests > 1000", "dropped_iterations == 0",
		"rps > 0.25", "bytes_sent < 50kb", "bytes_received < 1.5GiB", "p95 = 5ms", "p95 != 5ms",
		"p95 < -1s", "p95 < .5s", "p95 < 1e3ms", "p95 < 500", "error_rate < 5", "p95 < 5ms x",
		"p95 < 500 ms", "P95 < 5MS", "ttfb_p42 < 1s", "p99.99 < 1s", "error_rate < 101%",
		"p95 < 0." + "0000000000000000000000000000001us", "rps > 99999999999999999999999999999",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, expr string) {
		th, err := thresholds.Parse(expr)
		if err != nil {
			var pe *thresholds.ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("Parse(%q) error %T, want *ParseError", expr, err)
			}
			if pe.Pos < 0 || pe.Pos > len(expr) || pe.Expr != expr {
				t.Fatalf("Parse(%q): bad error %+v", expr, pe)
			}
			// Echoes are cut to about 128 bytes of input whatever its
			// length; quoting can grow each byte to 4.
			if len(err.Error()) > 2048 {
				t.Fatalf("Parse(%q): error is %d bytes", expr, len(err.Error()))
			}
			return
		}
		requireRoundTrip(t, th)
		for _, k := range []thresholds.ScopeKind{thresholds.ScopeTest, thresholds.ScopeScenario} {
			if err := th.ValidFor(k); err != nil {
				t.Fatalf("%q not valid for %s: %v", expr, k, err)
			}
		}
		_ = th.ValidFor(thresholds.ScopeStep)
	})
}
