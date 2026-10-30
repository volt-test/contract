package definition

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
)

// JSONBody is the body of a request with body_type "json": a JSON object
// or array, held as JSON text. UnmarshalJSON stores it compacted (white
// space removed) and otherwise as written: key order, duplicate keys,
// number literals such as 19.90 and string escapes are kept, and <, > and
// & are not escaped. The zero JSONBody (nil or empty) means absent.
//
// MarshalJSON writes the stored text, but encoding/json escapes <, > and
// & in every MarshalJSON output, so json.Marshal of a Request or a
// Definition writes them as \u003c, \u003e and \u0026, as it does in
// body and headers. The JSON value is the same.
type JSONBody []byte

var errJSONBody = errors.New("definition: must be an object or an array")

// UnmarshalJSON accepts a JSON object or array and stores it compacted.
// null leaves b unchanged, as encoding/json does for other types; Parse
// rejects null in v2 before decoding.
func (b *JSONBody) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if string(data) == "null" {
		return nil
	}
	if len(data) == 0 || (data[0] != '{' && data[0] != '[') {
		return errJSONBody
	}
	var buf bytes.Buffer
	buf.Grow(len(data))
	if err := json.Compact(&buf, data); err != nil {
		return err
	}
	*b = buf.Bytes()
	return nil
}

// MarshalJSON writes the stored JSON text. The zero JSONBody writes null;
// text that is not a JSON object or array is an error.
func (b JSONBody) MarshalJSON() ([]byte, error) {
	if len(b) == 0 {
		return []byte("null"), nil
	}
	t := bytes.TrimSpace(b)
	if len(t) == 0 || (t[0] != '{' && t[0] != '[') || !json.Valid(t) {
		return nil, errors.New("definition: JSONBody is not a JSON object or array")
	}
	return append([]byte(nil), b...), nil
}

// Field is one field of a form body: a name and its value.
type Field struct {
	Name  string
	Value string
}

// Fields are the fields of a request with body_type "form", in the order
// written. On the wire they are a JSON object whose values are strings;
// each name appears once. The zero Fields (nil or empty) means absent.
type Fields []Field

var fieldsType = reflect.TypeOf(Fields(nil))

const fieldDuplicate = "appears more than once; field names must be unique"

// fieldsProblems checks a fields object member by member: every value a
// string and every name (after unescaping) used once. It calls add with
// the path of each member that has a problem, in document order. raw must
// be an object that passed json.Unmarshal.
func fieldsProblems(raw []byte, path string, add func(path, msg string)) error {
	var seen map[string]bool
	var small [smallObject]string
	names := small[:0]
	return forEachMember(raw, func(k, _, v []byte) {
		switch {
		case string(v) == "null":
			add(joinKey(path, k), "must not be null")
		case v[0] != '"':
			add(joinKey(path, k), "must be a string")
		}
		dup := false
		if seen == nil && len(names) < smallObject {
			for _, n := range names {
				if n == string(k) {
					dup = true
					break
				}
			}
			if !dup {
				names = append(names, string(k))
			}
		} else {
			if seen == nil {
				seen = make(map[string]bool, 2*len(names))
				for _, n := range names {
					seen[n] = true
				}
			}
			dup = seen[string(k)]
			seen[string(k)] = true
		}
		if dup {
			add(joinKey(path, k), fieldDuplicate)
		}
	})
}

// UnmarshalJSON reads a JSON object whose values are strings, keeping the
// order of its members. A value that is not a string and a name that
// appears twice are errors. {} reads as absent (nil). null leaves f
// unchanged, as encoding/json does for other types; Parse rejects null in
// v2 before decoding.
func (f *Fields) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if string(data) == "null" {
		return nil
	}
	if len(data) == 0 || data[0] != '{' {
		return errors.New("definition: must be an object")
	}
	var first *Problem
	err := fieldsProblems(data, "fields", func(path, msg string) {
		if first == nil {
			first = &Problem{Path: path, Message: msg}
		}
	})
	if err != nil {
		return err
	}
	if first != nil {
		return errors.New("definition: " + first.String())
	}
	var out Fields
	var derr error
	_ = forEachMember(data, func(k, _, v []byte) {
		var s string
		if err := json.Unmarshal(v, &s); err != nil && derr == nil {
			derr = err
		}
		out = append(out, Field{Name: string(k), Value: s})
	})
	if derr != nil {
		return derr
	}
	*f = out
	return nil
}

// MarshalJSON writes the fields as a JSON object in order, without
// escaping <, > and &. nil writes null. A name used twice is an error.
func (f Fields) MarshalJSON() ([]byte, error) {
	if f == nil {
		return []byte("null"), nil
	}
	seen := make(map[string]bool, len(f))
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, fd := range f {
		if seen[fd.Name] {
			return nil, errors.New("definition: form field " + quoteShort(fd.Name) + " " + fieldDuplicate)
		}
		seen[fd.Name] = true
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(encodeString(fd.Name))
		buf.WriteByte(':')
		buf.Write(encodeString(fd.Value))
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Get returns the value of the field name and whether it is set.
func (f Fields) Get(name string) (string, bool) {
	for _, fd := range f {
		if fd.Name == name {
			return fd.Value, true
		}
	}
	return "", false
}
