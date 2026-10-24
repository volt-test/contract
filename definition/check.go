package definition

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// CheckType is the type of a Check: what part of the response it reads.
type CheckType string

// The check types.
const (
	CheckStatus CheckType = "status"
	CheckHeader CheckType = "header"
	CheckBody   CheckType = "body"
	CheckJSON   CheckType = "json"
	CheckHTML   CheckType = "html"
	CheckTiming CheckType = "timing"
	CheckURL    CheckType = "url"
	CheckCookie CheckType = "cookie"
)

// FailMode is what a failed check or a missed required extractor does.
//
// FailRecord counts the failure toward the check rate only; it does not
// fail the run. FailFail also fails the run. FailAbort fails the run and
// ends the virtual user's current iteration once the step's other checks
// have been evaluated; the test itself goes on (unlike a threshold's
// abort_on_fail, which stops the test).
type FailMode string

// The fail modes.
const (
	FailRecord FailMode = "record"
	FailFail   FailMode = "fail"
	FailAbort  FailMode = "abort"
)

// MaxLabelBytes is the longest derived check label, in bytes. A longer
// one is cut on a UTF-8 boundary. Names set with "name" are not cut.
const MaxLabelBytes = 128

// FailMode returns what a failure of c does: its on_fail, or the default
// for its type, FailFail for status checks and FailRecord for the others.
func (c Check) FailMode() FailMode {
	switch {
	case c.OnFail != "":
		return c.OnFail
	case c.Type == CheckStatus:
		return FailFail
	}
	return FailRecord
}

// FailMode returns what a miss of a required extractor does: its on_fail,
// or FailAbort by default.
func (e Extract) FailMode() FailMode {
	if e.OnFail != "" {
		return e.OnFail
	}
	return FailAbort
}

// Label returns the label a required extractor has among its step's
// checks: "extract " followed by the variable name as written.
func (e Extract) Label() string { return "extract " + e.VariableName }

// Label returns the check's identity in results, unique within its step
// (Parse rejects duplicates, counting required extractors).
//
// A name with a non-space character is returned as written. Otherwise the
// label is derived from the decoded check as
// "<type>[ <selector>] <operator>[ <value>]", cut to MaxLabelBytes:
//
//	status in [200,201]
//	json $.data.items[0].id equals 42
//	html h1 equals "Sale"
//	timing duration < 500ms
//	header Authorization equals
//
// The selector is written as it is. A string value is JSON-encoded without
// HTML escaping, a number keeps its literal, status codes are written as a
// JSON array, and comparisons (count, size, duration) and is are written
// as they are. exists, missing and empty have no value, and neither does
// any header operator, so header values never appear in results; two
// header checks that differ only in value need names. Every other derived
// label contains the expected value, so a check whose value is sensitive
// needs a name. Uniqueness is checked on the cut label, which is what
// results carry. In results a check is identified by its scenario name,
// step name and label together.
//
// Label is defined for every Check, valid or not; with no operator set it
// stops after the selector, and with several it uses the first in the
// order the fields are declared.
func (c Check) Label() string {
	if strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	var b strings.Builder
	b.WriteString(string(c.Type))
	if c.Selector != "" {
		b.WriteByte(' ')
		b.WriteString(c.Selector)
	}
	if op, value := c.operator(); op != "" {
		b.WriteByte(' ')
		b.WriteString(op)
		if value != "" && c.Type != CheckHeader {
			b.WriteByte(' ')
			b.WriteString(value)
		}
	}
	return cutLabel(b.String())
}

// operator returns the first operator set on c, in field order, and its
// value rendered for a label ("" for exists, missing and empty).
func (c Check) operator() (op, value string) {
	ops := c.operators()
	if len(ops) == 0 {
		return "", ""
	}
	return ops[0].key, ops[0].label()
}

// checkOp is one operator key set on a check.
type checkOp struct {
	key    string
	codes  []int   // in, not_in, between
	scalar *Scalar // equals, contains
	text   string  // not_contains, matches, is, count, size, duration
	flag   *bool   // exists, missing, empty
}

func (o checkOp) label() string {
	switch {
	case o.codes != nil:
		var b strings.Builder
		b.WriteByte('[')
		for i, c := range o.codes {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.Itoa(c))
		}
		b.WriteByte(']')
		return b.String()
	case o.scalar != nil:
		return o.scalar.label()
	case o.flag != nil:
		return ""
	case o.key == "not_contains" || o.key == "matches":
		return string(encodeString(o.text))
	}
	return o.text
}

// operators lists the operators set on c, in field order. A key counts as
// set when it holds a non-empty string or list, or a non-nil pointer.
func (c Check) operators() []checkOp {
	var ops []checkOp
	codes := func(key string, v []int) {
		if len(v) > 0 {
			ops = append(ops, checkOp{key: key, codes: v})
		}
	}
	scalar := func(key string, v *Scalar) {
		if v != nil {
			ops = append(ops, checkOp{key: key, scalar: v})
		}
	}
	text := func(key, v string) {
		if v != "" {
			ops = append(ops, checkOp{key: key, text: v})
		}
	}
	flag := func(key string, v *bool) {
		if v != nil {
			ops = append(ops, checkOp{key: key, flag: v})
		}
	}
	codes("in", c.In)
	codes("not_in", c.NotIn)
	codes("between", c.Between)
	scalar("equals", c.Equals)
	scalar("contains", c.Contains)
	text("not_contains", c.NotContains)
	text("matches", c.Matches)
	flag("exists", c.Exists)
	flag("missing", c.Missing)
	flag("empty", c.Empty)
	text("is", c.Is)
	text("count", c.Count)
	text("size", c.Size)
	text("duration", c.Duration)
	return ops
}

// cutLabel cuts a derived label to MaxLabelBytes on a UTF-8 boundary.
func cutLabel(s string) string {
	if len(s) <= MaxLabelBytes {
		return s
	}
	cut := MaxLabelBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
