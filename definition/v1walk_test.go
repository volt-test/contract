package definition

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// v1ReadSet pins how the v1 path treats every field, by path pattern:
//
//   - core: every v1 reader (headless CLI, cloud engine, backend) decodes
//     it, so a value of the wrong JSON type is an error.
//   - loose: some v1 reader ignores it, so a wrong type is recorded in
//     V1TypeProblems and the value is decoded as if absent.
//   - v2: the field does not exist in v1; the v1 path drops it unread.
//
// A field added to the types must be added here. If no v1 reader knows it,
// tag it contract:"v2".
var v1ReadSet = map[string]string{
	"name":              "core",
	"virtual_users":     "core",
	"duration":          "core",
	"ramp_up":           "core",
	"http_timeout":      "core",
	"http_debug":        "core",
	"target":            "core",
	"target.url":        "core",
	"stages":            "core",
	"stages[].duration": "core",
	"stages[].target":   "core",
	"scenarios":         "core",

	"scenarios[].name":                     "core",
	"scenarios[].description":              "core",
	"scenarios[].weight":                   "core",
	"scenarios[].think_time":               "core",
	"scenarios[].auto_handle_cookies":      "core",
	"scenarios[].steps":                    "core",
	"scenarios[].data_config":              "core",
	"scenarios[].data_config.data_source":  "core",
	"scenarios[].data_config.data_content": "core",
	"scenarios[].data_config.data_format":  "core",
	"scenarios[].data_config.has_header":   "core",
	"scenarios[].data_config.mode":         "core",

	"scenarios[].steps[].name":                    "core",
	"scenarios[].steps[].think_time":              "core",
	"scenarios[].steps[].request":                 "core",
	"scenarios[].steps[].request.method":          "core",
	"scenarios[].steps[].request.url":             "core",
	"scenarios[].steps[].request.body":            "core",
	"scenarios[].steps[].request.header":          "core",
	"scenarios[].steps[].extract":                 "core",
	"scenarios[].steps[].extract[].variable_name": "core",
	"scenarios[].steps[].extract[].selector":      "core",
	"scenarios[].steps[].extract[].attribute":     "core",
	"scenarios[].steps[].extract[].type":          "core",
	"scenarios[].steps[].validate":                "core",
	"scenarios[].steps[].validate[].name":         "core",
	"scenarios[].steps[].validate[].type":         "core",
	"scenarios[].steps[].validate[].selector":     "core",
	"scenarios[].steps[].validate[].expected":     "core",

	"description":                         "loose",
	"weights":                             "loose",
	"report_type":                         "loose",
	"region_config":                       "loose",
	"region_config[].region":              "loose",
	"region_config[].weight":              "loose",
	"cloud":                               "loose",
	"api_key":                             "loose",
	"cloud_timeout":                       "loose",
	"existing_test_id":                    "loose",
	"skip_lookup":                         "loose",
	"target.idle_timeout":                 "loose",
	"scenarios[].steps[].request.headers": "loose",
	"scenarios[].data_config.data_download_url": "loose",

	"target.base_url":                        "v2",
	"thresholds":                             "v2",
	"scenarios[].thresholds":                 "v2",
	"scenarios[].steps[].thresholds":         "v2",
	"scenarios[].steps[].checks":             "v2",
	"scenarios[].steps[].extract[].required": "v2",
	"scenarios[].steps[].extract[].on_fail":  "v2",
	"expected_statuses":                      "v2",
}

type classified struct {
	class string
	typ   reflect.Type
}

// classify lists every field reachable from t with its v1 class.
func classify(t reflect.Type, prefix string, loose bool, out map[string]classified) {
	fs := fieldsOf(derefType(t))
	for _, name := range fs.names {
		f := fs.byName[name]
		if f.typ == reflect.TypeOf(json.RawMessage{}) {
			continue // schema_version: taken from the exact-key peek
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		class := "core"
		switch {
		case f.v2Only:
			class = "v2"
		case loose || f.loose:
			class = "loose"
		}
		out[path] = classified{class: class, typ: f.typ}
		if f.v2Only {
			continue
		}
		et := derefType(f.typ)
		for et.Kind() == reflect.Slice || et.Kind() == reflect.Array {
			path += "[]"
			et = derefType(et.Elem())
		}
		if et.Kind() == reflect.Struct && !reflect.PointerTo(et).Implements(unmarshalerType) {
			classify(et, path, class == "loose", out)
		}
	}
}

func v1Classes() map[string]classified {
	out := map[string]classified{}
	classify(reflect.TypeOf(v1Wrapper{}), "", false, out)
	return out
}

// TestV1ReadSet pins the core, loose and v2-only fields. It fails when a
// field is added without deciding how v1 treats it.
func TestV1ReadSet(t *testing.T) {
	got := v1Classes()
	for path, c := range got {
		want, ok := v1ReadSet[path]
		if !ok {
			t.Errorf("field %s is not in v1ReadSet: add it as core/loose if a v1 reader decodes it, or tag it contract:\"v2\"", path)
			continue
		}
		if c.class != want {
			t.Errorf("field %s is %s, want %s", path, c.class, want)
		}
	}
	for path := range v1ReadSet {
		if _, ok := got[path]; !ok {
			t.Errorf("v1ReadSet lists %s, which is not a field", path)
		}
	}
}

// TestV2OnlyFieldsTagged checks that every field outside the v1 read set
// carries contract:"v2", so the v1 path never decodes it.
func TestV2OnlyFieldsTagged(t *testing.T) {
	for path, c := range v1Classes() {
		if v1ReadSet[path] == "v2" && c.class != "v2" {
			t.Errorf("%s exists only in v2 but is not tagged contract:\"v2\"", path)
		}
	}
}

// v1Sample builds a v1 document that sets every field the v1 path reads.
func v1Sample(t reflect.Type) any {
	t = derefType(t)
	switch t.Kind() {
	case reflect.Struct:
		m := map[string]any{}
		fs := fieldsOf(t)
		for name, f := range fs.byName {
			if f.v2Only || f.typ == reflect.TypeOf(json.RawMessage{}) {
				continue
			}
			m[name] = v1Sample(f.typ)
		}
		return m
	case reflect.Slice, reflect.Array:
		return []any{v1Sample(t.Elem())}
	case reflect.Map:
		return map[string]any{"k": v1Sample(t.Elem())}
	case reflect.String:
		return "s"
	case reflect.Bool:
		return true
	case reflect.Int:
		return 1
	}
	return "any"
}

// wrongValue returns a JSON value of the wrong kind for t, and the message
// the walker gives for it.
func wrongValue(t reflect.Type) (any, string) {
	t = derefType(t)
	switch t.Kind() {
	case reflect.String:
		return 5, "must be a string"
	case reflect.Int:
		return "7", "must be an integer"
	case reflect.Bool:
		return "yes", "must be true or false"
	case reflect.Slice:
		return "x", "must be an array"
	case reflect.Struct, reflect.Map:
		return "x", "must be an object"
	}
	return nil, ""
}

// edit applies fn to the parent object of a path pattern in doc, taking
// element 0 of every array.
func edit(t *testing.T, doc map[string]any, path string, fn func(parent map[string]any, key string)) {
	t.Helper()
	segs := strings.Split(path, ".")
	cur := doc
	for _, s := range segs[:len(segs)-1] {
		name := strings.TrimSuffix(s, "[]")
		v := cur[name]
		for strings.HasSuffix(s, "[]") {
			v = v.([]any)[0]
			s = strings.TrimSuffix(s, "[]")
		}
		cur = v.(map[string]any)
	}
	fn(cur, segs[len(segs)-1])
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func clone(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(marshal(t, doc), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestV1WrongTypeEveryField puts a value of the wrong JSON type in every
// field the v1 path reads. A core field fails Parse with the field's
// indexed path; a loose field parses, is decoded as if absent (the result equals the
// result without the key) and is recorded in V1TypeProblems.
func TestV1WrongTypeEveryField(t *testing.T) {
	base := v1Sample(reflect.TypeOf(v1Wrapper{})).(map[string]any)
	if _, err := Parse(marshal(t, base)); err != nil {
		t.Fatalf("sample does not parse: %v", err)
	}
	for path, c := range v1Classes() {
		if c.class == "v2" {
			continue
		}
		bad, msg := wrongValue(c.typ)
		if bad == nil {
			continue // any: every JSON value fits
		}
		t.Run(path, func(t *testing.T) {
			wantPath := strings.ReplaceAll(path, "[]", "[0]")
			doc := clone(t, base)
			edit(t, doc, path, func(p map[string]any, k string) { p[k] = bad })
			d, err := Parse(marshal(t, doc))

			if c.class == "core" {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("error = %v, want *ValidationError", err)
				}
				want := []Problem{{Path: wantPath, Message: msg}}
				if !reflect.DeepEqual(ve.Problems, want) {
					t.Errorf("Problems = %v, want %v", ve.Problems, want)
				}
				return
			}

			if err != nil {
				t.Fatalf("loose field: Parse: %v", err)
			}
			want := []Problem{{Path: wantPath, Message: msg, Count: 1}}
			if !reflect.DeepEqual(d.V1TypeProblems, want) {
				t.Errorf("V1TypeProblems = %v, want %v", d.V1TypeProblems, want)
			}
			without := clone(t, base)
			edit(t, without, path, func(p map[string]any, k string) { delete(p, k) })
			ref, err := Parse(marshal(t, without))
			if err != nil {
				t.Fatal(err)
			}
			d.V1TypeProblems = nil
			if !reflect.DeepEqual(d, ref) {
				t.Errorf("loose field with a wrong type is not left zero:\n got %+v\nwant %+v", d, ref)
			}
		})
	}
}

// TestV1RewriteMatchesDirectDecode checks that the v1 path decodes exactly
// what encoding/json decodes from the original bytes, for documents
// without type problems: dropping unknown keys and rewriting objects
// changes nothing.
func TestV1RewriteMatchesDirectDecode(t *testing.T) {
	var docs [][]byte
	err := filepath.Walk(filepath.Join("..", "testdata", "v1"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(p) == ".json" {
			b, rerr := os.ReadFile(p)
			docs = append(docs, b)
			return rerr
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	docs = append(docs,
		[]byte(`{"name": "a", "NAME": "b", "Virtual_Users": 3, "x": {"y": 1}, "scenarios": [{"Steps": [{"REQUEST": {"Header": {"A": "1"}, "header": {"B": "2"}}}]}]}`),
		[]byte(`{"name": "first", "name": "second", "scenarios": [null, {"name": "s"}], "target": null, "stages": [null]}`),
		[]byte(`{"weights": [1, 2], "Weights": null, "description": "d", "region_config": [{"Region": "eu", "WEIGHT": 2}]}`),
	)
	for i, doc := range docs {
		d, err := Parse(doc)
		if err != nil {
			t.Fatalf("doc %d: %v", i, err)
		}
		direct := &Definition{}
		w := v1Wrapper{Definition: direct}
		if err := json.Unmarshal(doc, &w); err != nil {
			t.Fatalf("doc %d: direct decode: %v", i, err)
		}
		direct.SchemaVersion = d.SchemaVersion
		direct.APIKey = NewSecret(w.APIKey)
		// The v1 path drops v2-only keys, which encoding/json reads.
		clearV2Only(reflect.ValueOf(direct).Elem())
		if !reflect.DeepEqual(d, direct) {
			t.Errorf("doc %d: Parse differs from encoding/json:\n got %+v\nwant %+v", i, d, direct)
		}
	}
}

func TestV1ProblemMessagesNameJSONKindsOnly(t *testing.T) {
	allowed := map[string]bool{
		"must be a string": true, "must be an integer": true, "must be true or false": true,
		"must be an array": true, "must be an object": true, "is out of range": true,
		"must be an integer without a decimal point or exponent": true,
	}
	doc := `{"virtual_users": 1.5, "stages": [{"target": 1e40}], "description": [], "weights": {"a": 1},
		"target": {"idle_timeout": {}}, "region_config": [5],
		"scenarios": [{"steps": [{"request": {"header": {"A": 1}, "headers": "x"}}]}]}`
	_, err := Parse([]byte(doc))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %v", err)
	}
	var paths []string
	for _, p := range ve.Problems {
		paths = append(paths, p.Path)
		if !allowed[p.Message] {
			t.Errorf("%s: message %q", p.Path, p.Message)
		}
	}
	sort.Strings(paths)
	want := []string{"scenarios[0].steps[0].request.header.A", "stages[0].target", "virtual_users"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("core problem paths = %q, want %q", paths, want)
	}
}

// The v1 readers decode into value structs: null for an object field is a
// no-op there, where a pointer field would be set to nil. mirrorDefinition
// is a value-shaped copy of the v1 keys (v2-only keys left out) for
// comparing Parse with encoding/json the way the readers use it.
type mirrorDefinition struct {
	Name           string           `json:"name"`
	Description    string           `json:"description"`
	VirtualUsers   int              `json:"virtual_users"`
	Duration       string           `json:"duration"`
	RampUp         string           `json:"ramp_up"`
	Stages         []Stage          `json:"stages"`
	HTTPTimeout    string           `json:"http_timeout"`
	HTTPDebug      bool             `json:"http_debug"`
	Target         mirrorTarget     `json:"target"`
	Scenarios      []mirrorScenario `json:"scenarios"`
	RegionConfig   []RegionWeight   `json:"region_config"`
	Weights        []int            `json:"weights"`
	ReportType     string           `json:"report_type"`
	Cloud          bool             `json:"cloud"`
	APIKey         string           `json:"api_key"`
	CloudTimeout   int              `json:"cloud_timeout"`
	ExistingTestID string           `json:"existing_test_id"`
	SkipLookup     bool             `json:"skip_lookup"`
}

type mirrorTarget struct {
	URL         string `json:"url"`
	IdleTimeout string `json:"idle_timeout"`
}

type mirrorScenario struct {
	Name              string           `json:"name"`
	Description       string           `json:"description"`
	Weight            int              `json:"weight"`
	ThinkTime         string           `json:"think_time"`
	AutoHandleCookies bool             `json:"auto_handle_cookies"`
	Steps             []mirrorStep     `json:"steps"`
	DataConfig        mirrorDataConfig `json:"data_config"`
}

type mirrorStep struct {
	Name      string            `json:"name"`
	Request   Request           `json:"request"`
	Extract   []mirrorExtract   `json:"extract"`
	Validate  []LegacyValidator `json:"validate"`
	ThinkTime string            `json:"think_time"`
}

type mirrorExtract struct {
	VariableName string `json:"variable_name"`
	Selector     string `json:"selector"`
	Attribute    string `json:"attribute"`
	Type         string `json:"type"`
}

type mirrorDataConfig struct {
	DataSource      string `json:"data_source"`
	DataContent     string `json:"data_content"`
	DataDownloadURL string `json:"data_download_url"`
	DataFormat      string `json:"data_format"`
	HasHeader       bool   `json:"has_header"`
	Mode            string `json:"mode"`
}

func toMirror(d *Definition) mirrorDefinition {
	m := mirrorDefinition{
		Name: d.Name, Description: d.Description, VirtualUsers: d.VirtualUsers, Duration: d.Duration,
		RampUp: d.RampUp, Stages: d.Stages, HTTPTimeout: d.HTTPTimeout, HTTPDebug: d.HTTPDebug,
		RegionConfig: d.RegionConfig, Weights: d.Weights, ReportType: d.ReportType, Cloud: d.Cloud,
		APIKey: d.APIKey.Reveal(), CloudTimeout: d.CloudTimeout, ExistingTestID: d.ExistingTestID,
		SkipLookup: d.SkipLookup,
	}
	if d.Target != nil {
		m.Target = mirrorTarget{URL: d.Target.URL, IdleTimeout: d.Target.IdleTimeout}
	}
	if d.Scenarios != nil {
		m.Scenarios = make([]mirrorScenario, len(d.Scenarios))
	}
	for i, sc := range d.Scenarios {
		ms := mirrorScenario{Name: sc.Name, Description: sc.Description, Weight: sc.Weight,
			ThinkTime: sc.ThinkTime, AutoHandleCookies: sc.AutoHandleCookies}
		if sc.Steps != nil {
			ms.Steps = make([]mirrorStep, len(sc.Steps))
		}
		for j, st := range sc.Steps {
			ms.Steps[j] = mirrorStep{Name: st.Name, Request: st.Request,
				Validate: st.Validate, ThinkTime: st.ThinkTime}
			if st.Extract != nil {
				ms.Steps[j].Extract = make([]mirrorExtract, len(st.Extract))
			}
			for k, ex := range st.Extract {
				ms.Steps[j].Extract[k] = mirrorExtract{VariableName: ex.VariableName, Selector: ex.Selector,
					Attribute: ex.Attribute, Type: ex.Type}
			}
		}
		if dc := sc.DataConfig; dc != nil {
			ms.DataConfig = mirrorDataConfig(*dc)
		}
		m.Scenarios[i] = ms
	}
	return m
}

// TestMirrorCoversV1Fields keeps mirrorDefinition in step with the types:
// it must have exactly the fields the v1 path reads.
func TestMirrorCoversV1Fields(t *testing.T) {
	want := map[string]bool{}
	for path, c := range v1Classes() {
		if c.class != "v2" {
			want[path] = true
		}
	}
	got := map[string]bool{}
	mirror := map[string]classified{}
	classify(reflect.TypeOf(mirrorDefinition{}), "", false, mirror)
	for path := range mirror {
		got[path] = true
	}
	if !reflect.DeepEqual(got, want) {
		for p := range want {
			if !got[p] {
				t.Errorf("mirror lacks %s", p)
			}
		}
		for p := range got {
			if !want[p] {
				t.Errorf("mirror has extra %s", p)
			}
		}
	}
}

// FuzzV1MatchesEncodingJSON checks the v1 path against encoding/json
// decoding into value structs, which is how the v1 readers decode: a core
// type problem fails both, a loose one fails encoding/json, and a v1
// definition without type problems decodes to the same values both ways.
func FuzzV1MatchesEncodingJSON(f *testing.F) {
	_ = filepath.Walk(filepath.Join("..", "testdata", "v1"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(p) == ".json" {
			if b, rerr := os.ReadFile(p); rerr == nil {
				f.Add(b)
			}
		}
		return nil
	})
	f.Add([]byte(`{"name": 1, "NAME": "x", "weights": ["a"], "target": {"base_url": 5}}`))
	f.Add([]byte(`{"target": {"url": "https://u"}, "Target": null, "scenarios": [{"data_config": {"mode": "random"}, "data_config": null}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		raw, err := decodeTop(data)
		if err != nil {
			return
		}
		if v, _, err := peekVersion(raw); err != nil || v.Major != 1 {
			return
		}
		d, perr := Parse(data)
		var mirror mirrorDefinition
		derr := json.Unmarshal(data, &mirror)

		var ve *ValidationError
		if errors.As(perr, &ve) {
			if derr == nil {
				t.Fatalf("Parse rejects what encoding/json accepts: %v\n%s", perr, data)
			}
			return
		}
		if perr != nil {
			t.Fatalf("unexpected Parse error: %v", perr)
		}
		if len(d.V1TypeProblems) > 0 {
			if derr == nil {
				t.Fatalf("Parse reports %v but encoding/json accepts\n%s", d.V1TypeProblems, data)
			}
			return
		}
		if derr != nil {
			t.Fatalf("encoding/json rejects what Parse accepts cleanly: %v\n%s", derr, data)
		}
		if got := toMirror(d); !reflect.DeepEqual(got, mirror) {
			t.Fatalf("Parse differs from encoding/json:\n got %+v\nwant %+v\n%s", got, mirror, data)
		}
	})
}

// clearV2Only zeroes every contract:"v2" field reachable from v.
func clearV2Only(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			clearV2Only(v.Elem())
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			clearV2Only(v.Index(i))
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			if !sf.IsExported() {
				continue
			}
			if contractFlags(sf.Tag)["v2"] {
				v.Field(i).SetZero()
				continue
			}
			clearV2Only(v.Field(i))
		}
	}
}

// withoutV2OnlyKeys returns doc without the keys v1ReadSet classifies as
// v2, matched case-insensitively as encoding/json matches them. It
// re-encodes doc, so use it only on documents without duplicate keys.
func withoutV2OnlyKeys(t *testing.T, doc []byte) []byte {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	var drop func(path string, v any)
	drop = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				p := strings.ToLower(k)
				if path != "" {
					p = path + "." + p
				}
				if v1ReadSet[p] == "v2" {
					delete(x, k)
					continue
				}
				drop(p, child)
			}
		case []any:
			for _, child := range x {
				drop(path+"[]", child)
			}
		}
	}
	drop("", v)
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestV1DropsCheckKeys checks that checks_ignored.json, a v1 definition
// carrying the v2 check keys (some with values v2 rejects), parses exactly
// as it does without them. TestV1IgnoresChecks covers keys of the wrong
// JSON type.
func TestV1DropsCheckKeys(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "v1", "cli", "checks_ignored.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	stripped := withoutV2OnlyKeys(t, raw)
	if bytes.Equal(stripped, raw) || bytes.Contains(stripped, []byte(`"checks"`)) ||
		bytes.Contains(stripped, []byte(`"required"`)) || bytes.Contains(stripped, []byte(`"expected_statuses"`)) {
		t.Fatalf("the fixture's v2 keys were not all removed: %s", stripped)
	}
	want, err := Parse(stripped)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsV1() || len(got.V1TypeProblems) != 0 {
		t.Errorf("IsV1 %v, V1TypeProblems %v", got.IsV1(), got.V1TypeProblems)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("v1 with check keys:\n got %+v\nwant %+v", got, want)
	}
	for _, sc := range got.Scenarios {
		for _, st := range sc.Steps {
			if st.Checks != nil {
				t.Errorf("step %s kept checks", st.Name)
			}
			for _, ex := range st.Extract {
				if ex.Required || ex.OnFail != "" {
					t.Errorf("extractor %s kept required/on_fail", ex.VariableName)
				}
			}
		}
	}
	if got.ExpectedStatuses != nil {
		t.Error("expected_statuses kept")
	}
}
