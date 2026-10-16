package definition

import (
	"bytes"
	"encoding/json"
	"errors"
)

// The walkers iterate the arrays and objects of a document without
// decoding each member, so a million-element array costs no per-element
// allocation.
//
// Precondition: the input must already have passed json.Unmarshal (Parse
// checks the whole document first). On malformed input the scanners never
// panic and usually return errMalformed, but they do not detect every
// syntax error: they find value boundaries, they do not validate values.

var errMalformed = errors.New("malformed JSON")

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// skipValue returns the index just past the JSON value starting at b[i].
func skipValue(b []byte, i int) (int, error) {
	if i >= len(b) {
		return 0, errMalformed
	}
	switch b[i] {
	case '"':
		for i++; i < len(b); i++ {
			switch b[i] {
			case '\\':
				i++
			case '"':
				return i + 1, nil
			}
		}
		return 0, errMalformed
	case '{', '[':
		depth := 0
		for ; i < len(b); i++ {
			switch b[i] {
			case '"':
				end, err := skipValue(b, i)
				if err != nil {
					return 0, err
				}
				i = end - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1, nil
				}
			}
		}
		return 0, errMalformed
	}
	start := i
scan:
	for ; i < len(b); i++ {
		switch b[i] {
		case ',', ']', '}', ' ', '\t', '\r', '\n':
			break scan
		}
	}
	if i == start {
		return 0, errMalformed
	}
	return i, nil
}

// forEachElement calls fn with each element of the JSON array raw.
func forEachElement(raw []byte, fn func(i int, elem []byte)) error {
	i := skipSpace(raw, 0)
	if i >= len(raw) || raw[i] != '[' {
		return errMalformed
	}
	i = skipSpace(raw, i+1)
	if i < len(raw) && raw[i] == ']' {
		return nil
	}
	for n := 0; ; n++ {
		end, err := skipValue(raw, i)
		if err != nil {
			return err
		}
		fn(n, raw[i:end])
		i = skipSpace(raw, end)
		if i >= len(raw) {
			return errMalformed
		}
		if raw[i] == ']' {
			return nil
		}
		if raw[i] != ',' {
			return errMalformed
		}
		i = skipSpace(raw, i+1)
	}
}

// forEachMember calls fn with each member of the JSON object raw, in
// document order, keeping duplicate keys. key is the unescaped key; it
// aliases raw when the key needs no unescaping. quoted is the key as
// written, quotes included.
func forEachMember(raw []byte, fn func(key, quoted, val []byte)) error {
	i := skipSpace(raw, 0)
	if i >= len(raw) || raw[i] != '{' {
		return errMalformed
	}
	i = skipSpace(raw, i+1)
	if i < len(raw) && raw[i] == '}' {
		return nil
	}
	for {
		if i >= len(raw) || raw[i] != '"' {
			return errMalformed
		}
		start := i
		end, err := skipValue(raw, i)
		if err != nil {
			return err
		}
		key, err := decodeKey(raw[start:end])
		if err != nil {
			return err
		}
		i = skipSpace(raw, end)
		if i >= len(raw) || raw[i] != ':' {
			return errMalformed
		}
		i = skipSpace(raw, i+1)
		vend, err := skipValue(raw, i)
		if err != nil {
			return err
		}
		fn(key, raw[start:end], raw[i:vend])
		i = skipSpace(raw, vend)
		if i >= len(raw) {
			return errMalformed
		}
		if raw[i] == '}' {
			return nil
		}
		if raw[i] != ',' {
			return errMalformed
		}
		i = skipSpace(raw, i+1)
	}
}

// decodeKey unescapes a quoted key the way encoding/json does. Plain
// printable ASCII keys are returned as a subslice, without copying.
func decodeKey(quoted []byte) ([]byte, error) {
	if len(quoted) < 2 {
		return nil, errMalformed
	}
	if bytes.IndexByte(quoted, '\\') < 0 && isASCIIPrintable(quoted) {
		return quoted[1 : len(quoted)-1], nil
	}
	var s string
	if err := json.Unmarshal(quoted, &s); err != nil {
		return nil, err
	}
	return []byte(s), nil
}

func isASCIIPrintable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c >= 0x80 {
			return false
		}
	}
	return true
}
