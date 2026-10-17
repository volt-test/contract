package definition

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// reachable returns the struct types reachable from t through fields that
// v2 accepts (or through every field when withV1 is set).
func reachable(t reflect.Type, withV1 bool, out map[reflect.Type]bool) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(objectFormerType) {
		ft := reflect.TypeOf(reflect.New(t).Interface().(objectFormer).objectForm())
		reachable(ft, withV1, out)
		return
	}
	if t.Kind() != reflect.Struct || out[t] || reflect.PointerTo(t).Implements(unmarshalerType) {
		return
	}
	out[t] = true
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.Tag.Get("json") == "-" || (!withV1 && contractFlags(sf.Tag)["v1"]) {
			continue
		}
		reachable(sf.Type, withV1, out)
	}
}

// sample builds a JSON value that sets every field v2 accepts.
func sample(t reflect.Type) any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == reflect.TypeOf(SchemaVersion{}) {
		return "2.0"
	}
	if reflect.PointerTo(t).Implements(objectFormerType) {
		return sample(reflect.TypeOf(reflect.New(t).Interface().(objectFormer).objectForm()))
	}
	switch t.Kind() {
	case reflect.Struct:
		m := map[string]any{}
		for name, f := range fieldsOf(t).byName {
			if !f.v1Only {
				m[name] = sample(f.typ)
			}
		}
		return m
	case reflect.Slice, reflect.Array:
		return []any{sample(t.Elem())}
	case reflect.Map:
		return map[string]any{"k": sample(t.Elem())}
	case reflect.String:
		return "s"
	case reflect.Bool:
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return 1
	case reflect.Float32, reflect.Float64:
		return 1.5
	}
	return "any"
}

// TestWalkerVisitsEveryStruct proves the walker descends into every struct
// type reachable from Definition, so a field added later is checked
// without changes to the walker.
func TestWalkerVisitsEveryStruct(t *testing.T) {
	want := map[reflect.Type]bool{}
	reachable(reflect.TypeOf(Definition{}), false, want)

	doc, err := json.Marshal(sample(reflect.TypeOf(Definition{})))
	if err != nil {
		t.Fatal(err)
	}
	w := walker{visited: map[reflect.Type]bool{}}
	w.walk(doc, reflect.TypeOf(Definition{}), "")
	if len(w.unknown) > 0 || len(w.problems.items) > 0 {
		t.Fatalf("sample document: unknown %q, problems %v", w.unknown, w.problems)
	}
	if !reflect.DeepEqual(w.visited, want) {
		t.Errorf("visited %v\nwant    %v", typeNames(w.visited), typeNames(want))
	}
	if len(want) < 9 {
		t.Errorf("only %d struct types reachable; the reflection is broken", len(want))
	}
}

func typeNames(m map[reflect.Type]bool) []string {
	var names []string
	for t := range m {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	return names
}

// TestV1OnlyFieldsMatchSchemaExclusions checks that the fields Parse
// rejects in v2 are exactly the fields the published schema leaves out,
// apart from dispatch-only fields that Parse accepts but users never write.
func TestV1OnlyFieldsMatchSchemaExclusions(t *testing.T) {
	dispatchOnly := map[string]bool{"DataConfig.DataDownloadURL": true}

	all := map[reflect.Type]bool{}
	reachable(reflect.TypeOf(Definition{}), true, all)
	var v1Only, excluded []string
	for typ := range all {
		for i := 0; i < typ.NumField(); i++ {
			sf := typ.Field(i)
			name := typ.Name() + "." + sf.Name
			if contractFlags(sf.Tag)["v1"] {
				v1Only = append(v1Only, name)
			}
			if sf.Tag.Get("jsonschema") == "-" && !dispatchOnly[name] {
				excluded = append(excluded, name)
			}
			flags := contractFlags(sf.Tag)
			for f := range flags {
				if f != "v1" && f != "v2" && f != "loose" {
					t.Errorf("%s: unknown contract flag %q", name, f)
				}
			}
			if flags["v1"] && flags["v2"] {
				t.Errorf("%s: tagged both v1 and v2", name)
			}
			if flags["v2"] && flags["loose"] {
				t.Errorf("%s: a v2-only field is never read in v1, so loose means nothing", name)
			}
		}
	}
	sort.Strings(v1Only)
	sort.Strings(excluded)
	if !reflect.DeepEqual(v1Only, excluded) {
		t.Errorf("contract:\"v1\" fields %q\n!= jsonschema:\"-\" fields %q", v1Only, excluded)
	}
	for name := range dispatchOnly {
		typ, field, _ := strings.Cut(name, ".")
		found := false
		for tt := range all {
			if sf, ok := tt.FieldByName(field); ok && tt.Name() == typ {
				found = sf.Tag.Get("jsonschema") == "-" && !contractFlags(sf.Tag)["v1"]
			}
		}
		if !found {
			t.Errorf("dispatch-only field %s must be jsonschema:\"-\" and accepted in v2", name)
		}
	}
	wantV1 := []string{
		"Definition.APIKey", "Definition.Cloud", "Definition.CloudTimeout", "Definition.ExistingTestID",
		"Definition.ReportType", "Definition.SkipLookup", "Definition.Weights",
		"Request.Header", "Step.Validate",
	}
	if !reflect.DeepEqual(v1Only, wantV1) {
		t.Errorf("v1-only fields = %q, want %q", v1Only, wantV1)
	}
}

// unionValue stands in for a value that is either a string or an object,
// such as a threshold: "p95<500ms" or {"expr": "p95<500ms", "abort_on_fail": true}.
type unionValue struct {
	Expr  string
	Abort bool
}

type unionObject struct {
	Expr  string `json:"expr"`
	Abort bool   `json:"abort_on_fail"`
}

func (u *unionValue) objectForm() any { return &unionObject{} }

func (u *unionValue) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '{' {
		var o unionObject
		if err := json.Unmarshal(b, &o); err != nil {
			return err
		}
		u.Expr, u.Abort = o.Expr, o.Abort
		return nil
	}
	if err := json.Unmarshal(b, &u.Expr); err != nil {
		return errors.New("must be a string or an object")
	}
	return nil
}

type embeddedBase struct {
	Shared string `json:"shared"`
	Shadow string `json:"shadow"`
}

type walkFixture struct {
	embeddedBase
	Shadow  int                     `json:"shadow"`
	One     unionValue              `json:"one"`
	Many    []unionValue            `json:"many"`
	ByName  map[string]unionObject  `json:"by_name"`
	Ptrs    map[string]*unionObject `json:"ptrs"`
	Any     any                     `json:"any"`
	Bytes   []byte                  `json:"bytes"`
	Fixed   [2]int                  `json:"fixed"`
	Untaged string
	Skipped string `json:"-"`
	Legacy  string `json:"legacy" contract:"v1"`
	private string
}

func TestWalkerHooksAndKinds(t *testing.T) {
	doc := `{
		"shared": "s", "shadow": 1,
		"one": {"expr": "p95<1s", "bogus": 1},
		"many": ["p99<2s", {"expr": "x", "abort_on_fail": "yes"}, 5],
		"by_name": {"b": {"expr": "y"}, "a": {"nope": 1}},
		"ptrs": {"x": null},
		"any": null,
		"bytes": "aGk=",
		"fixed": [1, "2"],
		"Untaged": "u",
		"untaged": "wrong case",
		"Skipped": "x",
		"legacy": "v1",
		"private": 1
	}`
	w := walker{visited: map[reflect.Type]bool{}}
	w.walk(json.RawMessage(doc), reflect.TypeOf(walkFixture{}), "")

	wantUnknown := []string{"Skipped", "by_name.a.nope", "legacy", "one.bogus", "private", "untaged"}
	if !reflect.DeepEqual(w.unknown, wantUnknown) {
		t.Errorf("unknown = %q\nwant      %q", w.unknown, wantUnknown)
	}
	var got []string
	for _, p := range w.problems.items {
		got = append(got, p.String())
	}
	wantProblems := []string{
		"fixed[1]: must be an integer",
		"many[1].abort_on_fail: must be true or false",
		"many[2]: must be a string or an object",
		"ptrs.x: must not be null",
	}
	if !reflect.DeepEqual(got, wantProblems) {
		t.Errorf("problems = %q\nwant       %q", got, wantProblems)
	}
	if !w.visited[reflect.TypeOf(unionObject{})] {
		t.Error("object form of the union was not walked")
	}
}

func TestFieldsOfShadowing(t *testing.T) {
	f := fieldsOf(reflect.TypeOf(walkFixture{})).byName
	if f["shadow"].typ.Kind() != reflect.Int {
		t.Errorf("direct field must win over the embedded one, got %v", f["shadow"].typ)
	}
	if _, ok := f["shared"]; !ok {
		t.Error("embedded field not promoted")
	}
	if !f["legacy"].v1Only {
		t.Error("contract:\"v1\" not recorded")
	}
}

func TestWalkerNeverPanicsOnOddInput(t *testing.T) {
	inputs := []string{``, ` `, `nul`, `{`, `[`, `"`, `-`, `{"scenarios": [[]]}`, `{"scenarios": "x"}`,
		`{"scenarios": [`, `{"scenarios": [{"name": "a\`, `{"name"`, `{"name":`, `{"name": 1,`, `{"a" 1}`,
		`{"weights": [1 2]}`, `{"weights": [1,]}`, `{1: 2}`, `{"target": {"url": "x"]}`}
	for _, in := range inputs {
		v1 := v1walker{}
		v1.value(json.RawMessage(in), reflect.TypeOf(v1Wrapper{}), "", "", false)
		w := walker{}
		w.walk(json.RawMessage(in), reflect.TypeOf(Definition{}), "")
	}
}

func TestJoinPath(t *testing.T) {
	cases := map[[2]string]string{
		{"", "name"}:          "name",
		{"a", "b"}:            "a.b",
		{"a[0]", "b-c_d"}:     "a[0].b-c_d",
		{"a", "x.y"}:          `a["x.y"]`,
		{"", ""}:              `[""]`,
		{"h", "Content Type"}: `h["Content Type"]`,
		{"", "é"}:             `["é"]`,
	}
	for in, want := range cases {
		if got := joinPath(in[0], in[1]); got != want {
			t.Errorf("joinPath(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

// TestNoFoldCollisions checks that no two JSON names in a struct the
// walkers visit are equal ignoring case. The v1 walker resolves a
// case-insensitive key to the first matching name in sorted order, which
// only matches encoding/json when no two names fold together.
func TestNoFoldCollisions(t *testing.T) {
	all := map[reflect.Type]bool{}
	reachable(reflect.TypeOf(Definition{}), true, all)
	reachable(reflect.TypeOf(v1Wrapper{}), true, all)
	for typ := range all {
		names := fieldsOf(typ).names
		for i := range names {
			for j := i + 1; j < len(names); j++ {
				if strings.EqualFold(names[i], names[j]) {
					t.Errorf("%s: JSON names %q and %q are equal ignoring case", typ.Name(), names[i], names[j])
				}
			}
		}
	}
	if !all[reflect.TypeOf(Request{})] || !all[reflect.TypeOf(LegacyValidator{})] {
		t.Fatal("reachable() missed types")
	}
}
