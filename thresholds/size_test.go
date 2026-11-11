package thresholds_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/volt-test/contract/thresholds"
)

// Every size unit, case and value form, in exact bytes.
func TestParseSize(t *testing.T) {
	cases := map[string]uint64{
		"0":        0,
		"100":      100,
		"100b":     100,
		"100B":     100,
		"10.0":     10,
		"007kb":    7000,
		"1.5MB":    1500000,
		"2gb":      2000000000,
		"1kib":     1024,
		"1mib":     1 << 20,
		"1.5GiB":   3 << 29,
		"0.5kib":   512,
		" 1mb\n":   1000000,
		"8gib":     8 << 30,
		"16777216": 16777216,
		// Above 2^53: exact, not rounded through float64.
		"9007199254740993":     9007199254740993,
		"18446744073709551615": 18446744073709551615,
	}
	for in, want := range cases {
		got, err := thresholds.ParseSize(in)
		if err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
}

// Invalid sizes, with the position and message of the error.
func TestParseSizeErrors(t *testing.T) {
	cases := []struct {
		s   string
		pos int
		msg string
	}{
		{"", 0, `the size is empty; write a number and a unit, e.g. "1mb"`},
		{"  ", 2, "the size is empty"},
		{"mb", 0, `expected a number, got "m"`},
		{"< 1mb", 0, `expected a number, got "<"`},
		{"-1kb", 0, "must not be negative"},
		{"+1kb", 0, `remove the "+" sign`},
		{".5kb", 0, "write a digit before the decimal point"},
		{"1.kb", 1, "a decimal point must be followed by digits"},
		{"1e3", 1, "exponents are not supported"},
		{"1 mb", 1, "remove the space between the number and its unit"},
		{"1mb 2", 4, `unexpected "2" after the value`},
		{"5ms", 1, `the value is a size; use b, kb, mb, gb, kib, mib or gib (or a plain number of bytes), not "ms"`},
		{"0.5", 0, "the value is a size; use a whole number of bytes"},
		{"0.1kib", 0, `use a whole number of bytes ("0.1kib" is "102.4" bytes)`},
		{"5ΜB", 1, `write "mb" for megabytes`},
		{"18446744073709551616", 0, "the value is too large"},
		{"20000000000gb", 0, "the value is too large"},
		{"1" + strings.Repeat("0", 400) + "gib", 0, "the value is too large"},
	}
	for _, tc := range cases {
		_, err := thresholds.ParseSize(tc.s)
		var pe *thresholds.ParseError
		if !errors.As(err, &pe) {
			t.Errorf("ParseSize(%q) error = %v, want *ParseError", tc.s, err)
			continue
		}
		if pe.Expr != tc.s || pe.Pos != tc.pos || !strings.Contains(pe.Msg, tc.msg) {
			t.Errorf("ParseSize(%q) = {Pos %d, Msg %q}, want {Pos %d, Msg containing %q}", tc.s, pe.Pos, pe.Msg, tc.pos, tc.msg)
		}
		if !strings.HasPrefix(err.Error(), "thresholds: invalid size ") {
			t.Errorf("ParseSize(%q) error text %q", tc.s, err)
		}
	}
}

// A size reads like the value of a size comparison: the same bytes.
func TestParseSizeAgreesWithComparison(t *testing.T) {
	for _, s := range []string{"0", "1", "100b", "1.5MB", "2gb", "1kib", "1.5GiB", "0.5kib", "123456789"} {
		n, err := thresholds.ParseSize(s)
		if err != nil {
			t.Fatalf("ParseSize(%q): %v", s, err)
		}
		c, err := thresholds.ParseComparison(thresholds.KindSize, "< "+s)
		if err != nil {
			t.Fatalf("ParseComparison(%q): %v", s, err)
		}
		if float64(n) != c.Value {
			t.Errorf("ParseSize(%q) = %d, comparison value %v", s, n, c.Value)
		}
	}
}
