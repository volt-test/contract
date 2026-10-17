package definition

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// SchemaVersion is the version of the definition format, written in JSON as
// "MAJOR.MINOR". A definition without schema_version is v1.
type SchemaVersion struct {
	Major int
	Minor int
}

var (
	// V1 is every definition written before schema_version existed.
	V1 = SchemaVersion{Major: 1, Minor: 0}
	// V2_0 is the first v2 format. It is an open preview until SDK 2.0.0
	// ships: fields may still join it without a minor bump.
	V2_0 = SchemaVersion{Major: 2, Minor: 0}
)

// supported lists the versions this module reads, oldest first.
var supported = []SchemaVersion{V1, V2_0}

// minEngineRelease maps each schema_version to the first VoltTest engine
// release that reads it. It drives UnsupportedVersionError text and must
// agree with the Compatibility table in the README (a test checks this).
// Update both when an engine release adds support for a version.
var minEngineRelease = map[string]string{
	"1":   "any",
	"2.0": "unreleased",
}

// SupportedSchemaVersions returns the schema versions this module reads,
// oldest first: ["1", "2.0"].
func SupportedSchemaVersions() []string {
	out := make([]string, len(supported))
	for i, v := range supported {
		out[i] = v.String()
	}
	return out
}

// MinEngineRelease returns the first engine release that reads v, as listed
// in the README Compatibility table. ok is false for versions this module
// does not know.
func MinEngineRelease(v SchemaVersion) (release string, ok bool) {
	release, ok = minEngineRelease[v.String()]
	return release, ok
}

// String returns "1" for v1 and "MAJOR.MINOR" otherwise.
func (v SchemaVersion) String() string {
	if v == V1 {
		return "1"
	}
	return strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor)
}

// ParseSchemaVersion parses the string form of a schema_version: "1",
// "1.0" or "MAJOR.MINOR" with decimal parts, no leading zeros and a major
// of at least 1.
func ParseSchemaVersion(s string) (SchemaVersion, error) {
	v, ok := parseVersionString(s)
	if !ok {
		return SchemaVersion{}, &InvalidVersionError{Value: strconv.Quote(s)}
	}
	return v, nil
}

func parseVersionString(s string) (SchemaVersion, bool) {
	if s == "1" {
		return V1, true
	}
	major, minor, found := strings.Cut(s, ".")
	if !found {
		return SchemaVersion{}, false
	}
	ma, ok := parseVersionPart(major)
	if !ok || ma == 0 {
		return SchemaVersion{}, false
	}
	mi, ok := parseVersionPart(minor)
	if !ok {
		return SchemaVersion{}, false
	}
	return SchemaVersion{Major: ma, Minor: mi}, true
}

// parseVersionPart accepts "0" or a decimal number without a leading zero
// or sign.
func parseVersionPart(s string) (int, bool) {
	if s == "" || len(s) > 9 {
		return 0, false
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// UnmarshalJSON accepts the number 1 and strings accepted by
// ParseSchemaVersion. Any other value, including "" and the number 2, is an
// *InvalidVersionError. JSON null leaves v unchanged.
func (v *SchemaVersion) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if string(data) == "null" {
		return nil
	}
	if string(data) == "1" {
		*v = V1
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err == nil {
			if parsed, ok := parseVersionString(s); ok {
				*v = parsed
				return nil
			}
		}
	}
	return &InvalidVersionError{Value: string(data)}
}

// MarshalJSON writes the string form ("1", "2.0"). The zero SchemaVersion
// is not a version and fails to marshal.
func (v SchemaVersion) MarshalJSON() ([]byte, error) {
	if v.Major <= 0 || v.Minor < 0 {
		return nil, fmt.Errorf("definition: cannot marshal schema_version %d.%d", v.Major, v.Minor)
	}
	return []byte(strconv.Quote(v.String())), nil
}

// CheckSupported reports whether a reader that supports the given versions
// can read a definition at version v. A reader supports v when it lists a
// version with the same major and an equal or newer minor, so a reader of
// "2.1" also reads "2.0". It returns *UnsupportedVersionError otherwise.
//
// The backend uses it to check a definition against the versions an engine
// image declares. An entry in supported that is not a valid version is an
// error (a caller bug, so it does not match ErrInvalidDefinition).
func CheckSupported(v SchemaVersion, supported []string) error {
	parsed := make([]SchemaVersion, len(supported))
	for i, s := range supported {
		sv, ok := parseVersionString(s)
		if !ok {
			return fmt.Errorf("definition: the supported version list has an invalid entry %q", s)
		}
		parsed[i] = sv
	}
	for _, sv := range parsed {
		if sv.Major == v.Major && sv.Minor >= v.Minor {
			return nil
		}
	}
	list := make([]string, len(supported))
	copy(list, supported)
	return &UnsupportedVersionError{Version: v, Supported: list}
}
