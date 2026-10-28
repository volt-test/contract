package definition_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/volt-test/contract/definition"
)

const check0 = "scenarios[0].steps[0].checks[0]"

// withChecks returns minimal.json with checks on its only step.
func withChecks(t *testing.T, checks ...any) []byte {
	t.Helper()
	if checks == nil {
		checks = []any{}
	}
	return v2doc(t, func(m map[string]any) { step0(m)["checks"] = checks })
}

// problemStrings parses data and returns its problems, or nil when Parse
// accepts it. Any other error fails the test.
func problemStrings(t *testing.T, data []byte) []string {
	t.Helper()
	_, err := definition.Parse(data)
	if err == nil {
		return nil
	}
	var ve *definition.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %v, want *ValidationError", err)
	}
	out := make([]string, len(ve.Problems))
	for i, p := range ve.Problems {
		out[i] = p.String()
	}
	return out
}

// mustParse parses data and returns the first step's checks.
func mustParse(t *testing.T, data []byte) *definition.Definition {
	t.Helper()
	d, err := definition.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, data)
	}
	return d
}

func TestChecksValid(t *testing.T) {
	schema := compileSchema(t)
	cases := []map[string]any{
		{"type": "status", "in": []any{200}},
		{"type": "status", "in": []any{200, 201, 204}},
		{"type": "status", "not_in": []any{500, 502}},
		{"type": "status", "between": []any{200, 299}},
		{"type": "status", "between": []any{200, 200}},
		{"type": "status", "between": []any{100, 599}},
		{"type": "header", "selector": "Content-Type", "equals": "application/json"},
		{"type": "header", "selector": "Content-Type", "equals": ""},
		{"type": "header", "selector": "Content-Type", "contains": "json"},
		{"type": "header", "selector": "X-Request-Id", "exists": true},
		{"type": "header", "selector": "ETag", "matches": `^"[0-9a-f]+"$`},
		{"type": "body", "equals": "OK"},
		{"type": "body", "contains": "Welcome"},
		{"type": "body", "not_contains": "error"},
		{"type": "body", "matches": `(?i)order #\d+`},
		{"type": "body", "size": "< 50kb"},
		{"type": "body", "empty": true},
		{"type": "json", "selector": "$.data.items[0].id", "exists": true},
		{"type": "json", "selector": "$.data.items[0].id", "equals": 42},
		{"type": "json", "selector": "$.data.items[0].id", "equals": json.Number("1234567890123456789")},
		{"type": "json", "selector": "$.data.name", "equals": "Sale"},
		{"type": "json", "selector": "$.data.active", "equals": true},
		{"type": "json", "selector": "$.data.error", "missing": true},
		{"type": "json", "selector": "$.data.items", "is": "array"},
		{"type": "json", "selector": "$.data", "is": "null"},
		{"type": "json", "selector": "$.data.items", "count": "> 0"},
		{"type": "json", "selector": "$.data.email", "matches": "@example\\.com$"},
		{"type": "json", "selector": "$.data.tags", "contains": "new"},
		{"type": "json", "selector": "$.data.tags", "contains": ""},
		{"type": "json", "selector": "$.data.ids", "contains": 7},
		{"type": "html", "selector": "h1", "exists": true},
		{"type": "html", "selector": "h1", "equals": "<b>Sale</b>"},
		{"type": "timing", "duration": "< 500ms"},
		{"type": "url", "equals": "https://shop.example.com/cart"},
		{"type": "url", "contains": "/cart"},
		{"type": "url", "matches": `/orders/\d+$`},
		{"type": "cookie", "selector": "session", "exists": true},
		{"type": "status", "in": []any{200}, "on_fail": "record"},
		{"type": "body", "contains": "x", "on_fail": "fail"},
		{"type": "json", "selector": "$.a", "exists": true, "on_fail": "abort"},
		{"type": "status", "in": []any{200}, "name": "ok"},
	}
	for _, c := range cases {
		doc := withChecks(t, c)
		t.Run(string(doc[bytes.Index(doc, []byte(`"checks"`)):]), func(t *testing.T) {
			if ps := problemStrings(t, doc); ps != nil {
				t.Fatalf("problems: %q", ps)
			}
			requireSchemaAccepts(t, schema, doc)
		})
	}
}

func TestChecksInvalid(t *testing.T) {
	cases := []struct {
		name  string
		check any
		want  []string
	}{
		// Type and operators.
		{"no type", map[string]any{"in": []any{200}}, []string{
			check0 + `.type: is required (one of "status", "header", "body", "json", "html", "timing", "url", "cookie")`}},
		{"unknown type", map[string]any{"type": "xml", "exists": true}, []string{
			check0 + `.type: must be one of "status", "header", "body", "json", "html", "timing", "url", "cookie", got "xml"`}},
		{"no operator", map[string]any{"type": "status"}, []string{
			check0 + ": needs one operator for type status: in, not_in or between"}},
		{"no operator, zero values only", map[string]any{"type": "timing", "duration": ""}, []string{
			check0 + ": needs one operator for type timing: duration"}},
		{"two operators", map[string]any{"type": "status", "in": []any{200}, "between": []any{200, 299}}, []string{
			check0 + ": has more than one operator (in, between); write one check per operator"}},
		{"operator for another type", map[string]any{"type": "status", "equals": "200"}, []string{
			check0 + ".equals: does not apply to type status (use in, not_in or between)"}},
		{"operator for another type next to a valid one", map[string]any{"type": "timing", "duration": "< 1s", "size": "< 1kb"}, []string{
			check0 + ".size: does not apply to type timing (use duration)"}},
		{"bad on_fail", map[string]any{"type": "status", "in": []any{200}, "on_fail": "warn"}, []string{
			check0 + `.on_fail: must be one of "record", "fail", "abort", got "warn"`}},
		{"bad is", map[string]any{"type": "json", "selector": "$.a", "is": "boolean"}, []string{
			check0 + `.is: must be one of "array", "object", "string", "number", "bool", "null", got "boolean"`}},

		// Selectors.
		{"header without selector", map[string]any{"type": "header", "exists": true}, []string{
			check0 + ".selector: is required for type header (the header name)"}},
		{"json with blank selector", map[string]any{"type": "json", "selector": " \t", "exists": true}, []string{
			check0 + ".selector: is required for type json (a JSON path)"}},
		{"html without selector", map[string]any{"type": "html", "exists": true}, []string{
			check0 + ".selector: is required for type html (a CSS selector)"}},
		{"cookie without selector", map[string]any{"type": "cookie", "exists": true}, []string{
			check0 + ".selector: is required for type cookie (the cookie name)"}},
		{"selector on status", map[string]any{"type": "status", "selector": "x", "in": []any{200}}, []string{
			check0 + ".selector: does not apply to type status"}},

		// Status codes.
		{"code 99", map[string]any{"type": "status", "in": []any{200, 99}}, []string{
			check0 + ".in[1]: must be an HTTP status code from 100 to 599, got 99"}},
		{"code 600", map[string]any{"type": "status", "not_in": []any{600}}, []string{
			check0 + ".not_in[0]: must be an HTTP status code from 100 to 599, got 600"}},
		{"between of one", map[string]any{"type": "status", "between": []any{200}}, []string{
			check0 + ".between: must be [low, high], two status codes; got 1"}},
		{"between of three", map[string]any{"type": "status", "between": []any{200, 250, 299}}, []string{
			check0 + ".between: must be [low, high], two status codes; got 3"}},
		{"between reversed", map[string]any{"type": "status", "between": []any{299, 200}}, []string{
			check0 + ".between: must be [low, high] with low <= high, got [299,200]"}},
		{"between out of range", map[string]any{"type": "status", "between": []any{0, 700}}, []string{
			check0 + ".between[0]: must be an HTTP status code from 100 to 599, got 0",
			check0 + ".between[1]: must be an HTTP status code from 100 to 599, got 700"}},

		// Values.
		{"numeric equals on header", map[string]any{"type": "header", "selector": "Content-Length", "equals": 5}, []string{
			check0 + ".equals: must be a string for type header"}},
		{"bool contains on body", map[string]any{"type": "body", "contains": true}, []string{
			check0 + ".contains: must be a string for type body"}},
		{"numeric equals on html", map[string]any{"type": "html", "selector": "h1", "equals": 1}, []string{
			check0 + ".equals: must be a string for type html"}},
		{"numeric equals on url", map[string]any{"type": "url", "equals": 1}, []string{
			check0 + ".equals: must be a string for type url"}},
		{"empty contains on header", map[string]any{"type": "header", "selector": "A", "contains": ""}, []string{
			check0 + ".contains: must not be empty: an empty string is in every value"}},
		{"empty contains on body", map[string]any{"type": "body", "contains": ""}, []string{
			check0 + ".contains: must not be empty: an empty string is in every value"}},
		{"empty contains on url", map[string]any{"type": "url", "contains": ""}, []string{
			check0 + ".contains: must not be empty: an empty string is in every value"}},
		{"bad regex", map[string]any{"type": "body", "matches": "(unclosed"}, []string{
			check0 + `.matches: is not a valid regular expression (RE2 syntax): missing closing ): "(unclosed"`}},
		{"perl-only regex", map[string]any{"type": "url", "matches": `a(?=b)`}, []string{
			check0 + `.matches: is not a valid regular expression (RE2 syntax): invalid or unsupported Perl syntax: "(?="`}},
		{"duration without unit", map[string]any{"type": "timing", "duration": "< 500"}, []string{
			check0 + `.duration: invalid comparison "< 500": the value is a duration and needs a unit (us, ms, s or m); did you mean "500ms"? (at offset 2)`}},
		{"size with a duration", map[string]any{"type": "body", "size": "< 1s"}, nil},
		{"count with a unit", map[string]any{"type": "json", "selector": "$.a", "count": "> 1kb"}, nil},
		{"comparison with a metric", map[string]any{"type": "timing", "duration": "p95 < 1s"}, nil},

		// exists, missing and empty must be true.
		{"exists false on json", map[string]any{"type": "json", "selector": "$.a", "exists": false}, []string{
			check0 + ".exists: must be true; to check that the path is absent, use missing: true"}},
		{"exists false on header", map[string]any{"type": "header", "selector": "A", "exists": false}, []string{
			check0 + ".exists: must be true; leave it out otherwise"}},
		{"missing false", map[string]any{"type": "json", "selector": "$.a", "missing": false}, []string{
			check0 + ".missing: must be true; to check that the path is present, use exists: true"}},
		{"empty false", map[string]any{"type": "body", "empty": false}, []string{
			check0 + `.empty: must be true; to check that the body is not empty, use size: "> 0"`}},
		{"exists false next to another operator", map[string]any{"type": "json", "selector": "$.a", "exists": false, "is": "array"}, []string{
			check0 + ".exists: must be true; to check that the path is absent, use missing: true",
			check0 + ": has more than one operator (exists, is); write one check per operator"}},

		// JSON types.
		{"equals object", map[string]any{"type": "json", "selector": "$.a", "equals": map[string]any{"a": 1}}, []string{
			check0 + ".equals: must be a string, a number or true/false"}},
		{"contains array", map[string]any{"type": "json", "selector": "$.a", "contains": []any{1}}, []string{
			check0 + ".contains: must be a string, a number or true/false"}},
		{"equals null", map[string]any{"type": "json", "selector": "$.a", "equals": nil}, []string{
			check0 + ".equals: must not be null"}},
		{"equals out of range", map[string]any{"type": "json", "selector": "$.a", "equals": json.Number("1e999")}, []string{
			check0 + ".equals: is out of range"}},
		{"contains out of range", map[string]any{"type": "json", "selector": "$.a", "contains": json.Number("-1e400")}, []string{
			check0 + ".contains: is out of range"}},
		{"exists as string", map[string]any{"type": "json", "selector": "$.a", "exists": "true"}, []string{
			check0 + ".exists: must be true or false"}},
		{"in as number", map[string]any{"type": "status", "in": 200}, []string{
			check0 + ".in: must be an array"}},
		{"code as string", map[string]any{"type": "status", "in": []any{"200"}}, []string{
			check0 + ".in[0]: must be an integer"}},
		{"duration as number", map[string]any{"type": "timing", "duration": 500}, []string{
			check0 + ".duration: must be a string"}},
		{"check as string", "status 200", []string{check0 + ": must be an object"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := problemStrings(t, withChecks(t, tc.check))
			if tc.want == nil {
				// Messages come from thresholds.ParseComparison; only the
				// path is pinned here.
				if len(got) != 1 || !strings.HasPrefix(got[0], check0+".") {
					t.Fatalf("problems = %q, want one comparison problem", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("problems:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestChecksZeroIsAbsent: "" and [] in every optional key read as absent,
// and Marshal leaves them out; equals/contains "" and exists: true are
// values and survive.
func TestChecksZeroIsAbsent(t *testing.T) {
	zeros := map[string]any{
		"name": "", "selector": "", "on_fail": "", "is": "", "matches": "", "not_contains": "",
		"count": "", "size": "", "duration": "", "in": []any{}, "not_in": []any{}, "between": []any{},
	}
	base := []map[string]any{
		{"type": "status", "in": []any{200}},
		{"type": "json", "selector": "$.a", "exists": true},
		{"type": "timing", "duration": "< 1s"},
	}
	for _, b := range base {
		for k, zero := range zeros {
			if _, set := b[k]; set {
				continue
			}
			c := map[string]any{k: zero}
			for bk, bv := range b {
				c[bk] = bv
			}
			doc := withChecks(t, c)
			t.Run(string(doc[bytes.Index(doc, []byte(`"checks"`)):]), func(t *testing.T) {
				d := mustParse(t, doc)
				out, err := json.Marshal(d.Scenarios[0].Steps[0].Checks[0])
				if err != nil {
					t.Fatal(err)
				}
				var got map[string]any
				if err := json.Unmarshal(out, &got); err != nil {
					t.Fatal(err)
				}
				if _, kept := got[k]; kept {
					t.Errorf("Marshal kept %s: %s", k, out)
				}
				if len(got) != len(b) {
					t.Errorf("Marshal = %s, want the keys of %v", out, b)
				}
			})
		}
	}

	t.Run("empty values survive", func(t *testing.T) {
		d := mustParse(t, withChecks(t,
			map[string]any{"type": "header", "selector": "A", "equals": ""},
			map[string]any{"type": "json", "selector": "$.a", "contains": ""},
			map[string]any{"type": "cookie", "selector": "s", "exists": true},
		))
		out, err := json.Marshal(d.Scenarios[0].Steps[0].Checks)
		if err != nil {
			t.Fatal(err)
		}
		want := `[{"type":"header","selector":"A","equals":""},{"type":"json","selector":"$.a","contains":""},{"type":"cookie","selector":"s","exists":true}]`
		if string(out) != want {
			t.Errorf("Marshal =\n%s\nwant\n%s", out, want)
		}
	})

	t.Run("empty in next to between", func(t *testing.T) {
		d := mustParse(t, withChecks(t, map[string]any{"type": "status", "in": []any{}, "between": []any{200, 299}}))
		if c := d.Scenarios[0].Steps[0].Checks[0]; len(c.In) != 0 || !reflect.DeepEqual(c.Between, []int{200, 299}) {
			t.Errorf("check = %+v", c)
		}
	})

	t.Run("empty checks", func(t *testing.T) {
		d := mustParse(t, withChecks(t))
		out, _ := json.Marshal(d)
		if bytes.Contains(out, []byte(`"checks"`)) {
			t.Errorf("Marshal kept checks: %s", out)
		}
	})
}

func TestScalar(t *testing.T) {
	cases := []struct {
		in   string
		kind definition.ScalarKind
		out  string
	}{
		{`"a"`, definition.ScalarString, `"a"`},
		{`""`, definition.ScalarString, `""`},
		{`"\u003cb\u003e"`, definition.ScalarString, `"<b>"`},
		{`42`, definition.ScalarNumber, `42`},
		{`42.0`, definition.ScalarNumber, `42.0`},
		{`-1e3`, definition.ScalarNumber, `-1e3`},
		{`1234567890123456789`, definition.ScalarNumber, `1234567890123456789`},
		{` true `, definition.ScalarBool, `true`},
		{`false`, definition.ScalarBool, `false`},
	}
	for _, tc := range cases {
		var s definition.Scalar
		if err := s.UnmarshalJSON([]byte(tc.in)); err != nil {
			t.Errorf("%s: %v", tc.in, err)
			continue
		}
		if s.Kind() != tc.kind {
			t.Errorf("%s: Kind = %v, want %v", tc.in, s.Kind(), tc.kind)
		}
		out, err := s.MarshalJSON()
		if err != nil || string(out) != tc.out {
			t.Errorf("%s: MarshalJSON = %s, %v; want %s", tc.in, out, err, tc.out)
		}
	}
	for _, in := range []string{`{}`, `{"a":1}`, `[]`, `[1]`, ``, `tru`, `-`, `1 2`, `'a'`} {
		var s definition.Scalar
		err := s.UnmarshalJSON([]byte(in))
		if err == nil || err.Error() != "definition: must be a string, a number or true/false" {
			t.Errorf("%q: error = %v", in, err)
		}
	}
	for _, in := range []string{`1e999`, `-1e999`, `1E+400`, `-179769313486231580793728971405303415079934132710037826936173778980444968292764750946649017977587207096330286416692887910946555547851940402630657488671505820681908902000708383676273854845817711531764475730270069855571366959622842914819860834936475292719074168444365510704342711559699508093042880177904174497792`} {
		var s definition.Scalar
		if err := s.UnmarshalJSON([]byte(in)); err == nil || err.Error() != "definition: is out of range" {
			t.Errorf("%s: error = %v", in, err)
		}
	}
	// Underflow is not out of range, as in any other value.
	for _, in := range []string{`1e-999`, `-1e-400`, `1.7976931348623157e308`} {
		var s definition.Scalar
		if err := s.UnmarshalJSON([]byte(in)); err != nil || string(s.Number()) != in {
			t.Errorf("%s: %v, %v", in, s, err)
		}
	}
	var s definition.Scalar
	if err := s.UnmarshalJSON([]byte("null")); err != nil || s.Kind() != 0 {
		t.Errorf("null: %v, %v", s, err)
	}

	if v := definition.StringScalar("x"); v.Kind() != definition.ScalarString || v.Text() != "x" || v.Number() != "" || v.Bool() {
		t.Errorf("StringScalar = %#v", v)
	}
	if v := definition.NumberScalar("7"); v.Kind() != definition.ScalarNumber || v.Number() != "7" || v.Text() != "" {
		t.Errorf("NumberScalar = %#v", v)
	}
	if v := definition.BoolScalar(true); v.Kind() != definition.ScalarBool || !v.Bool() {
		t.Errorf("BoolScalar = %#v", v)
	}
	if !definition.NumberScalar("1.0").Equal(definition.NumberScalar("1.0")) || definition.NumberScalar("1").Equal(definition.NumberScalar("1.0")) {
		t.Error("Equal does not compare literals")
	}
	if _, err := (definition.Scalar{}).MarshalJSON(); err == nil {
		t.Error("zero Scalar marshals")
	}
	for _, n := range []string{"", "x", `"1"`, "1 ", " 1", "1.", "0x1", "01"} {
		if _, err := definition.NumberScalar(json.Number(n)).MarshalJSON(); err == nil {
			t.Errorf("NumberScalar(%q) marshals", n)
		}
	}
	// Inside a definition, Marshal escapes HTML characters again and the
	// value still reads back the same.
	in := withChecks(t, map[string]any{"type": "html", "selector": "h1", "equals": "<b>Sale</b> & more"})
	d := mustParse(t, in)
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustParse(t, out).Scenarios[0].Steps[0].Checks[0].Equals.Text(); got != "<b>Sale</b> & more" {
		t.Errorf("round trip = %q", got)
	}
}

func TestCheckLabel(t *testing.T) {
	long := strings.Repeat("a", 200)
	cases := []struct {
		check string
		want  string
	}{
		{`{"type":"status","in":[200]}`, "status in [200]"},
		{`{"type":"status","in":[200,201]}`, "status in [200,201]"},
		{`{"type":"status","not_in":[500]}`, "status not_in [500]"},
		{`{"type":"status","between":[200,299]}`, "status between [200,299]"},
		{`{"type":"header","selector":"Authorization","equals":"Bearer abc"}`, "header Authorization equals"},
		{`{"type":"header","selector":"Content-Type","contains":"json"}`, "header Content-Type contains"},
		{`{"type":"header","selector":"ETag","matches":"x"}`, "header ETag matches"},
		{`{"type":"header","selector":"X-Id","exists":true}`, "header X-Id exists"},
		{`{"type":"body","equals":"OK"}`, `body equals "OK"`},
		{`{"type":"body","contains":"a \"quoted\" word"}`, `body contains "a \"quoted\" word"`},
		{`{"type":"body","not_contains":"error"}`, `body not_contains "error"`},
		{`{"type":"body","matches":"^\\d+$"}`, `body matches "^\\d+$"`},
		{`{"type":"body","size":"<  50kb"}`, "body size <  50kb"},
		{`{"type":"body","empty":true}`, "body empty"},
		{`{"type":"json","selector":"$.data.items[0].id","exists":true}`, "json $.data.items[0].id exists"},
		{`{"type":"json","selector":"$.data.items[0].id","equals":42}`, "json $.data.items[0].id equals 42"},
		{`{"type":"json","selector":"$.id","equals":1234567890123456789}`, "json $.id equals 1234567890123456789"},
		{`{"type":"json","selector":"$.n","equals":42.0}`, "json $.n equals 42.0"},
		{`{"type":"json","selector":"$.ok","equals":true}`, "json $.ok equals true"},
		{`{"type":"json","selector":"$.s","equals":"42"}`, `json $.s equals "42"`},
		{`{"type":"json","selector":"$.e","missing":true}`, "json $.e missing"},
		{`{"type":"json","selector":"$.a","is":"array"}`, "json $.a is array"},
		{`{"type":"json","selector":"$.a","count":"> 0"}`, "json $.a count > 0"},
		{`{"type":"json","selector":"$.t","contains":"new"}`, `json $.t contains "new"`},
		{`{"type":"html","selector":"h1","equals":"<b>Sale</b>"}`, `html h1 equals "<b>Sale</b>"`},
		{`{"type":"html","selector":"h1","equals":"\u003cb\u003e \u0026"}`, `html h1 equals "<b> &"`},
		{`{"type":"html","selector":"div.price > span","exists":true}`, "html div.price > span exists"},
		{`{"type":"timing","duration":"< 500ms"}`, "timing duration < 500ms"},
		{`{"type":"url","contains":"/cart"}`, `url contains "/cart"`},
		{`{"type":"cookie","selector":"session","exists":true}`, "cookie session exists"},
		{`{"type":"body","contains":"line\u2028sep"}`, `body contains "line\u2028sep"`},
		{`{"type":"body","contains":"tab\there"}`, `body contains "tab\there"`},
		{`{"type":"status","in":[200],"name":"Order placed"}`, "Order placed"},
		{`{"type":"status","in":[200],"name":"  padded  "}`, "  padded  "},
		{`{"type":"status","in":[200],"name":"  "}`, "status in [200]"},
		{`{"type":"status","in":[200],"on_fail":"record"}`, "status in [200]"},
		{`{"type":"body","contains":"` + long + `"}`, (`body contains "` + long)[:128]},
		{`{"type":"status","in":[200],"name":"` + long + `"}`, long},
		// Label is total, also for checks Parse rejects.
		{`{}`, ""},
		{`{"type":"status"}`, "status"},
		{`{"type":"status","in":[200],"between":[200,299]}`, "status in [200]"},
	}
	for _, tc := range cases {
		var c definition.Check
		if err := json.Unmarshal([]byte(tc.check), &c); err != nil {
			t.Fatalf("%s: %v", tc.check, err)
		}
		if got := c.Label(); got != tc.want {
			t.Errorf("%s: Label() = %q, want %q", tc.check, got, tc.want)
		}
	}

	// The cut lands on a UTF-8 boundary and stays within the limit.
	for pad := 0; pad < 4; pad++ {
		c := definition.Check{Type: definition.CheckBody, Contains: ptr(definition.StringScalar(strings.Repeat("x", pad) + strings.Repeat("\u00e9\u20ac\U0001F600", 40)))}
		got := c.Label()
		if len(got) > definition.MaxLabelBytes || len(got) < definition.MaxLabelBytes-3 || !utf8Valid(got) {
			t.Errorf("pad %d: label of %d bytes, valid UTF-8 %v: %q", pad, len(got), utf8Valid(got), got)
		}
	}

	if got := (definition.Extract{VariableName: "token"}).Label(); got != "extract token" {
		t.Errorf("Extract.Label() = %q", got)
	}
}

func ptr[T any](v T) *T { return &v }

func utf8Valid(s string) bool { return utf8.ValidString(s) }

func TestCheckLabelsUnique(t *testing.T) {
	long := strings.Repeat("a", 140)
	step := func(m map[string]any, checks []any, extract []any) {
		s := step0(m)
		if checks != nil {
			s["checks"] = checks
		}
		if extract != nil {
			s["extract"] = extract
		}
	}
	reqExtract := func(v string) map[string]any {
		return map[string]any{"variable_name": v, "selector": "$.a", "type": "json", "required": true}
	}
	cases := []struct {
		name    string
		checks  []any
		extract []any
		want    []string
	}{
		{"same derived label", []any{
			map[string]any{"type": "status", "in": []any{200}},
			map[string]any{"type": "status", "in": []any{200}, "on_fail": "abort"},
		}, nil, []string{check0[:len(check0)-3] + `[1]: the label "status in [200]" is already used by checks[0]; set a name that is unique in the step`}},
		{"name equal to a derived label", []any{
			map[string]any{"type": "status", "in": []any{200}},
			map[string]any{"type": "status", "in": []any{201}, "name": "status in [200]"},
		}, nil, []string{"scenarios[0].steps[0].checks[1].name: the label \"status in [200]\" is already used by checks[0]; set a name that is unique in the step"}},
		{"same name", []any{
			map[string]any{"type": "status", "in": []any{200}, "name": "ok"},
			map[string]any{"type": "body", "contains": "x", "name": "ok"},
		}, nil, []string{"scenarios[0].steps[0].checks[1].name: the label \"ok\" is already used by checks[0]; set a name that is unique in the step"}},
		{"header checks differing only in value", []any{
			map[string]any{"type": "header", "selector": "Authorization", "equals": "Bearer a"},
			map[string]any{"type": "header", "selector": "Authorization", "equals": "Bearer b"},
		}, nil, []string{"scenarios[0].steps[0].checks[1]: the label \"header Authorization equals\" is already used by checks[0]; set a name that is unique in the step"}},
		{"header checks with names", []any{
			map[string]any{"type": "header", "selector": "Authorization", "equals": "Bearer a", "name": "a"},
			map[string]any{"type": "header", "selector": "Authorization", "equals": "Bearer b", "name": "b"},
		}, nil, nil},
		{"labels equal after the cut", []any{
			map[string]any{"type": "body", "contains": long + "1"},
			map[string]any{"type": "body", "contains": long + "2"},
		}, nil, []string{"scenarios[0].steps[0].checks[1]: the label \"body contains \\\"" + long[:128-len(`body contains "`)] + "\" is already used by checks[0]; set a name that is unique in the step"}},
		{"long names are not cut", []any{
			map[string]any{"type": "status", "in": []any{200}, "name": long + "1"},
			map[string]any{"type": "status", "in": []any{201}, "name": long + "2"},
		}, nil, nil},
		{"duplicate required extractors", nil, []any{reqExtract("token"), reqExtract("token")},
			[]string{`scenarios[0].steps[0].extract[1].variable_name: the label "extract token" is already used by extract[0]; a step can have one required extractor per variable`}},
		{"required extractor and a check of the same label", []any{
			map[string]any{"type": "status", "in": []any{200}, "name": "extract token"},
		}, []any{reqExtract("token")},
			[]string{`scenarios[0].steps[0].extract[0].variable_name: the label "extract token" is already used by checks[0]; give that check another name`}},
		{"optional extractors do not collide", []any{
			map[string]any{"type": "status", "in": []any{200}, "name": "extract token"},
		}, []any{
			map[string]any{"variable_name": "token", "selector": "$.a", "type": "json"},
			map[string]any{"variable_name": "token", "selector": "$.b", "type": "json"},
		}, nil},
		{"same label in two steps", nil, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := v2doc(t, func(m map[string]any) {
				step(m, tc.checks, tc.extract)
				if tc.checks == nil && tc.extract == nil {
					c := []any{map[string]any{"type": "status", "in": []any{200}}}
					step0(m)["checks"] = c
					s := scenario0(m)
					s["steps"] = append(s["steps"].([]any), map[string]any{
						"name": "again", "request": map[string]any{"method": "GET", "url": "https://shop.example.com/"}, "checks": c,
					})
				}
			})
			if got := problemStrings(t, doc); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("problems:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestFailMode(t *testing.T) {
	cases := []struct {
		check definition.Check
		want  definition.FailMode
	}{
		{definition.Check{Type: definition.CheckStatus}, definition.FailFail},
		{definition.Check{Type: definition.CheckHeader}, definition.FailRecord},
		{definition.Check{Type: definition.CheckBody}, definition.FailRecord},
		{definition.Check{Type: definition.CheckJSON}, definition.FailRecord},
		{definition.Check{Type: definition.CheckHTML}, definition.FailRecord},
		{definition.Check{Type: definition.CheckTiming}, definition.FailRecord},
		{definition.Check{Type: definition.CheckURL}, definition.FailRecord},
		{definition.Check{Type: definition.CheckCookie}, definition.FailRecord},
		{definition.Check{Type: definition.CheckStatus, OnFail: definition.FailRecord}, definition.FailRecord},
		{definition.Check{Type: definition.CheckBody, OnFail: definition.FailAbort}, definition.FailAbort},
		{definition.Check{Type: definition.CheckJSON, OnFail: definition.FailFail}, definition.FailFail},
	}
	for _, tc := range cases {
		if got := tc.check.FailMode(); got != tc.want {
			t.Errorf("%+v: FailMode() = %q, want %q", tc.check, got, tc.want)
		}
	}
	if got := (definition.Extract{Required: true}).FailMode(); got != definition.FailAbort {
		t.Errorf("Extract default FailMode() = %q", got)
	}
	if got := (definition.Extract{Required: true, OnFail: definition.FailRecord}).FailMode(); got != definition.FailRecord {
		t.Errorf("Extract FailMode() = %q", got)
	}
	// The constants are the wire values.
	if definition.FailRecord != "record" || definition.FailFail != "fail" || definition.FailAbort != "abort" {
		t.Error("fail mode constants changed")
	}
	// on_fail "" reads as absent, so the default applies.
	d := mustParse(t, withChecks(t, map[string]any{"type": "status", "in": []any{200}, "on_fail": ""}))
	if got := d.Scenarios[0].Steps[0].Checks[0].FailMode(); got != definition.FailFail {
		t.Errorf("on_fail \"\": FailMode() = %q", got)
	}
}

func TestRequiredExtract(t *testing.T) {
	ex := func(fields map[string]any) []byte {
		return v2doc(t, func(m map[string]any) {
			e := map[string]any{"variable_name": "token", "selector": "$.token", "type": "json"}
			for k, v := range fields {
				e[k] = v
			}
			step0(m)["extract"] = []any{e}
		})
	}
	p := "scenarios[0].steps[0].extract[0]"
	cases := []struct {
		name   string
		fields map[string]any
		want   []string
	}{
		{"required", map[string]any{"required": true}, nil},
		{"required with on_fail", map[string]any{"required": true, "on_fail": "record"}, nil},
		{"required false", map[string]any{"required": false}, nil},
		{"empty on_fail", map[string]any{"on_fail": ""}, nil},
		{"on_fail without required", map[string]any{"on_fail": "abort"}, []string{p + ".on_fail: only applies with required: true"}},
		{"on_fail with required false", map[string]any{"required": false, "on_fail": "fail"}, []string{p + ".on_fail: only applies with required: true"}},
		{"bad on_fail", map[string]any{"required": true, "on_fail": "stop"}, []string{p + `.on_fail: must be one of "record", "fail", "abort", got "stop"`}},
		{"required as string", map[string]any{"required": "yes"}, []string{p + ".required: must be true or false"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := problemStrings(t, ex(tc.fields)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("problems:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
	d := mustParse(t, ex(map[string]any{"required": true, "on_fail": "fail"}))
	if e := d.Scenarios[0].Steps[0].Extract[0]; !e.Required || e.OnFail != definition.FailFail {
		t.Errorf("extract = %+v", e)
	}
}

func TestExtractPick(t *testing.T) {
	ex := func(typ string, fields map[string]any) []byte {
		return v2doc(t, func(m map[string]any) {
			e := map[string]any{"variable_name": "v", "selector": "x", "type": typ}
			for k, v := range fields {
				e[k] = v
			}
			step0(m)["extract"] = []any{e}
		})
	}
	p := "scenarios[0].steps[0].extract[0]"
	notApplicable := []string{p + ".pick: only applies to json, regex and html extractors"}
	type pickCase struct {
		name   string
		typ    string
		fields map[string]any
		want   []string
	}
	var cases []pickCase
	for _, typ := range []string{"json", "regex", "html"} {
		for _, pick := range []string{"random", "first", "last", "all"} {
			cases = append(cases, pickCase{typ + " " + pick, typ, map[string]any{"pick": pick}, nil})
		}
		cases = append(cases, pickCase{typ + " empty pick", typ, map[string]any{"pick": ""}, nil})
	}
	cases = append(cases,
		pickCase{"required json all", "json", map[string]any{"pick": "all", "required": true, "on_fail": "record"}, nil},
		pickCase{"header empty pick", "header", map[string]any{"pick": ""}, nil},
		pickCase{"cookie empty pick", "cookie", map[string]any{"pick": ""}, nil},
		pickCase{"header pick", "header", map[string]any{"pick": "first"}, notApplicable},
		pickCase{"cookie pick", "cookie", map[string]any{"pick": "all"}, notApplicable},
		pickCase{"header bad pick", "header", map[string]any{"pick": "any"}, notApplicable},
		pickCase{"bad pick", "json", map[string]any{"pick": "any"},
			[]string{p + `.pick: must be one of "random", "first", "last", "all", got "any"`}},
		pickCase{"pick case", "regex", map[string]any{"pick": "First"},
			[]string{p + `.pick: must be one of "random", "first", "last", "all", got "First"`}},
		pickCase{"pick as number", "json", map[string]any{"pick": 1}, []string{p + ".pick: must be a string"}},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := problemStrings(t, ex(tc.typ, tc.fields)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("problems:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
	for _, pick := range []definition.Pick{definition.PickRandom, definition.PickFirst, definition.PickLast, definition.PickAll} {
		d := mustParse(t, ex("html", map[string]any{"pick": string(pick), "attribute": "href"}))
		if e := d.Scenarios[0].Steps[0].Extract[0]; e.Pick != pick {
			t.Errorf("pick %q: extract = %+v", pick, e)
		}
		out, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(out, []byte(`"pick":"`+string(pick)+`"`)) {
			t.Errorf("Marshal lost pick %q: %s", pick, out)
		}
	}
	d := mustParse(t, ex("header", map[string]any{"pick": ""}))
	if out, _ := json.Marshal(d); bytes.Contains(out, []byte(`"pick"`)) {
		t.Errorf("Marshal kept an empty pick: %s", out)
	}
}

func TestExpectedStatuses(t *testing.T) {
	doc := func(v any) []byte {
		return v2doc(t, func(m map[string]any) { m["expected_statuses"] = v })
	}
	d := mustParse(t, doc([]any{200, 201, 404, 100, 599}))
	if !reflect.DeepEqual(d.ExpectedStatuses, []int{200, 201, 404, 100, 599}) {
		t.Errorf("ExpectedStatuses = %v", d.ExpectedStatuses)
	}
	d = mustParse(t, doc([]any{}))
	if len(d.ExpectedStatuses) != 0 {
		t.Errorf("[]: ExpectedStatuses = %v", d.ExpectedStatuses)
	}
	if out, _ := json.Marshal(d); bytes.Contains(out, []byte("expected_statuses")) {
		t.Errorf("Marshal kept an empty expected_statuses: %s", out)
	}
	cases := []struct {
		v    any
		want []string
	}{
		{[]any{200, 600}, []string{"expected_statuses[1]: must be an HTTP status code from 100 to 599, got 600"}},
		{[]any{99}, []string{"expected_statuses[0]: must be an HTTP status code from 100 to 599, got 99"}},
		{[]any{-1}, []string{"expected_statuses[0]: must be an HTTP status code from 100 to 599, got -1"}},
		{[]any{"200"}, []string{"expected_statuses[0]: must be an integer"}},
		{200, []string{"expected_statuses: must be an array"}},
		{nil, []string{"expected_statuses: must not be null"}},
	}
	for _, tc := range cases {
		if got := problemStrings(t, doc(tc.v)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%v: problems:\n got %q\nwant %q", tc.v, got, tc.want)
		}
	}
}

// TestV1IgnoresChecks: a v1 definition carrying the v2 check keys, some of
// them mistyped, parses exactly as it does without them.
func TestV1IgnoresChecks(t *testing.T) {
	plain := `{"name":"v1","virtual_users":1,"scenarios":[{"name":"s","steps":[{"name":"a",` +
		`"request":{"method":"GET","url":"https://shop.example.com/"},` +
		`"extract":[{"variable_name":"t","selector":"$.t","type":"json"%s}]%s}]}]%s}`
	without := []byte(strings.ReplaceAll(plain, "%s", ""))
	for _, extra := range [][3]string{
		{`,"required":true,"on_fail":"abort","pick":"all"`, `,"checks":[{"type":"status","in":[200]}]`, `,"expected_statuses":[200]`},
		{`,"required":"yes","on_fail":5,"pick":5`, `,"checks":"status 200"`, `,"expected_statuses":"2xx"`},
		{`,"Required":1,"ON_FAIL":[],"Pick":{"a":1}`, `,"Checks":{"type":1},"checks":[{"equals":{"a":1},"in":[9999]}]`, `,"Expected_Statuses":[true]`},
		{`,"required":true,"on_fail":"fail"`, `,"checks":[{"type":"json","selector":"$.a","equals":1e999,"contains":-1e400}]`, `,"expected_statuses":[200]`},
	} {
		with := []byte(strings.Replace(strings.Replace(strings.Replace(plain, "%s", extra[0], 1), "%s", extra[1], 1), "%s", extra[2], 1))
		got, err := definition.Parse(with)
		if err != nil {
			t.Fatalf("Parse: %v\n%s", err, with)
		}
		want := mustParse(t, without)
		if !got.IsV1() || len(got.V1TypeProblems) != 0 {
			t.Errorf("IsV1 %v, V1TypeProblems %v", got.IsV1(), got.V1TypeProblems)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("v1 with check keys:\n got %+v\nwant %+v", got, want)
		}
	}
}

// TestCheckLabelsSurviveMarshal: a check's label is the same after Marshal
// and Parse, including values that encoding/json escapes.
func TestCheckLabelsSurviveMarshal(t *testing.T) {
	doc := withChecks(t,
		map[string]any{"type": "html", "selector": "h1", "equals": "<b>Sale</b> & more"},
		map[string]any{"type": "body", "contains": "line\u2028sep\u2029end"},
		map[string]any{"type": "body", "not_contains": "é \u00e9 \x01"},
		map[string]any{"type": "json", "selector": "$.id", "equals": json.Number("1234567890123456789")},
		map[string]any{"type": "json", "selector": "$.n", "equals": json.Number("1.50")},
		map[string]any{"type": "url", "matches": `^https://[^/]+/a\?b=<c>$`},
		map[string]any{"type": "status", "between": []any{200, 299}},
		map[string]any{"type": "header", "selector": "Set-Cookie", "contains": "a&b"},
		map[string]any{"type": "body", "contains": strings.Repeat("<", 200)},
	)
	d := mustParse(t, doc)
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	d2 := mustParse(t, out)
	for i, c := range d.Scenarios[0].Steps[0].Checks {
		if a, b := c.Label(), d2.Scenarios[0].Steps[0].Checks[i].Label(); a != b {
			t.Errorf("checks[%d]: label %q became %q", i, a, b)
		}
	}
	out2, _ := json.Marshal(d2)
	if !bytes.Equal(out, out2) {
		t.Errorf("Marshal is not a fixed point:\n%s\n%s", out, out2)
	}
}
