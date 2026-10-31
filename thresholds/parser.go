package thresholds

import (
	"fmt"
	"math"
	"math/big"
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
// does not fit its metric, or a comparison that does not follow the
// grammar or does not fit its kind.
type ParseError struct {
	// Expr is the expression as given to Parse, or the comparison as given
	// to ParseComparison.
	Expr string
	// Pos is the byte offset in Expr where the problem starts.
	Pos int
	// Msg describes the problem, often with a hint.
	Msg string

	// comparison is set for errors from ParseComparison.
	comparison bool
}

func (e *ParseError) Error() string {
	what := "threshold"
	if e.comparison {
		what = "comparison"
	}
	return fmt.Sprintf("thresholds: invalid %s %s: %s (at offset %d)", what, quoteShort(e.Expr), e.Msg, e.Pos)
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
func quoteShort(s string) string { return strconv.Quote(cutShort(s)) }

// cutShort cuts s to about maxEcho bytes on a UTF-8 boundary, marking the
// cut with "...".
func cutShort(s string) string {
	if len(s) <= maxEcho {
		return s
	}
	cut := maxEcho
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
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

// Comparison is a parsed comparison: an operator and a value with no
// metric, such as "< 500ms", for a value measured elsewhere. It shares the
// threshold grammar, units and == rule; see the package documentation.
type Comparison struct {
	// Raw is the comparison as written.
	Raw  string
	Kind Kind
	Op   Op
	// Value is the bound in the kind's canonical unit: microseconds for
	// durations, bytes for sizes and the plain number for counts.
	Value float64
}

// ParseComparison parses a comparison of kind k: an operator and a value,
// such as "< 500ms" (a duration), "< 50kb" (a size) or "> 0" (a count),
// with the operators, value forms and units of a threshold expression of
// that kind. k must be KindDuration, KindSize or KindCount; any other kind
// is an error, not a *ParseError. Errors about s are *ParseError.
func ParseComparison(k Kind, s string) (Comparison, error) {
	var example string
	switch k {
	case KindDuration:
		example = "< 500ms"
	case KindSize:
		example = "< 50kb"
	case KindCount:
		example = "> 0"
	default:
		return Comparison{}, fmt.Errorf("thresholds: a comparison is a duration, size or count, not %s", k)
	}
	p := parser{s: s, bare: true}
	p.skipSpace()
	if p.i == len(p.s) {
		return Comparison{}, p.fail(p.i, "the comparison is empty; write <operator> <value>, e.g. %q", example)
	}
	op, v, err := p.comparison(k, "")
	if err != nil {
		return Comparison{}, err
	}
	return Comparison{Raw: s, Kind: k, Op: op, Value: v}, nil
}

// Holds reports whether a measured value, in the canonical unit of c's
// kind, satisfies c, with the == rule a threshold of the same kind uses:
// exact for counts and sizes, within a small tolerance for durations. It
// is false for NaN and for an unknown operator, so the zero Comparison
// holds for nothing.
func (c Comparison) Holds(actual float64) bool {
	return holds(c.Op, c.Kind, actual, c.Value)
}

type parser struct {
	s string
	i int
	// bare is set when parsing a comparison without a metric, for error
	// text.
	bare bool
}

func (p *parser) fail(pos int, format string, args ...any) error {
	return &ParseError{Expr: p.s, Pos: pos, Msg: fmt.Sprintf(format, args...), comparison: p.bare}
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

	op, v, err := p.comparison(m.Kind, m.Name)
	if err != nil {
		return Threshold{}, err
	}
	return Threshold{Raw: p.s, Metric: m, Op: op, Value: v}, nil
}

// comparison parses the operator and a value of kind k, from the current
// position to the end of the input. metric is the metric the operator
// follows, for messages; it is "" for a bare comparison.
func (p *parser) comparison(k Kind, metric string) (Op, float64, error) {
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
			return 0, 0, p.fail(opPos, "missing operator%s; use %s", afterMetric(metric), opList)
		case strings.HasPrefix(rest, "!="):
			return 0, 0, p.fail(opPos, `"!=" is not supported; use %s`, opList)
		case strings.HasPrefix(rest, "=<"):
			return 0, 0, p.fail(opPos, `unknown operator "=<"; did you mean "<="?`)
		case strings.HasPrefix(rest, "=>"):
			return 0, 0, p.fail(opPos, `unknown operator "=>"; did you mean ">="?`)
		case strings.HasPrefix(rest, "="):
			return 0, 0, p.fail(opPos, `use "==" to compare for equality, not "="`)
		}
		return 0, 0, p.fail(opPos, "expected an operator (%s)%s, got %q", opList, afterMetric(metric), p.next())
	}

	// Value: digits, an optional fraction and an optional unit.
	p.skipSpace()
	valPos := p.i
	if p.i == len(p.s) {
		return 0, 0, p.fail(valPos, "missing value after %q", op.String())
	}
	switch c := p.s[p.i]; {
	case c == '-':
		return 0, 0, p.fail(valPos, "the value must not be negative")
	case c == '+':
		return 0, 0, p.fail(valPos, `remove the "+" sign; values are never negative`)
	case c == '.':
		return 0, 0, p.fail(valPos, `write a digit before the decimal point, e.g. "0.5"`)
	case !isDigit(c):
		return 0, 0, p.fail(valPos, "expected a number after %q, got %q", op.String(), p.next())
	}
	for p.i < len(p.s) && isDigit(p.s[p.i]) {
		p.i++
	}
	whole := true
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		if p.i == len(p.s) || !isDigit(p.s[p.i]) {
			return 0, 0, p.fail(p.i-1, "a decimal point must be followed by digits")
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
			return 0, 0, p.fail(p.i, "a number has at most one decimal point")
		}
		if (c == 'e' || c == 'E') && p.i+1 < len(p.s) {
			if d := p.s[p.i+1]; isDigit(d) || d == '+' || d == '-' {
				return 0, 0, p.fail(p.i, "exponents are not supported; write the number out in full")
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
			return 0, 0, p.fail(unitPos, "remove the space between the number and its unit")
		}
		return 0, 0, p.fail(p.i, "unexpected %q after the value", string(r))
	}

	subject := metric
	if subject == "" {
		subject = "the value"
	}
	v, msg, atUnit := value(k, subject, number, unit, whole)
	if msg != "" {
		pos := valPos
		if atUnit {
			pos = unitPos
		}
		return 0, 0, p.fail(pos, "%s", msg)
	}
	return op, v, nil
}

// afterMetric is " after <metric>" for operator messages, or "" with no
// metric.
func afterMetric(metric string) string {
	if metric == "" {
		return ""
	}
	return " after " + metric
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

// normalizeUnit lowercases a unit for matching. Units are ASCII apart from
// the micro prefix, which may be written as µ (U+00B5, micro sign) or μ
// (U+03BC, Greek small mu) and becomes "u". Only ASCII letters are folded,
// so look-alikes such as the Kelvin sign (U+212A) and Greek capital Mu
// (U+039C, which reads as an ASCII M) match no unit. ok is false for any
// other non-ASCII rune.
func normalizeUnit(unit string) (u string, ok bool) {
	for _, micro := range []string{"\u00b5", "\u03bc"} {
		if strings.HasPrefix(unit, micro) {
			unit = "u" + unit[len(micro):]
			break
		}
	}
	b := []byte(unit)
	for i, c := range b {
		switch {
		case c >= utf8.RuneSelf:
			return "", false
		case c >= 'A' && c <= 'Z':
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b), true
}

// hasNonZeroDigit reports whether a decimal literal is not zero.
func hasNonZeroDigit(number string) bool {
	return strings.ContainsAny(number, "123456789")
}

// value converts a number and unit to the canonical unit of kind k. It
// returns a message when the value does not fit the kind, naming the value
// as subject (a metric name, or "the value"); atUnit is set when the
// problem is the unit rather than the number.
func value(k Kind, subject, number, unit string, whole bool) (v float64, msg string, atUnit bool) {
	if strings.HasPrefix(unit, "\u039c") {
		const mu = "%s starts with the Greek capital letter Mu, not an ASCII M; "
		switch k {
		case KindDuration:
			return 0, fmt.Sprintf(mu+`write "us" (or "µs") for microseconds, or "ms" for milliseconds`, quoteShort(unit)), true
		case KindSize:
			return 0, fmt.Sprintf(mu+`write "mb" for megabytes (or "mib" for mebibytes)`, quoteShort(unit)), true
		}
	}
	u, ascii := normalizeUnit(unit)
	if !ascii {
		u = "\x00" // matches no unit
	}
	// scale returns number x 10^exp x factor, or a message when that does
	// not fit a float64.
	scale := func(exp int, factor float64) (float64, string, bool) {
		v, ok := decimal(number, exp)
		if !ok {
			return 0, "the value is too large", false
		}
		v *= factor
		if math.IsInf(v, 0) {
			return 0, "the value is too large", false
		}
		if v == 0 && hasNonZeroDigit(number) {
			return 0, "the value is too small", false
		}
		return v, "", false
	}
	badUnit := func(format string) (float64, string, bool) {
		return 0, fmt.Sprintf(format, subject, quoteShort(unit)), true
	}
	switch k {
	case KindDuration:
		switch u {
		case "us":
			return scale(0, 1)
		case "ms":
			return scale(3, 1)
		case "s":
			return scale(6, 1)
		case "m":
			return scale(6, 60)
		case "":
			return 0, fmt.Sprintf("%s is a duration and needs a unit (us, ms, s or m); did you mean %s?", subject, quoteShort(number+"ms")), false
		case "ns":
			return 0, `"ns" is not supported; the smallest unit is us (microseconds)`, true
		}
		return badUnit("%s is a duration; use us, ms, s or m, not %s")
	case KindRate:
		switch u {
		case "%":
			v, msg, _ := scale(-2, 1)
			if msg == "" && v > 1 {
				return 0, "a rate cannot exceed 100%", false
			}
			return v, msg, false
		case "":
			hint := quoteShort(number + "%")
			if v, ok := decimal(number, 0); ok && v <= 1 {
				pct := shiftDecimal(number, 2)
				hint = fmt.Sprintf("%s (or %s if %s is a fraction)", hint, quoteShort(pct+"%"), cutShort(number))
			}
			return 0, fmt.Sprintf(`%s is a rate and needs "%%"; did you mean %s?`, subject, hint), false
		}
		return badUnit("%s is a rate; write a percentage such as 5%%, not %s")
	case KindCount:
		if unit != "" {
			return badUnit("%s is a count; write a plain number such as 100, without %s")
		}
		if !whole {
			return 0, fmt.Sprintf("%s is a count; use a whole number", subject), false
		}
		return scale(0, 1)
	case KindThroughput:
		if unit != "" {
			return badUnit("%s is requests per second; write a plain number such as 100, without %s")
		}
		return scale(0, 1)
	case KindSize:
		// Wholeness is decided exactly from the literal, before any float
		// rounding: the decimal point is moved as text for the x1000 units,
		// and the x1024 units are multiplied as exact rationals.
		var exp int
		var pow2 uint
		switch u {
		case "", "b":
			// Unscaled: the literal itself must be whole, as for counts.
			if !whole {
				return 0, fmt.Sprintf("%s is a size; use a whole number of bytes", subject), false
			}
			return scale(0, 1)
		case "kb":
			exp = 3
		case "mb":
			exp = 6
		case "gb":
			exp = 9
		case "kib":
			pow2 = 10
		case "mib":
			pow2 = 20
		case "gib":
			pow2 = 30
		default:
			return badUnit("%s is a size; use b, kb, mb, gb, kib, mib or gib (or a plain number of bytes), not %s")
		}
		if bytes, ok := wholeBytes(number, exp, pow2); !ok {
			return 0, fmt.Sprintf("%s is a size; use a whole number of bytes (%s is %s bytes)",
				subject, quoteShort(number+unit), quoteShort(bytes)), false
		}
		if pow2 > 0 {
			return scale(0, float64(uint64(1)<<pow2))
		}
		return scale(exp, 1)
	}
	return 0, "unknown metric kind", false
}

// wholeBytes computes number x 10^exp x 2^pow2 exactly and reports whether
// it is a whole number, with its exact decimal form for messages. One of
// exp and pow2 is zero.
func wholeBytes(number string, exp int, pow2 uint) (string, bool) {
	if pow2 == 0 {
		s := shiftDecimal(number, exp)
		return s, !strings.Contains(s, ".")
	}
	r, ok := new(big.Rat).SetString(number)
	if !ok {
		return number, false // not reached: the parser checked the syntax
	}
	r.Mul(r, new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), pow2)))
	if r.IsInt() {
		return r.Num().String(), true
	}
	// The denominator divides a power of ten no larger than the literal's,
	// so this many fraction digits are exact.
	_, frac, _ := strings.Cut(number, ".")
	s := r.FloatString(len(frac))
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s, false
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
// on the test and on scenarios but not on steps. It checks Valid first.
// Errors are *ScopeError, or wrap ErrInvalidThreshold.
func (t Threshold) ValidFor(k ScopeKind) error {
	if err := t.Valid(); err != nil {
		return err
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

// Valid reports whether t has the invariants every threshold from Parse
// has: a metric from the catalog, a known operator, and a finite,
// non-negative Value that fits the metric's kind (a whole number for
// counts and sizes, at most 1 for rates). It is necessary, not
// sufficient: a valid threshold need not be one Parse can produce. Evaluate
// gives an invalid threshold no_data; callers that build or receive
// thresholds by other means (the engine at startup, the backend before
// re-evaluating) should call Valid up front and report the error. Errors
// wrap ErrInvalidThreshold.
func (t Threshold) Valid() error {
	if m, ok := lookupMetric(t.Metric.Name); !ok || m != t.Metric {
		return fmt.Errorf("%w: the metric is not from the catalog; build it with Parse", ErrInvalidThreshold)
	}
	switch t.Op {
	case OpLT, OpLE, OpGT, OpGE, OpEQ:
	default:
		return fmt.Errorf("%w: unknown operator %s", ErrInvalidThreshold, t.Op)
	}
	v := t.Value
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return fmt.Errorf("%w: the value must be a finite number of at least 0", ErrInvalidThreshold)
	}
	switch t.Metric.Kind {
	case KindCount, KindSize:
		if v != math.Trunc(v) {
			return fmt.Errorf("%w: %s takes a whole number", ErrInvalidThreshold, t.Metric.Name)
		}
	case KindRate:
		if v > 1 {
			return fmt.Errorf("%w: a rate cannot exceed 100%%", ErrInvalidThreshold)
		}
	}
	return nil
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

// equal reports whether a measured value a and a bound b are equal for
// the == operator. Counts and sizes are whole numbers and compare exactly.
// Durations, rates and throughput are derived by floating-point
// arithmetic, so they compare with a tolerance:
//
//	|a-b| <= 1e-9 * max(1, |a|, |b|)
//
// An infinite value equals only the same infinity: the tolerance would
// otherwise be infinite too.
func equal(k Kind, a, b float64) bool {
	switch k {
	case KindCount, KindSize:
		return a == b
	}
	if math.IsInf(a, 0) || math.IsInf(b, 0) {
		return a == b
	}
	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}
