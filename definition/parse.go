package definition

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
)

// Parse reads a test definition. The input must be exactly one JSON object.
//
// The top-level "schema_version" key (an exact match after unescaping)
// selects the format.
// Without it, or with 1, "1", "1.0" or null, the definition is v1 and is
// read the way the v1 readers read it: unknown keys are ignored, keys match
// case-insensitively, a case variant such as "Schema_Version" is ignored,
// v2-only keys (target.base_url) are dropped unread, and values are not
// validated beyond their JSON type. A value of the wrong JSON type in a
// core field (one every v1 reader decodes) is a *ValidationError; in a
// loose field (one some v1 reader ignores) it is decoded as if absent and
// the problem is recorded in Definition.V1TypeProblems. See the package
// documentation.
//
// A v2 definition is strict: every key must be known to its schema_version
// (*UnknownFieldError), every value must have the right JSON type, and the
// structure must be valid (*ValidationError). When a v2 definition has
// unknown keys, Parse reports only those, not other problems.
//
// Every error matches ErrInvalidDefinition.
func Parse(data []byte) (*Definition, error) {
	top, err := decodeTop(data)
	if err != nil {
		return nil, &SyntaxError{Err: err}
	}
	version, declared, err := peekVersion(top)
	if err != nil {
		return nil, err
	}
	if err := CheckSupported(version, SupportedSchemaVersions()); err != nil {
		return nil, err
	}
	if version.Major == 1 {
		return parseV1(data, declared)
	}
	return parseV2(data, version)
}

// IsV1 reports whether the definition is in the v1 format.
func (d *Definition) IsV1() bool { return d.Version().Major == 1 }

// Version returns the definition's schema_version, V1 when it has none.
func (d *Definition) Version() SchemaVersion {
	if d.SchemaVersion == nil {
		return V1
	}
	return *d.SchemaVersion
}

var errNotObject = errors.New("a test definition must be a JSON object")

// decodeTop checks that data is exactly one JSON object and returns the
// raw value of its top-level "schema_version" key (an exact match after
// unescaping, so "schema_\u0076ersion" counts and "Schema_Version" does
// not; the last one wins, as in encoding/json), or nil when there is none.
func decodeTop(data []byte) (json.RawMessage, error) {
	// Decoding into an empty struct checks the syntax of the whole input
	// without keeping any of it.
	var probe struct{}
	if err := json.Unmarshal(data, &probe); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) {
			return nil, errNotObject
		}
		return nil, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errNotObject // null
	}
	var version json.RawMessage
	err := forEachMember(trimmed, func(k, _, v []byte) {
		if string(k) == "schema_version" {
			version = v
		}
	})
	return version, err
}

// peekVersion reads the top-level schema_version value. declared is false
// when it is missing or null.
func peekVersion(raw json.RawMessage) (v SchemaVersion, declared bool, err error) {
	if raw == nil || string(bytes.TrimSpace(raw)) == "null" {
		return V1, false, nil
	}
	if err := v.UnmarshalJSON(raw); err != nil {
		return SchemaVersion{}, false, err
	}
	return v, true, nil
}

func parseV1(data []byte, declared bool) (*Definition, error) {
	var w v1walker
	doc, _ := w.value(data, reflect.TypeOf(v1Wrapper{}), "", "", false)
	if err := w.core.err(); err != nil {
		return nil, err
	}
	d := &Definition{}
	wrapper := v1Wrapper{Definition: d}
	if err := json.Unmarshal(doc, &wrapper); err != nil {
		return nil, decodeError(err)
	}
	if declared {
		v := V1
		d.SchemaVersion = &v
	}
	d.APIKey = NewSecret(wrapper.APIKey)
	d.V1TypeProblems = w.loose
	return d, nil
}

func parseV2(data []byte, version SchemaVersion) (*Definition, error) {
	w := walker{}
	w.walk(data, reflect.TypeOf(Definition{}), "")
	if len(w.unknown) > 0 {
		return nil, &UnknownFieldError{Paths: w.unknown, Omitted: w.omitted, Version: version}
	}
	if err := w.problems.err(); err != nil {
		return nil, err
	}
	d := &Definition{}
	if err := json.Unmarshal(data, d); err != nil {
		return nil, decodeError(err)
	}
	if err := validateV2(d); err != nil {
		return nil, err
	}
	return d, nil
}

// decodeError turns a decoding error the walkers did not predict into a
// *ValidationError. It is a safety net: the walkers check every JSON type
// before decoding.
func decodeError(err error) error {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		// Go 1.24+ prefixes the path with the embedded Definition of the v1
		// wrapper; older releases do not.
		return &ValidationError{Problems: []Problem{{
			Path:    strings.TrimPrefix(te.Field, "Definition."),
			Message: "has the wrong JSON type (" + te.Value + ")",
		}}}
	}
	var ie *InvalidVersionError
	if errors.As(err, &ie) {
		return ie
	}
	return &ValidationError{Problems: []Problem{{Message: err.Error()}}}
}
