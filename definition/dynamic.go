package definition

import (
	"fmt"
	"strings"
)

// MaxRepeat is the largest repeat.max Parse accepts.
const MaxRepeat = 1000

// MaxForeach is the most passes the engine runs for one foreach.
const MaxForeach = 1000

const (
	inGroupMessage = "does not apply to a step inside a step group"
	setupLoopMsg   = " scenarios do not take repeat, foreach or step groups yet"
	foreachInMsg   = `must be exactly one ${name} reference, such as "${ids}"`
	foreachVarMsg  = `is not a valid foreach variable: use ASCII letters, digits and "_", not starting with a digit`
)

// validateGroup checks a step group at sp. The walker has already refused
// every key but name, repeat and steps, empty steps, and a group inside a
// group.
func validateGroup(ps *problems, ss *stepScope, sp string, st Step) {
	ss.uniqueStepName(ps, sp, st.Name)
	if msg := setupLoopProblem(ss.sc); msg != "" {
		ps.add(sp+".steps", msg)
	}
	if st.Repeat == nil {
		ps.add(sp+".repeat", "is required for a step group")
		return
	}
	validateRepeat(ps, sp+".repeat", *st.Repeat)
}

func validateDynamicStep(ps *problems, ss *stepScope, sp string, st Step, inGroup bool) {
	if st.SkipIf != "" {
		ps.expression(sp+".skip_if", st.SkipIf)
	}
	if st.OnlyIf != "" {
		if st.SkipIf != "" {
			ps.add(sp+".only_if", "cannot be combined with skip_if")
		} else {
			ps.expression(sp+".only_if", st.OnlyIf)
		}
	}
	if st.Repeat != nil {
		switch msg := setupLoopProblem(ss.sc); {
		case inGroup:
			ps.add(sp+".repeat", inGroupMessage+"; repeat the group")
		case msg != "":
			ps.add(sp+".repeat", msg)
		default:
			validateRepeat(ps, sp+".repeat", *st.Repeat)
		}
	}
	if st.Foreach != nil {
		switch msg := setupLoopProblem(ss.sc); {
		case inGroup:
			ps.add(sp+".foreach", inGroupMessage)
		case msg != "":
			ps.add(sp+".foreach", msg)
		case st.Repeat != nil:
			ps.add(sp+".foreach", "cannot be combined with repeat")
		default:
			validateForeach(ps, ss, sp+".foreach", *st.Foreach)
		}
	}
}

// setupLoopProblem returns why a run_once or teardown scenario cannot hold
// a loop, or "" for a load scenario.
func setupLoopProblem(sc Scenario) string {
	switch {
	case sc.RunOnce:
		return "run_once" + setupLoopMsg
	case sc.Teardown:
		return "teardown" + setupLoopMsg
	}
	return ""
}

func validateRepeat(ps *problems, rp string, r Repeat) {
	if r.Until == "" {
		ps.add(rp+".until", "is required")
	} else {
		ps.expression(rp+".until", r.Until)
	}
	switch {
	case r.Max == 0:
		ps.add(rp+".max", fmt.Sprintf("is required (1 to %d)", MaxRepeat))
	case r.Max < 1 || r.Max > MaxRepeat:
		ps.add(rp+".max", fmt.Sprintf("must be from 1 to %d, got %d", MaxRepeat, r.Max))
	}
}

func validateForeach(ps *problems, ss *stepScope, fp string, f Foreach) {
	switch {
	case f.Var == "":
		ps.add(fp+".var", "is required")
	case !isIdentifier(f.Var):
		ps.add(fp+".var", foreachVarMsg)
	case f.Var == "env" || f.Var == "fake":
		ps.add(fp+".var", varReservedMessage)
	case ss.extracted[f.Var]:
		ps.add(fp+".var", quoteShort(f.Var)+" is extracted by a step of this scenario; use another name")
	default:
		if owner, ok := ss.globals[f.Var]; ok {
			ps.add(fp+".var", fmt.Sprintf("%s is a global set by the run_once scenario scenarios[%d]; use another name",
				quoteShort(f.Var), owner))
		} else if _, ok := ss.sc.Vars[f.Var]; ok {
			ps.add(fp+".var", quoteShort(f.Var)+" is a var of this scenario; use another name")
		} else if _, ok := ss.d.Vars[f.Var]; ok {
			ps.add(fp+".var", quoteShort(f.Var)+" is a var of the test; use another name")
		}
	}
	name, ok := foreachList(f.In)
	switch {
	case f.In == "":
		ps.add(fp+".in", "is required")
	case !ok:
		ps.add(fp+".in", foreachInMsg)
	case name == f.Var:
		ps.add(fp+".in", "must not read the variable that var sets")
	}
}

// foreachList returns the variable a foreach in reads: in must be exactly
// one plain ${name} reference whose name is a vars name, not env or fake.
func foreachList(in string) (string, bool) {
	refs, err := ScanTemplate(in)
	if err != nil || len(refs) != 1 || refs[0].Expr || refs[0].Start != 0 || refs[0].End != len(in) {
		return "", false
	}
	name := refs[0].Body(in)
	if !isVarName(name) || name == "env" || name == "fake" {
		return "", false
	}
	return name, true
}

// extractedNames returns the variables the leaf steps of sc extract.
func extractedNames(sc Scenario) map[string]bool {
	out := map[string]bool{}
	for _, st := range sc.LeafSteps() {
		for _, ex := range st.Extract {
			out[ex.VariableName] = true
		}
	}
	return out
}

// expression checks a bare expression: skip_if, only_if, repeat.until or
// an assert check's expr. The contract does not compile it.
func (ps *problems) expression(path, v string) {
	switch t := strings.TrimSpace(v); {
	case t == "":
		ps.add(path, "must not be blank")
	case strings.IndexByte(v, 0) >= 0:
		ps.add(path, "must not contain NUL")
	case strings.HasPrefix(t, "${"):
		ps.add(path, "is an expression on its own; write it without ${...}")
	case hasComment(v):
		ps.add(path, `must not have "//" or "/*" outside a quoted string; expressions cannot hold comments`)
	}
}

// isIdentifier reports whether s is an expr-lang identifier made of ASCII
// letters, digits and '_', so that an expression can read it.
func isIdentifier(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return s != ""
}
