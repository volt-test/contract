package definition_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

const (
	sp0          = "scenarios[0].steps[0]"
	hostOnce     = "must have exactly one value: a request has one Host"
	headerKind   = "must be a string or an array of strings"
	headerEmpty  = "must not be an empty array; give at least one value"
	multipartCT  = `cannot be set with body_type "multipart": the Content-Type carries the part boundary and is set for you`
	needSource   = "needs path or content_base64"
	twoSources   = "cannot have both path and content_base64; use one of them"
	headerBreak  = "must not contain a line break (CR or LF) or NUL"
	b64          = "must be standard base64 with padding"
	readsBody    = "which reads the response body"
	binaryText   = "json and html read the body as text"
	onlyRaw      = `only applies with body_type "raw"`
	onlyMulti    = `only applies with body_type "multipart"`
	withBody     = "cannot be combined with body; use one of them"
	notPositive  = "must be greater than zero"
	mustBeString = "must be a string"
	noParts      = `"multipart" needs fields or files`
	partName     = "must not have a line break (CR or LF) or NUL in its name"
	alsoField    = "is also a field name; a name is either a field or a file"
	sizeLimit    = "must be at most 1gib (1073741824 bytes)"
)

// tier2FixtureProblems maps each invalid fixture for headers, multipart
// and binary bodies, response reading and the request timeout to the
// problems Parse reports, in order.
var tier2FixtureProblems = map[string][]string{
	"structure_headers_host_two_values": {rq + ".headers.Host: " + hostOnce},
	"structure_headers_host_twice":      {rq + `.headers.host: names the Host header again (as "Host"); a request has one Host`},
	"structure_headers_empty_array":     {rq + ".headers.Accept: " + headerEmpty},
	"type_headers_element_number":       {rq + ".headers.Accept[1]: " + mustBeString},
	"type_headers_element_null":         {rq + ".headers.Accept[0]: must not be null"},
	"type_headers_object":               {rq + ".headers.Accept: " + headerKind},
	"type_headers_array":                {rq + ".headers: must be an object"},

	"structure_multipart_content_type":          {rq + ".headers.content-type: " + multipartCT},
	"structure_multipart_with_body":             {rq + `.body: cannot be combined with body_type "multipart"; use fields and files`},
	"structure_multipart_no_parts":              {rq + ".body_type: " + noParts},
	"structure_multipart_field_name_line_break": {rq + `.fields["title\r\nX-Injected: 1"]: ` + partName},
	"structure_files_name_line_break":           {rq + `.files["doc\n"]: ` + partName},
	"structure_files_also_field":                {rq + ".files.doc: " + alsoField},
	"structure_files_blank_path":                {rq + ".files.doc.path: must not be blank"},
	"structure_files_path_line_break":           {rq + ".files.doc.path: " + headerBreak},
	"structure_files_without_multipart":         {rq + ".files: " + onlyMulti},
	"structure_files_no_source":                 {rq + ".files.doc: " + needSource},
	"structure_files_two_sources":               {rq + ".files.doc: " + twoSources},
	"structure_files_filename_line_break":       {rq + ".files.doc.filename: " + headerBreak},
	"structure_files_content_type_nul":          {rq + ".files.doc.content_type: " + headerBreak},
	"structure_files_bad_content_type":          {rq + `.files.doc.content_type: must be a media type such as "image/png", got "pdf"`},
	"structure_files_bad_base64":                {rq + ".files.doc.content_base64: " + b64 + ": invalid data at offset 3"},
	"type_files_string":                         {rq + ".files.doc: must be an object"},

	"structure_binary_with_body":  {rq + ".body_base64: " + withBody},
	"structure_binary_not_raw":    {rq + ".body_base64: " + onlyRaw},
	"structure_binary_line_break": {rq + ".body_base64: " + b64 + " and no line breaks"},
	"structure_binary_bad_length": {rq + ".body_base64: " + b64 + ": the length must be a multiple of 4"},

	"structure_discard_body_contains":        {sp0 + ".discard_body: cannot be combined with checks[1] (a body check with contains), " + readsBody},
	"structure_discard_body_json_reader":     {sp0 + ".discard_body: cannot be combined with checks[0] (a json check), " + readsBody},
	"structure_discard_body_html_reader":     {sp0 + ".discard_body: cannot be combined with checks[0] (an html check), " + readsBody},
	"structure_discard_body_regex_extractor": {sp0 + ".discard_body: cannot be combined with extract[0] (a regex extractor), " + readsBody},
	"structure_discard_body_json_extractor":  {sp0 + ".discard_body: cannot be combined with extract[0] (a json extractor), " + readsBody},
	"type_discard_body_string":               {sp0 + ".discard_body: must be true or false"},

	"structure_response_type_unknown":               {sp0 + `.response_type: must be one of "text", "binary", got "image"`},
	"structure_response_type_binary_json_reader":    {sp0 + `.response_type: "binary" cannot be combined with checks[0] (a json check); ` + binaryText},
	"structure_response_type_binary_html_extractor": {sp0 + `.response_type: "binary" cannot be combined with extract[0] (an html extractor); ` + binaryText},
	"type_response_type_number":                     {sp0 + ".response_type: " + mustBeString},

	"structure_request_timeout_zero": {rq + ".timeout: " + notPositive},
	"structure_request_timeout_bad":  {rq + `.timeout: must be a duration such as "30s" or "1m30s", got "5"`},
	"type_request_timeout_number":    {rq + ".timeout: " + mustBeString},
}

// tier2FixturePrefixes are the fixture name prefixes this file owns.
var tier2FixturePrefixes = []string{
	"structure_headers_", "type_headers_", "structure_multipart_", "structure_files_", "type_files_",
	"structure_binary_", "structure_discard_body_", "type_discard_body_", "structure_response_type_",
	"type_response_type_", "structure_request_", "type_request_",
}

// TestHTTPTier2Fixtures checks that each invalid fixture for the new
// request and step keys fails with exactly the problems listed for it, and
// that every such fixture is listed.
func TestHTTPTier2Fixtures(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures(t, "v2/invalid") {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		want, ok := tier2FixtureProblems[name]
		if !ok {
			for _, p := range tier2FixturePrefixes {
				if strings.HasPrefix(name, p) {
					t.Errorf("%s: no expected problem in tier2FixtureProblems", name)
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
	for name := range tier2FixtureProblems {
		if !seen[name] {
			t.Errorf("missing fixture testdata/v2/invalid/%s.json", name)
		}
	}
}

// tier2Case is one request or step, its expected problems, and whether
// the schema accepts it although Parse does not.
type tier2Case struct {
	name   string
	doc    []byte
	want   []string
	looser bool
}

func runTier2Cases(t *testing.T, cases []tier2Case) {
	t.Helper()
	schema := compileSchema(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !json.Valid(tc.doc) {
				t.Fatalf("test document is not JSON: %s", tc.doc)
			}
			if got := problemStrings(t, tc.doc); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("problems:\n got %q\nwant %q", got, tc.want)
			}
			schemaErr := validateAgainstSchema(t, schema, tc.doc)
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

// stepDoc is bodyDoc with step keys after the request.
func stepDoc(request, stepKeys string) []byte {
	return []byte(`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"a","steps":[{"name":"b","request":` +
		request + `,` + stepKeys + `}]}]}`)
}

func TestHeaderRules(t *testing.T) {
	hdr := func(h string) []byte { return bodyDoc(req(`"headers":` + h)) }
	runTier2Cases(t, []tier2Case{
		{name: "string", doc: hdr(`{"Accept":"text/html"}`)},
		{name: "array", doc: hdr(`{"Accept":["text/html","application/json"]}`)},
		{name: "one-element array", doc: hdr(`{"Accept":["text/html"]}`)},
		{name: "empty strings", doc: hdr(`{"X-A":"","X-B":["",""]}`)},
		{name: "Host", doc: hdr(`{"Host":"shop.example.com"}`)},
		{name: "Host as one-element array", doc: hdr(`{"host":["shop.example.com"]}`)},
		{name: "templated Host", doc: hdr(`{"HOST":"${host}"}`)},
		{name: "Content-Type with raw", doc: hdr(`{"Content-Type":"text/plain"}`)},
		{name: "empty headers", doc: hdr(`{}`)},

		{name: "Host twice", doc: hdr(`{"Host":["a","b"]}`), want: []string{rq + ".headers.Host: " + hostOnce}, looser: true},
		{name: "host in lower case twice", doc: hdr(`{"host":["a","b"]}`), want: []string{rq + ".headers.host: " + hostOnce}, looser: true},
		{name: "Host under three spellings", doc: hdr(`{"host":"a","Host":"b","HOST":"c"}`), want: []string{
			rq + `.headers.Host: names the Host header again (as "HOST"); a request has one Host`,
			rq + `.headers.host: names the Host header again (as "HOST"); a request has one Host`,
		}, looser: true},
		{name: "Host twice under two spellings", doc: hdr(`{"Host":["a","b"],"host":"c"}`), want: []string{
			rq + ".headers.Host: " + hostOnce,
			rq + `.headers.host: names the Host header again (as "Host"); a request has one Host`,
		}, looser: true},
		{name: "second Host key with two values", doc: hdr(`{"Host":"a","host":["b","c"]}`), want: []string{
			rq + `.headers.host: names the Host header again (as "Host"); a request has one Host`,
		}, looser: true},
		{name: "empty array", doc: hdr(`{"Accept":[]}`), want: []string{rq + ".headers.Accept: " + headerEmpty}},
		{name: "empty Host array", doc: hdr(`{"Host":[]}`), want: []string{rq + ".headers.Host: " + headerEmpty}},
		{name: "number", doc: hdr(`{"A":1}`), want: []string{rq + ".headers.A: " + headerKind}},
		{name: "true", doc: hdr(`{"A":true}`), want: []string{rq + ".headers.A: " + headerKind}},
		{name: "object", doc: hdr(`{"A":{}}`), want: []string{rq + ".headers.A: " + headerKind}},
		{name: "null", doc: hdr(`{"A":null}`), want: []string{rq + ".headers.A: must not be null"}},
		{name: "elements", doc: hdr(`{"A":["x",1,null,{},["y"],true]}`), want: []string{
			rq + ".headers.A[1]: " + mustBeString, rq + ".headers.A[2]: must not be null", rq + ".headers.A[3]: " + mustBeString,
			rq + ".headers.A[4]: " + mustBeString, rq + ".headers.A[5]: " + mustBeString,
		}},
		{name: "odd name", doc: hdr(`{"X Y":[1]}`), want: []string{rq + `.headers["X Y"][0]: ` + mustBeString}},
	})
}

// TestHeaderValuesRoundTrip: one value is written back as a plain string,
// several as an array in order, and both read back the same.
func TestHeaderValuesRoundTrip(t *testing.T) {
	d := mustParse(t, bodyDoc(req(`"headers":{"A":["one"],"B":"two","C":["x","y","x"]}`)))
	h := d.Scenarios[0].Steps[0].Request.Headers
	want := map[string]definition.HeaderValues{"A": {"one"}, "B": {"two"}, "C": {"x", "y", "x"}}
	if !reflect.DeepEqual(h, want) {
		t.Fatalf("Headers = %q, want %q", h, want)
	}
	out := mustMarshal(t, d)
	if !bytes.Contains(out, []byte(`"headers":{"A":"one","B":"two","C":["x","y","x"]}`)) {
		t.Errorf("Marshal = %s", out)
	}
	if again := mustParse(t, out); !reflect.DeepEqual(again, d) {
		t.Errorf("Parse(Marshal(d)) != d")
	}
}

// TestHeaderValuesDirect covers UnmarshalJSON and MarshalJSON outside Parse,
// the empty value included.
func TestHeaderValuesDirect(t *testing.T) {
	for in, want := range map[string]definition.HeaderValues{
		`"a"`: {"a"}, `""`: {""}, `["a"]`: {"a"}, `["a","b"]`: {"a", "b"}, ` [ "<&>" , "é" ] `: {"<&>", "é"},
	} {
		var h definition.HeaderValues
		if err := json.Unmarshal([]byte(in), &h); err != nil || !reflect.DeepEqual(h, want) {
			t.Errorf("%s: %q, %v; want %q", in, h, err, want)
		}
	}
	for in, msg := range map[string]string{
		`[]`: headerEmpty, `1`: headerKind, `{}`: headerKind, `true`: headerKind,
		`["a",1]`: "[1]: must be a string", `[null]`: "[0]: must be a string",
	} {
		var h definition.HeaderValues
		if err := json.Unmarshal([]byte(in), &h); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: err = %v, want %q", in, err, msg)
		}
	}
	h := definition.HeaderValues{"keep"}
	if err := json.Unmarshal([]byte(`null`), &h); err != nil || !reflect.DeepEqual(h, definition.HeaderValues{"keep"}) {
		t.Errorf("null: %q, %v", h, err)
	}
	for _, tc := range []struct {
		h    definition.HeaderValues
		want string
	}{
		{nil, `""`}, {definition.HeaderValues{}, `""`}, {definition.HeaderValues{""}, `""`},
		{definition.HeaderValues{"a"}, `"a"`}, {definition.HeaderValues{"a", "b"}, `["a","b"]`},
	} {
		out, err := json.Marshal(tc.h)
		if err != nil || string(out) != tc.want {
			t.Errorf("Marshal(%#v) = %s, %v; want %s", tc.h, out, err, tc.want)
			continue
		}
		// Whatever it writes reads back: the empty value as one empty value.
		var back definition.HeaderValues
		if err := json.Unmarshal(out, &back); err != nil || len(back) == 0 {
			t.Errorf("Unmarshal(%s) = %q, %v", out, back, err)
		}
	}
	// A request built in Go with an empty value still marshals to a
	// definition Parse reads.
	r := definition.Request{Method: "GET", URL: "https://example.com/", Headers: map[string]definition.HeaderValues{"A": nil}}
	if out := string(mustMarshal(t, r)); !strings.Contains(out, `"headers":{"A":""}`) {
		t.Errorf("Marshal = %s", out)
	}
}

// TestHeaderValuesEscapeHTML: a header map writes the bytes a
// map[string]string or map[string][]string writes, whether the encoder
// escapes <, > and & or not.
func TestHeaderValuesEscapeHTML(t *testing.T) {
	encode := func(v any, escape bool) string {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(escape)
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	one := map[string]definition.HeaderValues{"A": {"<a href='x'>&amp;</a>\u2028"}}
	oneOld := map[string]string{"A": "<a href='x'>&amp;</a>\u2028"}
	several := map[string]definition.HeaderValues{"A": {"<b>", "x&y"}}
	severalOld := map[string][]string{"A": {"<b>", "x&y"}}
	for _, escape := range []bool{true, false} {
		if got, want := encode(one, escape), encode(oneOld, escape); got != want {
			t.Errorf("escape %v, one value: %s, want %s", escape, got, want)
		}
		if got, want := encode(several, escape), encode(severalOld, escape); got != want {
			t.Errorf("escape %v, several values: %s, want %s", escape, got, want)
		}
	}
	if got := encode(one, false); !strings.Contains(got, "<a href") {
		t.Errorf("SetEscapeHTML(false) still escapes: %s", got)
	}
}

// TestV1HeaderValues: in v1 a header value is a string; an array is the
// loose type problem v0.3.0 gave, null reads as one empty value, and
// header (the v1 key) is unchanged.
func TestV1HeaderValues(t *testing.T) {
	doc := func(headers string) []byte {
		return []byte(`{"name":"v1","virtual_users":1,"scenarios":[{"name":"s","steps":[{"name":"a",` +
			`"request":{"method":"GET","url":"https://example.com/","headers":` + headers + `}}]}]}`)
	}
	d := mustParse(t, doc(`{"A":["x","y"],"B":"b","C":null,"D":[]}`))
	want := []definition.Problem{{Path: "scenarios[0].steps[0].request.headers.A", Message: mustBeString, Count: 2}}
	if !reflect.DeepEqual(d.V1TypeProblems, want) {
		t.Errorf("V1TypeProblems = %v, want %v", d.V1TypeProblems, want)
	}
	h := d.Scenarios[0].Steps[0].Request.Headers
	if wantH := (map[string]definition.HeaderValues{"B": {"b"}, "C": {""}}); !reflect.DeepEqual(h, wantH) {
		t.Errorf("Headers = %q, want %q", h, wantH)
	}
	if out := string(mustMarshal(t, d)); !strings.Contains(out, `"headers":{"B":"b","C":""}`) {
		t.Errorf("Marshal = %s", out)
	}
	// The cloud engine's read set still sees the problem.
	if ps := d.V1TypeProblemsIn(definition.MustReadSet("scenarios[].steps[].request.headers")); len(ps) != 1 {
		t.Errorf("V1TypeProblemsIn(headers) = %v", ps)
	}
}

func TestMultipartRules(t *testing.T) {
	const fp = rq + ".files.doc"
	runTier2Cases(t, []tier2Case{
		{name: "fields and files", doc: bodyDoc(req(`"body_type":"multipart","fields":{"t":"${x}"},"files":{"doc":{"path":"a.pdf"},"img":{"content_base64":"AAAA","filename":"${n}.png","content_type":"image/png"}}`))},
		{name: "fields only", doc: bodyDoc(req(`"body_type":"multipart","fields":{"t":"1"}`))},
		{name: "files only", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":"a.pdf"}}`))},
		{name: "empty files with raw", doc: bodyDoc(req(`"body":"x","files":{}`))},
		{name: "content type with parameters", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":"a.txt","content_type":"text/plain; charset=utf-8"}}`))},
		{name: "zero keys read as absent", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":"a","content_base64":"","filename":"","content_type":""}}`))},
		{name: "Content-Type with form", doc: bodyDoc(req(`"body_type":"form","fields":{"a":"1"},"headers":{"Content-Type":"text/plain"}`))},
		{name: "form field name with a line break", doc: bodyDoc(req(`"body_type":"form","fields":{"a\nb":"1"}`))},
		{name: "path with spaces", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":" my report.pdf "}}`))},

		{name: "no parts", doc: bodyDoc(req(`"body_type":"multipart"`)), want: []string{rq + ".body_type: " + noParts}},
		{name: "empty containers", doc: bodyDoc(req(`"body_type":"multipart","fields":{},"files":{}`)), want: []string{rq + ".body_type: " + noParts}},
		{name: "field name CR", doc: bodyDoc(req(`"body_type":"multipart","fields":{"ok":"1","a\rb":"2"}`)),
			want: []string{rq + `.fields["a\rb"]: ` + partName}, looser: true},
		{name: "file name LF", doc: bodyDoc(req(`"body_type":"multipart","files":{"a\nb":{"path":"a"}}`)),
			want: []string{rq + `.files["a\nb"]: ` + partName}, looser: true},
		{name: "file name NUL", doc: bodyDoc(req(`"body_type":"multipart","files":{"a\u0000":{"path":"a"}}`)),
			want: []string{rq + `.files["a\x00"]: ` + partName}, looser: true},
		{name: "name in fields and files", doc: bodyDoc(req(`"body_type":"multipart","fields":{"doc":"x","t":"y"},"files":{"doc":{"path":"a"},"img":{"path":"b"}}`)),
			want: []string{rq + ".files.doc: " + alsoField}, looser: true},
		{name: "blank path", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":" \t "}}`)),
			want: []string{fp + ".path: must not be blank"}, looser: true},
		{name: "path LF", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":"a\nb"}}`)),
			want: []string{fp + ".path: " + headerBreak}, looser: true},
		{name: "path CR", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":"a\r"}}`)),
			want: []string{fp + ".path: " + headerBreak}, looser: true},

		{name: "Content-Type", doc: bodyDoc(req(`"body_type":"multipart","fields":{"a":"1"},"headers":{"CONTENT-TYPE":"x"}`)),
			want: []string{rq + ".headers.CONTENT-TYPE: " + multipartCT}, looser: true},
		{name: "body", doc: bodyDoc(req(`"body_type":"multipart","fields":{"a":"1"},"body":"x"`)),
			want: []string{rq + `.body: cannot be combined with body_type "multipart"; use fields and files`}},
		{name: "body_json", doc: bodyDoc(req(`"body_type":"multipart","fields":{"a":"1"},"body_json":{}`)),
			want: []string{rq + `.body_json: only applies with body_type "json"`}},
		{name: "files with raw", doc: bodyDoc(req(`"files":{"doc":{"path":"a"}}`)), want: []string{rq + ".files: " + onlyMulti}},
		{name: "files with form", doc: bodyDoc(req(`"body_type":"form","files":{"doc":{"path":"a"}}`)), want: []string{rq + ".files: " + onlyMulti}},
		{name: "no source", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{}}`)), want: []string{fp + ": " + needSource}},
		{name: "both sources", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":"a","content_base64":"AAAA"}}`)),
			want: []string{fp + ": " + twoSources}},
		{name: "filename CR", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":"a","filename":"a\rb"}}`)),
			want: []string{fp + ".filename: " + headerBreak}, looser: true},
		{name: "filename NUL", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":"a","filename":"a\u0000"}}`)),
			want: []string{fp + ".filename: " + headerBreak}, looser: true},
		{name: "content type LF", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":"a","content_type":"text/plain\nX: y"}}`)),
			want: []string{fp + ".content_type: " + headerBreak}, looser: true},
		{name: "content type without subtype", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":"a","content_type":"image"}}`)),
			want: []string{fp + `.content_type: must be a media type such as "image/png", got "image"`}, looser: true},
		{name: "content type not a token", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":"a","content_type":"image/png extra"}}`)),
			want: []string{fp + `.content_type: must be a media type such as "image/png", got "image/png extra"`}, looser: true},
		{name: "files sorted by name in problems", doc: bodyDoc(req(`"body_type":"multipart","files":{"z":{},"a b":{},"m":{}}`)),
			want: []string{rq + `.files["a b"]: ` + needSource, rq + ".files.m: " + needSource, rq + ".files.z: " + needSource}},
		{name: "file not an object", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":"a.pdf"}`)), want: []string{fp + ": must be an object"}},
		{name: "files an array", doc: bodyDoc(req(`"body_type":"multipart","files":[]`)), want: []string{rq + ".files: must be an object"}},
		{name: "path a number", doc: bodyDoc(req(`"body_type":"multipart","files":{"doc":{"path":1}}`)), want: []string{fp + ".path: " + mustBeString}},
	})
}

func TestBinaryBodyRules(t *testing.T) {
	bb := func(v string) []byte { return bodyDoc(req(`"body_base64":` + v)) }
	runTier2Cases(t, []tier2Case{
		{name: "binary body", doc: bb(`"AAECAwQ="`)},
		{name: "two pad characters", doc: bb(`"AA=="`)},
		{name: "explicit raw", doc: bodyDoc(req(`"body_type":"raw","body_base64":"AAAA"`))},
		{name: "empty body next to it", doc: bodyDoc(req(`"body":"","body_base64":"AAAA"`))},
		{name: "empty reads as absent", doc: bodyDoc(req(`"body_type":"json","body_json":{},"body_base64":""`))},

		{name: "with body", doc: bodyDoc(req(`"body":"x","body_base64":"AAAA"`)), want: []string{rq + ".body_base64: " + withBody}},
		{name: "with form", doc: bodyDoc(req(`"body_type":"form","body_base64":"AAAA"`)), want: []string{rq + ".body_base64: " + onlyRaw}},
		{name: "with multipart", doc: bodyDoc(req(`"body_type":"multipart","fields":{"a":"1"},"body_base64":"AAAA"`)), want: []string{rq + ".body_base64: " + onlyRaw}},
		{name: "URL alphabet", doc: bb(`"AA-_"`), want: []string{rq + ".body_base64: " + b64 + ": invalid data at offset 2"}, looser: true},
		{name: "no padding", doc: bb(`"AAECAwQ"`), want: []string{rq + ".body_base64: " + b64 + ": the length must be a multiple of 4"}, looser: true},
		{name: "three pad characters", doc: bb(`"A==="`), want: []string{rq + ".body_base64: " + b64 + ": = may only end the value"}, looser: true},
		{name: "padding inside", doc: bb(`"AA==AAAA"`), want: []string{rq + ".body_base64: " + b64 + ": = may only end the value"}, looser: true},
		{name: "padding at a chunk boundary", doc: bb(`"` + strings.Repeat("A", 1022) + `==` + "AAAA" + `"`),
			want: []string{rq + ".body_base64: " + b64 + ": = may only end the value"}, looser: true},
		{name: "bad data after the first chunk", doc: bb(`"` + strings.Repeat("A", 2000) + `!AAA"`),
			want: []string{rq + ".body_base64: " + b64 + ": invalid data at offset 2000"}, looser: true},
		{name: "non-zero trailing bits", doc: bb(`"AB=="`), want: []string{rq + ".body_base64: " + b64 + ": invalid data at offset 2"}, looser: true},
		{name: "line break", doc: bb(`"AAAA\r\nAAAA"`), want: []string{rq + ".body_base64: " + b64 + " and no line breaks"}, looser: true},
		{name: "space", doc: bb(`"AA AA"`), want: []string{rq + ".body_base64: " + b64 + ": the length must be a multiple of 4"}, looser: true},
	})
}

// TestBase64ProblemsDoNotEchoValues: base64 problems never repeat the
// value, which may be a file's content.
func TestBase64ProblemsDoNotEchoValues(t *testing.T) {
	for _, v := range []string{"ZQX9ZQX9!", "ZQX9ZQX", "ZQX9\nZQX9", "ZQ==ZQX9"} {
		for _, set := range []func(r map[string]any){
			func(r map[string]any) { r["body_base64"] = v },
			func(r map[string]any) {
				r["body_type"] = "multipart"
				r["files"] = map[string]any{"f": map[string]any{"content_base64": v}}
			},
		} {
			got := problemStrings(t, v2doc(t, func(m map[string]any) { set(request0(m)) }))
			if len(got) == 0 {
				t.Fatalf("%q: no problem", v)
			}
			for _, p := range got {
				if strings.Contains(p, "ZQX9") {
					t.Errorf("problem repeats the value: %s", p)
				}
			}
		}
	}
}

// TestInlineBinaryCap: the decoded size of every content_base64 and
// body_base64 together may reach MaxInlineBinaryBytes, not more. The
// problem names the value that crosses it, once.
func TestInlineBinaryCap(t *testing.T) {
	enc := func(n int) string { return base64.StdEncoding.EncodeToString(make([]byte, n)) }
	half := definition.MaxInlineBinaryBytes / 2
	doc := func(a, b, c string) []byte {
		return []byte(`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"s","steps":[` +
			`{"name":"a","request":{"method":"POST","url":"https://example.com/","body_base64":"` + a + `"}},` +
			`{"name":"b","request":{"method":"POST","url":"https://example.com/","body_type":"multipart",` +
			`"files":{"y":{"content_base64":"` + c + `"},"x":{"content_base64":"` + b + `"}}}}]}]}`)
	}
	if got := problemStrings(t, doc(enc(half), enc(half-1), "AA==")); got != nil {
		t.Errorf("exactly the cap: %q", got)
	}
	over := "brings the inline binary content of the definition (every content_base64 and body_base64, decoded) " +
		"over 10485760 bytes (10 MiB); send large files by path"
	// Files are counted in field name order: x crosses the cap, y does not
	// add a second problem.
	want := []string{"scenarios[0].steps[1].request.files.x.content_base64: " + over}
	if got := problemStrings(t, doc(enc(half), enc(half+1), "AA==")); !reflect.DeepEqual(got, want) {
		t.Errorf("one byte over:\n got %q\nwant %q", got, want)
	}
	want = []string{"scenarios[0].steps[0].request.body_base64: " + over}
	if got := problemStrings(t, doc(enc(definition.MaxInlineBinaryBytes+1), "AA==", "AA==")); !reflect.DeepEqual(got, want) {
		t.Errorf("one value over:\n got %q\nwant %q", got, want)
	}
}

func TestDiscardBodyRules(t *testing.T) {
	get := `{"method":"GET","url":"https://example.com/"}`
	step := func(keys string) []byte { return stepDoc(get, keys) }
	const dp = sp0 + ".discard_body: cannot be combined with "
	runTier2Cases(t, []tier2Case{
		{name: "discard alone", doc: step(`"discard_body":true`)},
		{name: "false", doc: step(`"discard_body":false,"checks":[{"type":"json","selector":"$.a","exists":true}]`)},
		{name: "checks that do not read the body", doc: step(`"discard_body":true,"checks":[{"type":"status","in":[200]},` +
			`{"type":"header","selector":"X","exists":true},{"type":"body","size":"> 0"},{"type":"body","empty":true},` +
			`{"type":"timing","duration":"< 1s"},{"type":"url","contains":"a"},{"type":"cookie","selector":"s","exists":true}]`)},
		{name: "extractors that do not read the body", doc: step(`"discard_body":true,"extract":[` +
			`{"variable_name":"h","selector":"X","type":"header"},{"variable_name":"c","selector":"s","type":"cookie"}]`)},
		{name: "test-level discard_bodies with readers", doc: []byte(`{"schema_version":"2.0","name":"x","virtual_users":1,` +
			`"http":{"discard_bodies":true},"scenarios":[{"name":"a","steps":[{"name":"b","request":` + get + `,` +
			`"checks":[{"type":"body","contains":"ok"}],"extract":[{"variable_name":"v","selector":"$.a","type":"json"}]}]}]}`)},

		{name: "every body operator", doc: step(`"discard_body":true,"checks":[{"type":"body","equals":"a"},` +
			`{"type":"body","contains":"b"},{"type":"body","not_contains":"c"},{"type":"body","matches":"d"}]`), want: []string{
			dp + "checks[0] (a body check with equals), " + readsBody, dp + "checks[1] (a body check with contains), " + readsBody,
			dp + "checks[2] (a body check with not_contains), " + readsBody, dp + "checks[3] (a body check with matches), " + readsBody,
		}, looser: true},
		{name: "json and html", doc: step(`"discard_body":true,"checks":[{"type":"html","selector":"h1","equals":"x"},` +
			`{"type":"json","selector":"$.a","is":"array"}],"extract":[{"variable_name":"t","selector":"h1","type":"html"},` +
			`{"variable_name":"r","selector":"(a)","type":"regex"},{"variable_name":"j","selector":"$.a","type":"json"}]`), want: []string{
			dp + "checks[0] (an html check), " + readsBody, dp + "checks[1] (a json check), " + readsBody,
			dp + "extract[0] (an html extractor), " + readsBody, dp + "extract[1] (a regex extractor), " + readsBody,
			dp + "extract[2] (a json extractor), " + readsBody,
		}, looser: true},
		{name: "unknown check type is reported once", doc: step(`"discard_body":true,"checks":[{"type":"","equals":"a"}]`),
			want: []string{sp0 + `.checks[0].type: is required (one of "status", "header", "body", "json", "html", "timing", "url", "cookie")`}},
	})
}

// TestDiscardBodyOverride: a step's discard_body: false keeps its body
// under http.discard_bodies, readers included, and is kept as a value.
func TestDiscardBodyOverride(t *testing.T) {
	doc := []byte(`{"schema_version":"2.0","name":"x","virtual_users":1,"http":{"discard_bodies":true},` +
		`"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://example.com/"},` +
		`"discard_body":false,"checks":[{"type":"json","selector":"$.a","exists":true}]},` +
		`{"name":"c","request":{"method":"GET","url":"https://example.com/"}}]}]}`)
	d := mustParse(t, doc)
	steps := d.Scenarios[0].Steps
	if steps[0].DiscardBody == nil || *steps[0].DiscardBody {
		t.Errorf("discard_body = %v, want false", steps[0].DiscardBody)
	}
	if steps[1].DiscardBody != nil {
		t.Errorf("absent discard_body = %v, want nil", *steps[1].DiscardBody)
	}
	if out := mustMarshal(t, d); !bytes.Contains(out, []byte(`"discard_body":false`)) {
		t.Errorf("Marshal drops discard_body: false: %s", out)
	}
}

// TestStepReadsBody: ReadsBody is true exactly for the steps on which
// Parse refuses discard_body: true.
func TestStepReadsBody(t *testing.T) {
	for _, keys := range []string{
		``,
		`"checks":[{"type":"status","in":[200]},{"type":"header","selector":"X","exists":true},{"type":"body","size":"> 0"},` +
			`{"type":"body","empty":true},{"type":"timing","duration":"< 1s"},{"type":"url","contains":"a"},` +
			`{"type":"cookie","selector":"s","exists":true}]`,
		`"extract":[{"variable_name":"h","selector":"X","type":"header"},{"variable_name":"c","selector":"s","type":"cookie"}]`,
		`"checks":[{"type":"body","equals":"a"}]`,
		`"checks":[{"type":"body","contains":"a"}]`,
		`"checks":[{"type":"body","not_contains":"a"}]`,
		`"checks":[{"type":"body","matches":"a"}]`,
		`"checks":[{"type":"body","size":"> 0"},{"type":"body","contains":"a"}]`,
		`"checks":[{"type":"json","selector":"$.a","exists":true}]`,
		`"checks":[{"type":"html","selector":"h1","exists":true}]`,
		`"extract":[{"variable_name":"v","selector":"$.a","type":"json"}]`,
		`"extract":[{"variable_name":"v","selector":"(a)","type":"regex"}]`,
		`"extract":[{"variable_name":"v","selector":"h1","type":"html"}]`,
		`"checks":[{"type":"status","in":[200]}],"extract":[{"variable_name":"v","selector":"(a)","type":"regex"}]`,
	} {
		get := `{"method":"GET","url":"https://example.com/"}`
		withKeys := func(extra string) []byte {
			if keys == "" {
				return stepDoc(get, strings.TrimPrefix(extra, ","))
			}
			return stepDoc(get, keys+extra)
		}
		reads := mustParse(t, withKeys(`,"think_time":"1ms"`)).Scenarios[0].Steps[0].ReadsBody()
		refused := false
		for _, p := range problemStrings(t, withKeys(`,"discard_body":true`)) {
			refused = refused || strings.HasPrefix(p, sp0+".discard_body: ")
		}
		if reads != refused {
			t.Errorf("%s: ReadsBody() = %v, discard_body: true refused = %v", keys, reads, refused)
		}
	}
	var zero definition.Step
	if zero.ReadsBody() {
		t.Error("zero Step reads its body")
	}
}

func TestResponseTypeRules(t *testing.T) {
	get := `{"method":"GET","url":"https://example.com/"}`
	step := func(keys string) []byte { return stepDoc(get, keys) }
	const rt = sp0 + `.response_type: "binary" cannot be combined with `
	runTier2Cases(t, []tier2Case{
		{name: "text with readers", doc: step(`"response_type":"text","checks":[{"type":"json","selector":"$.a","exists":true}],` +
			`"extract":[{"variable_name":"t","selector":"h1","type":"html"}]`)},
		{name: "empty", doc: step(`"response_type":""`)},
		{name: "binary with raw readers", doc: step(`"response_type":"binary","checks":[{"type":"body","matches":"PNG"},` +
			`{"type":"body","size":"< 1mb"}],"extract":[{"variable_name":"r","selector":"(a)","type":"regex"}]`)},
		{name: "binary and discard_body", doc: step(`"response_type":"binary","discard_body":true`)},

		{name: "binary with text readers", doc: step(`"response_type":"binary","checks":[{"type":"html","selector":"h1","exists":true},` +
			`{"type":"json","selector":"$.a","missing":true}],"extract":[{"variable_name":"j","selector":"$.a","type":"json"},` +
			`{"variable_name":"t","selector":"h1","type":"html"}]`), want: []string{
			rt + "checks[0] (an html check); " + binaryText, rt + "checks[1] (a json check); " + binaryText,
			rt + "extract[0] (a json extractor); " + binaryText, rt + "extract[1] (an html extractor); " + binaryText,
		}, looser: true},
		{name: "binary and discard_body with a json check", doc: step(`"response_type":"binary","discard_body":true,` +
			`"checks":[{"type":"json","selector":"$.a","exists":true}]`), want: []string{
			sp0 + ".discard_body: cannot be combined with checks[0] (a json check), " + readsBody,
			rt + "checks[0] (a json check); " + binaryText,
		}, looser: true},
		{name: "upper case", doc: step(`"response_type":"BINARY"`), want: []string{sp0 + `.response_type: must be one of "text", "binary", got "BINARY"`}},
		{name: "null", doc: step(`"response_type":null`), want: []string{sp0 + ".response_type: must not be null"}},
	})
}

func TestTier2Timeouts(t *testing.T) {
	withHTTP := func(h string) []byte {
		return []byte(`{"schema_version":"2.0","name":"x","virtual_users":1,"http":` + h +
			`,"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://example.com/"}}]}]}`)
	}
	dur := func(v string) string { return `must be a duration such as "30s" or "1m30s", got ` + v }
	runTier2Cases(t, []tier2Case{
		{name: "every key", doc: withHTTP(`{"max_body_size":"512kib","discard_bodies":false,"connect_timeout":"2s",` +
			`"tls_handshake_timeout":"1500ms","response_header_timeout":"1m"}`)},
		{name: "zero values", doc: withHTTP(`{"max_body_size":"","connect_timeout":"","tls_handshake_timeout":"","response_header_timeout":""}`)},
		{name: "size forms", doc: withHTTP(`{"max_body_size":"1048576"}`)},
		{name: "size at the limit", doc: withHTTP(`{"max_body_size":"1gib"}`)},
		{name: "size at the limit in bytes", doc: withHTTP(`{"max_body_size":"1073741824b"}`)},
		{name: "request timeout", doc: bodyDoc(req(`"timeout":"750ms"`))},

		{name: "zero durations", doc: withHTTP(`{"connect_timeout":"0s","tls_handshake_timeout":"0","response_header_timeout":"-5ms"}`), want: []string{
			"http.connect_timeout: " + notPositive, "http.tls_handshake_timeout: " + notPositive, "http.response_header_timeout: " + notPositive,
		}, looser: true},
		{name: "bad durations", doc: withHTTP(`{"connect_timeout":"5","tls_handshake_timeout":"1d"}`), want: []string{
			"http.connect_timeout: " + dur(`"5"`), "http.tls_handshake_timeout: " + dur(`"1d"`),
		}, looser: true},
		{name: "size with an operator", doc: withHTTP(`{"max_body_size":"< 1mb"}`),
			want: []string{`http.max_body_size: invalid size "< 1mb": expected a number, got "<" (at offset 0)`}, looser: true},
		{name: "size with a time unit", doc: withHTTP(`{"max_body_size":"5s"}`),
			want: []string{`http.max_body_size: invalid size "5s": the value is a size; use b, kb, mb, gb, kib, mib or gib (or a plain number of bytes), not "s" (at offset 1)`}, looser: true},
		{name: "zero size", doc: withHTTP(`{"max_body_size":"0"}`),
			want: []string{"http.max_body_size: must be greater than zero; to drop every body, set discard_bodies: true"}, looser: true},
		{name: "size one byte over the limit", doc: withHTTP(`{"max_body_size":"1073741825b"}`),
			want: []string{"http.max_body_size: " + sizeLimit}, looser: true},
		{name: "size in gb over the limit", doc: withHTTP(`{"max_body_size":"2gb"}`),
			want: []string{"http.max_body_size: " + sizeLimit}, looser: true},
		{name: "size above int64", doc: withHTTP(`{"max_body_size":"18446744073709551615"}`),
			want: []string{"http.max_body_size: " + sizeLimit}, looser: true},
		{name: "size a number", doc: withHTTP(`{"max_body_size":1024}`), want: []string{"http.max_body_size: " + mustBeString}},
		{name: "request timeout negative", doc: bodyDoc(req(`"timeout":"-1s"`)), want: []string{rq + ".timeout: " + notPositive}, looser: true},
		{name: "request timeout null", doc: bodyDoc(req(`"timeout":null`)), want: []string{rq + ".timeout: must not be null"}},
	})
}

// TestHTTPTier2Values checks what Parse reads from the valid fixtures for
// the new keys.
func TestHTTPTier2Values(t *testing.T) {
	d := mustParse(t, readFile(t, "../testdata/v2/valid/http_tier2.json"))
	want := definition.HTTPOptions{MaxBodySize: "4mib", DiscardBodies: true, ConnectTimeout: "5s",
		TLSHandshakeTimeout: "5s", ResponseHeaderTimeout: "20s"}
	if d.HTTP == nil || *d.HTTP != want {
		t.Errorf("http = %+v, want %+v", d.HTTP, want)
	}
	steps := d.Scenarios[0].Steps
	home := steps[0]
	if home.DiscardBody == nil || !*home.DiscardBody || home.Request.Timeout != "2s" {
		t.Errorf("home step = %+v", home)
	}
	wantH := map[string]definition.HeaderValues{
		"Host": {"shop.example.com"}, "Accept": {"text/html", "application/xhtml+xml"},
		"User-Agent": {"volt-test-fixture/1.0"}, "X-Trace": {"a", "${uuid}", "a"},
	}
	if !reflect.DeepEqual(home.Request.Headers, wantH) {
		t.Errorf("headers = %q, want %q", home.Request.Headers, wantH)
	}
	if steps[1].ResponseType != definition.ResponseBinary || steps[2].ResponseType != definition.ResponseText {
		t.Errorf("response types = %q, %q", steps[1].ResponseType, steps[2].ResponseType)
	}

	d = mustParse(t, readFile(t, "../testdata/v2/valid/multipart.json"))
	up := d.Scenarios[0].Steps[0].Request
	if up.BodyKind() != definition.BodyTypeMultipart || len(up.Fields) != 2 || up.Fields[0].Name != "title" {
		t.Errorf("upload = %+v", up)
	}
	wantFiles := map[string]definition.FilePart{
		"report": {Path: "fixtures/report.pdf", Filename: "report-${vu_id}.pdf", ContentType: "application/pdf"},
		"avatar": {ContentBase64: "iVBORw0KGgo=", ContentType: "image/png"},
	}
	if !reflect.DeepEqual(up.Files, wantFiles) {
		t.Errorf("files = %+v, want %+v", up.Files, wantFiles)
	}
	png, err := base64.StdEncoding.DecodeString(up.Files["avatar"].ContentBase64)
	if err != nil || string(png) != "\x89PNG\r\n\x1a\n" {
		t.Errorf("avatar = %q, %v", png, err)
	}
	if raw := d.Scenarios[0].Steps[1].Request; raw.BodyKind() != definition.BodyTypeRaw || raw.BodyBase64 != "AAECAwQ=" {
		t.Errorf("raw step = %+v", raw)
	}
}

// FuzzHeaderValues checks that header values UnmarshalJSON accepts are
// never empty and marshal back to JSON that reads the same.
func FuzzHeaderValues(f *testing.F) {
	for _, s := range []string{`"a"`, `""`, `["a"]`, `["a","b"]`, `[]`, `[1]`, `null`, `["<&>","\ud800"]`, `{}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if !json.Valid(data) {
			return
		}
		var h definition.HeaderValues
		if err := json.Unmarshal(data, &h); err != nil || h == nil {
			return
		}
		if len(h) == 0 {
			t.Fatalf("empty HeaderValues from %s", data)
		}
		out, err := json.Marshal(h)
		if err != nil {
			t.Fatal(err)
		}
		var again definition.HeaderValues
		if err := json.Unmarshal(out, &again); err != nil || !reflect.DeepEqual(again, h) {
			t.Fatalf("round trip %s -> %s: %q, %v", data, out, again, err)
		}
	})
}
