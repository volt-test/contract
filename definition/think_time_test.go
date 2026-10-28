package definition_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/volt-test/contract/definition"
)

const thinkTimeNegative = "must not be negative"

func thinkTimeSyntax(s string) string {
	return `must be a duration such as "500ms" or a range such as "1s-3s", got "` + s + `"`
}

func TestParseThinkTime(t *testing.T) {
	ok := []struct {
		in       string
		min, max time.Duration
	}{
		{"", 0, 0},
		{"0", 0, 0},
		{"0s", 0, 0},
		{"500ms", 500 * time.Millisecond, 500 * time.Millisecond},
		{"1m30s", 90 * time.Second, 90 * time.Second},
		{"1s-3s", time.Second, 3 * time.Second},
		{"500ms-2s", 500 * time.Millisecond, 2 * time.Second},
		{"1m30s-2m", 90 * time.Second, 2 * time.Minute},
		{"0s-1s", 0, time.Second},
		{"0-3s", 0, 3 * time.Second},
		{"2s-2s", 2 * time.Second, 2 * time.Second},
		{"5ns-7ns", 5, 7},
		{"1.5s-2s", 1500 * time.Millisecond, 2 * time.Second},
	}
	for _, c := range ok {
		min, max, err := definition.ParseThinkTime(c.in)
		if err != nil || min != c.min || max != c.max {
			t.Errorf("ParseThinkTime(%q) = %v, %v, %v; want %v, %v, nil", c.in, min, max, err, c.min, c.max)
		}
	}

	bad := []struct{ in, msg string }{
		{"-1s", thinkTimeNegative},
		{"-1s-3s", thinkTimeNegative},
		{"-0s-1s", thinkTimeNegative},
		{"3s-1s", `must give the shorter duration first, got "3s-1s"`},
		{"1s - 3s", thinkTimeSyntax("1s - 3s")},
		{" 1s-3s", thinkTimeSyntax(" 1s-3s")},
		{"1s-3s ", thinkTimeSyntax("1s-3s ")},
		{"1-3s", thinkTimeSyntax("1-3s")},
		{"1s-3", thinkTimeSyntax("1s-3")},
		{"1s-", thinkTimeSyntax("1s-")},
		{"-", thinkTimeSyntax("-")},
		{"-1s-", thinkTimeSyntax("-1s-")},
		{"1s-3s-5s", thinkTimeSyntax("1s-3s-5s")},
		{"1s–3s", `must be a duration such as "500ms" or a range such as "1s-3s", got "1s–3s"`},
		{"1s--3s", thinkTimeSyntax("1s--3s")},
		{"1s-+3s", thinkTimeSyntax("1s-+3s")},
		{"+1s-3s", thinkTimeSyntax("+1s-3s")},
		{"1x", thinkTimeSyntax("1x")},
		{"s-3s", thinkTimeSyntax("s-3s")},
	}
	for _, c := range bad {
		min, max, err := definition.ParseThinkTime(c.in)
		if err == nil {
			t.Errorf("ParseThinkTime(%q) = %v, %v, nil; want error %q", c.in, min, max, c.msg)
			continue
		}
		if err.Error() != c.msg {
			t.Errorf("ParseThinkTime(%q) error = %q, want %q", c.in, err, c.msg)
		}
		if min != 0 || max != 0 {
			t.Errorf("ParseThinkTime(%q) = %v, %v with an error; want 0, 0", c.in, min, max)
		}
	}
}

// TestParseThinkTimeEchoBounded keeps a long value out of the message.
func TestParseThinkTimeEchoBounded(t *testing.T) {
	long := "1s-" + strings.Repeat("9", 4096)
	_, _, err := definition.ParseThinkTime(long)
	if err == nil {
		t.Fatal("no error")
	}
	if n := len(err.Error()); n > 300 {
		t.Errorf("message is %d bytes; the echo is not bounded", n)
	}
}

// thinkTimeFixtureProblems maps each invalid think_time fixture to the
// problems Parse reports, in order.
var thinkTimeFixtureProblems = map[string][]string{
	"structure_think_time_reversed.json":        {`scenarios[0].steps[0].think_time: must give the shorter duration first, got "3s-1s"`},
	"structure_think_time_whitespace.json":      {`scenarios[0].think_time: ` + thinkTimeSyntax("1s - 3s")},
	"structure_think_time_missing_unit.json":    {`scenarios[0].steps[0].think_time: ` + thinkTimeSyntax("1-3s")},
	"structure_think_time_negative_min.json":    {`scenarios[0].think_time: ` + thinkTimeNegative},
	"structure_think_time_trailing_hyphen.json": {`scenarios[0].steps[0].think_time: ` + thinkTimeSyntax("1s-")},
}

// TestThinkTimeFixtures checks that each invalid think_time fixture fails
// with exactly the problems listed for it, and that every such fixture is
// listed.
func TestThinkTimeFixtures(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures(t, "v2/invalid") {
		name := filepath.Base(f)
		want, ok := thinkTimeFixtureProblems[name]
		if !ok {
			if strings.HasPrefix(name, "structure_think_time_") {
				t.Errorf("%s: no expected problem in thinkTimeFixtureProblems", name)
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
	for name := range thinkTimeFixtureProblems {
		if !seen[name] {
			t.Errorf("missing fixture testdata/v2/invalid/%s", name)
		}
	}
}

// TestThinkTimeValidFixture checks that think_time_range.json keeps each
// value as written, at scenario and step level, and that each one parses.
func TestThinkTimeValidFixture(t *testing.T) {
	d := mustParse(t, readFile(t, "../testdata/v2/valid/think_time_range.json"))
	sc := d.Scenarios[0]
	got := []string{sc.ThinkTime}
	for _, st := range sc.Steps {
		got = append(got, st.ThinkTime)
	}
	want := []string{"1s-3s", "", "500ms-2s", "250ms", "0-1m30s"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("think_time = %q, want %q", got, want)
	}
	for _, s := range got {
		if _, _, err := definition.ParseThinkTime(s); err != nil {
			t.Errorf("ParseThinkTime(%q): %v", s, err)
		}
	}
}

// TestThinkTimeV2BothLevels reports a bad think_time at each level, and
// accepts a range at each level.
func TestThinkTimeV2BothLevels(t *testing.T) {
	got := problemStrings(t, v2doc(t, func(m map[string]any) {
		scenario0(m)["think_time"] = "2s-1s"
		step0(m)["think_time"] = "-1s"
	}))
	want := []string{
		`scenarios[0].think_time: must give the shorter duration first, got "2s-1s"`,
		`scenarios[0].steps[0].think_time: ` + thinkTimeNegative,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("problems = %q, want %q", got, want)
	}
	mustParse(t, v2doc(t, func(m map[string]any) {
		scenario0(m)["think_time"] = "0s-1s"
		step0(m)["think_time"] = "1s-3s"
	}))
}

// TestV1ThinkTimeNotValidated keeps v1 as it was: a range and a negative
// value parse without error and stay as written.
func TestV1ThinkTimeNotValidated(t *testing.T) {
	for _, c := range []struct{ scenario, step string }{
		{"1s-3s", "-1s"},
		{"-1s", "1s-3s"},
		{"3s-1s", "1s - 3s"},
	} {
		doc := `{"name": "v1", "virtual_users": 1, "duration": "10s", "scenarios": [{"name": "a", "think_time": "` +
			c.scenario + `", "steps": [{"name": "b", "think_time": "` + c.step +
			`", "request": {"method": "GET", "url": "https://shop.example.com/"}}]}]}`
		d, err := definition.Parse([]byte(doc))
		if err != nil {
			t.Errorf("%s: %v", doc, err)
			continue
		}
		if !d.IsV1() {
			t.Errorf("%s: not v1", doc)
		}
		sc := d.Scenarios[0]
		if sc.ThinkTime != c.scenario || sc.Steps[0].ThinkTime != c.step {
			t.Errorf("think_time = %q, %q; want %q, %q", sc.ThinkTime, sc.Steps[0].ThinkTime, c.scenario, c.step)
		}
	}
}
