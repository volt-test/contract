package definition_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

const (
	checkSelectorMsg   = " is not a supported JSON path: use $.key, $.key[0] or $.a.b[1].c, where a key before [N] uses only letters, digits, _ and - (no white space, wildcards, filters, bracket keys, negative or chained indexes)"
	extractSelectorMsg = " is not a supported JSON path: use $.key, $.key[0], $.a.b[1].c, $.items[*].id, $[0] or $[*], where a key before [N] or [*] uses only letters, digits, _ and - (no white space, filters, bracket keys, negative or chained indexes)"
)

// selectorDef is a hand-built 2.0 definition with one step holding a json
// extractor and a json check, both reading selector.
func selectorDef(selector string) *definition.Definition {
	v2 := definition.SchemaVersion{Major: 2}
	return &definition.Definition{SchemaVersion: &v2, Scenarios: []definition.Scenario{{Name: "s", Steps: []definition.Step{{
		Name:    "a",
		Extract: []definition.Extract{{VariableName: "v", Type: "json", Selector: selector}},
		Checks:  []definition.Check{{Type: definition.CheckJSON, Selector: selector}},
	}}}}}
}

// The selector grammar. A check takes $.key, key[N] and dotted paths of
// them; an extractor also takes key[*], $[N] and $[*].
func TestJSONSelectorGrammar(t *testing.T) {
	for _, tc := range []struct {
		selector       string
		check, extract bool
	}{
		// Accepted by both.
		{"$.data.items[0].id", true, true},
		{"data.items[0].id", true, true}, // the "$." prefix is optional
		{"a", true, true},
		{"$.a.b[1].c", true, true},
		{"$.data.items[00].id", true, true},
		{"$.a[2147483647]", true, true},
		{"$.top", true, true},
		{"$.data.k-1[1]", true, true},
		{"$.data.a:b", true, true}, // a key without [N] takes any character
		{"$.a@b", true, true},
		{"$.é", true, true},
		{"data.$", true, true},
		{"$.$", true, true},
		{"$a", true, true}, // the key "$a", not a prefix
		{"$.a\xff", true, true},

		// Extractors only.
		{"$.items[*].id", false, true},
		{"$.items[*]", false, true},
		{"$.a[*].b[*]", false, true},
		{"$.a[*].b[1].c", false, true},
		{"$[0]", false, true},
		{"$[00]", false, true},
		{"$[*]", false, true},
		{"$[*].id", false, true},
		{"$[0].a[*].b", false, true},

		// Refused by both.
		{"", false, false},
		{"$", false, false},
		{"$.", false, false},
		{"$..a", false, false},
		{"$.a.", false, false},
		{".a", false, false},
		{"a..b", false, false},
		{"$.a..b", false, false},
		{"*", false, false},
		{"$.*", false, false},
		{"$.a.*", false, false},
		{"$.*[0]", false, false},
		{"$.items[-1]", false, false},
		{"$.items[+1]", false, false},
		{"$.items['a']", false, false},
		{"$['a']", false, false},
		{"[0]", false, false},
		{"[*]", false, false},
		{"$.items[?(@.id)]", false, false},
		{"$.a[0][1]", false, false},
		{"$.a[*][*]", false, false},
		{"$.a[0]b", false, false},
		{"$.a[]", false, false},
		{"$.[0]", false, false},
		{"$.a.[*]", false, false},
		{"$.a]", false, false},
		{"$.a[1", false, false},
		{"$.a[", false, false},
		{"$.a[0]]", false, false},
		{"$.a[[0]", false, false},
		{"$.a.b:c[0]", false, false}, // a key before [N] is letters, digits, _ and - only
		{"$.a@b[0]", false, false},
		{"$a[0]", false, false},
		{"$.é[0]", false, false},
		{"$.a[0x1]", false, false},
		{"$.a[٣]", false, false},
		{"$.a[99999999999999999999]", false, false},
		{"$[0][1]", false, false},
		{"$[]", false, false},
		{"$[-1]", false, false},
		{"$[0", false, false},
		{"$[*]*", false, false},
		{" $.id", false, false},
		{"$.id ", false, false},
		{"$.a b", false, false},
		{"$.a\tb", false, false},
		{"$.id\n", false, false},
		{"$.a[ 0]", false, false},
		{"$.a b", false, false}, // no-break space
		{"$.a b", false, false}, // line separator
		{"$.a\u0085b", false, false},
	} {
		var want []definition.Problem
		if !tc.extract {
			want = append(want, definition.Problem{Path: "scenarios[0].steps[0].extract[0].selector", Message: strconv.Quote(tc.selector) + extractSelectorMsg})
		}
		if !tc.check {
			want = append(want, definition.Problem{Path: "scenarios[0].steps[0].checks[0].selector", Message: strconv.Quote(tc.selector) + checkSelectorMsg})
		}
		if got := definition.JSONSelectorProblems(selectorDef(tc.selector)); !reflect.DeepEqual(got, want) {
			t.Errorf("%q (check %v, extract %v):\n%s\nwant\n%s", tc.selector, tc.check, tc.extract, cloudProblemLines(got), cloudProblemLines(want))
		}
	}
}

// Only json extractors and json checks are scanned. Each step's extractors
// come before its checks, and steps and scenarios in order.
func TestJSONSelectorProblemsPaths(t *testing.T) {
	data := []byte(`{"schema_version": "2.0", "name": "t", "virtual_users": 1, "duration": "1s",
		"scenarios": [
			{"name": "s0", "steps": [
				{"name": "a", "request": {"method": "GET", "url": "https://x.test/a"},
					"checks": [
						{"type": "json", "selector": "$.ok", "exists": true},
						{"type": "json", "selector": "$.items[*].id", "exists": true},
						{"type": "header", "selector": "X..Y", "exists": true},
						{"type": "html", "selector": "a b", "exists": true}],
					"extract": [
						{"variable_name": "a", "selector": "$..a", "type": "json"},
						{"variable_name": "b", "selector": "$.items[*].id", "type": "json", "pick": "all"},
						{"variable_name": "c", "selector": "(.*)", "type": "regex"},
						{"variable_name": "d", "selector": "li a", "type": "html"}]},
				{"name": "b", "request": {"method": "GET", "url": "https://x.test/b"},
					"checks": [{"type": "json", "selector": " $.id", "is": "number"}]}]},
			{"name": "s1", "run_once": true, "steps": [
				{"name": "a", "request": {"method": "GET", "url": "https://x.test/c"},
					"extract": [{"variable_name": "e", "selector": "$[0][1]", "type": "json"}]}]}]}`)
	got := definition.JSONSelectorProblems(mustParse(t, data))
	want := []definition.Problem{
		{Path: "scenarios[0].steps[0].extract[0].selector", Message: `"$..a"` + extractSelectorMsg},
		{Path: "scenarios[0].steps[0].checks[1].selector", Message: `"$.items[*].id"` + checkSelectorMsg},
		{Path: "scenarios[0].steps[1].checks[0].selector", Message: `" $.id"` + checkSelectorMsg},
		{Path: "scenarios[1].steps[0].extract[0].selector", Message: `"$[0][1]"` + extractSelectorMsg},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JSONSelectorProblems =\n%s\nwant\n%s", cloudProblemLines(got), cloudProblemLines(want))
	}
}

// A long selector is echoed cut to 128 bytes on a rune boundary.
func TestJSONSelectorProblemsLongSelector(t *testing.T) {
	selector := "$.a" + strings.Repeat("é", 2000) + "[*]" // byte 128 is mid-rune
	got := definition.JSONSelectorProblems(selectorDef(selector))
	echo := strconv.Quote("$.a" + strings.Repeat("é", 62) + "...")
	if len(got) != 2 || got[0].Message != echo+extractSelectorMsg || got[1].Message != echo+checkSelectorMsg {
		t.Errorf("JSONSelectorProblems(long) =\n%s", cloudProblemLines(got))
	}
}

// v1 definitions keep their own JSON walker: nothing is reported.
func TestJSONSelectorProblemsV1(t *testing.T) {
	for _, version := range []string{``, `"schema_version": 1, `, `"schema_version": "1", `} {
		data := []byte(`{` + version + `"name": "t", "virtual_users": 1, "duration": "1s",
			"scenarios": [{"name": "s", "steps": [{"name": "a", "request": {"method": "GET", "url": "https://x.test/"},
				"extract": [{"variable_name": "a", "selector": "$..a", "type": "json"},
					{"variable_name": "b", "selector": "$.items[*].id", "type": "json"}]}]}]}`)
		d := mustParse(t, data)
		if !d.IsV1() {
			t.Fatalf("%q: not v1", version)
		}
		if got := definition.JSONSelectorProblems(d); got != nil {
			t.Errorf("%q: JSONSelectorProblems(v1) = %v, want nil", version, got)
		}
	}
	if got := definition.JSONSelectorProblems(nil); got != nil {
		t.Errorf("JSONSelectorProblems(nil) = %v, want nil", got)
	}
	if got := definition.JSONSelectorProblems(selectorDef("$.a")); got != nil {
		t.Errorf("JSONSelectorProblems(clean v2) = %v, want nil", got)
	}
}
