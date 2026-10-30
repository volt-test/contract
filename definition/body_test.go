package definition_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

const (
	rq            = "scenarios[0].steps[0].request"
	bodyNeedsJSON = `is required with body_type "json"`
	bodyOnlyJSON  = `only applies with body_type "json"`
	bodyOnlyForm  = `only applies with body_type "form"`
	bodyWithJSON  = `cannot be combined with body_type "json"; use body_json`
	bodyWithForm  = `cannot be combined with body_type "form"; use fields`
	fieldDup      = "appears more than once; field names must be unique"
	badBodyType   = `must be one of "raw", "json", "form", got `
)

// bodyFixtureProblems maps each invalid fixture for request bodies to the
// problems Parse reports, in order.
var bodyFixtureProblems = map[string][]string{
	"structure_body_type_unknown.json":      {rq + `.body_type: ` + badBodyType + `"xml"`},
	"structure_body_json_missing.json":      {rq + `.body_json: ` + bodyNeedsJSON},
	"structure_body_json_without_json.json": {rq + `.body_json: ` + bodyOnlyJSON},
	"structure_body_with_json.json":         {rq + `.body: ` + bodyWithJSON},
	"structure_body_with_form.json":         {rq + `.body: ` + bodyWithForm},
	"structure_fields_without_form.json":    {rq + `.fields: ` + bodyOnlyForm},
	"structure_fields_duplicate.json":       {rq + `.fields.a: ` + fieldDup},
	"type_body_json_scalar.json":            {rq + `.body_json: must be an object or an array`},
	"type_body_type_number.json":            {rq + `.body_type: must be a string`},
	"type_fields_array.json":                {rq + `.fields: must be an object`},
	"type_fields_value_number.json":         {rq + `.fields.qty: must be a string`},
}

// bodyFixturePrefixes are the fixture name prefixes this file owns.
var bodyFixturePrefixes = []string{"structure_body_", "type_body_", "structure_fields_", "type_fields_"}

// TestBodyFixtures checks that each invalid body fixture fails with exactly
// the problems listed for it, and that every such fixture is listed.
func TestBodyFixtures(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures(t, "v2/invalid") {
		name := filepath.Base(f)
		want, ok := bodyFixtureProblems[name]
		if !ok {
			for _, p := range bodyFixturePrefixes {
				if strings.HasPrefix(name, p) {
					t.Errorf("%s: no expected problem in bodyFixtureProblems", name)
				}
			}
			continue
		}
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			if got := problemStrings(t, readFile(t, f)); !reflect.DeepEqual(got, want) {
				t.Errorf("problems = %q, want %q", got, want)
			}
		})
	}
	for name := range bodyFixtureProblems {
		if !seen[name] {
			t.Errorf("missing fixture testdata/v2/invalid/%s", name)
		}
	}
}

// bodyDoc returns a minimal v2 definition whose only request is request,
// written as raw JSON text so that literals and duplicate keys survive.
func bodyDoc(request string) []byte {
	return []byte(`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"a","steps":[{"name":"b","request":` +
		request + `}]}]}`)
}

// req builds request text from the keys after method and url.
func req(keys string) string {
	if keys == "" {
		return `{"method":"POST","url":"https://example.com/"}`
	}
	return `{"method":"POST","url":"https://example.com/",` + keys + `}`
}

func TestBodyRules(t *testing.T) {
	schema := compileSchema(t)
	cases := []struct {
		name   string
		keys   string
		want   []string
		looser bool // the schema accepts it; Parse alone applies the rule
	}{
		{name: "no body keys", keys: ``},
		{name: "raw body", keys: `"body":"x"`},
		{name: "explicit raw", keys: `"body_type":"raw","body":"x"`},
		{name: "empty body_type", keys: `"body_type":"","body":"x"`},
		{name: "json object", keys: `"body_type":"json","body_json":{"a":1}`},
		{name: "json array", keys: `"body_type":"json","body_json":[1,"${x}"]`},
		{name: "json empty object", keys: `"body_type":"json","body_json":{}`},
		{name: "json empty array", keys: `"body_type":"json","body_json":[]`},
		{name: "json with empty body", keys: `"body_type":"json","body_json":{"a":1},"body":""`},
		{name: "json with empty fields", keys: `"body_type":"json","body_json":{"a":1},"fields":{}`},
		{name: "form", keys: `"body_type":"form","fields":{"a":"1","b":"${x}"}`},
		{name: "form without fields", keys: `"body_type":"form"`},
		{name: "form with empty fields", keys: `"body_type":"form","fields":{}`},
		{name: "form with empty body", keys: `"body_type":"form","fields":{"a":"1"},"body":""`},
		{name: "empty fields without body_type", keys: `"fields":{}`},
		{name: "empty fields with raw", keys: `"body_type":"raw","body":"x","fields":{}`},
		{name: "empty field name and value", keys: `"body_type":"form","fields":{"":""}`},
		{name: "names that differ by case", keys: `"body_type":"form","fields":{"a":"1","A":"2"}`},
		{name: "many fields", keys: `"body_type":"form","fields":{` + manyFields(40, "") + `}`},

		{name: "unknown body_type", keys: `"body_type":"multipart"`,
			want: []string{rq + ".body_type: " + badBodyType + `"multipart"`}},
		{name: "unknown body_type reports only itself", keys: `"body_type":"JSON","body_json":{"a":1},"body":"x"`,
			want: []string{rq + ".body_type: " + badBodyType + `"JSON"`}},
		{name: "json without body_json", keys: `"body_type":"json"`,
			want: []string{rq + ".body_json: " + bodyNeedsJSON}},
		{name: "json with body only", keys: `"body_type":"json","body":"{}"`,
			want: []string{rq + ".body: " + bodyWithJSON, rq + ".body_json: " + bodyNeedsJSON}},
		{name: "json with body", keys: `"body_type":"json","body_json":{},"body":"x"`,
			want: []string{rq + ".body: " + bodyWithJSON}},
		{name: "json with fields", keys: `"body_type":"json","body_json":{},"fields":{"a":"1"}`,
			want: []string{rq + ".fields: " + bodyOnlyForm}},
		{name: "form with body", keys: `"body_type":"form","body":"a=1"`,
			want: []string{rq + ".body: " + bodyWithForm}},
		{name: "form with body_json", keys: `"body_type":"form","body_json":{"a":1}`,
			want: []string{rq + ".body_json: " + bodyOnlyJSON}},
		{name: "body_json without body_type", keys: `"body_json":{"a":1}`,
			want: []string{rq + ".body_json: " + bodyOnlyJSON}},
		{name: "body_json with raw", keys: `"body_type":"raw","body_json":[]`,
			want: []string{rq + ".body_json: " + bodyOnlyJSON}},
		{name: "fields without body_type", keys: `"fields":{"a":"1"}`,
			want: []string{rq + ".fields: " + bodyOnlyForm}},
		{name: "everything with raw", keys: `"body_type":"raw","body":"x","body_json":{},"fields":{"a":""}`,
			want: []string{rq + ".body_json: " + bodyOnlyJSON, rq + ".fields: " + bodyOnlyForm}},

		{name: "body_type number", keys: `"body_type":1`, want: []string{rq + ".body_type: must be a string"}},
		{name: "body_type null", keys: `"body_type":null`, want: []string{rq + ".body_type: must not be null"}},
		{name: "body_json string", keys: `"body_type":"json","body_json":"{}"`,
			want: []string{rq + ".body_json: must be an object or an array"}},
		{name: "body_json number", keys: `"body_type":"json","body_json":1`,
			want: []string{rq + ".body_json: must be an object or an array"}},
		{name: "body_json true", keys: `"body_type":"json","body_json":true`,
			want: []string{rq + ".body_json: must be an object or an array"}},
		{name: "body_json null", keys: `"body_type":"json","body_json":null`,
			want: []string{rq + ".body_json: must not be null"}},
		{name: "fields string", keys: `"body_type":"form","fields":"a=1"`, want: []string{rq + ".fields: must be an object"}},
		{name: "fields null", keys: `"body_type":"form","fields":null`, want: []string{rq + ".fields: must not be null"}},
		{name: "field values of every other kind", keys: `"body_type":"form","fields":{"n":1,"b":true,"o":{},"a":["x"],"z":null}`,
			want: []string{
				rq + ".fields.n: must be a string", rq + ".fields.b: must be a string", rq + ".fields.o: must be a string",
				rq + ".fields.a: must be a string", rq + ".fields.z: must not be null",
			}},
		{name: "field paths quote odd names", keys: `"body_type":"form","fields":{"a b":1,"ü":2,"x.y":3}`,
			want: []string{rq + `.fields["a b"]: must be a string`, rq + `.fields["ü"]: must be a string`, rq + `.fields["x.y"]: must be a string`}},
		{name: "duplicate name", keys: `"body_type":"form","fields":{"a":"1","a":"2"}`,
			want: []string{rq + ".fields.a: " + fieldDup}, looser: true},
		{name: "duplicate name after unescaping", keys: `"body_type":"form","fields":{"a b":"1","a\u0020b":"2"}`,
			want: []string{rq + `.fields["a b"]: ` + fieldDup}, looser: true},
		{name: "duplicate name reported each time", keys: `"body_type":"form","fields":{"a":"1","a":"2","a":3}`,
			want: []string{rq + ".fields.a: " + fieldDup, rq + ".fields.a: must be a string", rq + ".fields.a: " + fieldDup}},
		{name: "duplicate in a large object", keys: `"body_type":"form","fields":{` + manyFields(40, `,"f7":"again"`) + `}`,
			want: []string{rq + ".fields.f7: " + fieldDup}, looser: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := bodyDoc(req(tc.keys))
			if !json.Valid(doc) {
				t.Fatalf("test document is not JSON: %s", doc)
			}
			if got := problemStrings(t, doc); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("problems:\n got %q\nwant %q", got, tc.want)
			}
			schemaErr := validateAgainstSchema(t, schema, doc)
			switch {
			case tc.want == nil && schemaErr != nil:
				t.Errorf("schema rejects a definition Parse accepts: %v", schemaErr)
			case tc.want != nil && !tc.looser && schemaErr == nil:
				t.Error("schema accepts a definition Parse rejects")
			case tc.looser && schemaErr != nil:
				t.Errorf("marked looser but the schema rejects it: %v", schemaErr)
			}
		})
	}
}

// manyFields writes n distinct fields f0..f(n-1), then extra.
func manyFields(n int, extra string) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"f` + itoa(i) + `":"v"`)
	}
	return b.String() + extra
}

// TestJSONBodyRoundTrip checks that body_json keeps its literals, key
// order and string escapes, and that MarshalJSON leaves <, > and &
// unescaped. json.Marshal escapes them in every MarshalJSON output (as it
// does in body and headers), which keeps the JSON value and is a fixed
// point.
func TestJSONBodyRoundTrip(t *testing.T) {
	const written = `{ "z": 19.90, "a": [1, 2.50e3, -0.0, 12345678901234567890],
		"tags": ["<b>", "a & b", "\u00e9", "\"q\""], "n": null, "ok": true, "z2": {} }`
	const compact = `{"z":19.90,"a":[1,2.50e3,-0.0,12345678901234567890],"tags":["<b>","a & b","\u00e9","\"q\""],"n":null,"ok":true,"z2":{}}`
	const escaped = `{"z":19.90,"a":[1,2.50e3,-0.0,12345678901234567890],"tags":["\u003cb\u003e","a \u0026 b","\u00e9","\"q\""],"n":null,"ok":true,"z2":{}}`
	d := mustParse(t, bodyDoc(req(`"body_type":"json","body_json":`+written)))
	r := d.Scenarios[0].Steps[0].Request
	if got := string(r.BodyJSON); got != compact {
		t.Fatalf("BodyJSON =\n%s\nwant\n%s", got, compact)
	}
	if r.BodyKind() != definition.BodyTypeJSON {
		t.Errorf("BodyKind = %q", r.BodyKind())
	}
	out, err := r.BodyJSON.MarshalJSON()
	if err != nil || string(out) != compact {
		t.Errorf("MarshalJSON = %s, %v", out, err)
	}
	again := mustParse(t, bodyDoc(req(`"body_type":"json","body_json":`+string(out))))
	if got := string(again.Scenarios[0].Steps[0].Request.BodyJSON); got != compact {
		t.Errorf("Parse of MarshalJSON output =\n%s\nwant\n%s", got, compact)
	}

	m1 := mustMarshal(t, d)
	if !bytes.Contains(m1, []byte(`"body_json":`+escaped)) {
		t.Errorf("json.Marshal body:\n%s\nwant\n%s", m1, escaped)
	}
	if got := string(mustParse(t, m1).Scenarios[0].Steps[0].Request.BodyJSON); got != escaped {
		t.Errorf("after json.Marshal and Parse BodyJSON =\n%s\nwant\n%s", got, escaped)
	}
	var v1, v2 any
	if err := json.Unmarshal([]byte(compact), &v1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(escaped), &v2); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v1, v2) {
		t.Errorf("json.Marshal changed the body's value:\n%v\n%v", v1, v2)
	}
	if m2 := mustMarshal(t, mustParse(t, m1)); !bytes.Equal(m1, m2) {
		t.Errorf("json.Marshal is not a fixed point:\n%s\n%s", m1, m2)
	}
}

func TestFieldsRoundTrip(t *testing.T) {
	const written = `{"zeta": "1", "alpha": "a b&c=d", "a b": "<x>", "\u00fc": "${fake.paragraph}", "": "", "\u0041": "upper"}`
	want := definition.Fields{
		{Name: "zeta", Value: "1"}, {Name: "alpha", Value: "a b&c=d"}, {Name: "a b", Value: "<x>"},
		{Name: "ü", Value: "${fake.paragraph}"}, {Name: "", Value: ""}, {Name: "A", Value: "upper"},
	}
	d := mustParse(t, bodyDoc(req(`"body_type":"form","fields":`+written)))
	r := d.Scenarios[0].Steps[0].Request
	if !reflect.DeepEqual(r.Fields, want) {
		t.Fatalf("Fields = %q, want %q", r.Fields, want)
	}
	if v, ok := r.Fields.Get("alpha"); !ok || v != "a b&c=d" {
		t.Errorf("Get(alpha) = %q, %v", v, ok)
	}
	if _, ok := r.Fields.Get("missing"); ok {
		t.Error("Get(missing) found a field")
	}
	const compact = "{\"zeta\":\"1\",\"alpha\":\"a b&c=d\",\"a b\":\"<x>\",\"ü\":\"${fake.paragraph}\",\"\":\"\",\"A\":\"upper\"}"
	out, err := r.Fields.MarshalJSON()
	if err != nil || string(out) != compact {
		t.Errorf("MarshalJSON = %s, %v", out, err)
	}
	const escaped = "{\"zeta\":\"1\",\"alpha\":\"a b\\u0026c=d\",\"a b\":\"\\u003cx\\u003e\",\"ü\":\"${fake.paragraph}\",\"\":\"\",\"A\":\"upper\"}"
	m1 := mustMarshal(t, d)
	if !bytes.Contains(m1, []byte(`"fields":`+escaped)) {
		t.Errorf("json.Marshal does not keep the field order:\n%s\nwant\n%s", m1, escaped)
	}
	for name, doc := range map[string][]byte{
		"MarshalJSON":  bodyDoc(req(`"body_type":"form","fields":` + string(out))),
		"json.Marshal": m1,
	} {
		again := mustParse(t, doc)
		if got := again.Scenarios[0].Steps[0].Request.Fields; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: after Marshal and Parse Fields = %q, want %q", name, got, want)
		}
	}

	// {} reads as absent, and an absent or empty Fields is left out.
	d = mustParse(t, bodyDoc(req(`"body_type":"form","fields":{}`)))
	if f := d.Scenarios[0].Steps[0].Request.Fields; f != nil {
		t.Errorf("fields {} = %#v, want nil", f)
	}
	if out := mustMarshal(t, d); bytes.Contains(out, []byte(`"fields"`)) || !bytes.Contains(out, []byte(`"body_type":"form"`)) {
		t.Errorf("Marshal = %s", out)
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestBodyMarshalByValue checks that JSONBody and Fields marshal as JSON
// (not base64, not an array of pairs) wherever they sit: Request marshals
// itself by value, so a pointer-receiver MarshalJSON would be skipped.
func TestBodyMarshalByValue(t *testing.T) {
	r := definition.Request{
		Method: "POST", URL: "https://example.com/", BodyType: definition.BodyTypeJSON,
		BodyJSON: definition.JSONBody(`{"a":[1,"x"]}`),
	}
	f := definition.Request{
		Method: "POST", URL: "https://example.com/", BodyType: definition.BodyTypeForm,
		Fields: definition.Fields{{Name: "b", Value: "2"}, {Name: "a", Value: "1"}},
	}
	for name, v := range map[string]any{
		"request": r, "pointer": &r, "step": definition.Step{Name: "s", Request: r},
		"body": r.BodyJSON, "body in a map": map[string]any{"b": r.BodyJSON},
	} {
		out := string(mustMarshal(t, v))
		if !strings.Contains(out, `{"a":[1,"x"]}`) {
			t.Errorf("%s: %s", name, out)
		}
	}
	for name, v := range map[string]any{
		"request": f, "pointer": &f, "step": definition.Step{Name: "s", Request: f},
		"fields": f.Fields, "fields in a map": map[string]any{"f": f.Fields},
	} {
		out := string(mustMarshal(t, v))
		if !strings.Contains(out, `{"b":"2","a":"1"}`) {
			t.Errorf("%s: %s", name, out)
		}
	}
	if out := string(mustMarshal(t, definition.Request{Method: "GET", URL: "u"})); strings.Contains(out, "body") || strings.Contains(out, "fields") {
		t.Errorf("zero body keys marshalled: %s", out)
	}
}

func TestJSONBodyDirect(t *testing.T) {
	for _, in := range []string{`{}`, `[]`, ` { "a" : 1 } `, `[null]`, `{"a":{"b":[{"c":"\u2028"}]}}`} {
		var b definition.JSONBody
		if err := json.Unmarshal([]byte(in), &b); err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		var want bytes.Buffer
		_ = json.Compact(&want, []byte(in))
		if !bytes.Equal(b, want.Bytes()) {
			t.Errorf("%s: stored %s, want %s", in, b, want.Bytes())
		}
	}
	for _, in := range []string{`1`, `"{}"`, `true`, `false`} {
		var b definition.JSONBody
		if err := json.Unmarshal([]byte(in), &b); err == nil || !strings.Contains(err.Error(), "must be an object or an array") {
			t.Errorf("%s: err = %v", in, err)
		}
	}
	b := definition.JSONBody(`{"keep":1}`)
	if err := json.Unmarshal([]byte(`null`), &b); err != nil || string(b) != `{"keep":1}` {
		t.Errorf("null: %s, %v", b, err)
	}
	// The input buffer may be reused by the caller: the body is a copy.
	in := []byte(`{"a":1}`)
	var c definition.JSONBody
	if err := c.UnmarshalJSON(in); err != nil {
		t.Fatal(err)
	}
	in[2] = 'X'
	if string(c) != `{"a":1}` {
		t.Errorf("JSONBody aliases its input: %s", c)
	}

	if out, err := definition.JSONBody(nil).MarshalJSON(); err != nil || string(out) != "null" {
		t.Errorf("nil MarshalJSON = %s, %v", out, err)
	}
	for _, bad := range []string{`1`, `"x"`, `{`, `{"a":1} x`} {
		if out, err := definition.JSONBody(bad).MarshalJSON(); err == nil {
			t.Errorf("MarshalJSON(%s) = %s, want an error", bad, out)
		}
	}
}

func TestFieldsDirect(t *testing.T) {
	var f definition.Fields
	if err := json.Unmarshal([]byte(`{"b":"2","a":"1"}`), &f); err != nil {
		t.Fatal(err)
	}
	if want := (definition.Fields{{Name: "b", Value: "2"}, {Name: "a", Value: "1"}}); !reflect.DeepEqual(f, want) {
		t.Errorf("Fields = %q", f)
	}
	if err := json.Unmarshal([]byte(`null`), &f); err != nil || len(f) != 2 {
		t.Errorf("null: %q, %v", f, err)
	}
	for in, msg := range map[string]string{
		`[]`:                     "must be an object",
		`"a"`:                    "must be an object",
		`{"a":1}`:                "fields.a: must be a string",
		`{"a b":null}`:           `fields["a b"]: must not be null`,
		`{"a":"1","a":"2"}`:      "fields.a: " + fieldDup,
		`{"a":"1","\u0061":"2"}`: "fields.a: " + fieldDup,
	} {
		var g definition.Fields
		if err := json.Unmarshal([]byte(in), &g); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: err = %v, want %q", in, err, msg)
		}
	}
	if out, err := (definition.Fields{{Name: "a"}, {Name: "a"}}).MarshalJSON(); err == nil {
		t.Errorf("MarshalJSON with a duplicate name = %s, want an error", out)
	}
	if out, err := definition.Fields(nil).MarshalJSON(); err != nil || string(out) != "null" {
		t.Errorf("nil MarshalJSON = %s, %v", out, err)
	}
	if out, err := (definition.Fields{}).MarshalJSON(); err != nil || string(out) != "{}" {
		t.Errorf("empty MarshalJSON = %s, %v", out, err)
	}
}

// TestBodyValidFixtures checks what Parse reads from the valid body
// fixtures.
func TestBodyValidFixtures(t *testing.T) {
	d := mustParse(t, readFile(t, "../testdata/v2/valid/body_json.json"))
	steps := d.Scenarios[0].Steps
	kinds := []definition.BodyType{}
	for _, st := range steps {
		kinds = append(kinds, st.Request.BodyKind())
	}
	want := []definition.BodyType{definition.BodyTypeJSON, definition.BodyTypeJSON, definition.BodyTypeJSON,
		definition.BodyTypeRaw, definition.BodyTypeJSON}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("body kinds = %q, want %q", kinds, want)
	}
	cart := string(steps[1].Request.BodyJSON)
	for _, s := range []string{`{"sku":"SKU-1","qty":"${random_int(1,3)}","ok":"${flag}","note":"n ${uuid}","price":19.90,`,
		`"big":12345678901234567890`, `"tags":["<b>","a & b",null,true]`, `"nested":{"z":1,"a":{"empty":{},"list":[]}}}`} {
		if !strings.Contains(cart, s) {
			t.Errorf("cart body %s lacks %s", cart, s)
		}
	}
	if got := string(steps[2].Request.BodyJSON); got != `[{"id":1,"qty":2},{"id":2,"qty":"${counter}"}]` {
		t.Errorf("array body = %s", got)
	}

	d = mustParse(t, readFile(t, "../testdata/v2/valid/body_form.json"))
	var names []string
	for _, f := range d.Scenarios[0].Steps[0].Request.Fields {
		names = append(names, f.Name)
	}
	if want := []string{"q", "page", "a b", "ü", "comment", "empty"}; !reflect.DeepEqual(names, want) {
		t.Errorf("field names = %q, want %q (source order)", names, want)
	}
	if v, _ := d.Scenarios[0].Steps[0].Request.Fields.Get("q"); v != "a b&c=d" {
		t.Errorf("q = %q", v)
	}

	d = mustParse(t, readFile(t, "../testdata/v2/valid/body_form_empty.json"))
	for i, st := range d.Scenarios[0].Steps {
		if st.Request.Fields != nil {
			t.Errorf("step %d: Fields = %#v, want nil", i, st.Request.Fields)
		}
	}
	if k := d.Scenarios[0].Steps[2].Request.BodyKind(); k != definition.BodyTypeRaw {
		t.Errorf(`body_type "" reads as %q, want raw`, k)
	}
}

// FuzzJSONBody checks that a JSONBody UnmarshalJSON accepts is a compact
// JSON object or array that MarshalJSON writes back unchanged.
func FuzzJSONBody(f *testing.F) {
	for _, s := range []string{`{}`, `[]`, ` {"a" : [1, 19.90, "<&>"]} `, `1`, `"x"`, `null`, `{"a":1,"a":2}`, `[{"\u0000":"\ud800"}]`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if !json.Valid(data) {
			return
		}
		var b definition.JSONBody
		if err := json.Unmarshal(data, &b); err != nil || b == nil {
			return
		}
		if b[0] != '{' && b[0] != '[' {
			t.Fatalf("stored %s", b)
		}
		var c bytes.Buffer
		if err := json.Compact(&c, b); err != nil || !bytes.Equal(c.Bytes(), b) {
			t.Fatalf("stored body is not compact JSON: %s (%v)", b, err)
		}
		out, err := b.MarshalJSON()
		if err != nil || !bytes.Equal(out, b) {
			t.Fatalf("MarshalJSON = %s, %v; stored %s", out, err, b)
		}
	})
}

// FuzzFields checks that Fields read from any JSON object marshal back to
// an object that reads the same.
func FuzzFields(f *testing.F) {
	for _, s := range []string{`{}`, `{"a":"1","b":"2"}`, `{"b":"1","a":"2"}`, `{"a":"1","a":"2"}`, `{"a b":"\u00fc&=+%\n"}`, `{"\ud800":"x"}`, `{"a":1}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if !json.Valid(data) {
			return
		}
		var fs definition.Fields
		if err := json.Unmarshal(data, &fs); err != nil {
			return
		}
		out, err := json.Marshal(fs)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var again definition.Fields
		if err := json.Unmarshal(out, &again); err != nil {
			t.Fatalf("re-read %s: %v", out, err)
		}
		if len(fs) == 0 && again == nil {
			return
		}
		if !reflect.DeepEqual(fs, again) {
			t.Fatalf("round trip changed the fields:\n%q\n%q", fs, again)
		}
	})
}
