package thresholds_test

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/volt-test/contract/thresholds"
)

// The catalog, literally.
func TestMetrics(t *testing.T) {
	want := []string{
		"avg", "bytes_received", "bytes_sent", "check_rate", "dropped_iterations", "error_rate", "failed",
		"iteration_duration", "iteration_duration_avg", "iteration_duration_max", "iteration_duration_min",
		"iteration_duration_p50", "iteration_duration_p75", "iteration_duration_p90", "iteration_duration_p95",
		"iteration_duration_p99", "iteration_duration_p99.9",
		"iterations", "max", "min",
		"p50", "p75", "p90", "p95", "p99", "p99.9",
		"requests", "rps",
		"ttfb", "ttfb_avg", "ttfb_max", "ttfb_min", "ttfb_p50", "ttfb_p75", "ttfb_p90", "ttfb_p95", "ttfb_p99", "ttfb_p99.9",
	}
	got := thresholds.Metrics()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Metrics() = %q\nwant        %q", got, want)
	}
	got[0] = "changed"
	if thresholds.Metrics()[0] != "avg" {
		t.Error("Metrics returns shared state")
	}
}

// kindOf is the kind each metric must have, by name.
func kindOf(name string) thresholds.Kind {
	switch name {
	case "error_rate", "check_rate":
		return thresholds.KindRate
	case "requests", "failed", "iterations", "dropped_iterations":
		return thresholds.KindCount
	case "bytes_sent", "bytes_received":
		return thresholds.KindSize
	case "rps":
		return thresholds.KindThroughput
	}
	return thresholds.KindDuration
}

// Every metric parses with a value of its kind.
func TestParseEveryMetric(t *testing.T) {
	values := map[thresholds.Kind]struct {
		text string
		want float64
	}{
		thresholds.KindDuration:   {"250ms", 250000},
		thresholds.KindRate:       {"2.5%", 0.025},
		thresholds.KindCount:      {"10", 10},
		thresholds.KindSize:       {"50kb", 50000},
		thresholds.KindThroughput: {"120.5", 120.5},
	}
	for _, name := range thresholds.Metrics() {
		kind := kindOf(name)
		v := values[kind]
		expr := name + " < " + v.text
		th, err := thresholds.Parse(expr)
		if err != nil {
			t.Errorf("Parse(%q): %v", expr, err)
			continue
		}
		if th.Metric.Name != name || th.Metric.Kind != kind || th.Op != thresholds.OpLT || th.Value != v.want || th.Raw != expr {
			t.Errorf("Parse(%q) = %+v, want metric %s (%s) < %v", expr, th, name, kind, v.want)
		}
	}

	// The verification example from the issue.
	th, err := thresholds.Parse("dropped_iterations == 0")
	if err != nil {
		t.Fatal(err)
	}
	if th.Metric.Name != "dropped_iterations" || th.Metric.Kind != thresholds.KindCount || th.Op != thresholds.OpEQ || th.Value != 0 {
		t.Errorf("dropped_iterations == 0 parsed as %+v", th)
	}
	if err := th.ValidFor(thresholds.ScopeTest); err != nil {
		t.Errorf("dropped_iterations == 0 on the test: %v", err)
	}
}

// Every operator, with and without spaces.
func TestParseOperators(t *testing.T) {
	ops := map[string]thresholds.Op{"<": thresholds.OpLT, "<=": thresholds.OpLE, ">": thresholds.OpGT, ">=": thresholds.OpGE, "==": thresholds.OpEQ}
	for text, op := range ops {
		if op.String() != text {
			t.Errorf("Op %d prints %q, want %q", op, op.String(), text)
		}
		for _, expr := range []string{
			"p99" + text + "1s",
			"p99 " + text + " 1s",
			"  p99\t" + text + "\n1s\r\n",
			"p99" + text + " 1s",
			"p99 " + text + "1s",
		} {
			th, err := thresholds.Parse(expr)
			if err != nil {
				t.Errorf("Parse(%q): %v", expr, err)
				continue
			}
			if th.Op != op || th.Metric.Name != "p99" || th.Value != 1e6 {
				t.Errorf("Parse(%q) = %+v", expr, th)
			}
		}
	}
}

// Every value form and unit, in any case.
func TestParseValues(t *testing.T) {
	cases := []struct {
		expr string
		want float64
	}{
		// Durations, in microseconds.
		{"p95 < 1us", 1},
		{"p95 < 1µs", 1}, // micro sign U+00B5
		{"p95 < 1μs", 1}, // Greek mu U+03BC
		{"p95 < 1µS", 1},
		{"p95 < 1US", 1}, // upper case
		{"p95 < 2.5us", 2.5},
		{"p95 < 500ms", 500000},
		{"p95 < 500MS", 500000},
		{"p95 < 0.5ms", 500},
		{"p95 < 2s", 2e6},
		{"p95 < 2S", 2e6},
		{"p95 < 1.5s", 1.5e6},
		{"p95 < 1m", 60e6},
		{"p95 < 1M", 60e6},
		{"p95 < 1.5m", 90e6},
		{"p95 < 0s", 0},
		{"ttfb_p99.9 <= 007ms", 7000},
		// Rates, as fractions.
		{"error_rate < 1%", 0.01},
		{"error_rate < 0.5%", 0.005},
		{"error_rate <= 100%", 1},
		{"error_rate == 0%", 0},
		{"check_rate > 99.9%", 0.999},
		{"check_rate > 0.07%", 0.0007},
		// Counts.
		{"requests > 1000", 1000},
		{"failed == 0", 0},
		{"iterations >= 10.0", 10},
		{"dropped_iterations == 0", 0},
		// Throughput.
		{"rps > 100", 100},
		{"rps > 0.25", 0.25},
		// Sizes, in bytes.
		{"bytes_sent < 100", 100},
		{"bytes_sent < 100b", 100},
		{"bytes_sent < 100B", 100},
		{"bytes_sent < 50kb", 50000},
		{"bytes_sent < 50KB", 50000},
		{"bytes_sent < 1.5mb", 1.5e6},
		{"bytes_sent < 2gb", 2e9},
		{"bytes_received < 1kib", 1024},
		{"bytes_received < 1KiB", 1024},
		{"bytes_received < 0.5mib", 524288},
		{"bytes_sent < 1.001kb", 1001},
		{"bytes_sent < 0.25kib", 256},
		{"bytes_sent < 0.0009765625kib", 1},
		{"bytes_sent < 1.5mib", 1572864},
		{"bytes_sent < 1000000000000.000000000kb", 1e15},
		{"bytes_sent < 1.0b", 1},
		// Tiny but non-zero durations, rates and throughput are kept.
		{"p95 < 0.000001us", 1e-6},
		{"rps > 0.0001", 1e-4},
		{"bytes_received < 2gib", 2 << 30},
		{"bytes_received < 2GIB", 2 << 30},
	}
	for _, tc := range cases {
		th, err := thresholds.Parse(tc.expr)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.expr, err)
			continue
		}
		if th.Value != tc.want {
			t.Errorf("Parse(%q).Value = %v, want %v", tc.expr, th.Value, tc.want)
		}
	}
}

// Invalid expressions, with the position and message of the error.
func TestParseErrors(t *testing.T) {
	cases := []struct {
		expr string
		pos  int
		msg  string
	}{
		{"", 0, "the expression is empty"},
		{"   ", 3, "the expression is empty"},
		{"< 5ms", 0, "missing metric"},
		{"p98 < 5ms", 0, `unknown metric "p98"; the percentiles are p50, p75, p90, p95, p99 and p99.9`},
		{"p99.99 < 5ms", 0, `unknown metric "p99.99"; the percentiles are`},
		{"ttfb_p42 < 5ms", 0, `unknown metric "ttfb_p42"; ttfb takes avg, min, max, p50, p75, p90, p95, p99 or p99.9`},
		{"iteration_duration_p1 < 5ms", 0, `iteration_duration takes avg`},
		{"P95 < 5ms", 0, `metric names are lowercase: "p95"`},
		{"latency < 5ms", 0, `unknown metric "latency"; the metrics are avg`},
		{"p95", 3, "missing operator after p95"},
		{"p95 5ms", 4, `expected an operator (<, <=, >, >= or ==) after p95, got "5"`},
		{"p95 = 5ms", 4, `use "==" to compare for equality, not "="`},
		{"p95=5ms", 3, `use "==" to compare for equality`},
		{"p95 != 5ms", 4, `"!=" is not supported`},
		{"p95 =< 5ms", 4, `did you mean "<="?`},
		{"p95 => 5ms", 4, `did you mean ">="?`},
		{"p95 <> 5ms", 5, `expected a number after "<", got ">"`},
		{"p95 === 5ms", 6, `expected a number after "==", got "="`},
		{"p95 <", 5, `missing value after "<"`},
		{"p95 < ", 6, `missing value after "<"`},
		{"p95 < -5ms", 6, "must not be negative"},
		{"failed > -1", 9, "must not be negative"},
		{"p95 < +5ms", 6, `remove the "+" sign`},
		{"p95 < .5s", 6, "write a digit before the decimal point"},
		{"p95 < 5.s", 7, "a decimal point must be followed by digits"},
		{"p95 < 5.", 7, "a decimal point must be followed by digits"},
		{"p95 < 1.2.3s", 9, "at most one decimal point"},
		{"p95 < 1e3ms", 7, "exponents are not supported"},
		{"p95 < 1E+3ms", 7, "exponents are not supported"},
		{"rps > 1e-3", 7, "exponents are not supported"},
		{"p95 < 500ms garbage", 12, `unexpected "g" after the value`},
		{"p95 < 500ms;", 11, `unexpected ";" after the value`},
		{"p95 < 500ms < 1s", 12, `unexpected "<" after the value`},
		{"p95 < 500 ms", 9, "remove the space between the number and its unit"},
		{"p95 < 5ms5", 9, `unexpected "5" after the value`},
		{"p95 < x", 6, `expected a number after "<", got "x"`},
		// Unit and kind mismatches, both ways.
		{"p95 < 500", 6, `p95 is a duration and needs a unit (us, ms, s or m); did you mean "500ms"?`},
		{"ttfb < 1", 7, "ttfb is a duration and needs a unit"},
		{"p95 < 5ns", 7, `"ns" is not supported`},
		{"p95 < 5%", 7, `p95 is a duration; use us, ms, s or m, not "%"`},
		{"p95 < 5kb", 7, `p95 is a duration; use us, ms, s or m, not "kb"`},
		{"p95 < 5h", 7, `not "h"`},
		{"error_rate < 5", 13, `error_rate is a rate and needs "%"; did you mean "5%"?`},
		{"error_rate < 0.05", 13, `did you mean "0.05%" (or "5%" if 0.05 is a fraction)?`},
		{"check_rate > 1", 13, `did you mean "1%" (or "100%" if 1 is a fraction)?`},
		{"error_rate < 5ms", 14, `error_rate is a rate; write a percentage such as 5%, not "ms"`},
		{"error_rate < 101%", 13, "a rate cannot exceed 100%"},
		{"requests > 10ms", 13, `requests is a count; write a plain number such as 100, without "ms"`},
		{"failed < 1%", 10, `failed is a count; write a plain number`},
		{"iterations > 1.5", 13, "iterations is a count; use a whole number"},
		{"rps > 10%", 8, `rps is requests per second; write a plain number such as 100, without "%"`},
		{"rps > 10s", 8, `without "s"`},
		{"bytes_sent < 5ms", 14, `bytes_sent is a size; use b, kb, mb, gb, kib, mib or gib (or a plain number of bytes), not "ms"`},
		{"bytes_sent < 5%", 14, `bytes_sent is a size`},
		// Sizes are whole numbers of bytes.
		{"bytes_sent < 0.5", 13, "bytes_sent is a size; use a whole number of bytes"},
		{"bytes_sent < 1.5b", 13, "bytes_sent is a size; use a whole number of bytes"},
		{"bytes_sent < 1000000000.000000001", 13, "bytes_sent is a size; use a whole number of bytes"},
		{"bytes_sent < 0.1kib", 13, `use a whole number of bytes ("0.1kib" is "102.4" bytes)`},
		{"bytes_sent < 1.0001KB", 13, `("1.0001KB" is "1000.1" bytes)`},
		// Exact even where a float64 would round to a whole number.
		{"bytes_sent < 1.000000000000000001gb", 13, `("1.000000000000000001gb" is "1000000000.000000001" bytes)`},
		{"bytes_sent < 1000000000000.000000001kb", 13, `("1000000000000.000000001kb" is "1000000000000000.000001" bytes)`},
		{"bytes_sent < 1.0000000000000000001gib", 13, `("1.0000000000000000001gib" is "1073741824.0000000001073741824" bytes)`},
		{"bytes_sent < 4096.000000000000000001kib", 13, "use a whole number of bytes"},
		{"bytes_sent < 0.0000000001" + strings.Repeat("0", 300) + "1kb", 13, `use a whole number of bytes ("0.0000000001000`},
		// Greek capital Mu reads as an ASCII M, so it is not a micro sign.
		{"p95 < 1\u039cs", 7, "\"\u039cs\" starts with the Greek capital letter Mu, not an ASCII M; write \"us\" (or \"µs\") for microseconds, or \"ms\" for milliseconds"},
		{"p95 < 500\u039cS", 9, "Greek capital letter Mu"},
		{"bytes_sent < 5\u039cB", 14, "\"\u039cB\" starts with the Greek capital letter Mu, not an ASCII M; write \"mb\" for megabytes (or \"mib\" for mebibytes)"},
		{"bytes_sent < 5\u039cib", 14, `write "mb" for megabytes`},
		{"requests > 5\u039c", 12, "requests is a count"},
		// Units are ASCII apart from the micro forms: the Kelvin sign
		// (U+212A) folds to "k" in Unicode but is not a unit here, nor are
		// other look-alikes.
		{"bytes_sent < 5\u212ab", 14, "bytes_sent is a size; use b, kb, mb, gb, kib, mib or gib (or a plain number of bytes), not \"\u212ab\""},
		{"bytes_sent < 5\u212aib", 14, "is a size"},
		{"p95 < 5m\u0455", 7, "p95 is a duration"},       // Cyrillic dze
		{"p95 < 5\u00b5\u00b5s", 7, "p95 is a duration"}, // two micro signs
		{"p95 < 5u\u00b5s", 7, "p95 is a duration"},
		// A non-zero value that rounds to zero.
		{"p95 < 0." + strings.Repeat("0", 330) + "1us", 6, "the value is too small"},
		{"error_rate < 0." + strings.Repeat("0", 330) + "1%", 13, "the value is too small"},
		// Values that do not fit a float64.
		{"rps > 1" + strings.Repeat("0", 400), 6, "the value is too large"},
		{"bytes_sent < 1" + strings.Repeat("0", 300) + "gib", 13, "the value is too large"},
		{"p95 < 1" + strings.Repeat("0", 302) + "m", 6, "the value is too large"},
	}
	for _, tc := range cases {
		_, err := thresholds.Parse(tc.expr)
		var pe *thresholds.ParseError
		if !errors.As(err, &pe) {
			t.Errorf("Parse(%q) error = %v, want *ParseError", tc.expr, err)
			continue
		}
		if pe.Expr != tc.expr || pe.Pos != tc.pos || !strings.Contains(pe.Msg, tc.msg) {
			t.Errorf("Parse(%q) = {Pos %d, Msg %q}, want {Pos %d, Msg containing %q}", tc.expr, pe.Pos, pe.Msg, tc.pos, tc.msg)
		}
		if !strings.HasPrefix(err.Error(), "thresholds: invalid threshold ") {
			t.Errorf("Parse(%q) error text %q", tc.expr, err)
		}
	}
}

func TestParseErrorText(t *testing.T) {
	_, err := thresholds.Parse("p95 < 500")
	want := `thresholds: invalid threshold "p95 < 500": p95 is a duration and needs a unit (us, ms, s or m); did you mean "500ms"? (at offset 6)`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v\nwant    %s", err, want)
	}

	// Hints that repeat the number are cut too, however long it is.
	for _, expr := range []string{
		"p90 > " + strings.Repeat("1", 5000),
		"error_rate < " + strings.Repeat("1", 5000),
		"error_rate < 0." + strings.Repeat("1", 5000),
		"bytes_sent < 0." + strings.Repeat("1", 5000) + "kib",
	} {
		_, err := thresholds.Parse(expr)
		if err == nil || len(err.Error()) > 1024 {
			t.Errorf("Parse(%.20q...) error is %d bytes", expr, len(fmt.Sprint(err)))
		}
	}

	// A long expression is cut in the message, on a UTF-8 boundary.
	long := "p95 < 5ms " + strings.Repeat("é", 200)
	_, err = thresholds.Parse(long)
	var pe *thresholds.ParseError
	if !errors.As(err, &pe) || pe.Expr != long {
		t.Fatalf("error = %v", err)
	}
	if len(err.Error()) > 300 {
		t.Errorf("error is %d bytes: %s", len(err.Error()), err)
	}
	if !strings.Contains(err.Error(), `é..."`) || strings.Contains(err.Error(), `\x`) {
		t.Errorf("echo not cut on a rune boundary: %s", err)
	}
}

// Which metrics each scope accepts.
func TestValidFor(t *testing.T) {
	notOnSteps := map[string]bool{"iterations": true, "dropped_iterations": true}
	for _, name := range thresholds.Metrics() {
		if strings.HasPrefix(name, "iteration_duration") {
			notOnSteps[name] = true
		}
	}
	if len(notOnSteps) != 12 {
		t.Fatalf("%d metrics not on steps, want 12", len(notOnSteps))
	}
	sample := map[thresholds.Kind]string{
		thresholds.KindDuration: "1s", thresholds.KindRate: "1%", thresholds.KindCount: "1",
		thresholds.KindSize: "1kb", thresholds.KindThroughput: "1",
	}
	for _, name := range thresholds.Metrics() {
		th, err := thresholds.Parse(name + ">" + sample[kindOf(name)])
		if err != nil {
			t.Fatal(err)
		}
		for _, scope := range []thresholds.ScopeKind{thresholds.ScopeTest, thresholds.ScopeScenario, thresholds.ScopeStep} {
			err := th.ValidFor(scope)
			wantErr := scope == thresholds.ScopeStep && notOnSteps[name]
			if (err != nil) != wantErr {
				t.Errorf("%s.ValidFor(%s) = %v, want error %v", name, scope, err, wantErr)
			}
			if err == nil {
				continue
			}
			var se *thresholds.ScopeError
			if !errors.As(err, &se) || se.Metric != name || se.Scope != thresholds.ScopeStep {
				t.Errorf("%s.ValidFor(step) = %#v", name, err)
			}
			if want := "thresholds: " + name + " is not available on a step; use it on the test or a scenario"; err.Error() != want {
				t.Errorf("error text %q, want %q", err, want)
			}
		}
		if err := th.ValidFor(0); err == nil {
			t.Errorf("%s.ValidFor(0) = nil", name)
		}
	}
	if err := (thresholds.Threshold{}).ValidFor(thresholds.ScopeTest); err == nil {
		t.Error("the zero Threshold is valid")
	}
	// A Metric built by hand is not the catalog's, even with a known name.
	forged := thresholds.Threshold{Metric: thresholds.Metric{Name: "iterations", Kind: thresholds.KindCount}, Op: thresholds.OpGT, Value: 1}
	if err := forged.ValidFor(thresholds.ScopeStep); err == nil || !strings.Contains(err.Error(), "build it with Parse") {
		t.Errorf("hand-built metric: ValidFor(step) = %v", err)
	}
}

func TestRefScope(t *testing.T) {
	cases := map[thresholds.Ref]thresholds.ScopeKind{
		{}:                         thresholds.ScopeTest,
		{Scenario: "s"}:            thresholds.ScopeScenario,
		{Scenario: "s", Step: "a"}: thresholds.ScopeStep,
	}
	for ref, want := range cases {
		if got := ref.Scope(); got != want {
			t.Errorf("%+v.Scope() = %s, want %s", ref, got, want)
		}
	}
}

func TestEnumStrings(t *testing.T) {
	cases := map[string]string{
		thresholds.KindDuration.String():   "duration",
		thresholds.KindRate.String():       "rate",
		thresholds.KindCount.String():      "count",
		thresholds.KindSize.String():       "size",
		thresholds.KindThroughput.String(): "throughput",
		thresholds.Kind(0).String():        "Kind(0)",
		thresholds.Op(0).String():          "Op(0)",
		thresholds.ScopeTest.String():      "test",
		thresholds.ScopeScenario.String():  "scenario",
		thresholds.ScopeStep.String():      "step",
		thresholds.ScopeKind(9).String():   "ScopeKind(9)",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}

// String prints a canonical form that parses back to the same threshold.
func TestString(t *testing.T) {
	cases := map[string]string{
		"p95<0.5s":                    "p95 < 500ms",
		"p95 < 500000us":              "p95 < 500ms",
		"p95<=1.5m":                   "p95 <= 90s",
		"p95 < 999us":                 "p95 < 999us",
		"p95 < 0.001ms":               "p95 < 1us",
		"p95 < 0.0001ms":              "p95 < 0.1us",
		"p95 < 0s":                    "p95 < 0us",
		"ttfb_p99.9 >= 1000ms":        "ttfb_p99.9 >= 1s",
		"ttfb<1s":                     "ttfb < 1s",
		"error_rate<0.07%":            "error_rate < 0.07%",
		"error_rate < 100%":           "error_rate < 100%",
		"check_rate>=99.990%":         "check_rate >= 99.99%",
		"requests==010":               "requests == 10",
		"dropped_iterations == 0":     "dropped_iterations == 0",
		"rps>0.10":                    "rps > 0.1",
		"bytes_sent < 1kib":           "bytes_sent < 1.024kb",
		"bytes_sent < 1500000":        "bytes_sent < 1.5mb",
		"bytes_received<2gib":         "bytes_received < 2.147483648gb",
		"bytes_received < 12b":        "bytes_received < 12b",
		"iteration_duration_max < 2m": "iteration_duration_max < 120s",
	}
	for expr, want := range cases {
		th, err := thresholds.Parse(expr)
		if err != nil {
			t.Fatalf("Parse(%q): %v", expr, err)
		}
		if got := th.String(); got != want {
			t.Errorf("Parse(%q).String() = %q, want %q", expr, got, want)
		}
		requireRoundTrip(t, th)
	}
}

// Values whose float64 form needs care: they must survive String exactly.
func TestStringExact(t *testing.T) {
	for _, expr := range []string{
		"error_rate < 0.07%", "error_rate < 33.333333333333333333%", "check_rate > 0.000000000000000000001%",
		"p95 < 0.1ms", "p95 < 0.3us", "p95 < 123456789.987654321s", "p95 < 0.7m", "p95 < 1.1m",
		"rps > 0.1", "rps > 179769313486231570000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		"bytes_sent < 0.3kb", "bytes_sent < 0.25kib", "bytes_sent < 1.000000000000000000gb", "bytes_sent < 0.0009765625kib",
		"p95 < 0." + strings.Repeat("0", 300) + "1us", "p95 < 0.000000000000000000000000000001us",
	} {
		th, err := thresholds.Parse(expr)
		if err != nil {
			t.Fatalf("Parse(%q): %v", expr, err)
		}
		requireRoundTrip(t, th)
	}
}

func requireRoundTrip(t testing.TB, th thresholds.Threshold) {
	t.Helper()
	s := th.String()
	again, err := thresholds.Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q) (String of %q): %v", s, th.Raw, err)
	}
	if again.Raw != s {
		t.Errorf("Raw = %q, want %q", again.Raw, s)
	}
	again.Raw = th.Raw
	if again != th {
		t.Errorf("Parse(String()) of %q = %+v (value %v), want %+v (value %v)", th.Raw, again, again.Value, th, th.Value)
	}
	if again.String() != s {
		t.Errorf("String is not stable: %q then %q", s, again.String())
	}
	if math.IsNaN(th.Value) || math.IsInf(th.Value, 0) || th.Value < 0 {
		t.Errorf("Value of %q is %v", th.Raw, th.Value)
	}
}
