package definition_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

const (
	notClosed    = `is not closed: no "}" balances its braces outside quoted text, or a quoted string runs into a line break or the end`
	hasCommentAt = ` has "//" or "/*" outside a quoted string; expressions cannot hold comments`
	foreachVar   = `is not a valid foreach variable: use ASCII letters, digits and "_", not starting with a digit`
	noComment    = `must not have "//" or "/*" outside a quoted string; expressions cannot hold comments`
	groupKey     = "does not apply to a step group, which takes only name, repeat and steps"
	nestedGroup  = "is not allowed here: a step group cannot contain another step group"
	inGroup      = "does not apply to a step inside a step group"
	setupLoops   = " scenarios do not take repeat, foreach or step groups yet"
	foreachIn    = `must be exactly one ${name} reference, such as "${ids}"`
	bareExpr     = "is an expression on its own; write it without ${...}"
	groupInner   = "scenarios[0].steps[0].steps[0]"
	unterminated = "${expr: ...} at byte 0 " + notClosed
)

// dynamicFixtureProblems maps each invalid fixture for dynamic steps to
// the problems Parse reports, in order.
var dynamicFixtureProblems = map[string][]string{
	"structure_dynamic_expr_unterminated.json": {
		"target.base_url: ${expr: ...} at byte 25 " + notClosed,
		"vars.v: ${expr: ...} at byte 3 " + notClosed,
		"scenarios[0].steps[0].request.url: ${expr: ...} at byte 25 " + notClosed,
		"scenarios[0].steps[0].request.fields.f: " + unterminated,
		"scenarios[0].steps[0].request.files.doc.filename: " + unterminated,
		"scenarios[0].steps[0].request.headers.X-A: " + unterminated,
		"scenarios[0].steps[0].request.headers.X-B[1]: " + unterminated,
		"scenarios[0].steps[0].cookies.c: " + unterminated,
		"scenarios[0].steps[1].steps[0].request.body_json.a[0]: " + unterminated,
	},
	"structure_dynamic_expr_empty.json": {"scenarios[0].steps[0].request.body: ${expr:} at byte 1 has no expression"},
	"structure_dynamic_group_nested.json": {
		"scenarios[0].steps[0].steps[0].steps: " + nestedGroup,
	},
	"structure_dynamic_group_with_request.json": {
		"scenarios[0].steps[0].request: " + groupKey,
		"scenarios[0].steps[0].think_time: " + groupKey,
	},
	"structure_dynamic_group_empty.json":     {"scenarios[0].steps[1].steps: must have at least one step"},
	"structure_dynamic_group_no_repeat.json": {"scenarios[0].steps[0].repeat: is required for a step group"},
	"structure_dynamic_group_leaf_loops.json": {
		"scenarios[0].steps[1].steps[0].repeat: " + inGroup + "; repeat the group",
		"scenarios[0].steps[1].steps[1].foreach: " + inGroup,
		"scenarios[0].steps[1].steps[2].once_per_vu: " + inGroup,
	},
	"structure_dynamic_group_in_run_once.json": {
		"scenarios[0].steps[0].steps: run_once" + setupLoops,
		"scenarios[0].steps[1].repeat: run_once" + setupLoops,
		"scenarios[1].steps[0].foreach: teardown" + setupLoops,
	},
	"structure_dynamic_group_duplicate_name.json": {
		`scenarios[0].steps[1].steps[0].name: "Status" is already used by scenarios[0].steps[0]`,
		`scenarios[0].steps[1].steps[1].name: "Loop" is already used by scenarios[0].steps[1]`,
	},
	"structure_dynamic_group_discard_body.json": {
		groupInner + ".discard_body: cannot be combined with checks[0] (a body check with contains), which reads the response body",
	},
	"structure_dynamic_repeat_and_foreach.json":  {"scenarios[0].steps[1].foreach: cannot be combined with repeat"},
	"structure_dynamic_skip_if_and_only_if.json": {"scenarios[0].steps[0].only_if: cannot be combined with skip_if"},
	"structure_dynamic_repeat_max.json":          {"scenarios[0].steps[0].repeat.max: must be from 1 to 1000, got 1001"},
	"structure_dynamic_foreach_in.json":          {"scenarios[0].steps[0].foreach.in: " + foreachIn},
	"structure_dynamic_assert_no_expr.json":      {"scenarios[0].steps[0].checks[0].expr: is required for type assert"},
	"structure_dynamic_discard_body_assert.json": {
		"scenarios[0].steps[0].discard_body: cannot be combined with checks[0] (an assert check), which reads the response body",
	},
	"type_dynamic_repeat_max_string.json": {"scenarios[0].steps[0].repeat.max: must be an integer"},
}

func TestDynamicFixtures(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures(t, "v2/invalid") {
		name := filepath.Base(f)
		want, ok := dynamicFixtureProblems[name]
		if !ok {
			if strings.HasPrefix(name, "structure_dynamic_") || strings.HasPrefix(name, "type_dynamic_") {
				t.Errorf("%s: no expected problem in dynamicFixtureProblems", name)
			}
			continue
		}
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			if got := problemStrings(t, readFile(t, f)); !reflect.DeepEqual(got, want) {
				t.Errorf("problems:\n got %q\nwant %q", got, want)
			}
		})
	}
	for name := range dynamicFixtureProblems {
		if !seen[name] {
			t.Errorf("missing fixture testdata/v2/invalid/%s", name)
		}
	}
}

// TestDynamicValidFixture checks what Parse reads from dynamic_steps.json
// and that LeafSteps flattens its group in document order.
func TestDynamicValidFixture(t *testing.T) {
	d := mustParse(t, readFile(t, "../testdata/v2/valid/dynamic_steps.json"))
	sc := d.Scenarios[1]
	var names []string
	for _, st := range sc.LeafSteps() {
		names = append(names, st.Name)
		if st.IsGroup() {
			t.Errorf("LeafSteps returned the group %s", st.Name)
		}
	}
	want := []string{"Products", "Product", "Add to cart", "Order status", "Order events", "Poll payment"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("LeafSteps = %q, want %q", names, want)
	}
	group := sc.Steps[3]
	if !group.IsGroup() || group.Repeat == nil || group.Repeat.Max != 50 || group.Repeat.Until != `order_status == "ready"` {
		t.Errorf("group = %+v", group)
	}
	if st := sc.Steps[1]; st.Foreach == nil || st.Foreach.Var != "id" || st.Foreach.In != "${ids}" || st.OnlyIf != "count > 0" {
		t.Errorf("foreach step = %+v", st)
	}
	if c := sc.Steps[0].Checks[0]; c.Type != definition.CheckAssert || c.Expr != "status == 200 && len(ids) > 0" ||
		c.Label() != "has products" || c.FailMode() != definition.FailAbort {
		t.Errorf("assert check = %+v", c)
	}
	if !sc.Steps[0].ReadsBody() {
		t.Error("a step with an assert check does not read its body")
	}
}

func TestLeafSteps(t *testing.T) {
	sc := definition.Scenario{Steps: []definition.Step{
		{Name: "a"},
		{Name: "g", Steps: []definition.Step{{Name: "b"}, {Name: "c"}}},
		{Name: "d", Steps: []definition.Step{}},
		{Name: "e"},
	}}
	leaves := sc.LeafSteps()
	var names []string
	for _, st := range leaves {
		names = append(names, st.Name)
	}
	if want := []string{"a", "b", "c", "d", "e"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("LeafSteps = %q, want %q", names, want)
	}
	// The pointers alias the definition, so callers can rewrite in place.
	leaves[2].Request.URL = "https://example.com/c"
	if sc.Steps[1].Steps[1].Request.URL != "https://example.com/c" {
		t.Error("a change through LeafSteps did not reach the definition")
	}
	if sc.Steps[2].IsGroup() || !sc.Steps[1].IsGroup() {
		t.Error("IsGroup: a step with empty steps is not a group; one with steps is")
	}
	if (definition.Scenario{}).LeafSteps() != nil {
		t.Error("LeafSteps of no steps is not nil")
	}
}

// TestStepMarshal: a group marshals without request, a leaf exactly as
// before (TestMarshalUnchanged pins the bytes), and Step.MarshalJSON
// leaves HTML escaping to the caller's encoder.
func TestStepMarshal(t *testing.T) {
	group := definition.Step{Name: "g", Repeat: &definition.Repeat{Until: "a < b", Max: 2},
		Steps: []definition.Step{{Name: "l", Request: definition.Request{Method: "GET", URL: "https://x/?a<b&c"}}}}
	got, err := json.Marshal(group)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"g","repeat":{"until":"a \u003c b","max":2},"steps":[{"name":"l","request":{"method":"GET","url":"https://x/?a\u003cb\u0026c"}}]}`
	if string(got) != want {
		t.Errorf("json.Marshal =\n%s\nwant\n%s", got, want)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(group); err != nil {
		t.Fatal(err)
	}
	// Request.MarshalJSON escapes on its own, as it did before.
	want = `{"name":"g","repeat":{"until":"a < b","max":2},"steps":[{"name":"l","request":{"method":"GET","url":"https://x/?a\u003cb\u0026c"}}]}`
	if got := strings.TrimSpace(buf.String()); got != want {
		t.Errorf("Encoder without HTML escaping =\n%s\nwant\n%s", got, want)
	}

	type plain definition.Step // no MarshalJSON: what encoding/json wrote before
	leaf := definition.Step{Name: "<b>", Request: definition.Request{Method: "GET", URL: "https://x/"}, ThinkTime: "1s"}
	for _, escape := range []bool{true, false} {
		var a, b bytes.Buffer
		ea, eb := json.NewEncoder(&a), json.NewEncoder(&b)
		ea.SetEscapeHTML(escape)
		eb.SetEscapeHTML(escape)
		if err := ea.Encode(leaf); err != nil {
			t.Fatal(err)
		}
		if err := eb.Encode(plain(leaf)); err != nil {
			t.Fatal(err)
		}
		if a.String() != b.String() {
			t.Errorf("escape %v: leaf marshals as\n%s\nnot as before\n%s", escape, a.String(), b.String())
		}
	}

	d := mustParse(t, readFile(t, "../testdata/v2/valid/dynamic_steps.json"))
	out := mustMarshal(t, d)
	if again := mustParse(t, out); !reflect.DeepEqual(again, d) {
		t.Errorf("round trip changed the definition:\n%s", out)
	}
	if bytes.Contains(out, []byte(`"name":"Wait for order","request"`)) {
		t.Errorf("group marshals a request: %s", out)
	}
}

// dynDoc is a 2.0 definition with one load scenario of the given steps.
func dynDoc(steps string) []byte {
	return []byte(`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"a","steps":[` + steps + `]}]}`)
}

const (
	get   = `"request":{"method":"GET","url":"https://example.com/"}`
	ids   = `{"name":"list",` + get + `,"extract":[{"variable_name":"ids","selector":"$.ids","type":"json","pick":"all"}]}`
	loop5 = `"repeat":{"until":"done","max":5}`
)

func TestDynamicRules(t *testing.T) {
	sp := "scenarios[0].steps[0]"
	sp1 := "scenarios[0].steps[1]"
	runTier2Cases(t, []tier2Case{
		{name: "skip_if", doc: dynDoc(`{"name":"b",` + get + `,"skip_if":"x == 1"}`)},
		{name: "only_if", doc: dynDoc(`{"name":"b",` + get + `,"only_if":"x == 1"}`)},
		{name: "skip_if empty next to only_if", doc: dynDoc(`{"name":"b",` + get + `,"skip_if":"","only_if":"x"}`)},
		{name: "repeat at the bounds", doc: dynDoc(`{"name":"b",` + get + `,"repeat":{"until":"x","max":1}},` +
			`{"name":"c",` + get + `,"repeat":{"until":"x","max":1000}}`)},
		{name: "foreach", doc: dynDoc(ids + `,{"name":"b",` + get + `,"foreach":{"var":"_id_1","in":"${ids}"}}`)},
		{name: "once_per_vu with repeat", doc: dynDoc(`{"name":"b",` + get + `,"once_per_vu":true,` + loop5 + `},{"name":"c",` + get + `}`)},
		{name: "group alone in a load scenario", doc: dynDoc(`{"name":"g",` + loop5 + `,"steps":[{"name":"b",` + get + `,"once_per_vu":true}]}`),
			want: []string{sp + ".steps[0].once_per_vu: " + inGroup}},
		{name: "assert", doc: dynDoc(`{"name":"b",` + get + `,"checks":[{"type":"assert","name":"n","expr":"status == 200","on_fail":"fail"}]}`)},

		{name: "blank expressions", doc: dynDoc(`{"name":"b",` + get + `,"skip_if":" ",` + `"checks":[{"type":"assert","name":"n","expr":" "}]},` +
			`{"name":"c",` + get + `,"only_if":"\t"}`),
			want: []string{sp + ".checks[0].expr: must not be blank", sp + ".skip_if: must not be blank",
				sp1 + ".only_if: must not be blank"}, looser: true},
		{name: "blank until", doc: dynDoc(`{"name":"b",` + get + `,"repeat":{"until":" ","max":2}}`),
			want: []string{sp + ".repeat.until: must not be blank"}},
		{name: "wrapped until", doc: dynDoc(`{"name":"b",` + get + `,"repeat":{"until":"${done}","max":2}}`),
			want: []string{sp + ".repeat.until: " + bareExpr}, looser: true},
		{name: "wrapped and NUL expressions", doc: dynDoc(`{"name":"b",` + get + `,"skip_if":" ${expr: x}","only_if":"",` +
			`"checks":[{"type":"assert","name":"n","expr":"a\u0000"}]}`),
			want: []string{sp + ".checks[0].expr: must not contain NUL", sp + ".skip_if: " + bareExpr}, looser: true},
		{name: "assert keys", doc: dynDoc(`{"name":"b",` + get + `,"checks":[{"type":"assert","expr":"x","selector":"$.a","in":[200]},` +
			`{"type":"status","in":[200],"expr":"x"}]}`),
			want: []string{
				sp + ".checks[0].name: is required for type assert",
				sp + ".checks[0].selector: does not apply to type assert",
				sp + ".checks[0].in: does not apply to type assert (use expr)",
				sp + ".checks[1].expr: does not apply to type status (use in, not_in or between)",
			}, looser: true},
		{name: "repeat without until or max", doc: dynDoc(`{"name":"b",` + get + `,"repeat":{"until":"","max":0}}`),
			want: []string{sp + ".repeat.until: is required", sp + ".repeat.max: is required (1 to 1000)"}},
		{name: "repeat max below 1", doc: dynDoc(`{"name":"b",` + get + `,"repeat":{"until":"x","max":-1}}`),
			want: []string{sp + ".repeat.max: must be from 1 to 1000, got -1"}},
		{name: "foreach var and in", doc: dynDoc(`{"name":"b",` + get + `,"foreach":{"var":"a.b","in":"${ids|first}"}},` +
			`{"name":"c",` + get + `,"foreach":{"var":"env","in":"x${ids}"}},` +
			`{"name":"d",` + get + `,"foreach":{"var":"","in":""}},` +
			`{"name":"e",` + get + `,"foreach":{"var":"id","in":"${env.IDS}"}},` +
			`{"name":"f",` + get + `,"foreach":{"var":"id","in":"${expr: ids}"}},` +
			`{"name":"g",` + get + `,"foreach":{"var":"fake","in":"${fake}"}}`),
			want: []string{
				sp + ".foreach.var: " + foreachVar,
				sp + ".foreach.in: " + foreachIn,
				sp1 + ".foreach.var: is reserved for the ${env.NAME} and ${fake.NAME} built-ins; use another name",
				sp1 + ".foreach.in: " + foreachIn,
				"scenarios[0].steps[2].foreach.var: is required",
				"scenarios[0].steps[2].foreach.in: is required",
				"scenarios[0].steps[3].foreach.in: " + foreachIn,
				"scenarios[0].steps[4].foreach.in: " + foreachIn,
				"scenarios[0].steps[5].foreach.var: is reserved for the ${env.NAME} and ${fake.NAME} built-ins; use another name",
				"scenarios[0].steps[5].foreach.in: " + foreachIn,
			}},
		{name: "foreach var reads itself", doc: dynDoc(`{"name":"b",` + get + `,"foreach":{"var":"ids","in":"${ids}"}}`),
			want: []string{sp + ".foreach.in: must not read the variable that var sets"}, looser: true},
		{name: "foreach var shadows an extracted variable", doc: dynDoc(ids + `,{"name":"b",` + get + `,"foreach":{"var":"ids","in":"${ids}"}}`),
			want: []string{sp1 + `.foreach.var: "ids" is extracted by a step of this scenario; use another name`,
				sp1 + ".foreach.in: must not read the variable that var sets"}, looser: true},
		{name: "foreach var shadows a var", doc: []byte(`{"schema_version":"2.0","name":"x","virtual_users":1,"vars":{"t":"1"},` +
			`"scenarios":[{"name":"a","vars":{"s":"1"},"steps":[` + ids + `,{"name":"b",` + get + `,"foreach":{"var":"t","in":"${ids}"}},` +
			`{"name":"c",` + get + `,"foreach":{"var":"s","in":"${ids}"}}]}]}`),
			want: []string{sp1 + `.foreach.var: "t" is a var of the test; use another name`,
				`scenarios[0].steps[2].foreach.var: "s" is a var of this scenario; use another name`}, looser: true},
		{name: "foreach var shadows a setup global", doc: []byte(`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[` +
			`{"name":"s","run_once":true,"steps":[{"name":"b",` + get + `,"extract":[{"variable_name":"g","selector":"$.g","type":"json"}]}]},` +
			`{"name":"a","steps":[` + ids + `,{"name":"b",` + get + `,"foreach":{"var":"g","in":"${ids}"}}]}]}`),
			want:   []string{`scenarios[1].steps[1].foreach.var: "g" is a global set by the run_once scenario scenarios[0]; use another name`},
			looser: true},

		{name: "group keys", doc: dynDoc(`{"name":"g",` + loop5 + `,"steps":[{"name":"b",` + get + `}],` + get + `,` +
			`"extract":[],"checks":[],"thresholds":[],"foreach":{"var":"a","in":"${b}"},"once_per_vu":false,"think_time":"",` +
			`"cookies":{},"clear_cookies":false,"skip_if":"","only_if":"","discard_body":false,"response_type":"",` +
			`"follow_redirects":true,"max_redirects":0}`),
			want: []string{
				sp + ".checks: " + groupKey, sp + ".clear_cookies: " + groupKey, sp + ".cookies: " + groupKey,
				sp + ".discard_body: " + groupKey, sp + ".extract: " + groupKey, sp + ".follow_redirects: " + groupKey,
				sp + ".foreach: " + groupKey, sp + ".max_redirects: " + groupKey, sp + ".once_per_vu: " + groupKey,
				sp + ".only_if: " + groupKey, sp + ".request: " + groupKey, sp + ".response_type: " + groupKey,
				sp + ".skip_if: " + groupKey, sp + ".think_time: " + groupKey, sp + ".thresholds: " + groupKey,
			}},
		{name: "null steps", doc: dynDoc(`{"name":"g",` + loop5 + `,"steps":null}`),
			want: []string{sp + ".steps: must not be null"}},
		{name: "steps next to a request", doc: dynDoc(`{"name":"b",` + get + `,"steps":[]}`),
			want: []string{sp + ".steps: must have at least one step"}},
		{name: "foreach var not an identifier", doc: dynDoc(ids + `,{"name":"b",` + get + `,"foreach":{"var":"id-1","in":"${ids}"}},` +
			`{"name":"c",` + get + `,"foreach":{"var":"1id","in":"${ids}"}}`),
			want: []string{sp1 + ".foreach.var: " + foreachVar, "scenarios[0].steps[2].foreach.var: " + foreachVar}},
		{name: "comment in a template expression", doc: dynDoc(`{"name":"b","request":{"method":"POST",` +
			`"url":"https://example.com/${expr: a // } + 1}","body":"${expr: \"//\" + x}"}}`),
			want: []string{sp + ".request.url: ${expr: ...} at byte 20" + hasCommentAt}, looser: true},
		{name: "comments in bare expressions", doc: dynDoc(`{"name":"b",` + get + `,"skip_if":"a // b",` +
			`"checks":[{"type":"assert","name":"n","expr":"x /* y */"}]},` +
			`{"name":"c",` + get + `,"only_if":"a /*","repeat":{"until":"b//","max":2}}`),
			want: []string{sp + ".checks[0].expr: " + noComment, sp + ".skip_if: " + noComment,
				sp1 + ".only_if: " + noComment, sp1 + ".repeat.until: " + noComment}, looser: true},
		{name: "slashes in literals and division", doc: dynDoc(`{"name":"b",` + get + `,"skip_if":"a / b > 1 && x == \"//\"",` +
			`"checks":[{"type":"assert","name":"n","expr":"body contains '/*' || url == ` + "`//`" + `"}]}`)},
	})
}

// TestDynamicWalkersRecurse proves that each rule reaches the steps inside
// a step group, at their own paths.
func TestDynamicWalkersRecurse(t *testing.T) {
	g := func(inner string) []byte {
		return dynDoc(`{"name":"g",` + loop5 + `,"steps":[{"name":"z",` + get + `},` + inner + `]}`)
	}
	at := "scenarios[0].steps[0].steps[1]"
	runTier2Cases(t, []tier2Case{
		{name: "method and url", doc: g(`{"name":"b","request":{"method":"","url":"/x"}}`),
			want: []string{at + ".request.method: is required", at + ".request.url: is relative, so target.base_url is required"}},
		{name: "body rules", doc: g(`{"name":"b","request":{"method":"POST","url":"https://x/","body_type":"json"}}`),
			want: []string{at + `.request.body_json: is required with body_type "json"`}},
		{name: "think_time", doc: g(`{"name":"b",` + get + `,"think_time":"2s-1s"}`),
			want: []string{at + `.think_time: must give the shorter duration first, got "2s-1s"`}, looser: true},
		{name: "cookies", doc: g(`{"name":"b",` + get + `,"cookies":{"a b":"x"}}`),
			want: []string{at + `.cookies["a b"]: is not a valid cookie name: use letters, digits and !#$%&'*+-.^_` + "`" +
				`|~, without spaces, "=", ";" or other separators`}},
		{name: "checks and labels", doc: g(`{"name":"b",` + get + `,"checks":[{"type":"status","in":[99]},{"type":"status","in":[99]}]}`),
			want: []string{at + ".checks[0].in[0]: must be an HTTP status code from 100 to 599, got 99",
				at + ".checks[1].in[0]: must be an HTTP status code from 100 to 599, got 99",
				at + `.checks[1]: the label "status in [99]" is already used by checks[0]; set a name that is unique in the step`}},
		{name: "redirects", doc: g(`{"name":"b",` + get + `,"follow_redirects":false,"max_redirects":2}`),
			want:   []string{at + ".max_redirects: has no effect: redirects are not followed for this step; set follow_redirects: true on the step"},
			looser: true},
		{name: "response_type", doc: g(`{"name":"b",` + get + `,"response_type":"binary","extract":[{"variable_name":"v","selector":"$.a","type":"json"}]}`),
			want:   []string{at + `.response_type: "binary" cannot be combined with extract[0] (a json extractor); json and html read the body as text`},
			looser: true},
		{name: "extract", doc: g(`{"name":"b",` + get + `,"extract":[{"variable_name":"v","selector":"$.a","type":"xml"}]}`),
			want: []string{at + `.extract[0].type: must be one of "json", "regex", "header", "cookie", "html", got "xml"`}},
		{name: "thresholds", doc: g(`{"name":"b",` + get + `,"thresholds":["iterations > 1"]}`),
			want: []string{at + ".thresholds[0]: iterations is not available on a step; use it on the test or a scenario"}, looser: true},
		{name: "clear_cookies with auto_handle_cookies false", doc: []byte(`{"schema_version":"2.0","name":"x","virtual_users":1,` +
			`"scenarios":[{"name":"a","auto_handle_cookies":false,"steps":[{"name":"g",` + loop5 + `,"steps":[{"name":"b",` + get +
			`,"clear_cookies":true}]}]}]}`),
			want: []string{"scenarios[0].steps[0].steps[0].clear_cookies: has no effect when auto_handle_cookies is false"}},
		{name: "a setup global extracted inside a group", doc: []byte(`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[` +
			`{"name":"s","run_once":true,"steps":[{"name":"b",` + get + `,"extract":[{"variable_name":"t","selector":"$.t","type":"json"}]}]},` +
			`{"name":"a","steps":[{"name":"g",` + loop5 + `,"steps":[{"name":"b",` + get +
			`,"extract":[{"variable_name":"t","selector":"$.t","type":"json"}]}]}]}]}`),
			want: []string{`scenarios[1].steps[0].steps[0].extract[0].variable_name: "t" is a global set by the run_once scenario scenarios[0]; ` +
				"no other scenario may extract it"}, looser: true},
		{name: "foreach var shadows a variable extracted inside a group", doc: dynDoc(`{"name":"g",` + loop5 + `,"steps":[` + ids + `]},` +
			`{"name":"b",` + get + `,"foreach":{"var":"ids","in":"${x}"}}`),
			want: []string{`scenarios[0].steps[1].foreach.var: "ids" is extracted by a step of this scenario; use another name`}, looser: true},
	})
}

// TestDynamicPublicWalkers checks CloudProblems, JSONSelectorProblems and
// Bounds on the steps inside a step group.
func TestDynamicPublicWalkers(t *testing.T) {
	d := mustParse(t, []byte(`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"a","steps":[`+
		`{"name":"first",`+get+`},`+
		`{"name":"g",`+loop5+`,"steps":[`+
		`{"name":"up","request":{"method":"POST","url":"https://x/${env.A}","body_type":"multipart","fields":{"f":"${expr: env.B}"},`+
		`"files":{"doc":{"path":"a.pdf","filename":"${env.C}"}}},"cookies":{"c":"${env.D}"},"thresholds":["p95 < 1s"],`+
		`"extract":[{"variable_name":"v","selector":"$.a b","type":"json"}]},`+
		`{"name":"raw","request":{"method":"PUT","url":"https://x/","body_base64":"AAAA"}}]}]}]}`))
	want := []definition.Problem{
		{Path: "scenarios[0].steps[1].steps[0].request.files.doc.path", Message: "is not available on VoltTest Cloud yet; run this test headless"},
		{Path: "scenarios[0].steps[1].steps[1].request.body_base64", Message: "is not available on VoltTest Cloud yet; run this test headless"},
		cloudEnv("scenarios[0].steps[1].steps[0].request.url", "${env.A}"),
		cloudEnv("scenarios[0].steps[1].steps[0].request.files.doc.filename", "${env.C}"),
		cloudEnv("scenarios[0].steps[1].steps[0].cookies.c", "${env.D}"),
	}
	if got := definition.CloudProblems(d, 1); !reflect.DeepEqual(got, want) {
		t.Errorf("CloudProblems =\n%s\nwant\n%s", cloudProblemLines(got), cloudProblemLines(want))
	}
	sel := definition.JSONSelectorProblems(d)
	if len(sel) != 1 || sel[0].Path != "scenarios[0].steps[1].steps[0].extract[0].selector" {
		t.Errorf("JSONSelectorProblems = %v", sel)
	}
	bounds, err := definition.Bounds(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(bounds) != 1 || bounds[0].Ref.Scenario != "a" || bounds[0].Ref.Step != "up" {
		t.Errorf("Bounds = %+v", bounds)
	}

	// A group built in Go with thresholds is refused by Bounds, as Parse
	// refuses it.
	d.Scenarios[0].Steps[1].Thresholds = []definition.ThresholdConfig{{Expr: "p95 < 1s"}}
	_, err = definition.Bounds(d)
	var ve *definition.ValidationError
	if !errors.As(err, &ve) || len(ve.Problems) != 1 || ve.Problems[0].String() != "scenarios[0].steps[1].thresholds: "+groupKey {
		t.Errorf("Bounds on a group with thresholds: %v", err)
	}
}

// oldForEachRef is the first-} reference scanner, the reference for
// templates without ${expr:.
func oldForEachRef(src string, fn func(open, end int)) {
	for i := 0; i < len(src); {
		open := strings.Index(src[i:], "${")
		if open < 0 {
			return
		}
		open += i
		end := strings.IndexByte(src[open+2:], '}')
		if end < 0 {
			return
		}
		end += open + 3
		fn(open, end)
		i = end
	}
}

func oldRefs(src string) []definition.TemplateRef {
	var out []definition.TemplateRef
	oldForEachRef(src, func(open, end int) { out = append(out, definition.TemplateRef{Start: open, End: end}) })
	return out
}

func TestScanTemplate(t *testing.T) {
	type ref = definition.TemplateRef
	nested := strings.Repeat("{", 1500) + strings.Repeat("}", 1500)
	for _, tc := range []struct {
		src     string
		want    []ref
		bodies  []string
		errAt   int // -1: no error
		empty   bool
		comment bool
	}{
		{src: "", errAt: -1},
		{src: "plain text", errAt: -1},
		{src: "${a}-${b|upper}", want: []ref{{0, 4, false}, {5, 15, false}}, bodies: []string{"a", "b|upper"}, errAt: -1},
		{src: `${expr: {"a":"}"} }`, want: []ref{{0, 19, true}}, bodies: []string{` {"a":"}"} `}, errAt: -1},
		{src: `${expr: "\"}" }`, want: []ref{{0, 15, true}}, bodies: []string{` "\"}" `}, errAt: -1},
		{src: `${expr: '}' }`, want: []ref{{0, 13, true}}, bodies: []string{` '}' `}, errAt: -1},
		{src: `${expr: '\'}' }`, want: []ref{{0, 15, true}}, bodies: []string{` '\'}' `}, errAt: -1},
		{src: `${expr: "\\" }x}`, want: []ref{{0, 14, true}}, bodies: []string{` "\\" `}, errAt: -1},
		{src: "${expr: `}` + `a\\` }", want: []ref{{0, 20, true}}, bodies: []string{" `}` + `a\\` "}, errAt: -1},
		{src: "${expr: `a``}` }", want: []ref{{0, 16, true}}, errAt: -1},
		{src: "${expr: `\n}` }", want: []ref{{0, 14, true}}, errAt: -1},
		{src: `${expr: x ? {"k": v} : {"value": random_int(0,1) == 1}}`,
			want: []ref{{0, 55, true}}, errAt: -1},
		{src: `${expr: qty * price} and ${n}`, want: []ref{{0, 20, true}, {25, 29, false}}, bodies: []string{" qty * price", "n"}, errAt: -1},
		{src: "${expr: x + ${y}}", want: []ref{{0, 17, true}}, bodies: []string{" x + ${y}"}, errAt: -1},
		{src: "${expr:" + nested + "}", want: []ref{{0, 3008, true}}, errAt: -1},
		{src: "${exprx}", want: []ref{{0, 8, false}}, bodies: []string{"exprx"}, errAt: -1},
		{src: "${expr}", want: []ref{{0, 7, false}}, errAt: -1},
		{src: "${ expr: {a} }", want: []ref{{0, 12, false}}, errAt: -1},
		{src: "${Expr: {a}}", want: []ref{{0, 11, false}}, errAt: -1},
		{src: "$${expr: 1}", want: []ref{{1, 11, true}}, errAt: -1},
		{src: "$${a}", want: []ref{{1, 5, false}}, errAt: -1},
		{src: "${a${expr: 1}}", want: []ref{{0, 13, false}}, errAt: -1},
		{src: "${a${env.B}}", want: []ref{{0, 11, false}}, errAt: -1},
		{src: "${unterminated", errAt: -1},

		{src: "${expr:}", errAt: 0, empty: true},
		{src: "a ${expr: \t}", errAt: 2, empty: true},
		{src: "${expr:", errAt: 0},
		{src: "${a} ${expr: x", want: []ref{{0, 4, false}}, errAt: 5},
		{src: `${expr: "unterminated }`, errAt: 0},
		{src: `${expr: 'a\'}`, errAt: 0},
		{src: `${expr: "a\`, errAt: 0},
		{src: "${expr: `}", errAt: 0},
		{src: "${expr: \"a\nb\" }", errAt: 0},
		{src: "${expr: {}", errAt: 0},
		{src: "${expr: \"a\\\nb\" }", errAt: 0},
		{src: "${expr: 'a\\\n' }", errAt: 0},
		{src: "${expr: a // }", errAt: 0, comment: true},
		{src: "${expr: a /* } */ + 1}", errAt: 0, comment: true},
		{src: "${expr:0//}", errAt: 0, comment: true},
		{src: "x ${expr: a /}", want: []ref{{2, 14, true}}, errAt: -1},
		{src: "${expr: \"//\" + '/*' + `//` + `/*`}", want: []ref{{0, 34, true}}, errAt: -1},
		{src: "${expr: a / b}", want: []ref{{0, 14, true}}, errAt: -1},
		{src: "${expr:" + nested, errAt: 0},
	} {
		got, err := definition.ScanTemplate(tc.src)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ScanTemplate(%q) = %v, want %v", tc.src, got, tc.want)
		}
		var te *definition.TemplateError
		switch {
		case tc.errAt < 0 && err != nil:
			t.Errorf("ScanTemplate(%q) error %v", tc.src, err)
		case tc.errAt >= 0 && (!errors.As(err, &te) || te.Offset != tc.errAt || te.Empty != tc.empty || te.Comment != tc.comment):
			t.Errorf("ScanTemplate(%q) error %#v, want offset %d, empty %v, comment %v", tc.src, err, tc.errAt, tc.empty, tc.comment)
		}
		for i, b := range tc.bodies {
			if body := got[i].Body(tc.src); body != b {
				t.Errorf("ScanTemplate(%q)[%d].Body = %q, want %q", tc.src, i, body, b)
			}
		}
	}
	if err := (&definition.TemplateError{Offset: 3}).Error(); err != "definition: ${expr: ...} at byte 3 "+notClosed {
		t.Errorf("Error() = %q", err)
	}
}

// TestScanTemplateLinear: deep nesting costs time linear in its size.
func TestScanTemplateLinear(t *testing.T) {
	n := 1 << 20
	src := "${expr:" + strings.Repeat("{", n) + strings.Repeat("}", n+1)
	refs, err := definition.ScanTemplate(src)
	if err != nil || len(refs) != 1 || refs[0].End != len(src) {
		t.Fatalf("refs %v, err %v", refs, err)
	}
	quotes := "${expr:" + strings.Repeat(`"}"`, n) + "}"
	if refs, err := definition.ScanTemplate(quotes); err != nil || len(refs) != 1 {
		t.Fatalf("refs %v, err %v", refs, err)
	}
}

// FuzzScanTemplate: no panic; references in order, inside src, not
// overlapping, each from "${" to "}"; identical to the old scanner when
// src has no "${expr:"; and every "${expr:" sits inside a returned
// reference, is where the error is, or follows a plain "${" that has no
// "}" after it.
func FuzzScanTemplate(f *testing.F) {
	for _, s := range []string{
		"", "${a}", `${expr: {"a":"}"} }`, `${expr: "\"}" }`, `${expr: '}' }`, "${expr: `}` }", "${expr:}",
		"${expr:", `${expr: "unterminated }`, "${exprx}", "$${expr: 1}", "${expr: x + ${y}}", "${a${env.B}}",
		"${expr: \"a\nb\"}", "x ${expr: {} } ${b} ${expr: '\\'' }", "${${expr:", "${a ${expr: x}", "${expr: a // }", "${expr: \"a\\\n\" }",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		refs, err := definition.ScanTemplate(src)
		last := 0
		for _, r := range refs {
			if r.Start < last || r.End <= r.Start || r.End > len(src) ||
				!strings.HasPrefix(src[r.Start:], "${") || src[r.End-1] != '}' {
				t.Fatalf("bad reference %v in %q (refs %v)", r, src, refs)
			}
			if r.Expr != strings.HasPrefix(src[r.Start:], "${expr:") {
				t.Fatalf("reference %v: Expr %v in %q", r, r.Expr, src)
			}
			last = r.End
		}
		if !strings.Contains(src, "${expr:") {
			if err != nil || !reflect.DeepEqual(refs, oldRefs(src)) {
				t.Fatalf("%q: refs %v, err %v; the old scanner gives %v", src, refs, err, oldRefs(src))
			}
			return
		}
		var te *definition.TemplateError
		if err != nil && (!errors.As(err, &te) || te.Offset < last || !strings.HasPrefix(src[te.Offset:], "${expr:")) {
			t.Fatalf("%q: error %#v after refs %v", src, err, refs)
		}
		for i := 0; ; {
			n := strings.Index(src[i:], "${expr:")
			if n < 0 {
				break
			}
			at := i + n
			i = at + 1
			if te != nil && at >= te.Offset {
				if at == te.Offset {
					break
				}
				t.Fatalf("%q: ${expr: at %d past the error at %d", src, at, te.Offset)
			}
			inside, lastEnd := false, 0
			for _, r := range refs {
				inside = inside || (r.Start <= at && at < r.End)
				if r.End <= at {
					lastEnd = r.End
				}
			}
			// The scan stops at a plain "${" with no "}" after it: the
			// rest is literal, an "${expr:" in it included.
			if p := strings.Index(src[lastEnd:at], "${"); !inside && p >= 0 && !strings.Contains(src[lastEnd+p+2:], "}") {
				break
			}
			if !inside {
				t.Fatalf("%q: ${expr: at %d is in no reference and is not the error (refs %v, err %v)", src, at, refs, err)
			}
		}
	})
}

// TestTemplateScanOldVsNew: a plain reference reads by the first-} rule,
// ${a${env.B}} included, and only ${expr: ...} bodies read differently.
func TestTemplateScanOldVsNew(t *testing.T) {
	for _, src := range []string{
		"${a${env.B}}", "${env.A}${env.B", "x${env.A|default:\"}\"}", "${expr} ${env.X}", "${exprx}${env.Y}",
	} {
		got, err := definition.ScanTemplate(src)
		if err != nil || !reflect.DeepEqual(got, oldRefs(src)) {
			t.Errorf("%q: %v, %v; old %v", src, got, err, oldRefs(src))
		}
	}
	// The first-} rule reads "${a${env.B}" as one reference, which is not
	// an env reference, so only ${env.C} is flagged.
	wantCloudProblems(t, cloudStepsDoc(`{}`, `{"name":"a","request":{"method":"GET","url":"https://x/${a${env.B}}${env.C}"}}`), 1,
		[]definition.Problem{cloudEnv("scenarios[0].steps[0].request.url", "${env.C}")})
	// An expression body is not scanned for ${env.*}.
	wantCloudProblems(t, cloudStepsDoc(`{}`, `{"name":"a","request":{"method":"GET",`+
		`"url":"https://x/${expr: {\"a\": 1} + ${env.B}}${env.C}"}}`), 1,
		[]definition.Problem{cloudEnv("scenarios[0].steps[0].request.url", "${env.C}")})
}
