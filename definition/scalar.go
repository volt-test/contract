package definition

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
)

// ScalarKind is the JSON kind of a Scalar.
type ScalarKind int

// The kinds of a Scalar. The zero Scalar has kind 0, which is none of them.
const (
	ScalarString ScalarKind = iota + 1
	ScalarNumber
	ScalarBool
)

// Scalar is a check's expected value: a JSON string, number or true/false.
// A number keeps its literal as written, so 42 and 42.0 stay distinct
// Scalars (Equal, MarshalJSON and labels use the literal) and a 19-digit
// number loses no digits; it must be within float64 range. How a check
// compares a Scalar with a response value is defined on Check.Equals. A
// check field holds a *Scalar, so "equals": "" (an empty string) is a
// value, not an absent key.
type Scalar struct {
	kind ScalarKind
	text string      // ScalarString
	num  json.Number // ScalarNumber
	b    bool        // ScalarBool
}

// StringScalar returns a string Scalar.
func StringScalar(s string) Scalar { return Scalar{kind: ScalarString, text: s} }

// NumberScalar returns a number Scalar with the literal n, which must be a
// JSON number (MarshalJSON fails otherwise).
func NumberScalar(n json.Number) Scalar { return Scalar{kind: ScalarNumber, num: n} }

// BoolScalar returns a true/false Scalar.
func BoolScalar(b bool) Scalar { return Scalar{kind: ScalarBool, b: b} }

// Kind returns the Scalar's JSON kind, or 0 for the zero Scalar.
func (s Scalar) Kind() ScalarKind { return s.kind }

// Text returns the value of a string Scalar, and "" for other kinds.
func (s Scalar) Text() string { return s.text }

// Number returns the literal of a number Scalar, and "" for other kinds.
func (s Scalar) Number() json.Number { return s.num }

// Bool returns the value of a true/false Scalar, and false for other kinds.
func (s Scalar) Bool() bool { return s.b }

// Equal reports whether two Scalars have the same kind and value; numbers
// compare by literal. go-cmp uses it.
func (s Scalar) Equal(o Scalar) bool { return s == o }

var (
	errScalar      = errors.New("definition: must be a string, a number or true/false")
	errScalarRange = errors.New("definition: is out of range")
)

// UnmarshalJSON reads a JSON string, number or true/false. A number must
// be within float64 range: 1e999 is rejected as out of range, as it is in
// any other value, while 1e-999 is accepted. null leaves s unchanged, as
// encoding/json does for other types; Parse rejects null in v2 before
// decoding.
func (s *Scalar) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return errScalar
	}
	switch c := b[0]; {
	case string(b) == "null":
		return nil
	case string(b) == "true" || string(b) == "false":
		*s = BoolScalar(b[0] == 't')
		return nil
	case c == '"':
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = StringScalar(v)
		return nil
	case isNumberLiteral(b):
		if _, err := strconv.ParseFloat(string(b), 64); errors.Is(err, strconv.ErrRange) {
			return errScalarRange
		}
		*s = NumberScalar(json.Number(b))
		return nil
	}
	return errScalar
}

// MarshalJSON writes the string, the number literal as it was read, or
// true/false. The zero Scalar and a number Scalar whose literal is not a
// JSON number are errors.
func (s Scalar) MarshalJSON() ([]byte, error) {
	switch s.kind {
	case ScalarString:
		return encodeString(s.text), nil
	case ScalarNumber:
		if !isNumberLiteral([]byte(s.num)) {
			return nil, errors.New("definition: Scalar number " + strconv.Quote(truncate(string(s.num), maxEcho)) + " is not a JSON number")
		}
		return []byte(s.num), nil
	case ScalarBool:
		return []byte(strconv.FormatBool(s.b)), nil
	}
	return nil, errors.New("definition: the zero Scalar has no value")
}

// label renders the value for a check label: the JSON encoding of a
// string (without HTML escaping), the literal of a number, true or false.
func (s Scalar) label() string {
	switch s.kind {
	case ScalarString:
		return string(encodeString(s.text))
	case ScalarNumber:
		return string(s.num)
	case ScalarBool:
		return strconv.FormatBool(s.b)
	}
	return ""
}

// isNumberLiteral reports whether b is exactly one JSON number, with no
// space around it.
func isNumberLiteral(b []byte) bool {
	if len(b) == 0 || !isNumberStart(b[0]) || b[len(b)-1] < '0' || b[len(b)-1] > '9' {
		return false
	}
	var n json.Number
	return json.Unmarshal(b, &n) == nil
}

// encodeString writes s as a JSON string without escaping <, > and &.
func encodeString(s string) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return bytes.TrimRight(buf.Bytes(), "\n")
}
