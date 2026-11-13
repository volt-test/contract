package definition

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Values the engines accept. The jsonschema enum tags in types.go list the
// same values; a test keeps them in step.
var (
	extractTypes = []string{"json", "regex", "header", "cookie", "html"}
	dataFormats  = []string{"csv", "json"}
	dataModes    = []string{"sequential", "random", "unique"}
	checkTypes   = []string{string(CheckStatus), string(CheckHeader), string(CheckBody), string(CheckJSON),
		string(CheckHTML), string(CheckTiming), string(CheckURL), string(CheckCookie), string(CheckAssert)}
	failModes = []string{string(FailRecord), string(FailFail), string(FailAbort)}
	picks     = []string{string(PickRandom), string(PickFirst), string(PickLast), string(PickAll)}
	jsonKinds = []string{"array", "object", "string", "number", "bool", "null"}
	bodyTypes = []string{string(BodyTypeRaw), string(BodyTypeJSON), string(BodyTypeForm), string(BodyTypeMultipart)}
)

// validateV2 checks the structure of a decoded v2 definition. objectForms
// holds the paths of thresholds written as objects. It returns nil or a
// *ValidationError with the problems found.
func validateV2(d *Definition, objectForms map[string]bool) *ValidationError {
	var ps problems

	if strings.TrimSpace(d.Name) == "" {
		ps.add("name", "is required")
	}

	ps.nonNegative("virtual_users", d.VirtualUsers)
	if d.Duration != "" {
		ps.positiveDuration("duration", d.Duration, "; leave it out to run the scenario once per virtual user")
	}
	ps.duration("ramp_up", d.RampUp)
	ps.duration("http_timeout", d.HTTPTimeout)
	// Load model: load alone, non-empty stages, or virtual_users (at least
	// 1) with optional duration and ramp_up. Without duration each virtual
	// user runs its scenario once. Zero values count as absent, except
	// initial_virtual_users, where 0 is a value. When every load scenario
	// has its own load, there is none.
	if ownLoadOnly(d) {
		for _, k := range []struct {
			key string
			set bool
		}{
			{"virtual_users", d.VirtualUsers != 0},
			{"duration", d.Duration != ""},
			{"ramp_up", d.RampUp != ""},
			{"stages", len(d.Stages) > 0},
			{"load", d.Load != nil},
			{"initial_virtual_users", d.InitialVirtualUsers != nil},
			{"ramp_down_grace", d.RampDownGrace != ""},
			{"stop_grace", d.StopGrace != ""},
		} {
			if k.set {
				ps.add(k.key, unusedTopLoadMessage)
			}
		}
	} else if d.Load != nil {
		validateLoad(&ps, d)
	} else if len(d.Stages) > 0 {
		if d.VirtualUsers != 0 {
			ps.add("virtual_users", "cannot be combined with stages")
		}
		if d.Duration != "" {
			ps.add("duration", "cannot be combined with stages")
		}
		if d.RampUp != "" {
			ps.add("ramp_up", "cannot be combined with stages")
		}
	} else if d.VirtualUsers == 0 {
		ps.add("virtual_users", "is required (at least 1) unless stages or load are set")
	}
	for i, s := range d.Stages {
		p := "stages[" + strconv.Itoa(i) + "]"
		if s.Duration == "" {
			ps.add(p+".duration", "is required")
		} else {
			// The engines reject a stage that does not last.
			ps.positiveDuration(p+".duration", s.Duration, "")
		}
		ps.nonNegative(p+".target", s.Target)
	}
	if !ownLoadOnly(d) {
		validateLoadLifecycle(&ps, d)
	}

	baseURL := ""
	if d.Target != nil {
		ps.duration("target.idle_timeout", d.Target.IdleTimeout)
		baseURL = d.Target.BaseURL
		if baseURL != "" {
			switch {
			case !isAbsoluteHTTP(baseURL) && !isTemplated(baseURL):
				ps.add("target.base_url", "must be an absolute http(s) URL or start with ${...}")
			case strings.ContainsAny(baseURL, "?#"):
				ps.add("target.base_url", "must not contain a query string or fragment")
			}
		}
	}

	for i, code := range d.ExpectedStatuses {
		ps.statusCode(indexPath("expected_statuses", i), code)
	}

	validateHTTPTLS(&ps, d)
	follows := httpFollows(d)
	validateCookies(&ps, "cookies", d.Cookies)
	validateVars(&ps, "vars", d.Vars)

	for i, r := range d.RegionConfig {
		p := "region_config[" + strconv.Itoa(i) + "]"
		if strings.TrimSpace(r.Region) == "" {
			ps.add(p+".region", "is required")
		}
		ps.nonNegative(p+".weight", r.Weight)
	}

	if len(d.Scenarios) == 0 {
		ps.add("scenarios", "must have at least one scenario")
	}
	globals := setupGlobals(d.Scenarios)
	var inline inlineBudget
	scenarioNames := map[string]int{}
	for i, sc := range d.Scenarios {
		p := "scenarios[" + strconv.Itoa(i) + "]"
		ps.uniqueName(p, sc.Name, scenarioNames, i, "scenarios")
		ps.nonNegative(p+".weight", sc.Weight)
		validateSetupTeardown(&ps, p, sc)
		validateOwnLoad(&ps, p, sc)
		validateCookies(&ps, p+".cookies", sc.Cookies)
		validateVars(&ps, p+".vars", sc.Vars)
		ps.thinkTime(p+".think_time", sc.ThinkTime)
		if sc.DataConfig != nil {
			validateDataConfig(&ps, p+".data_config", sc.DataConfig)
		}
		if len(sc.Steps) == 0 {
			ps.add(p+".steps", "must have at least one step")
		}
		ss := stepScope{d: d, sc: sc, index: i, baseURL: baseURL, follows: follows, globals: globals,
			inline: &inline, names: map[string]string{}, extracted: extractedNames(sc)}
		for j := range sc.Steps {
			st := sc.Steps[j]
			sp := p + ".steps[" + strconv.Itoa(j) + "]"
			if st.Steps == nil {
				validateStep(&ps, &ss, sp, st, false)
				continue
			}
			validateGroup(&ps, &ss, sp, st)
			for k, child := range st.Steps {
				validateStep(&ps, &ss, indexPath(sp+".steps", k), child, true)
			}
		}
		if sc.IsLoad() && len(sc.Steps) > 0 && allOncePerVU(sc.Steps) {
			ps.add(p+".steps", "must have at least one step without once_per_vu")
		}
	}
	if len(d.Scenarios) > 0 && !hasLoadScenario(d.Scenarios) {
		ps.add("scenarios", "must have at least one load scenario, one without run_once or teardown")
	}
	forEachTemplate(d, func(path, src string) {
		var te *TemplateError
		if err := scanTemplate(src, func(TemplateRef) {}); errors.As(err, &te) {
			ps.add(path, te.message())
		}
	})
	collectBounds(d, &ps, false, false, func(path string, _ ThresholdConfig) bool { return objectForms[path] })
	return ps.err()
}

// stepScope is what the rules for one step need from its scenario and test.
type stepScope struct {
	d         *Definition
	sc        Scenario
	index     int // of the scenario
	baseURL   string
	follows   bool
	globals   map[string]int
	inline    *inlineBudget
	names     map[string]string // step name -> path of the first step with it
	extracted map[string]bool   // variables the scenario's steps extract
}

// validateStep checks one step that sends a request at path sp. inGroup
// is set for a step inside a step group.
func validateStep(ps *problems, ss *stepScope, sp string, st Step, inGroup bool) {
	ss.uniqueStepName(ps, sp, st.Name)
	ps.thinkTime(sp+".think_time", st.ThinkTime)
	validateCookies(ps, sp+".cookies", st.Cookies)
	if st.ClearCookies && ss.sc.AutoHandleCookies != nil && !*ss.sc.AutoHandleCookies {
		ps.add(sp+".clear_cookies", "has no effect when auto_handle_cookies is false")
	}
	if strings.TrimSpace(st.Request.Method) == "" {
		ps.add(sp+".request.method", "is required")
	}
	if msg := stepURLProblem(st.Request.URL, ss.baseURL); msg != "" {
		ps.add(sp+".request.url", msg)
	}
	validateRequestBody(ps, sp+".request", st.Request)
	validateRequestRules(ps, sp+".request", st.Request, ss.inline)
	for k, ex := range st.Extract {
		ep := sp + ".extract[" + strconv.Itoa(k) + "]"
		if strings.TrimSpace(ex.VariableName) == "" {
			ps.add(ep+".variable_name", "is required")
		}
		if strings.TrimSpace(ex.Selector) == "" {
			ps.add(ep+".selector", "is required")
		}
		ps.oneOf(ep+".type", ex.Type, extractTypes)
		if ex.OnFail != "" {
			if ex.Required {
				ps.oneOf(ep+".on_fail", string(ex.OnFail), failModes)
			} else {
				ps.add(ep+".on_fail", "only applies with required: true")
			}
		}
		if ex.Pick != "" {
			if ex.Type == "header" || ex.Type == "cookie" {
				ps.add(ep+".pick", "only applies to json, regex and html extractors")
			} else {
				ps.oneOf(ep+".pick", string(ex.Pick), picks)
			}
		}
		if owner, ok := ss.globals[ex.VariableName]; ok && owner != ss.index {
			ps.add(ep+".variable_name", fmt.Sprintf("%s is a global set by the run_once scenario scenarios[%d]; "+
				"no other scenario may extract it", quoteShort(ex.VariableName), owner))
		}
	}
	for k, c := range st.Checks {
		validateCheck(ps, sp+".checks["+strconv.Itoa(k)+"]", c)
	}
	checkLabels(ps, sp, st)
	validateStepRedirects(ps, sp, st, ss.follows)
	validateResponseReading(ps, sp, st)
	if inGroup && st.OncePerVU {
		ps.add(sp+".once_per_vu", inGroupMessage)
	} else {
		validateOncePerVU(ps, sp, ss.sc, st)
	}
	validateDynamicStep(ps, ss, sp, st, inGroup)
}

func (ss *stepScope) uniqueStepName(ps *problems, sp, name string) {
	if strings.TrimSpace(name) == "" {
		ps.add(sp+".name", "is required")
		return
	}
	if first, dup := ss.names[name]; dup {
		ps.add(sp+".name", fmt.Sprintf("%s is already used by %s", quoteShort(name), first))
		return
	}
	ss.names[name] = sp
}

// validateRequestBody applies the body_type rules to the request at rp:
// body and body_base64 (not both) go with body_type raw, body_json with
// json (which requires it), fields with form or multipart, and files with
// multipart, which needs fields or files. Zero values ("", {}) read as
// absent. With an unknown body_type only body_type is reported.
func validateRequestBody(ps *problems, rp string, r Request) {
	if r.BodyType != "" {
		ps.oneOf(rp+".body_type", string(r.BodyType), bodyTypes)
	}
	kind := r.BodyKind()
	switch kind {
	case BodyTypeRaw, BodyTypeJSON, BodyTypeForm, BodyTypeMultipart:
	default:
		return
	}
	if r.Body != "" && kind != BodyTypeRaw {
		ps.add(rp+".body", fmt.Sprintf("cannot be combined with body_type %q; use %s", kind, bodyKey(kind)))
	}
	switch {
	case len(r.BodyJSON) > 0 && kind != BodyTypeJSON:
		ps.add(rp+".body_json", `only applies with body_type "json"`)
	case len(r.BodyJSON) == 0 && kind == BodyTypeJSON:
		ps.add(rp+".body_json", `is required with body_type "json"`)
	}
	if len(r.Fields) > 0 && kind != BodyTypeForm && kind != BodyTypeMultipart {
		ps.add(rp+".fields", `only applies with body_type "form" or "multipart"`)
	}
	switch {
	case len(r.Files) > 0 && kind != BodyTypeMultipart:
		ps.add(rp+".files", `only applies with body_type "multipart"`)
	case len(r.Files) == 0 && len(r.Fields) == 0 && kind == BodyTypeMultipart:
		ps.add(rp+".body_type", `"multipart" needs fields or files`)
	}
	switch {
	case r.BodyBase64 == "":
	case kind != BodyTypeRaw:
		ps.add(rp+".body_base64", `only applies with body_type "raw"`)
	case r.Body != "":
		ps.add(rp+".body_base64", "cannot be combined with body; use one of them")
	}
}

// bodyKey is the key that holds the body for a body type.
func bodyKey(kind BodyType) string {
	switch kind {
	case BodyTypeJSON:
		return "body_json"
	case BodyTypeForm:
		return "fields"
	case BodyTypeMultipart:
		return "fields and files"
	}
	return "body"
}

// validateSetupTeardown applies the run_once and teardown rules to one
// scenario: not both, and no weight, since neither is picked for the load.
func validateSetupTeardown(ps *problems, p string, sc Scenario) {
	if sc.RunOnce && sc.Teardown {
		ps.add(p+".teardown", "cannot be combined with run_once")
	}
	if sc.Weight > 0 {
		switch {
		case sc.RunOnce:
			ps.add(p+".weight", "does not apply to a run_once scenario, which is never picked for the load")
		case sc.Teardown:
			ps.add(p+".weight", "does not apply to a teardown scenario, which is never picked for the load")
		}
	}
}

// validateOwnLoad applies the rules for a scenario's own load and
// start_at: no load on a run_once or teardown scenario, no weight next to
// a load, and start_at, greater than zero, only with a load.
func validateOwnLoad(ps *problems, p string, sc Scenario) {
	if sc.Load != nil {
		validateScenarioLoad(ps, p, sc.Load)
		switch {
		case sc.RunOnce:
			ps.add(p+".load", "does not apply to a run_once scenario, which runs once")
		case sc.Teardown:
			ps.add(p+".load", "does not apply to a teardown scenario, which runs once")
		case sc.Weight > 0:
			ps.add(p+".weight", "does not apply to a scenario with its own load, which is never picked by weight")
		}
	}
	if sc.StartAt != "" {
		if sc.Load == nil {
			ps.add(p+".start_at", "needs a load on this scenario")
		} else {
			ps.positiveDuration(p+".start_at", sc.StartAt, "; leave it out to start with the load")
		}
	}
}

// validateOncePerVU rejects once_per_vu on a step of a run_once or
// teardown scenario, which runs once anyway. false reads as absent.
func validateOncePerVU(ps *problems, sp string, sc Scenario, st Step) {
	if !st.OncePerVU {
		return
	}
	switch {
	case sc.RunOnce:
		ps.add(sp+".once_per_vu", "does not apply to a run_once scenario, which runs once")
	case sc.Teardown:
		ps.add(sp+".once_per_vu", "does not apply to a teardown scenario, which runs once")
	}
}

// allOncePerVU reports whether every step has once_per_vu, which would
// leave later iterations of a load scenario with nothing to run. A step
// group never has it.
func allOncePerVU(steps []Step) bool {
	for _, st := range steps {
		if !st.OncePerVU {
			return false
		}
	}
	return true
}

// setupGlobals maps each variable a run_once scenario extracts to the
// index of the first run_once scenario that extracts it. Blank names are
// left out; they are reported as missing.
func setupGlobals(scenarios []Scenario) map[string]int {
	globals := map[string]int{}
	for i, sc := range scenarios {
		if !sc.RunOnce {
			continue
		}
		for _, st := range sc.LeafSteps() {
			for _, ex := range st.Extract {
				if strings.TrimSpace(ex.VariableName) == "" {
					continue
				}
				if _, seen := globals[ex.VariableName]; !seen {
					globals[ex.VariableName] = i
				}
			}
		}
	}
	return globals
}

func hasLoadScenario(scenarios []Scenario) bool {
	for _, sc := range scenarios {
		if sc.IsLoad() {
			return true
		}
	}
	return false
}

func validateDataConfig(ps *problems, p string, dc *DataConfig) {
	ps.oneOf(p+".data_format", dc.DataFormat, dataFormats)
	ps.oneOf(p+".mode", dc.Mode, dataModes)
	if dc.HasHeader && dc.DataFormat == "json" {
		ps.add(p+".has_header", `only applies to data_format "csv"`)
	}
	if dc.DataSource == "" && dc.DataContent == "" && dc.DataDownloadURL == "" {
		ps.add(p+".data_source", "is required")
	}
}

// stepURLProblem applies the v2 URL rules to a step URL and returns a
// problem message, or "" when the URL is valid.
func stepURLProblem(u, baseURL string) string {
	switch {
	case u == "":
		return "is required"
	case isAbsoluteHTTP(u), isTemplated(u):
		return ""
	case strings.HasPrefix(u, "//"):
		return `must not start with "//"; use an absolute URL or a path starting with a single "/"`
	case strings.HasPrefix(u, "/"):
		if baseURL == "" {
			return "is relative, so target.base_url is required"
		}
		return ""
	default:
		return `must be an absolute http(s) URL, start with ${...}, or be a path starting with "/"`
	}
}

type problems struct{ problemList }

func durationMessage(s string) string {
	return fmt.Sprintf("must be a duration such as \"30s\" or \"1m30s\", got %s", quoteShort(s))
}

// quoteShort quotes a user-supplied value for an error message, cut to
// about maxEcho bytes.
func quoteShort(s string) string {
	return strconv.Quote(truncate(s, maxEcho))
}

// maxEcho bounds how much of a user-supplied key or value an error repeats.
const maxEcho = 128

func (ps *problems) nonNegative(path string, n int) {
	if n < 0 {
		ps.add(path, "must not be negative")
	}
}

func (ps *problems) duration(path, s string) {
	if s == "" {
		return
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		ps.add(path, durationMessage(s))
		return
	}
	if d < 0 {
		ps.add(path, "must not be negative")
	}
}

// positiveDuration requires a duration greater than zero, as the engines
// do for the test and stage durations.
func (ps *problems) positiveDuration(path, s, hint string) {
	d, err := time.ParseDuration(s)
	if err != nil {
		ps.add(path, durationMessage(s))
		return
	}
	if d <= 0 {
		ps.add(path, "must be greater than zero"+hint)
	}
}

func (ps *problems) oneOf(path, v string, allowed []string) {
	for _, a := range allowed {
		if v == a {
			return
		}
	}
	quoted := make([]string, len(allowed))
	for i, a := range allowed {
		quoted[i] = strconv.Quote(a)
	}
	if v == "" {
		ps.add(path, "is required (one of "+strings.Join(quoted, ", ")+")")
		return
	}
	ps.add(path, fmt.Sprintf("must be one of %s, got %s", strings.Join(quoted, ", "), quoteShort(v)))
}

// uniqueName checks that a scenario has a name and that no earlier
// sibling uses it. listPath is the path of the enclosing list.
func (ps *problems) uniqueName(path, name string, seen map[string]int, idx int, listPath string) {
	if strings.TrimSpace(name) == "" {
		ps.add(path+".name", "is required")
		return
	}
	if first, dup := seen[name]; dup {
		ps.add(path+".name", fmt.Sprintf("%s is already used by %s[%d]", quoteShort(name), listPath, first))
		return
	}
	seen[name] = idx
}
