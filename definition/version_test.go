package definition_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/volt-test/contract/definition"
)

func TestSchemaVersionUnmarshal(t *testing.T) {
	valid := []struct {
		in   string
		want definition.SchemaVersion
	}{
		{`1`, definition.V1},
		{`"1"`, definition.V1},
		{`"1.0"`, definition.V1},
		{`"2.0"`, definition.V2_0},
		{`"2.1"`, definition.SchemaVersion{Major: 2, Minor: 1}},
		{`"1.1"`, definition.SchemaVersion{Major: 1, Minor: 1}},
		{`"3.0"`, definition.SchemaVersion{Major: 3, Minor: 0}},
		{`"10.12"`, definition.SchemaVersion{Major: 10, Minor: 12}},
		{`"2.0"`, definition.V2_0},
	}
	for _, tc := range valid {
		t.Run(tc.in, func(t *testing.T) {
			var v definition.SchemaVersion
			if err := json.Unmarshal([]byte(tc.in), &v); err != nil {
				t.Fatalf("Unmarshal(%s): %v", tc.in, err)
			}
			if v != tc.want {
				t.Fatalf("Unmarshal(%s) = %+v, want %+v", tc.in, v, tc.want)
			}
		})
	}

	invalid := []string{
		`""`, `1.0`, `2`, `2.0`, `"2"`, `"v2.0"`, `"2.0.1"`, `"02.0"`, `"2.00"`, `" 2.0"`,
		`"2.0 "`, `"0.1"`, `"0.0"`, `"-1.0"`, `"+2.0"`, `"2."`, `".0"`, `"2.x"`, `1e0`, `-1`,
		`true`, `false`, `{}`, `[]`, `["2.0"]`, `"9999999999.0"`, `"2.0\n"`, `"V2.0"`,
	}
	for _, in := range invalid {
		t.Run("invalid "+in, func(t *testing.T) {
			var v definition.SchemaVersion
			err := json.Unmarshal([]byte(in), &v)
			var ive *definition.InvalidVersionError
			if !errors.As(err, &ive) {
				t.Fatalf("Unmarshal(%s) error = %v, want *InvalidVersionError", in, err)
			}
			if ive.Value != in {
				t.Errorf("Value = %q, want %q", ive.Value, in)
			}
			if !errors.Is(err, definition.ErrInvalidDefinition) {
				t.Errorf("errors.Is(ErrInvalidDefinition) = false")
			}
		})
	}
}

func TestSchemaVersionNullLeavesValue(t *testing.T) {
	v := definition.V2_0
	if err := v.UnmarshalJSON([]byte("null")); err != nil || v != definition.V2_0 {
		t.Fatalf("UnmarshalJSON(null) = %v, %+v; want nil, unchanged", err, v)
	}
}

func TestInvalidVersionErrorText(t *testing.T) {
	_, err := definition.Parse([]byte(`{"schema_version": 2, "name": "x"}`))
	want := `definition: invalid schema_version 2: use a "MAJOR.MINOR" string such as "2.0" (or 1 for v1)`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v\nwant    %s", err, want)
	}
	_, err = definition.ParseSchemaVersion("v2.0")
	want = `definition: invalid schema_version "v2.0": use a "MAJOR.MINOR" string such as "2.0" (or 1 for v1)`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v\nwant    %s", err, want)
	}
}

func TestSchemaVersionMarshal(t *testing.T) {
	cases := []struct {
		v    definition.SchemaVersion
		want string
	}{
		{definition.V1, `"1"`},
		{definition.V2_0, `"2.0"`},
		{definition.SchemaVersion{Major: 2, Minor: 11}, `"2.11"`},
	}
	for _, tc := range cases {
		got, err := json.Marshal(tc.v)
		if err != nil || string(got) != tc.want {
			t.Errorf("Marshal(%+v) = %s, %v; want %s", tc.v, got, err, tc.want)
		}
	}
	if _, err := json.Marshal(definition.SchemaVersion{}); err == nil {
		t.Error("Marshal(zero SchemaVersion) succeeded, want error")
	}
	// A zero Definition has no schema_version: it must never marshal "0.0".
	out, err := json.Marshal(definition.Definition{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "schema_version") || strings.Contains(string(out), "0.0") {
		t.Errorf("Marshal(Definition{}) = %s, want no schema_version", out)
	}
}

func TestSchemaVersionString(t *testing.T) {
	for in, want := range map[string]string{"1": "1", "1.0": "1", "2.0": "2.0", "2.3": "2.3"} {
		v, err := definition.ParseSchemaVersion(in)
		if err != nil || v.String() != want {
			t.Errorf("ParseSchemaVersion(%q).String() = %q, %v; want %q", in, v.String(), err, want)
		}
	}
}

func TestSupported(t *testing.T) {
	if got, want := definition.SupportedSchemaVersions(), []string{"1", "2.0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedSchemaVersions() = %q, want %q", got, want)
	}
	// Callers get copies.
	definition.SupportedSchemaVersions()[0] = "9.9"
	if definition.SupportedSchemaVersions()[0] != "1" {
		t.Fatal("Supported results share state with the package")
	}
	for _, s := range definition.SupportedSchemaVersions() {
		v, err := definition.ParseSchemaVersion(s)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := definition.MinEngineRelease(v); !ok {
			t.Errorf("supported version %s has no minimum engine release", s)
		}
	}
}

func TestCheckSupported(t *testing.T) {
	v21 := definition.SchemaVersion{Major: 2, Minor: 1}
	cases := []struct {
		name      string
		v         definition.SchemaVersion
		supported []string
		wantErr   string
	}{
		{"v1 on v1 reader", definition.V1, []string{"1"}, ""},
		{"2.0 on 2.0 reader", definition.V2_0, []string{"1", "2.0"}, ""},
		{"2.0 on 2.1 reader", definition.V2_0, []string{"1", "2.1"}, ""},
		{"1.0 string form", definition.V1, []string{"1.0"}, ""},
		{"2.1 on 2.0 reader", v21, []string{"1", "2.0"},
			`definition: schema_version "2.1" is not supported (supported: "1", "2.0"); it needs a newer engine release; see https://github.com/volt-test/contract#compatibility`},
		{"2.0 on v1-only engine", definition.V2_0, []string{"1"},
			`definition: schema_version "2.0" is not supported (supported: "1"); minimum engine release: unreleased; see https://github.com/volt-test/contract#compatibility`},
		{"v1 on 2.x-only reader", definition.V1, []string{"2.0"},
			`definition: schema_version "1" is not supported (supported: "2.0"); minimum engine release: any; see https://github.com/volt-test/contract#compatibility`},
		{"3.0 on empty list", definition.SchemaVersion{Major: 3}, nil,
			`definition: schema_version "3.0" is not supported (supported: none); it needs a newer engine release; see https://github.com/volt-test/contract#compatibility`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := definition.CheckSupported(tc.v, tc.supported)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("CheckSupported: %v", err)
				}
				return
			}
			var uve *definition.UnsupportedVersionError
			if !errors.As(err, &uve) {
				t.Fatalf("error = %v, want *UnsupportedVersionError", err)
			}
			if uve.Version != tc.v {
				t.Errorf("Version = %v, want %v", uve.Version, tc.v)
			}
			if err.Error() != tc.wantErr {
				t.Errorf("error = %s\nwant    %s", err, tc.wantErr)
			}
		})
	}

	err := definition.CheckSupported(definition.V1, []string{"1", "2"})
	if want := `definition: the supported version list has an invalid entry "2"`; err == nil || err.Error() != want {
		t.Fatalf("bad supported entry: error = %v, want %s", err, want)
	}
	if errors.Is(err, definition.ErrInvalidDefinition) {
		t.Error("a bad supported list is a caller bug, not an invalid definition")
	}
}

func TestParseUnsupportedVersion(t *testing.T) {
	cases := map[string]string{
		`"2.1"`: `definition: schema_version "2.1" is not supported (supported: "1", "2.0"); it needs a newer engine release; see https://github.com/volt-test/contract#compatibility`,
		`"3.0"`: `definition: schema_version "3.0" is not supported (supported: "1", "2.0"); it needs a newer engine release; see https://github.com/volt-test/contract#compatibility`,
		`"1.1"`: `definition: schema_version "1.1" is not supported (supported: "1", "2.0"); it needs a newer engine release; see https://github.com/volt-test/contract#compatibility`,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			// The 2.1-only field must not turn this into an unknown-field
			// error: the version check runs first.
			_, err := definition.Parse([]byte(`{"schema_version": ` + in + `, "name": "x", "field_from_the_future": true}`))
			var uve *definition.UnsupportedVersionError
			if !errors.As(err, &uve) {
				t.Fatalf("error = %v, want *UnsupportedVersionError", err)
			}
			if !reflect.DeepEqual(uve.Supported, []string{"1", "2.0"}) {
				t.Errorf("Supported = %q", uve.Supported)
			}
			if err.Error() != want {
				t.Errorf("error = %s\nwant    %s", err, want)
			}
			if !errors.Is(err, definition.ErrInvalidDefinition) {
				t.Error("errors.Is(ErrInvalidDefinition) = false")
			}
		})
	}
}

func TestParseVersionRouting(t *testing.T) {
	const body = `"name": "x", "virtual_users": 1, "duration": "1s", "scenarios": [{"name": "s", "steps": [{"name": "a", "request": {"method": "GET", "url": "https://example.com/"}}]}]`
	cases := []struct {
		name     string
		doc      string
		wantV1   bool
		declared bool
		errType  any
	}{
		{"missing", `{` + body + `}`, true, false, nil},
		{"null", `{"schema_version": null, ` + body + `}`, true, false, nil},
		{"number 1", `{"schema_version": 1, ` + body + `}`, true, true, nil},
		{"string 1", `{"schema_version": "1", ` + body + `}`, true, true, nil},
		{"string 1.0", `{"schema_version": "1.0", ` + body + `}`, true, true, nil},
		{"2.0", `{"schema_version": "2.0", ` + body + `}`, false, true, nil},
		{"escaped key", `{"schema_\u0076ersion": "2.0", ` + body + `}`, false, true, nil},
		{"escaped key and value", `{"\u0073chema_version": "\u0032.0", ` + body + `}`, false, true, nil},
		{"case variant is not the key", `{"Schema_Version": "2.0", ` + body + `}`, true, false, nil},
		{"duplicate keys, last wins (v1)", `{"schema_version": "2.0", "schema_version": "1", ` + body + `}`, true, true, nil},
		{"duplicate keys, last wins (v2)", `{"schema_version": "1", "schema_version": "2.0", ` + body + `}`, false, true, nil},
		{"empty string", `{"schema_version": "", ` + body + `}`, false, false, (*definition.InvalidVersionError)(nil)},
		{"number 1.0", `{"schema_version": 1.0, ` + body + `}`, false, false, (*definition.InvalidVersionError)(nil)},
		{"number 2", `{"schema_version": 2, ` + body + `}`, false, false, (*definition.InvalidVersionError)(nil)},
		{"padded", `{"schema_version": " 2.0", ` + body + `}`, false, false, (*definition.InvalidVersionError)(nil)},
		{"bool", `{"schema_version": true, ` + body + `}`, false, false, (*definition.InvalidVersionError)(nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := definition.Parse([]byte(tc.doc))
			if tc.errType != nil {
				var ive *definition.InvalidVersionError
				if !errors.As(err, &ive) {
					t.Fatalf("error = %v, want *InvalidVersionError", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if d.IsV1() != tc.wantV1 {
				t.Errorf("IsV1() = %v, want %v", d.IsV1(), tc.wantV1)
			}
			if (d.SchemaVersion != nil) != tc.declared {
				t.Errorf("SchemaVersion = %v, want declared=%v", d.SchemaVersion, tc.declared)
			}
			if tc.wantV1 && d.Version() != definition.V1 {
				t.Errorf("Version() = %v, want 1", d.Version())
			}
			if !tc.wantV1 && d.Version() != definition.V2_0 {
				t.Errorf("Version() = %v, want 2.0", d.Version())
			}
		})
	}
}

func TestInvalidVersionErrorTruncatesValue(t *testing.T) {
	long := `"` + strings.Repeat("é", 100) + `"`
	_, err := definition.Parse([]byte(`{"schema_version": ` + long + `}`))
	var ive *definition.InvalidVersionError
	if !errors.As(err, &ive) {
		t.Fatalf("error = %v", err)
	}
	if ive.Value != long {
		t.Error("Value must keep the full input")
	}
	msg := err.Error()
	if strings.Contains(msg, long) || !strings.Contains(msg, "...") || len(msg) > 200 {
		t.Errorf("message not truncated: %s", msg)
	}
	if !utf8.ValidString(msg) {
		t.Errorf("truncation split a UTF-8 sequence: %q", msg)
	}
}
