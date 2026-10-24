package thresholds_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/volt-test/contract/thresholds"
)

// comparisonKinds are the kinds ParseComparison accepts, each with a
// metric of that kind, so a comparison can be checked against the
// threshold parser.
var comparisonKinds = []struct {
	kind   thresholds.Kind
	metric string
}{
	{thresholds.KindDuration, "p95"},
	{thresholds.KindSize, "bytes_received"},
	{thresholds.KindCount, "requests"},
}

// Every operator, spacing, value form and unit of the accepted kinds.
func TestParseComparison(t *testing.T) {
	d, sz, n := thresholds.KindDuration, thresholds.KindSize, thresholds.KindCount
	cases := []struct {
		kind  thresholds.Kind
		s     string
		op    thresholds.Op
		value float64
	}{
		// The examples.
		{d, "< 500ms", thresholds.OpLT, 500000},
		{sz, "< 50kb", thresholds.OpLT, 50000},
		{n, "> 0", thresholds.OpGT, 0},
		// Every operator, with and without spaces.
		{d, "<500ms", thresholds.OpLT, 500000},
		{d, "<=1s", thresholds.OpLE, 1e6},
		{d, ">= 1s", thresholds.OpGE, 1e6},
		{d, "> 0.5us", thresholds.OpGT, 0.5},
		{d, "== 200ms", thresholds.OpEQ, 200000},
		{d, " \t< 500ms\r\n", thresholds.OpLT, 500000},
		{d, "<\n500ms", thresholds.OpLT, 500000},
		// Duration units, case-insensitive, with both micro forms.
		{d, "< 1us", thresholds.OpLT, 1},
		{d, "< 1µs", thresholds.OpLT, 1},
		{d, "< 1μs", thresholds.OpLT, 1},
		{d, "< 500MS", thresholds.OpLT, 500000},
		{d, "< 1.5m", thresholds.OpLT, 90e6},
		{d, "< 0s", thresholds.OpLT, 0},
		{d, "< 0.000001us", thresholds.OpLT, 1e-6},
		// Sizes, in bytes.
		{sz, "< 100", thresholds.OpLT, 100},
		{sz, "< 100b", thresholds.OpLT, 100},
		{sz, "<= 1.5MB", thresholds.OpLE, 1.5e6},
		{sz, "< 2gb", thresholds.OpLT, 2e9},
		{sz, "< 1kib", thresholds.OpLT, 1024},
		{sz, "< 1.5GiB", thresholds.OpLT, 1.5 * (1 << 30)},
		{sz, "== 0", thresholds.OpEQ, 0},
		// Counts.
		{n, "== 3", thresholds.OpEQ, 3},
		{n, ">= 10.0", thresholds.OpGE, 10},
		{n, "< 007", thresholds.OpLT, 7},
	}
	for _, tc := range cases {
		c, err := thresholds.ParseComparison(tc.kind, tc.s)
		if err != nil {
			t.Errorf("ParseComparison(%s, %q): %v", tc.kind, tc.s, err)
			continue
		}
		want := thresholds.Comparison{Raw: tc.s, Kind: tc.kind, Op: tc.op, Value: tc.value}
		if c != want {
			t.Errorf("ParseComparison(%s, %q) = %+v, want %+v", tc.kind, tc.s, c, want)
		}
	}
}

// Invalid comparisons, with the position and message of the error.
func TestParseComparisonErrors(t *testing.T) {
	d, sz, n := thresholds.KindDuration, thresholds.KindSize, thresholds.KindCount
	cases := []struct {
		kind thresholds.Kind
		s    string
		pos  int
		msg  string
	}{
		// Grammar.
		{d, "", 0, `the comparison is empty; write <operator> <value>, e.g. "< 500ms"`},
		{sz, "  ", 2, `the comparison is empty; write <operator> <value>, e.g. "< 50kb"`},
		{n, "\t", 1, `the comparison is empty; write <operator> <value>, e.g. "> 0"`},
		{d, "500ms", 0, `expected an operator (<, <=, >, >= or ==), got "5"`},
		{d, "p95 < 500ms", 0, `expected an operator (<, <=, >, >= or ==), got "p"`},
		{d, " = 5ms", 1, `use "==" to compare for equality, not "="`},
		{d, "!= 5ms", 0, `"!=" is not supported`},
		{d, "=< 5ms", 0, `did you mean "<="?`},
		{d, "=> 5ms", 0, `did you mean ">="?`},
		{d, "<> 5ms", 1, `expected a number after "<", got ">"`},
		{d, "<", 1, `missing value after "<"`},
		{d, "< ", 2, `missing value after "<"`},
		{n, "> -1", 2, "must not be negative"},
		{n, "> +1", 2, `remove the "+" sign`},
		{d, "< .5s", 2, "write a digit before the decimal point"},
		{d, "< 5.", 3, "a decimal point must be followed by digits"},
		{d, "< 1.2.3s", 5, "at most one decimal point"},
		{n, "< 1e3", 3, "exponents are not supported"},
		{d, "< 500ms x", 8, `unexpected "x" after the value`},
		{d, "< 500 ms", 5, "remove the space between the number and its unit"},
		// Durations.
		{d, "< 500", 2, `the value is a duration and needs a unit (us, ms, s or m); did you mean "500ms"?`},
		{d, "< 5ns", 3, `"ns" is not supported`},
		{d, "< 5kb", 3, `the value is a duration; use us, ms, s or m, not "kb"`},
		{d, "< 5%", 3, `the value is a duration; use us, ms, s or m, not "%"`},
		{d, "< 1Μs", 3, "Greek capital letter Mu"},
		// Sizes.
		{sz, "< 5ms", 3, `the value is a size; use b, kb, mb, gb, kib, mib or gib (or a plain number of bytes), not "ms"`},
		{sz, "< 0.5", 2, "the value is a size; use a whole number of bytes"},
		{sz, "< 0.1kib", 2, `use a whole number of bytes ("0.1kib" is "102.4" bytes)`},
		{sz, "< 5ΜB", 3, `write "mb" for megabytes`},
		{sz, "< 1" + strings.Repeat("0", 300) + "gib", 2, "the value is too large"},
		// Counts.
		{n, "> 1.5", 2, "the value is a count; use a whole number"},
		{n, "> 10ms", 4, `the value is a count; write a plain number such as 100, without "ms"`},
		{n, "> 5%", 3, `the value is a count; write a plain number such as 100, without "%"`},
	}
	for _, tc := range cases {
		_, err := thresholds.ParseComparison(tc.kind, tc.s)
		var pe *thresholds.ParseError
		if !errors.As(err, &pe) {
			t.Errorf("ParseComparison(%s, %q) error = %v, want *ParseError", tc.kind, tc.s, err)
			continue
		}
		if pe.Expr != tc.s || pe.Pos != tc.pos || !strings.Contains(pe.Msg, tc.msg) {
			t.Errorf("ParseComparison(%s, %q) = {Pos %d, Msg %q}, want {Pos %d, Msg containing %q}", tc.kind, tc.s, pe.Pos, pe.Msg, tc.pos, tc.msg)
		}
		if !strings.HasPrefix(err.Error(), "thresholds: invalid comparison ") {
			t.Errorf("ParseComparison(%s, %q) error text %q", tc.kind, tc.s, err)
		}
	}
}

func TestParseComparisonErrorText(t *testing.T) {
	_, err := thresholds.ParseComparison(thresholds.KindDuration, "< 500")
	want := `thresholds: invalid comparison "< 500": the value is a duration and needs a unit (us, ms, s or m); did you mean "500ms"? (at offset 2)`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v\nwant    %s", err, want)
	}
	// A threshold error still says threshold.
	_, err = thresholds.Parse("p95 < 500")
	if err == nil || !strings.HasPrefix(err.Error(), "thresholds: invalid threshold ") {
		t.Errorf("Parse error = %v", err)
	}
}

// Rates, throughput and unknown kinds have no comparison form. That is a
// mistake by the caller, not in the text, so it is not a *ParseError.
func TestParseComparisonRejectsKind(t *testing.T) {
	for _, k := range []thresholds.Kind{thresholds.KindRate, thresholds.KindThroughput, 0, 99} {
		for _, s := range []string{"< 5%", "> 100", "", "< 500ms"} {
			c, err := thresholds.ParseComparison(k, s)
			if err == nil {
				t.Errorf("ParseComparison(%s, %q) = %+v, want an error", k, s, c)
				continue
			}
			var pe *thresholds.ParseError
			if errors.As(err, &pe) {
				t.Errorf("ParseComparison(%s, %q) error is a *ParseError: %v", k, s, err)
			}
			if want := "thresholds: a comparison is a duration, size or count, not " + k.String(); err.Error() != want {
				t.Errorf("ParseComparison(%s, %q) error %q, want %q", k, s, err, want)
			}
			if c != (thresholds.Comparison{}) {
				t.Errorf("ParseComparison(%s, %q) returned %+v with an error", k, s, c)
			}
		}
	}
}

// A comparison is read exactly as the same text after a metric of its
// kind: the same operator and value, or an error at the same place.
func TestParseComparisonMatchesParse(t *testing.T) {
	inputs := []string{
		"", " ", "<", "< ", "< 500ms", "<=1s", "> 0", "== 0", "< 50kb", "< 1.5GiB", "< 500", "< 5ms x",
		"p95 < 500ms", "!= 5", "= 5", "< -1", "> 1.5", "< 0.1kib", "< 1kib", "<= 1.5mb", "== 3", ">= 10.0",
		"< 5Μs", "< 1µs", "< 1e3", "< 500 ms", "<\t5", "== 0.000001us", "> 1" + strings.Repeat("0", 400),
	}
	for _, kc := range comparisonKinds {
		for _, s := range inputs {
			requireSameAsParse(t, kc.kind, kc.metric, s)
		}
	}
}

func requireSameAsParse(t testing.TB, k thresholds.Kind, metric, s string) {
	t.Helper()
	prefix := metric + " "
	c, cerr := thresholds.ParseComparison(k, s)
	th, terr := thresholds.Parse(prefix + s)
	if (cerr == nil) != (terr == nil) {
		t.Fatalf("%s %q: ParseComparison error %v, Parse(%q) error %v", k, s, cerr, prefix+s, terr)
	}
	if cerr != nil {
		var cpe, tpe *thresholds.ParseError
		if !errors.As(cerr, &cpe) || !errors.As(terr, &tpe) {
			t.Fatalf("%s %q: errors %T and %T, want *ParseError", k, s, cerr, terr)
		}
		if cpe.Pos+len(prefix) != tpe.Pos {
			t.Fatalf("%s %q: ParseComparison error at %d, Parse error at %d (%d after the metric)", k, s, cpe.Pos, tpe.Pos, tpe.Pos-len(prefix))
		}
		return
	}
	if c.Raw != s || c.Kind != k || c.Op != th.Op || c.Value != th.Value || th.Metric.Kind != k {
		t.Fatalf("%s %q: ParseComparison = %+v, Parse = %+v", k, s, c, th)
	}
}

// ParseComparison never panics, reads every input exactly as the threshold
// parser reads it after a metric of the same kind, and returns values of
// the kind's form.
func FuzzParseComparison(f *testing.F) {
	for _, s := range []string{
		"", " ", "<", "< ", "< 500ms", "<=1s", "> 0", "== 0", "< 50kb", "< 1.5GiB", "< 500", "< 5ms x",
		"p95 < 500ms", "!= 5", "= 5", "< -1", "> 1.5", "< 0.1kib", "== 1µs", "< 1Μs", "< 1e3",
		"< 0." + "0000000000000000000000000000001us", "> 99999999999999999999999999999",
	} {
		for k := range comparisonKinds {
			f.Add(uint8(k), s)
		}
	}
	f.Fuzz(func(t *testing.T, ki uint8, s string) {
		kc := comparisonKinds[int(ki)%len(comparisonKinds)]
		requireSameAsParse(t, kc.kind, kc.metric, s)
		c, err := thresholds.ParseComparison(kc.kind, s)
		if err != nil {
			var pe *thresholds.ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("ParseComparison(%s, %q) error %T, want *ParseError", kc.kind, s, err)
			}
			if pe.Pos < 0 || pe.Pos > len(s) || pe.Expr != s {
				t.Fatalf("ParseComparison(%s, %q): bad error %+v", kc.kind, s, pe)
			}
			if len(err.Error()) > 2048 {
				t.Fatalf("ParseComparison(%s, %q): error is %d bytes", kc.kind, s, len(err.Error()))
			}
			return
		}
		v := c.Value
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			t.Fatalf("ParseComparison(%s, %q).Value = %v", kc.kind, s, v)
		}
		if kc.kind != thresholds.KindDuration && v != math.Trunc(v) {
			t.Fatalf("ParseComparison(%s, %q).Value = %v, want a whole number", kc.kind, s, v)
		}
		// The bound itself satisfies exactly the inclusive operators.
		inclusive := c.Op == thresholds.OpLE || c.Op == thresholds.OpGE || c.Op == thresholds.OpEQ
		if c.Holds(v) != inclusive {
			t.Fatalf("ParseComparison(%s, %q).Holds(%v) = %v", kc.kind, s, v, !inclusive)
		}
		if c.Holds(math.NaN()) {
			t.Fatalf("ParseComparison(%s, %q) holds for NaN", kc.kind, s)
		}
	})
}
