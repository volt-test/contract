package definition_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

// scenarioLoadFixtureProblems maps each invalid fixture for a scenario's
// own load, start_at, cookies, vars and data_format to the problems Parse
// reports, in order.
var scenarioLoadFixtureProblems = map[string][]string{
	"structure_pool_constant_zero_vus.json":    {`scenarios[1].load.virtual_users: is required (at least 1) for type constant`},
	"structure_pool_constant_no_vus.json":      {`scenarios[1].load.virtual_users: is required (at least 1) for type constant`},
	"structure_pool_stages_all_zero.json":      {`scenarios[1].load: ` + stagesZero},
	"structure_pool_constant_rate_keys.json":   {`scenarios[1].load.rate: does not apply to type constant`, `scenarios[1].load.preallocated_vus: does not apply to type constant`, `scenarios[1].load.max_vus: does not apply to type constant`},
	"structure_pool_stages_constant_keys.json": {`scenarios[1].load.duration: does not apply to type stages`, `scenarios[1].load.virtual_users: does not apply to type stages`},
	"structure_pool_rate_no_preallocated.json": {`scenarios[1].load.preallocated_vus: is required (at least 1)`},
	"structure_pool_type_unknown.json":         {`scenarios[1].load.type: must be one of "rate", "ramping_rate", "constant", "stages", got "burst"`},
	"structure_pool_weight.json":               {`scenarios[1].weight: does not apply to a scenario with its own load, which is never picked by weight`},
	"structure_pool_run_once.json":             {`scenarios[1].load: does not apply to a run_once scenario, which runs once`},
	"structure_pool_teardown.json":             {`scenarios[1].load: does not apply to a teardown scenario, which runs once`},
	"structure_pool_unused_top_level.json":     {`virtual_users: ` + unusedTopLoad, `duration: ` + unusedTopLoad},
	"structure_pool_top_level_missing.json":    {`virtual_users: is required (at least 1) unless stages or load are set`},
	"type_pool_string.json":                    {`scenarios[1].load: must be an object`},
	"type_pool_virtual_users_string.json":      {`scenarios[1].load.virtual_users: must be an integer`},

	"structure_start_at_without_load.json": {`scenarios[0].start_at: needs a load on this scenario`},
	"structure_start_at_zero.json":         {`scenarios[1].start_at: must be greater than zero; leave it out to start with the load`},
	"structure_start_at_bad.json":          {`scenarios[1].start_at: must be a duration such as "30s" or "1m30s", got "soon"`},
	"type_start_at_number.json":            {`scenarios[1].start_at: must be a string`},

	"structure_cookies_bad_name.json":           {`cookies["a b"]: ` + badCookieName},
	"structure_cookies_bad_value.json":          {`scenarios[0].cookies.session: ` + badCookieValue},
	"structure_cookies_step_bad_name.json":      {`scenarios[0].steps[0].cookies["="]: ` + badCookieName},
	"structure_cookies_step_line_break.json":    {`scenarios[0].steps[0].cookies.s: ` + badCookieBreak},
	"structure_cookies_value_space.json":        {`cookies.a: ` + badCookieValue},
	"structure_cookies_value_comma.json":        {`scenarios[0].cookies.a: ` + badCookieValue},
	"structure_cookies_value_quote.json":        {`scenarios[0].steps[0].cookies.a: ` + badCookieValue},
	"structure_cookies_value_backslash.json":    {`cookies.a: ` + badCookieValue},
	"structure_cookies_value_non_ascii.json":    {`scenarios[0].steps[0].cookies.a: ` + badCookieValue},
	"structure_clear_cookies_without_auto.json": {`scenarios[0].steps[0].clear_cookies: has no effect when auto_handle_cookies is false`},
	"type_cookies_number.json":                  {`cookies.s: must be a string`},
	"type_cookies_array.json":                   {`scenarios[0].cookies: must be an object`},
	"type_clear_cookies_string.json":            {`scenarios[0].steps[0].clear_cookies: must be true or false`},

	"structure_vars_bad_name.json":   {`vars["a.b"]: ` + badVarName},
	"structure_vars_empty_name.json": {`scenarios[0].vars[""]: ` + badVarName},
	"structure_vars_env.json":        {`vars.env: ` + reservedVar},
	"structure_vars_fake.json":       {`scenarios[0].vars.fake: ` + reservedVar},
	"type_vars_number.json":          {`vars.n: must be a string`},

	"structure_data_format_json_header.json": {`scenarios[0].data_config.has_header: only applies to data_format "csv"`},
	"structure_data_format_unknown.json":     {`scenarios[0].data_config.data_format: must be one of "csv", "json", got "xml"`},
}

// scenarioLoadFixturePrefixes are the fixture name prefixes this file owns.
var scenarioLoadFixturePrefixes = []string{"structure_pool_", "type_pool_", "structure_start_at_", "type_start_at_",
	"structure_cookies_", "type_cookies_", "structure_clear_cookies_", "type_clear_cookies_", "structure_vars_", "type_vars_", "structure_data_format_"}

const (
	stagesZero     = "never exceeds 0 virtual users, so the scenario would do nothing; set a stage target above 0"
	unusedTopLoad  = "applies to no scenario: every load scenario has its own load"
	badCookieName  = "is not a valid cookie name: use letters, digits and !#$%&'*+-.^_`|~, without spaces, \"=\", \";\" or other separators"
	badCookieValue = `must not contain a space, '"', ",", ";", "\", a control character or non-ASCII outside ${...} references`
	badCookieBreak = `must not contain a line break (CR or LF) or NUL`
	badVarName     = `is not a valid variable name: use ASCII letters, digits, "_" and "-"`
	reservedVar    = `is reserved for the ${env.NAME} and ${fake.NAME} built-ins; use another name`
)

// TestScenarioLoadFixtures checks that each invalid fixture of these
// families fails with exactly the problems listed for it, and that every
// such fixture is listed.
func TestScenarioLoadFixtures(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures(t, "v2/invalid") {
		name := filepath.Base(f)
		want, ok := scenarioLoadFixtureProblems[name]
		if !ok {
			for _, p := range scenarioLoadFixturePrefixes {
				if strings.HasPrefix(name, p) {
					t.Errorf("%s: no expected problem in scenarioLoadFixtureProblems", name)
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
	for name := range scenarioLoadFixtureProblems {
		if !seen[name] {
			t.Errorf("missing fixture testdata/v2/invalid/%s", name)
		}
	}
}

// poolDoc returns minimal.json with a second scenario, "Pool", that has
// the given load (when not nil) and the keys kv.
func poolDoc(t *testing.T, load map[string]any, edit func(m map[string]any), kv ...any) []byte {
	t.Helper()
	return v2doc(t, func(m map[string]any) {
		sc := map[string]any{"name": "Pool", "steps": []any{map[string]any{
			"name": "Cart", "request": map[string]any{"method": "GET", "url": "https://shop.example.com/cart"},
		}}}
		if load != nil {
			sc["load"] = load
		}
		for i := 0; i < len(kv); i += 2 {
			sc[kv[i].(string)] = kv[i+1]
		}
		m["scenarios"] = append(m["scenarios"].([]any), sc)
		if edit != nil {
			edit(m)
		}
	})
}

// TestScenarioLoadRules covers the rules for a scenario's own load and
// start_at that have no fixture of their own. Every definition Parse
// accepts must pass the schema too, and the schema rejects what Parse
// rejects except for duration forms.
func TestScenarioLoadRules(t *testing.T) {
	schema := compileSchema(t)
	constant := func(kv ...any) map[string]any {
		m := map[string]any{"type": "constant", "virtual_users": 3, "duration": "1m"}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	stage := func(d string, target int) map[string]any { return map[string]any{"duration": d, "target": target} }
	stages := func(s ...any) map[string]any { return map[string]any{"type": "stages", "stages": s} }
	noTop := func(m map[string]any) {
		delete(m, "virtual_users")
		delete(m, "duration")
		scenario0(m)["load"] = constant()
	}
	cases := []struct {
		name   string
		load   map[string]any
		edit   func(m map[string]any)
		kv     []any
		want   []string
		looser bool // durations are plain strings in the schema
	}{
		{"constant with one virtual user", constant("virtual_users", 1), nil, nil, nil, false},
		{"constant with zero values of the other keys", constant("rate", 0, "time_unit", "", "stages", []any{},
			"preallocated_vus", 0, "max_vus", 0), nil, nil, nil, false},
		{"stages to 1", stages(stage("1m", 0), stage("1m", 1)), nil, nil, nil, false},
		{"stages with zero values of the other keys", map[string]any{"type": "stages", "stages": []any{stage("1m", 2)},
			"rate": 0, "time_unit": "", "duration": "", "virtual_users": 0, "preallocated_vus": 0, "max_vus": 0}, nil, nil, nil, false},
		{"rate", map[string]any{"type": "rate", "rate": 5, "duration": "1m", "preallocated_vus": 1}, nil, nil, nil, false},
		{"ramping rate", map[string]any{"type": "ramping_rate", "stages": []any{stage("1m", 5)}, "preallocated_vus": 1}, nil, nil, nil, false},
		{"start_at", constant(), nil, []any{"start_at", "1m30s"}, nil, false},
		{"start_at empty reads as absent", nil, nil, []any{"start_at", ""}, nil, false},
		{"weight 0 reads as absent", constant(), nil, []any{"weight", 0}, nil, false},
		{"run_once false and teardown false", constant(), nil, []any{"run_once", false, "teardown", false}, nil, false},
		{"no test-level model when every load scenario has its own load", constant(), noTop, nil, nil, false},
		{"test-level zero values when every load scenario has its own load", constant(), func(m map[string]any) {
			noTop(m)
			m["virtual_users"] = 0
			m["duration"] = ""
			m["ramp_up"] = ""
			m["stages"] = []any{}
		}, nil, nil, false},
		{"no test-level model next to a setup scenario", constant(), func(m map[string]any) {
			noTop(m)
			m["scenarios"] = append(m["scenarios"].([]any), map[string]any{"name": "Setup", "run_once": true,
				"steps": []any{map[string]any{"name": "s", "request": map[string]any{"method": "GET", "url": "https://a.example.com/"}}}})
		}, nil, nil, false},

		{"constant without duration", map[string]any{"type": "constant", "virtual_users": 1}, nil, nil, []string{
			"scenarios[1].load.duration: is required for type constant"}, false},
		{"constant with zero duration", constant("duration", "0s"), nil, nil, []string{
			"scenarios[1].load.duration: must be greater than zero"}, true},
		{"constant with negative virtual users", constant("virtual_users", -1), nil, nil, []string{
			"scenarios[1].load.virtual_users: must not be negative"}, false},
		{"constant with time_unit and stages", constant("time_unit", "1s", "stages", []any{stage("1m", 1)}), nil, nil, []string{
			"scenarios[1].load.time_unit: does not apply to type constant",
			"scenarios[1].load.stages: does not apply to type constant"}, false},
		{"stages without stages", map[string]any{"type": "stages"}, nil, nil, []string{
			"scenarios[1].load.stages: is required for type stages (at least one stage)"}, false},
		{"stages with one zero target", stages(stage("1m", 0)), nil, nil, []string{"scenarios[1].load: " + stagesZero}, false},
		{"stages with an absent target", stages(map[string]any{"duration": "1m"}), nil, nil, []string{
			"scenarios[1].load: " + stagesZero}, false},
		{"stages with a negative target", stages(stage("1m", -1), stage("1m", 0)), nil, nil, []string{
			"scenarios[1].load.stages[0].target: must not be negative"}, false},
		{"stages without a stage duration", stages(map[string]any{"target": 1}), nil, nil, []string{
			"scenarios[1].load.stages[0].duration: is required"}, false},
		{"stages with time_unit, rate and max_vus", map[string]any{"type": "stages", "stages": []any{stage("1m", 1)},
			"time_unit": "1s", "rate": 1, "max_vus": 2}, nil, nil, []string{
			"scenarios[1].load.rate: does not apply to type stages",
			"scenarios[1].load.time_unit: does not apply to type stages",
			"scenarios[1].load.max_vus: does not apply to type stages"}, false},
		{"rate with virtual_users", map[string]any{"type": "rate", "rate": 5, "duration": "1m", "preallocated_vus": 1,
			"virtual_users": 3}, nil, nil, []string{"scenarios[1].load.virtual_users: does not apply to type rate"}, false},
		{"ramping rate all zero", map[string]any{"type": "ramping_rate", "stages": []any{stage("1m", 0)}, "preallocated_vus": 1},
			nil, nil, []string{"scenarios[1].load: " + rampingZero[len("load: "):]}, false},
		{"ramping rate with virtual_users", map[string]any{"type": "ramping_rate", "stages": []any{stage("1m", 1)},
			"preallocated_vus": 1, "virtual_users": 2}, nil, nil, []string{
			"scenarios[1].load.virtual_users: does not apply to type ramping_rate"}, false},
		{"empty load", map[string]any{}, nil, nil, []string{
			`scenarios[1].load.type: is required (one of "rate", "ramping_rate", "constant", "stages")`}, false},
		{"negative start_at", constant(), nil, []any{"start_at", "-1s"}, []string{
			"scenarios[1].start_at: must be greater than zero; leave it out to start with the load"}, true},
		{"start_at on a setup scenario", nil, nil, []any{"start_at", "1s", "run_once", true}, []string{
			"scenarios[1].start_at: needs a load on this scenario"}, false},
		{"load on a setup scenario with a weight", constant(), nil, []any{"run_once", true, "weight", 1}, []string{
			"scenarios[1].weight: does not apply to a run_once scenario, which is never picked for the load",
			"scenarios[1].load: does not apply to a run_once scenario, which runs once"}, false},
		{"test-level stages when every load scenario has its own load", constant(), func(m map[string]any) {
			noTop(m)
			m["stages"] = []any{stage("1m", 1)}
		}, nil, []string{"stages: " + unusedTopLoad}, false},
		{"test-level load when every load scenario has its own load", constant(), func(m map[string]any) {
			noTop(m)
			m["load"] = map[string]any{"type": "rate", "rate": 1, "duration": "1m", "preallocated_vus": 1}
		}, nil, []string{"load: " + unusedTopLoad}, false},
		{"test-level ramp_up when every load scenario has its own load", constant(), func(m map[string]any) {
			noTop(m)
			m["ramp_up"] = "10s"
		}, nil, []string{"ramp_up: " + unusedTopLoad}, false},
		{"no load scenario at all", constant(), func(m map[string]any) {
			noTop(m)
			for _, sc := range m["scenarios"].([]any) {
				delete(sc.(map[string]any), "load")
				sc.(map[string]any)["teardown"] = true
			}
		}, nil, []string{
			"virtual_users: is required (at least 1) unless stages or load are set",
			"scenarios: must have at least one load scenario, one without run_once or teardown"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := poolDoc(t, tc.load, tc.edit, tc.kv...)
			if got := problemStrings(t, doc); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("problems = %q, want %q", got, tc.want)
			}
			if tc.want == nil {
				requireSchemaAccepts(t, schema, doc)
			} else if err := validateAgainstSchema(t, schema, doc); err == nil && !tc.looser {
				t.Error("schema accepts a definition Parse rejects")
			}
		})
	}
}

// TestCookieVarRules covers the cookie and variable name and value rules
// that have no fixture of their own, at every level.
func TestCookieVarRules(t *testing.T) {
	schema := compileSchema(t)
	at := func(level, key string, v map[string]any) func(m map[string]any) {
		return func(m map[string]any) {
			switch level {
			case "test":
				m[key] = v
			case "scenario":
				scenario0(m)[key] = v
			case "step":
				step0(m)[key] = v
			}
		}
	}
	cases := []struct {
		name string
		edit func(m map[string]any)
		want []string
	}{
		{"every token character", at("test", "cookies", map[string]any{"!#$%&'*+-.^_`|~09azAZ": "v"}), nil},
		{"empty value", at("scenario", "cookies", map[string]any{"a": ""}), nil},
		{"value with every cookie-octet", at("step", "cookies",
			map[string]any{"a": "!#$%&'()*+-./09:<=>?@AZ[]^_`az{|}~"}), nil},
		{"spaces, commas, quotes and non-ASCII inside references", at("step", "cookies",
			map[string]any{"a": `x${token | default:"a b, \ü"}y${b;}`}), nil},
		{"unterminated reference is literal", at("step", "cookies", map[string]any{"a": "x${y"}), nil},
		{"empty cookies", at("step", "cookies", map[string]any{}), nil},
		{"var names", at("test", "vars", map[string]any{"a": "1", "_": "", "A-1": "${uuid}", "9": "x"}), nil},
		{"empty vars", at("scenario", "vars", map[string]any{}), nil},
		{"clear_cookies with auto_handle_cookies absent", func(m map[string]any) { step0(m)["clear_cookies"] = true }, nil},
		{"clear_cookies false with auto_handle_cookies false", func(m map[string]any) {
			scenario0(m)["auto_handle_cookies"] = false
			step0(m)["clear_cookies"] = false
		}, nil},
		{"var value with any text", at("scenario", "vars", map[string]any{"a": "x;\r\n\x00y"}), nil},

		{"empty cookie name", at("test", "cookies", map[string]any{"": "v"}), []string{`cookies[""]: ` + badCookieName}},
		{"templated cookie name", at("scenario", "cookies", map[string]any{"${name}": "v"}), []string{
			`scenarios[0].cookies["${name}"]: ` + badCookieName}},
		{"cookie name with a semicolon", at("step", "cookies", map[string]any{"a;b": "v"}), []string{
			`scenarios[0].steps[0].cookies["a;b"]: ` + badCookieName}},
		{"cookie name with non-ASCII", at("test", "cookies", map[string]any{"ü": "v"}), []string{
			`cookies["ü"]: ` + badCookieName}},
		{"cookie value with NUL", at("test", "cookies", map[string]any{"a": "x\x00"}), []string{"cookies.a: " + badCookieBreak}},
		{"cookie value with LF", at("scenario", "cookies", map[string]any{"a": "x\ny"}), []string{
			"scenarios[0].cookies.a: " + badCookieBreak}},
		{"cookie value with CR inside a reference", at("step", "cookies", map[string]any{"a": "${x\r}"}), []string{
			"scenarios[0].steps[0].cookies.a: " + badCookieBreak}},
		{"cookie value with a tab", at("test", "cookies", map[string]any{"a": "x\ty"}), []string{"cookies.a: " + badCookieValue}},
		{"cookie value with DEL", at("test", "cookies", map[string]any{"a": "x\x7f"}), []string{"cookies.a: " + badCookieValue}},
		{"space after a reference", at("test", "cookies", map[string]any{"a": "${a} b"}), []string{"cookies.a: " + badCookieValue}},
		{"space in an unterminated reference", at("test", "cookies", map[string]any{"a": "${a b"}), []string{"cookies.a: " + badCookieValue}},
		{"bad name and value, sorted", at("test", "cookies", map[string]any{"z": "a;", "a b": "c\r"}), []string{
			`cookies["a b"]: ` + badCookieName, `cookies["a b"]: ` + badCookieBreak, "cookies.z: " + badCookieValue}},
		{"var name with a space", at("scenario", "vars", map[string]any{"a b": "1"}), []string{
			`scenarios[0].vars["a b"]: ` + badVarName}},
		{"var name with a filter", at("test", "vars", map[string]any{"a|upper": "1"}), []string{`vars["a|upper"]: ` + badVarName}},
		{"var name with braces", at("test", "vars", map[string]any{"${a}": "1"}), []string{`vars["${a}"]: ` + badVarName}},
		{"env var name", at("test", "vars", map[string]any{"env.HOME": "1"}), []string{`vars["env.HOME"]: ` + badVarName}},
		{"names that only start like a reserved one", at("scenario", "vars", map[string]any{"envs": "1", "fake_user": "2", "ENV": "3"}), nil},
		{"var named like a built-in or an extracted value", at("test", "vars", map[string]any{"uuid": "x", "token": "default"}), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := v2doc(t, tc.edit)
			if got := problemStrings(t, doc); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("problems = %q, want %q", got, tc.want)
			}
			if tc.want == nil {
				requireSchemaAccepts(t, schema, doc)
			} else if err := validateAgainstSchema(t, schema, doc); err == nil {
				t.Error("schema accepts a definition Parse rejects")
			}
		})
	}
}

// TestScenarioLoadRoundTrip checks the decoded values of the scenario
// load and cookie fixtures.
func TestScenarioLoadRoundTrip(t *testing.T) {
	d := mustParse(t, readFile(t, "../testdata/v2/valid/scenario_load.json"))
	if d.VirtualUsers != 20 || d.Load != nil {
		t.Fatalf("test-level load model = %d virtual users, load %+v", d.VirtualUsers, d.Load)
	}
	checkout := d.Scenarios[2]
	if want := (definition.Load{Type: definition.LoadConstant, VirtualUsers: 5, Duration: "2m"}); checkout.Load == nil ||
		!reflect.DeepEqual(*checkout.Load, want) || checkout.StartAt != "30s" || !checkout.IsLoad() {
		t.Errorf("Checkout = load %+v, start_at %q", checkout.Load, checkout.StartAt)
	}
	spike := d.Scenarios[3].Load
	if spike == nil || spike.Type != definition.LoadStages || len(spike.Stages) != 3 || spike.PreallocatedVUs != 0 {
		t.Errorf("Spike load = %+v", spike)
	}
	// A pool marshals without preallocated_vus.
	b, err := json.Marshal(checkout.Load)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"constant","duration":"2m","virtual_users":5}`; string(b) != want {
		t.Errorf("Marshal(load) = %s, want %s", b, want)
	}

	only := mustParse(t, readFile(t, "../testdata/v2/valid/scenario_load_only.json"))
	if only.VirtualUsers != 0 || only.Duration != "" || only.Stages != nil || only.Load != nil {
		t.Errorf("test-level load model set: %+v", only)
	}

	c := mustParse(t, readFile(t, "../testdata/v2/valid/cookies_vars.json"))
	if want := map[string]string{"locale": "en-GB", "consent": "all", "ab_test": "${variant}"}; !reflect.DeepEqual(c.Cookies, want) {
		t.Errorf("cookies = %v", c.Cookies)
	}
	if want := map[string]string{"variant": "B", "currency": "EUR", "api-version": "2"}; !reflect.DeepEqual(c.Vars, want) {
		t.Errorf("vars = %v", c.Vars)
	}
	sc := c.Scenarios[0]
	if sc.AutoHandleCookies == nil || !*sc.AutoHandleCookies || sc.Cookies["__Host-cart"] != "" || sc.Vars["currency"] != "CHF" {
		t.Errorf("scenario = %+v", sc)
	}
	if sc.Steps[0].Cookies["session"] != "${session_id}" || !sc.Steps[1].ClearCookies || sc.Steps[2].ClearCookies {
		t.Errorf("steps = %+v", sc.Steps)
	}
	if sc.DataConfig.DataFormat != "json" || c.Scenarios[1].AutoHandleCookies == nil || *c.Scenarios[1].AutoHandleCookies {
		t.Errorf("data_config %+v, auto_handle_cookies %v", sc.DataConfig, c.Scenarios[1].AutoHandleCookies)
	}
}

// TestAutoHandleCookiesExplicit checks that auto_handle_cookies keeps an
// explicit false apart from an absent key through Parse and Marshal, in
// v1 and v2.
func TestAutoHandleCookiesExplicit(t *testing.T) {
	for _, version := range []string{`"schema_version": "2.0", `, ""} {
		for _, tc := range []struct {
			key  string
			want *bool
		}{
			{"", nil},
			{`"auto_handle_cookies": false, `, new(bool)},
			{`"auto_handle_cookies": true, `, func() *bool { b := true; return &b }()},
		} {
			doc := `{` + version + `"name": "c", "virtual_users": 1, "scenarios": [{"name": "s", ` + tc.key +
				`"steps": [{"name": "a", "request": {"method": "GET", "url": "https://a.example.com/"}}]}]}`
			d := mustParse(t, []byte(doc))
			got := d.Scenarios[0].AutoHandleCookies
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s: AutoHandleCookies = %v, want %v", doc, got, tc.want)
			}
			b, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(mustParse(t, b).Scenarios[0].AutoHandleCookies, tc.want) {
				t.Errorf("%s: lost through Marshal: %s", doc, b)
			}
			if tc.key != "" && !strings.Contains(string(b), strings.ReplaceAll(strings.TrimSuffix(tc.key, ", "), " ", "")) {
				t.Errorf("%s: Marshal = %s", doc, b)
			}
		}
	}
}

// TestCloudProblemsCookiesVars checks that ${env.*} references in cookie
// and var values are refused at each level, in the documented order, and
// that a scenario's own load and start_at are not refused.
func TestCloudProblemsCookiesVars(t *testing.T) {
	data := []byte(`{"schema_version": "2.0", "name": "cloud",
		"cookies": {"b": "${env.B}", "a": "x${env.A}${env.A}"},
		"vars": {"v": "${env.V}", "w": "${uuid}"},
		"target": {"base_url": "https://api.test/${env.T}"},
		"scenarios": [{"name": "s", "start_at": "30s", "load": {"type": "constant", "virtual_users": 1, "duration": "1m"},
			"cookies": {"c": "${env.C}"}, "vars": {"x": "${env.X}"},
			"steps": [{"name": "a", "cookies": {"s": "${env.S}"}, "clear_cookies": true,
				"request": {"method": "GET", "url": "/a", "headers": {"H": "${env.H}"}}}]},
			{"name": "t", "load": {"type": "stages", "stages": [{"duration": "1m", "target": 1}]}, "vars": {"y": "${env.Y}"},
			"steps": [{"name": "b", "request": {"method": "GET", "url": "/b"}}]}]}`)
	wantCloudProblems(t, data, 3, []definition.Problem{
		cloudEnv("target.base_url", "${env.T}"),
		cloudEnv("cookies.a", "${env.A}"),
		cloudEnv("cookies.b", "${env.B}"),
		cloudEnv("vars.v", "${env.V}"),
		cloudEnv("scenarios[0].cookies.c", "${env.C}"),
		cloudEnv("scenarios[0].vars.x", "${env.X}"),
		cloudEnv("scenarios[0].steps[0].request.headers.H", "${env.H}"),
		cloudEnv("scenarios[0].steps[0].cookies.s", "${env.S}"),
		cloudEnv("scenarios[1].vars.y", "${env.Y}"),
	})
	pools := mustParse(t, readFile(t, "../testdata/v2/valid/scenario_load.json"))
	if ps := definition.CloudProblems(pools, 4); ps != nil {
		t.Errorf("CloudProblems(scenario_load.json) = %v, want none", ps)
	}
}

// TestCookieValuePatternMatchesParse checks the schema's cookie value
// pattern against Parse on every value of up to four characters from an
// alphabet that covers references, cookie-octets and the bytes refused
// inside and outside them.
func TestCookieValuePatternMatchesParse(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			AdditionalProperties struct {
				Pattern string `json:"pattern"`
			} `json:"additionalProperties"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(readFile(t, schemaPath), &schema); err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(schema.Properties["cookies"].AdditionalProperties.Pattern)
	alphabet := []string{"a", "$", "{", "}", " ", ";", "\r", "ü"}
	values := []string{""}
	for n, last := 0, []string{""}; n < 4; n++ {
		var next []string
		for _, p := range last {
			for _, c := range alphabet {
				next = append(next, p+c)
			}
		}
		values, last = append(values, next...), next
	}
	for _, v := range values {
		doc, err := json.Marshal(map[string]any{"schema_version": "2.0", "name": "c", "virtual_users": 1,
			"cookies": map[string]string{"c": v}, "scenarios": []any{map[string]any{"name": "s", "steps": []any{
				map[string]any{"name": "a", "request": map[string]any{"method": "GET", "url": "https://a.example.com/"}}}}}})
		if err != nil {
			t.Fatal(err)
		}
		_, perr := definition.Parse(doc)
		if accepted := perr == nil; accepted != pattern.MatchString(v) {
			t.Errorf("value %q: Parse accepts %v, pattern matches %v", v, accepted, !accepted)
		}
	}
}

// TestUsesTestLoad checks the predicate and that test-level keys holding
// their zero value read as absent when no scenario uses the test-level
// model.
func TestUsesTestLoad(t *testing.T) {
	constant := map[string]any{"type": "constant", "virtual_users": 1, "duration": "1m"}
	for _, tc := range []struct {
		file string
		want bool
	}{
		{"minimal.json", true},
		{"scenario_load.json", true},
		{"scenario_load_only.json", false},
		{"setup_teardown.json", true},
	} {
		if got := mustParse(t, readFile(t, "../testdata/v2/valid/"+tc.file)).UsesTestLoad(); got != tc.want {
			t.Errorf("%s: UsesTestLoad = %v, want %v", tc.file, got, tc.want)
		}
	}
	if (*definition.Definition)(nil).UsesTestLoad() {
		t.Error("a nil definition uses the test-level model")
	}
	if (&definition.Definition{}).UsesTestLoad() {
		t.Error("a definition without scenarios uses the test-level model")
	}
	poolsOnly := func(vus any, duration string) []byte {
		return v2doc(t, func(m map[string]any) {
			m["virtual_users"], m["duration"] = vus, duration
			scenario0(m)["load"] = constant
		})
	}
	if d := mustParse(t, poolsOnly(0, "")); d.UsesTestLoad() {
		t.Error("UsesTestLoad is true for a pools-only definition")
	}
	if got, want := problemStrings(t, poolsOnly(5, "")), []string{"virtual_users: " + unusedTopLoad}; !reflect.DeepEqual(got, want) {
		t.Errorf("pools only with virtual_users 5: problems = %q, want %q", got, want)
	}
}
