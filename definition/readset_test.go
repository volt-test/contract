package definition_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

// The read sets of the v1 readers, declared the way a reader would.
var (
	headlessReads = definition.MustReadSet("description", "weights", "report_type", "region_config",
		"cloud", "api_key", "cloud_timeout", "existing_test_id", "skip_lookup", "target.idle_timeout")
	cloudReads = definition.MustReadSet("scenarios[].steps[].request.headers",
		"scenarios[].data_config.data_download_url")
)

func TestV1TypeProblemsIn(t *testing.T) {
	d := &definition.Definition{V1TypeProblems: []definition.Problem{
		{Path: "weights[1]", Message: "must be an integer", Count: 3},
		{Path: "scenarios[0].steps[1].request.headers.A", Message: "must be a string", Count: 1},
		{Path: "target.idle_timeout", Message: "must be a string", Count: 1},
		{Path: "weightsx", Message: "must be a string", Count: 1},
		{Path: `scenarios[12].data_config.data_download_url`, Message: "must be a string", Count: 1},
	}}
	cases := []struct {
		rs   definition.ReadSet
		want []string
	}{
		{definition.MustReadSet("weights"), []string{"weights[1]"}},
		{definition.MustReadSet("region_config[].weight"), nil},
		{definition.MustReadSet("scenarios[].steps[].request.headers"), []string{"scenarios[0].steps[1].request.headers.A"}},
		{definition.MustReadSet("scenarios[].steps[].request.header"), nil},
		{definition.MustReadSet("scenarios"), []string{"scenarios[0].steps[1].request.headers.A", "scenarios[12].data_config.data_download_url"}},
		{definition.MustReadSet("scenarios[].data_config"), []string{"scenarios[12].data_config.data_download_url"}},
		{definition.MustReadSet("target"), []string{"target.idle_timeout"}},
		{headlessReads, []string{"weights[1]", "target.idle_timeout"}},
		{cloudReads, []string{"scenarios[0].steps[1].request.headers.A", "scenarios[12].data_config.data_download_url"}},
		{definition.MustReadSet(), nil},
		{definition.ReadSet{}, nil},
	}
	for i, tc := range cases {
		var got []string
		for _, p := range d.V1TypeProblemsIn(tc.rs) {
			got = append(got, p.Path)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("case %d: V1TypeProblemsIn = %q, want %q", i, got, tc.want)
		}
	}
}

func TestNewReadSetRejectsBadPatterns(t *testing.T) {
	bad := map[string]string{
		"":                                      "empty field pattern",
		"weights[1]":                            "never a literal index",
		"scenarios[0].steps[].request.headers":  "never a literal index",
		"scenarios.steps[].request.headers":     "use [] for every array level",
		"scenarios[].steps.request":             "use [] for every array level",
		"scenarios[].steps[].request.headers.A": "stop at the map field",
		"scenarios[].steps[].request.header.X":  "stop at the map field",
		"weight":                                "unknown field",
		"weightsx":                              "unknown field",
		"target.idle":                           "unknown field",
		"target.base_url":                       "v2-only field",
		"name.first":                            "no fields",
		"weights[]":                             `end the pattern at the field name ("weights"); it already covers every element`,
		"region_config[]":                       `end the pattern at the field name ("region_config")`,
		"weights[][]":                           `end the pattern at the field name ("weights")`,
		"target[].url":                          "array levels",
		"weights.":                              `end the pattern at a field name, not a dot ("weights")`,
		".weights":                              "empty segment",
		"target..url":                           "empty segment",
	}
	for pattern, want := range bad {
		_, err := definition.NewReadSet("weights", pattern)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("NewReadSet(%q) error = %v, want it to mention %q", pattern, err, want)
			continue
		}
		if !strings.HasPrefix(err.Error(), "definition: read set: ") {
			t.Errorf("error %q lacks the prefix", err)
		}
		if errors.Is(err, definition.ErrInvalidDefinition) {
			t.Errorf("a bad read set is a caller bug, not an invalid definition: %v", err)
		}
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("MustReadSet(%q) did not panic", pattern)
				}
			}()
			definition.MustReadSet(pattern)
		}()
	}
	// Every loose field, and patterns that end early or with [], are valid.
	if _, err := definition.NewReadSet("description", "weights", "report_type", "region_config",
		"region_config[].weight", "cloud", "api_key", "cloud_timeout", "existing_test_id",
		"skip_lookup", "target", "target.idle_timeout", "scenarios",
		"scenarios[].steps[].request.headers", "scenarios[].data_config.data_download_url",
		"scenarios[].steps[].validate[].expected"); err != nil {
		t.Fatal(err)
	}
}

// TestReadSetCoversWholeField checks that a pattern catches a problem with
// the field itself as well as with its elements.
func TestReadSetCoversWholeField(t *testing.T) {
	for _, doc := range []string{`{"weights": "x"}`, `{"weights": [1, "x"]}`, `{"region_config": 5}`, `{"region_config": [{"weight": "1"}]}`} {
		d, err := definition.Parse([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		if len(d.V1TypeProblems) != 1 || len(d.V1TypeProblemsIn(headlessReads)) != 1 {
			t.Errorf("%s: V1TypeProblems %v, in headless read set %v", doc, d.V1TypeProblems, d.V1TypeProblemsIn(headlessReads))
		}
	}
}

func TestReadSetCopiesPatterns(t *testing.T) {
	patterns := []string{"weights"}
	rs := definition.MustReadSet(patterns...)
	patterns[0] = "cloud"
	d := &definition.Definition{V1TypeProblems: []definition.Problem{{Path: "weights[0]", Message: "m", Count: 1}}}
	if len(d.V1TypeProblemsIn(rs)) != 1 {
		t.Error("ReadSet shares the caller's slice")
	}
}
