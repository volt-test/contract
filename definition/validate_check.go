package definition

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
	"strconv"
	"strings"

	"github.com/volt-test/contract/thresholds"
)

// checkRule is what a check type allows.
type checkRule struct {
	ops      []string // operator keys, in the order messages list them
	selector string   // what the selector names; "" when the type takes none
	// stringValue: equals and contains must be strings.
	stringValue bool
	// noEmptyContains: contains must not be "".
	noEmptyContains bool
}

var checkRules = map[CheckType]checkRule{
	CheckStatus: {ops: []string{"in", "not_in", "between"}},
	CheckHeader: {ops: []string{"equals", "contains", "exists", "matches"}, selector: "the header name",
		stringValue: true, noEmptyContains: true},
	CheckBody: {ops: []string{"equals", "contains", "not_contains", "matches", "size", "empty"},
		stringValue: true, noEmptyContains: true},
	CheckJSON:   {ops: []string{"equals", "exists", "missing", "is", "count", "matches", "contains"}, selector: "a JSON path"},
	CheckHTML:   {ops: []string{"exists", "equals"}, selector: "a CSS selector", stringValue: true},
	CheckTiming: {ops: []string{"duration"}},
	CheckURL:    {ops: []string{"equals", "contains", "matches"}, stringValue: true, noEmptyContains: true},
	CheckCookie: {ops: []string{"exists"}, selector: "the cookie name"},
}

// comparisonKinds maps the comparison keys to the kind they compare.
var comparisonKinds = map[string]thresholds.Kind{
	"count":    thresholds.KindCount,
	"size":     thresholds.KindSize,
	"duration": thresholds.KindDuration,
}

// validateCheck checks one check at path p.
func validateCheck(ps *problems, p string, c Check) {
	ps.oneOf(p+".type", string(c.Type), checkTypes)
	rule, known := checkRules[c.Type]
	if c.OnFail != "" {
		ps.oneOf(p+".on_fail", string(c.OnFail), failModes)
	}
	if known {
		switch {
		case rule.selector != "" && strings.TrimSpace(c.Selector) == "":
			ps.add(p+".selector", fmt.Sprintf("is required for type %s (%s)", c.Type, rule.selector))
		case rule.selector == "" && c.Selector != "":
			ps.add(p+".selector", fmt.Sprintf("does not apply to type %s", c.Type))
		}
	}

	ops := c.operators()
	var allowed []checkOp
	for _, op := range ops {
		if known && !slices.Contains(rule.ops, op.key) {
			ps.add(p+"."+op.key, fmt.Sprintf("does not apply to type %s (use %s)", c.Type, orList(rule.ops)))
			continue
		}
		allowed = append(allowed, op)
		validateOperator(ps, p+"."+op.key, c, op, rule, known)
	}
	if !known {
		return
	}
	switch len(allowed) {
	case 0:
		if len(ops) == 0 {
			ps.add(p, fmt.Sprintf("needs one operator for type %s: %s", c.Type, orList(rule.ops)))
		}
	case 1:
	default:
		keys := make([]string, len(allowed))
		for i, op := range allowed {
			keys[i] = op.key
		}
		ps.add(p, fmt.Sprintf("has more than one operator (%s); write one check per operator", strings.Join(keys, ", ")))
	}
}

// validateOperator checks the value of one operator key at path p.
func validateOperator(ps *problems, p string, c Check, op checkOp, rule checkRule, known bool) {
	switch op.key {
	case "in", "not_in":
		for i, code := range op.codes {
			ps.statusCode(indexPath(p, i), code)
		}
	case "between":
		if len(op.codes) != 2 {
			ps.add(p, fmt.Sprintf("must be [low, high], two status codes; got %d", len(op.codes)))
			return
		}
		ok := ps.statusCode(indexPath(p, 0), op.codes[0])
		ok = ps.statusCode(indexPath(p, 1), op.codes[1]) && ok
		if ok && op.codes[0] > op.codes[1] {
			ps.add(p, fmt.Sprintf("must be [low, high] with low <= high, got [%d,%d]", op.codes[0], op.codes[1]))
		}
	case "equals", "contains":
		if known && rule.stringValue && op.scalar.Kind() != ScalarString {
			ps.add(p, fmt.Sprintf("must be a string for type %s", c.Type))
			return
		}
		if op.key == "contains" && known && rule.noEmptyContains && op.scalar.Kind() == ScalarString && op.scalar.Text() == "" {
			ps.add(p, "must not be empty: an empty string is in every value")
		}
	case "matches":
		if _, err := regexp.Compile(op.text); err != nil {
			ps.add(p, regexpMessage(err))
		}
	case "exists":
		if !*op.flag {
			if c.Type == CheckJSON {
				ps.add(p, "must be true; to check that the path is absent, use missing: true")
			} else {
				ps.add(p, "must be true; leave it out otherwise")
			}
		}
	case "missing":
		if !*op.flag {
			ps.add(p, "must be true; to check that the path is present, use exists: true")
		}
	case "empty":
		if !*op.flag {
			ps.add(p, `must be true; to check that the body is not empty, use size: "> 0"`)
		}
	case "is":
		ps.oneOf(p, op.text, jsonKinds)
	case "count", "size", "duration":
		if _, err := thresholds.ParseComparison(comparisonKinds[op.key], op.text); err != nil {
			ps.add(p, strings.TrimPrefix(err.Error(), "thresholds: "))
		}
	}
}

// regexpMessage describes a regular expression that does not compile,
// echoing at most maxEcho bytes of it.
func regexpMessage(err error) string {
	var se *syntax.Error
	if errors.As(err, &se) {
		return fmt.Sprintf("is not a valid regular expression (RE2 syntax): %s: %s", se.Code, quoteShort(se.Expr))
	}
	return "is not a valid regular expression (RE2 syntax)"
}

// statusCode checks an HTTP status code and reports whether it is valid.
func (ps *problems) statusCode(path string, code int) bool {
	if code < 100 || code > 599 {
		ps.add(path, fmt.Sprintf("must be an HTTP status code from 100 to 599, got %d", code))
		return false
	}
	return true
}

// checkLabels rejects two checks of the step at path sp with the same
// label, counting required extractors as checks labelled "extract <var>".
func checkLabels(ps *problems, sp string, st Step) {
	n := len(st.Checks)
	for _, ex := range st.Extract {
		if ex.Required {
			n++
		}
	}
	if n < 2 {
		return
	}
	seen := make(map[string]string, n) // label -> path of its first holder
	for k, c := range st.Checks {
		p := sp + ".checks[" + strconv.Itoa(k) + "]"
		label := c.Label()
		first, dup := seen[label]
		if !dup {
			seen[label] = p
			continue
		}
		at := p
		if strings.TrimSpace(c.Name) != "" {
			at = p + ".name"
		}
		ps.add(at, fmt.Sprintf("the label %s is already used by %s; set a name that is unique in the step", quoteShort(label), relPath(sp, first)))
	}
	for k, ex := range st.Extract {
		if !ex.Required {
			continue
		}
		p := sp + ".extract[" + strconv.Itoa(k) + "]"
		label := ex.Label()
		first, dup := seen[label]
		if !dup {
			seen[label] = p
			continue
		}
		hint := "a step can have one required extractor per variable"
		if strings.HasPrefix(first, sp+".checks[") {
			hint = "give that check another name"
		}
		ps.add(p+".variable_name", fmt.Sprintf("the label %s is already used by %s; %s", quoteShort(label), relPath(sp, first), hint))
	}
}

// relPath returns path relative to the step at sp, e.g. "checks[0]".
func relPath(sp, path string) string { return strings.TrimPrefix(path, sp+".") }

// orList joins keys as "a, b or c".
func orList(keys []string) string {
	if len(keys) == 1 {
		return keys[0]
	}
	return strings.Join(keys[:len(keys)-1], ", ") + " or " + keys[len(keys)-1]
}
