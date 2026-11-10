package definition

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
)

// HeaderValues are the values of one request header, in order: a string,
// or in v2 a non-empty array of strings sent once per value.
type HeaderValues []string

var headerValuesType = reflect.TypeOf(HeaderValues(nil))

const (
	headerValuesKind  = "must be a string or an array of strings"
	headerValuesEmpty = "must not be an empty array; give at least one value"
)

// UnmarshalJSON accepts a string or a non-empty array of strings. null
// leaves h unchanged, as encoding/json does for other types; Parse rejects
// null in v2 before decoding.
func (h *HeaderValues) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if string(data) == "null" {
		return nil
	}
	if len(data) == 0 {
		return errors.New("definition: " + headerValuesKind)
	}
	switch data[0] {
	case '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*h = HeaderValues{s}
		return nil
	case '[':
		var out HeaderValues
		var bad error
		err := forEachElement(data, func(i int, e []byte) {
			if bad != nil {
				return
			}
			if len(e) == 0 || e[0] != '"' {
				bad = errors.New("definition: [" + strconv.Itoa(i) + "]: must be a string")
				return
			}
			var s string
			if err := json.Unmarshal(e, &s); err != nil {
				bad = err
				return
			}
			out = append(out, s)
		})
		switch {
		case err != nil:
			return errors.New("definition: " + headerValuesKind)
		case bad != nil:
			return bad
		case len(out) == 0:
			return errors.New("definition: " + headerValuesEmpty)
		}
		*h = out
		return nil
	}
	return errors.New("definition: " + headerValuesKind)
}

// MarshalJSON writes one value as a plain string, several as an array,
// and none as "". It leaves <, > and & unescaped, so the outer encoder's
// SetEscapeHTML decides, as it does for a string.
func (h HeaderValues) MarshalJSON() ([]byte, error) {
	var v any = []string(h)
	switch len(h) {
	case 0:
		return []byte(`""`), nil
	case 1:
		v = h[0]
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// headerValueProblems checks a v2 header value before it is decoded: a
// string, or a non-empty array whose elements are strings. Element
// problems are reported at path[i], building the path only for problems
// that are kept. raw is not null and passed json.Unmarshal.
func headerValueProblems(raw []byte, path string, pl *problemList) {
	switch raw[0] {
	case '"':
		return
	case '[':
		n := 0
		err := forEachElement(raw, func(i int, e []byte) {
			n++
			msg := ""
			switch {
			case string(e) == "null":
				msg = "must not be null"
			case e[0] != '"':
				msg = "must be a string"
			default:
				return
			}
			if pl.full() {
				pl.omitted++
				return
			}
			pl.add(indexPath(path, i), msg)
		})
		switch {
		case err != nil:
			pl.add(path, headerValuesKind)
		case n == 0:
			pl.add(path, headerValuesEmpty)
		}
		return
	}
	pl.add(path, headerValuesKind)
}
