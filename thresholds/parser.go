package thresholds

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Op is a comparison operator.
type Op int

const (
	OpLT Op = iota + 1 // <
	OpLE               // <=
	OpGT               // >
	OpGE               // >=
	OpEQ               // ==, within a small tolerance; see the package documentation
)

func (o Op) String() string {
	switch o {
	case OpLT:
		return "<"
	case OpLE:
		return "<="
	case OpGT:
		return ">"
	case OpGE:
		return ">="
	case OpEQ:
		return "=="
	}
	return "Op(" + strconv.Itoa(int(o)) + ")"
}

// ops lists the operators longest first, the order Parse matches them in.
var ops = []struct {
	text string
	op   Op
}{{"<=", OpLE}, {">=", OpGE}, {"==", OpEQ}, {"<", OpLT}, {">", OpGT}}

// ScopeKind is the level of a definition a threshold is attached to.
type ScopeKind int

const (
	ScopeTest     ScopeKind = iota + 1 // the whole test
	ScopeScenario                      // one scenario
	ScopeStep                          // one step of a scenario
)

func (k ScopeKind) String() string {
	switch k {
	case ScopeTest:
		return "test"
	case ScopeScenario:
		return "scenario"
	case ScopeStep:
		return "step"
	}
	return "ScopeKind(" + strconv.Itoa(int(k)) + ")"
}

// Threshold is a parsed threshold expression.
type Threshold struct {
	// Raw is the expression as written.
	Raw    string
	Metric Metric
	Op     Op
	// Value is the bound in the metric kind's canonical unit: microseconds
	// for durations, a fraction for rates (5% is 0.05), bytes for sizes,
	// and the plain number for counts and rps.
	Value float64
}

// ParseError reports an expression that does not follow the grammar or
// does not fit its metric.
type ParseError struct {
	// Expr is the expression as given to Parse.
	Expr string
	// Pos is the byte offset in Expr where the problem starts.
	Pos int
	// Msg describes the problem, often with a hint.
	Msg string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("thresholds: invalid threshold %s: %s (at offset %d)", quoteShort(e.Expr), e.Msg, e.Pos)
}

// ScopeError reports a metric used at a scope where it is not available.
type ScopeError struct {
	Metric string
	Scope  ScopeKind
}

func (e *ScopeError) Error() string {
	return fmt.Sprintf("thresholds: %s is not available on a %s; use it on the test or a scenario", e.Metric, e.Scope)
}

// maxEcho bounds how much of an expression an error repeats.
const maxEcho = 128

// quoteShort quotes s for an error message, cut to about maxEcho bytes on a
// UTF-8 boundary.
func quoteShort(s string) string {
	if len(s) > maxEcho {
		cut := maxEcho
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "..."
	}
	return strconv.Quote(s)
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isOpChar(c byte) bool { return c == '<' || c == '>' || c == '=' || c == '!' }

const opList = "<, <=, >, >= or =="

// Parse parses a threshold expression: a metric, an operator and a value,
// such as "p95 < 500ms". See the package documentation for the grammar,
// the metrics and the units. Errors are *ParseError.
func Parse(expr string) (Threshold, error) {
	p := parser{s: expr}
	return p.parse()
}

type parser struct {
	s string
	i int
}

func (p *parser) fail(pos int, format string, args ...any) error {
	return &ParseError{Expr: p.s, Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) skipSpace() {
	for p.i < len(p.s) && isSpace(p.s[p.i]) {
		p.i++
	}
}

// next returns the rune at the current position, for messages.
func (p *parser) next() string {
	r, _ := utf8.DecodeRuneInString(p.s[p.i:])
	return string(r)
}

func (p *parser) parse() (Threshold, error) {
	p.skipSpace()
	if p.i == len(p.s) {
		return Threshold{}, p.fail(p.i, `the expression is empty; write <metric> <operator> <value>, e.g. "p95 < 500ms"`)
	}

	// Metric.
	start := p.i
	for p.i < len(p.s) && !isSpace(p.s[p.i]) && !isOpChar(p.s[p.i]) {
		p.i++
	}
	name := p.s[start:p.i]
	if name == "" {
		return Threshold{}, p.fail(start, "missing metric before the operator")
	}
	m, ok := lookupMetric(name)
	if !ok {
		return Threshold{}, p.fail(start, "%s", unknownMetric(name))
	}

	// Operator.
	p.skipSpace()
	opPos := p.i
	rest := p.s[p.i:]
	var op Op
	for _, o := range ops {
		if strings.HasPrefix(rest, o.text) {
			op = o.op
			p.i += len(o.text)
			break
		}
	}
	if op == 0 {
		switch {
		case rest == "":
			return Threshold{}, p.fail(opPos, "missing operator after %s; use %s", name, opList)
		case strings.HasPrefix(rest, "!="):
			return Threshold{}, p.fail(opPos, `"!=" is not supported; use %s`, opList)
		case strings.HasPrefix(rest, "=<"):
			return Threshold{}, p.fail(opPos, `unknown operator "=<"; did you mean "<="?`)
		case strings.HasPrefix(rest, "=>"):
			return Threshold{}, p.fail(opPos, `unknown operator "=>"; did you mean ">="?`)
		case strings.HasPrefix(rest, "="):
			return Threshold{}, p.fail(opPos, `use "==" to compare for equality, not "="`)
		}
		return Threshold{}, p.fail(opPos, "expected an operator (%s) after %s, got %q", opList, name, p.next())
	}

	// Value: digits, an optional fraction and an optional unit.
	p.skipSpace()
	valPos := p.i
	if p.i == len(p.s) {
		return Threshold{}, p.fail(valPos, "missing value after %q", op.String())
	}
	switch c := p.s[p.i]; {
	case c == '-':
		return Threshold{}, p.fail(valPos, "the value must not be negative")
	case c == '+':
		return Threshold{}, p.fail(valPos, `remove the "+" sign; values are never negative`)
	case c == '.':
		return Threshold{}, p.fail(valPos, `write a digit before the decimal point, e.g. "0.5"`)
	case !isDigit(c):
		return Threshold{}, p.fail(valPos, "expected a number after %q, got %q", op.String(), p.next())
	}
	for p.i < len(p.s) && isDigit(p.s[p.i]) {
		p.i++
	}
	whole := true
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		if p.i == len(p.s) || !isDigit(p.s[p.i]) {
			return Threshold{}, p.fail(p.i-1, "a decimal point must be followed by digits")
		}
		for p.i < len(p.s) && isDigit(p.s[p.i]) {
			if p.s[p.i] != '0' {
				whole = false
			}
			p.i++
		}
	}
	number := p.s[valPos:p.i]
	if p.i < len(p.s) {
		c := p.s[p.i]
		if c == '.' {
			return Threshold{}, p.fail(p.i, "a number has at most one decimal point")
		}
		if (c == 'e' || c == 'E') && p.i+1 < len(p.s) {
			if d := p.s[p.i+1]; isDigit(d) || d == '+' || d == '-' {
				return Threshold{}, p.fail(p.i, "exponents are not supported; write the number out in full")
			}
		}
	}
	unitPos := p.i
	for p.i < len(p.s) {
		r, size := utf8.DecodeRuneInString(p.s[p.i:])
		if r != '%' && !unicode.IsLetter(r) {
			break
		}
		p.i += size
	}
	unit := p.s[unitPos:p.i]
	p.skipSpace()
	if p.i < len(p.s) {
		r, _ := utf8.DecodeRuneInString(p.s[p.i:])
		if unit == "" && unitPos < p.i && (r == '%' || unicode.IsLetter(r)) {
			return Threshold{}, p.fail(unitPos, "remove the space between the number and its unit")
		}
		return Threshold{}, p.fail(p.i, "unexpected %q after the value", string(r))
	}

	v, msg, atUnit := value(m, number, unit, whole)
	if msg != "" {
		pos := valPos
		if atUnit {
			pos = unitPos
		}
		return Threshold{}, p.fail(pos, "%s", msg)
	}
	return Threshold{Raw: p.s, Metric: m, Op: op, Value: v}, nil
}

// decimal returns number×10^exp as the nearest float64. Applying a power of
// ten as an exponent of the decimal literal, rather than multiplying
// floats, rounds once, so String can print the value back exactly.
func decimal(number string, exp int) (float64, bool) {
	s := number
	if exp != 0 {
		s += "e" + strconv.Itoa(exp)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

const (
	tooLarge     = "the value is too large"
	rateAbove100 = "a rate cannot exceed 100%"
)

// value converts a number and unit to the canonical unit of m's kind. It
// returns a message when the value does not fit the metric; atUnit is set
// when the problem is the unit rather than the number.
func value(m Metric, number, unit string, whole bool) (v float64, msg string, atUnit bool) {
	v, msg = valueOf(m, number, unit, whole)
	// With a unit, every message is about the unit except those about the
	// size of the number.
	if msg == "" || msg == tooLarge || msg == rateAbove100 || unit == "" {
		return v, msg, false
	}
	return 0, msg, true
}

func valueOf(m Metric, number, unit string, whole bool) (float64, string) {
	u := strings.ToLower(unit)
	scale := func(exp int, factor float64) (float64, string) {
		v, ok := decimal(number, exp)
		if !ok {
			return 0, tooLarge
		}
		v *= factor
		if math.IsInf(v, 0) {
			return 0, tooLarge
		}
		return v, ""
	}
	switch m.Kind {
	case KindDuration:
		switch u {
		case "us", "µs", "μs":
			return scale(0, 1)
		case "ms":
			return scale(3, 1)
		case "s":
			return scale(6, 1)
		case "m":
			return scale(6, 60)
		case "":
			return 0, fmt.Sprintf("%s is a duration and needs a unit (us, ms, s or m); did you mean %s?", m.Name, strconv.Quote(number+"ms"))
		case "ns":
			return 0, `"ns" is not supported; the smallest unit is us (microseconds)`
		}
		return 0, fmt.Sprintf("%s is a duration; use us, ms, s or m, not %s", m.Name, quoteShort(unit))
	case KindRate:
		switch u {
		case "%":
			v, msg := scale(-2, 1)
			if msg == "" && v > 1 {
				return 0, rateAbove100
			}
			return v, msg
		case "":
			hint := strconv.Quote(number + "%")
			if v, ok := decimal(number, 0); ok && v <= 1 {
				pct := shiftDecimal(number, 2)
				hint = fmt.Sprintf("%s (or %s if %s is a fraction)", hint, strconv.Quote(pct+"%"), number)
			}
			return 0, fmt.Sprintf(`%s is a rate and needs "%%"; did you mean %s?`, m.Name, hint)
		}
		return 0, fmt.Sprintf("%s is a rate; write a percentage such as 5%%, not %s", m.Name, quoteShort(unit))
	case KindCount:
		if u != "" {
			return 0, fmt.Sprintf("%s is a count; write a plain number such as 100, without %s", m.Name, quoteShort(unit))
		}
		if !whole {
			return 0, fmt.Sprintf("%s is a count; use a whole number", m.Name)
		}
		return scale(0, 1)
	case KindThroughput:
		if u != "" {
			return 0, fmt.Sprintf("%s is requests per second; write a plain number such as 100, without %s", m.Name, quoteShort(unit))
		}
		return scale(0, 1)
	case KindSize:
		switch u {
		case "", "b":
			return scale(0, 1)
		case "kb":
			return scale(3, 1)
		case "mb":
			return scale(6, 1)
		case "gb":
			return scale(9, 1)
		case "kib":
			return scale(0, 1<<10)
		case "mib":
			return scale(0, 1<<20)
		case "gib":
			return scale(0, 1<<30)
		}
		return 0, fmt.Sprintf("%s is a size; use b, kb, mb, gb, kib, mib or gib (or a plain number of bytes), not %s", m.Name, quoteShort(unit))
	}
	return 0, "unknown metric kind"
}

// unknownMetric describes an unknown metric name, with a hint.
func unknownMetric(name string) string {
	q := quoteShort(name)
	if lower := strings.ToLower(name); lower != name {
		if _, ok := lookupMetric(lower); ok {
			return fmt.Sprintf("unknown metric %s; metric names are lowercase: %s", q, strconv.Quote(lower))
		}
	}
	aggs := "avg, min, max, p50, p75, p90, p95, p99 or p99.9"
	for _, prefix := range []string{"ttfb_", "iteration_duration_"} {
		if strings.HasPrefix(name, prefix) {
			fam := strings.TrimSuffix(prefix, "_")
			return fmt.Sprintf("unknown metric %s; %s takes %s (e.g. %sp95)", q, fam, aggs, prefix)
		}
	}
	if len(name) > 1 && name[0] == 'p' && isDigit(name[1]) {
		return fmt.Sprintf("unknown metric %s; the percentiles are p50, p75, p90, p95, p99 and p99.9", q)
	}
	return fmt.Sprintf("unknown metric %s; the metrics are avg, min, max, p50, p75, p90, p95, p99, p99.9, "+
		"ttfb and ttfb_<aggregate>, iteration_duration and iteration_duration_<aggregate>, "+
		"error_rate, check_rate, rps, requests, failed, bytes_sent, bytes_received, iterations and dropped_iterations", q)
}

// ValidFor reports whether the threshold may be attached at scope k. The
// iteration metrics (iteration_duration and its variants, iterations and
// dropped_iterations) describe whole scenario runs, so they are available
// on the test and on scenarios but not on steps. Errors are *ScopeError.
func (t Threshold) ValidFor(k ScopeKind) error {
	if _, ok := lookupMetric(t.Metric.Name); !ok || t.Metric.Kind == 0 {
		return errors.New("thresholds: the threshold has no metric from the catalog; build it with Parse")
	}
	switch k {
	case ScopeTest, ScopeScenario:
		return nil
	case ScopeStep:
		if !t.Metric.stepScoped() {
			return &ScopeError{Metric: t.Metric.Name, Scope: k}
		}
		return nil
	}
	return fmt.Errorf("thresholds: unknown scope %s", k)
}

// String returns the threshold in canonical form, such as "p95 < 500ms":
// the metric, the operator and the value separated by single spaces, the
// value in the largest of us, ms and s (durations) or b, kb, mb and gb
// (sizes) that keeps it at least 1, rates as a percentage, and no
// redundant zeros. Parse(t.String()) returns t apart from Raw.
func (t Threshold) String() string {
	return t.Metric.Name + " " + t.Op.String() + " " + formatValue(t.Metric.Kind, t.Value)
}

// formatValue prints v exactly: the shortest decimal that parses back to
// v, with the decimal point moved for the unit as text, never by float
// arithmetic.
func formatValue(k Kind, v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return s
	}
	switch k {
	case KindDuration:
		switch {
		case v >= 1e6:
			return shiftDecimal(s, -6) + "s"
		case v >= 1e3:
			return shiftDecimal(s, -3) + "ms"
		}
		return s + "us"
	case KindRate:
		return shiftDecimal(s, 2) + "%"
	case KindSize:
		switch {
		case v >= 1e9:
			return shiftDecimal(s, -9) + "gb"
		case v >= 1e6:
			return shiftDecimal(s, -6) + "mb"
		case v >= 1e3:
			return shiftDecimal(s, -3) + "kb"
		}
		return s + "b"
	}
	return s
}

// shiftDecimal moves the decimal point of a plain non-negative decimal
// (digits with an optional fraction) n places to the right (left when n is
// negative) and trims redundant zeros: shiftDecimal("1.5", 2) is "150",
// shiftDecimal("5", -3) is "0.005".
func shiftDecimal(s string, n int) string {
	intPart, frac, _ := strings.Cut(s, ".")
	digits := intPart + frac
	point := len(intPart) + n
	var out string
	switch {
	case point <= 0:
		out = "0." + strings.Repeat("0", -point) + digits
	case point >= len(digits):
		out = digits + strings.Repeat("0", point-len(digits))
	default:
		out = digits[:point] + "." + digits[point:]
	}
	intPart, frac, hasFrac := strings.Cut(out, ".")
	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	if hasFrac {
		frac = strings.TrimRight(frac, "0")
	}
	if frac == "" {
		return intPart
	}
	return intPart + "." + frac
}

// equalWithin reports whether a and b are equal for the == operator: within
// a relative tolerance of 1e-9, and an absolute one of 1e-9 near zero:
// |a-b| <= 1e-9 * max(1, |a|, |b|).
func equalWithin(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}
