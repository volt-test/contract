package definition_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

// checkFixtureProblems maps each invalid fixture for checks, required
// extractors and expected_statuses to the one problem Parse reports, so
// every fixture fails for the rule it is named after. A long label is
// matched by prefix.
var checkFixtureProblems = map[string]string{
	"structure_check_missing_type.json":    check0 + `.type: is required (one of "status", "header", "body", "json", "html", "timing", "url", "cookie")`,
	"structure_check_bad_type.json":        check0 + `.type: must be one of "status", "header", "body", "json", "html", "timing", "url", "cookie", got "latency"`,
	"structure_check_bad_on_fail.json":     check0 + `.on_fail: must be one of "record", "fail", "abort", got "warn"`,
	"structure_check_bad_is.json":          check0 + `.is: must be one of "array", "object", "string", "number", "bool", "null", got "integer"`,
	"structure_check_status_code_99.json":  check0 + `.in[1]: must be an HTTP status code from 100 to 599, got 99`,
	"structure_check_status_code_600.json": check0 + `.not_in[0]: must be an HTTP status code from 100 to 599, got 600`,
	"structure_check_between_one.json":     check0 + `.between: must be [low, high], two status codes; got 1`,
	"structure_check_between_three.json":   check0 + `.between: must be [low, high], two status codes; got 3`,
	"structure_check_exists_false.json":    check0 + `.exists: must be true; leave it out otherwise`,
	"structure_check_missing_false.json":   check0 + `.missing: must be true; to check that the path is present, use exists: true`,
	"structure_check_empty_false.json":     check0 + `.empty: must be true; to check that the body is not empty, use size: "> 0"`,

	"structure_check_no_operator.json":             check0 + `: needs one operator for type status: in, not_in or between`,
	"structure_check_two_operators.json":           check0 + `: has more than one operator (in, between); write one check per operator`,
	"structure_check_wrong_operator.json":          check0 + `.equals: does not apply to type status (use in, not_in or between)`,
	"structure_check_missing_selector.json":        check0 + `.selector: is required for type json (a JSON path)`,
	"structure_check_blank_selector.json":          check0 + `.selector: is required for type header (the header name)`,
	"structure_check_selector_not_applicable.json": check0 + `.selector: does not apply to type status`,
	"structure_check_header_number_equals.json":    check0 + `.equals: must be a string for type header`,
	"structure_check_empty_contains.json":          check0 + `.contains: must not be empty: an empty string is in every value`,
	"structure_check_bad_regex.json":               check0 + `.matches: is not a valid regular expression (RE2 syntax): missing closing ): "(unclosed"`,
	"structure_check_bad_comparison.json":          check0 + `.duration: invalid comparison "< 5 parsecs": remove the space between the number and its unit (at offset 3)`,
	"structure_check_between_reversed.json":        check0 + `.between: must be [low, high] with low <= high, got [299,200]`,
	"structure_check_duplicate_label.json":         `scenarios[0].steps[0].checks[1]: the label "status in [200]" is already used by checks[0]; set a name that is unique in the step`,
	"structure_check_duplicate_name.json":          `scenarios[0].steps[0].checks[1].name: the label "ok" is already used by checks[0]; set a name that is unique in the step`,
	"structure_check_header_label_collision.json":  `scenarios[0].steps[0].checks[1]: the label "header Authorization equals" is already used by checks[0]; set a name that is unique in the step`,
	"structure_check_label_cut_collision.json":     `scenarios[0].steps[0].checks[1]: the label "body contains \"xxxxxxxx`,
	"structure_check_duplicate_required_extract.json": `scenarios[0].steps[0].extract[1].variable_name: the label "extract token" is already used by extract[0]; ` +
		`a step can have one required extractor per variable`,

	"structure_extract_on_fail_without_required.json": `scenarios[0].steps[0].extract[0].on_fail: only applies with required: true`,
	"structure_extract_on_fail_required_false.json":   `scenarios[0].steps[0].extract[0].on_fail: only applies with required: true`,
	"structure_extract_bad_on_fail.json":              `scenarios[0].steps[0].extract[0].on_fail: must be one of "record", "fail", "abort", got "skip"`,
	"structure_extract_bad_pick.json":                 `scenarios[0].steps[0].extract[0].pick: must be one of "random", "first", "last", "all", got "any"`,
	"structure_extract_pick_on_header.json":           `scenarios[0].steps[0].extract[1].pick: only applies to json, regex and html extractors`,
	"structure_expected_statuses_600.json":            `expected_statuses[1]: must be an HTTP status code from 100 to 599, got 600`,
	"type_check_equals_object.json":                   check0 + `.equals: must be a string, a number or true/false`,
	"type_check_equals_null.json":                     check0 + `.equals: must not be null`,
	"type_check_equals_out_of_range.json":             check0 + `.equals: is out of range`,
	"type_check_in_string.json":                       check0 + `.in: must be an array`,
	"type_check_exists_string.json":                   check0 + `.exists: must be true or false`,
	"type_check_duration_number.json":                 check0 + `.duration: must be a string`,
	"type_check_string.json":                          check0 + `: must be an object`,
	"type_extract_required_string.json":               `scenarios[0].steps[0].extract[0].required: must be true or false`,
	"type_expected_statuses_string.json":              `expected_statuses: must be an array`,
}

// TestCheckFixtures checks that each invalid fixture for checks fails with
// exactly the problem it is named after, and that every such fixture is
// listed.
func TestCheckFixtures(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures(t, "v2/invalid") {
		name := filepath.Base(f)
		want, ok := checkFixtureProblems[name]
		if !ok {
			related := strings.Contains(name, "_check") || strings.Contains(name, "on_fail") || strings.Contains(name, "pick") ||
				strings.Contains(name, "expected_statuses") || strings.Contains(name, "required")
			if related && !strings.HasPrefix(name, "unknown_") {
				t.Errorf("%s: no expected problem in checkFixtureProblems", name)
			}
			continue
		}
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			got := problemStrings(t, readFile(t, f))
			if len(got) != 1 || !strings.HasPrefix(got[0], want) {
				t.Errorf("problems = %q, want one starting with %q", got, want)
			}
		})
	}
	for name := range checkFixtureProblems {
		if !seen[name] {
			t.Errorf("missing fixture testdata/v2/invalid/%s", name)
		}
	}
}

// checkLabels returns the labels of every check and required extractor,
// by step.
func checkLabels(d *definition.Definition) [][]string {
	var out [][]string
	for _, sc := range d.Scenarios {
		for _, st := range sc.Steps {
			var labels []string
			for _, c := range st.Checks {
				labels = append(labels, c.Label())
			}
			for _, ex := range st.Extract {
				if ex.Required {
					labels = append(labels, ex.Label())
				}
			}
			out = append(out, labels)
		}
	}
	return out
}

// TestCheckLabelsStable checks that every label in checks.json is the same
// after Marshal and Parse: results key on labels, so a definition stored
// and read back must name its checks the same way.
func TestCheckLabelsStable(t *testing.T) {
	d1 := mustParse(t, readFile(t, "../testdata/v2/valid/checks.json"))
	b, err := json.Marshal(d1)
	if err != nil {
		t.Fatal(err)
	}
	d2 := mustParse(t, b)
	before, after := checkLabels(d1), checkLabels(d2)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("labels changed on Marshal and Parse:\nbefore %q\n after %q", before, after)
	}

	// The fixture exercises the labels most at risk.
	all := map[string]bool{}
	for _, step := range before {
		for _, l := range step {
			all[l] = true
		}
	}
	for _, want := range []string{
		"status in [200,201]",
		"status between [100,599]",
		"header Content-Type equals",
		"header X-Request-Id exists",
		`body equals "<p>Café & Bar > Shop</p>"`,
		"body contains \"Café \\u2028 <b>\"",
		`body equals ""`,
		"json $.data.items[0].id exists",
		"json $.data.items[0].id equals 42",
		"json $.data.items[0].sku equals 1234567890123456789",
		"json $.data.items[0].price equals 1.50",
		"json $.data.items[0].name equals \"Café <&> \\u2028\"",
		"json $.data.total equals -1e3",
		"json $.data.items is array",
		`json $.data.labels contains ""`,
		`html h1 equals "Sale"`,
		"timing duration < 500ms",
		"cookie session_id exists",
		"status <ok> & é \u2028",
		"vary on encoding",
		"extract first_id",
		"extract cursor",
	} {
		if !all[want] {
			t.Errorf("checks.json has no check labelled %q", want)
		}
	}
	if all["extract etag"] || all["extract page"] {
		t.Error("an optional extractor has a label")
	}
}
