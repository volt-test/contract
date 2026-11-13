package definition

import (
	"fmt"
	"strconv"
	"strings"
)

// LoadType is the type of a Load: how the arrival rate, or the number of
// virtual users, is set over the load.
type LoadType string

// The load types. LoadConstant and LoadStages are valid on a scenario's
// load only.
const (
	LoadRate        LoadType = "rate"
	LoadRampingRate LoadType = "ramping_rate"
	LoadConstant    LoadType = "constant"
	LoadStages      LoadType = "stages"
)

// loadTypes are the types a scenario's load takes; testLoadTypes are the
// ones the test-level load takes.
var (
	loadTypes     = []string{string(LoadRate), string(LoadRampingRate), string(LoadConstant), string(LoadStages)}
	testLoadTypes = loadTypes[:2]
)

// rampingZeroMessage refuses a ramping_rate load whose every stage target
// is 0.
const rampingZeroMessage = "never exceeds a rate of 0, so the run would do nothing; set a stage target above 0"

// stagesZeroMessage refuses a stages load whose every stage target is 0
// and that starts with no virtual users: a pool without virtual users.
const stagesZeroMessage = "never exceeds 0 virtual users, so the scenario would do nothing; set a stage target or initial_virtual_users above 0"

// unusedTopLoadMessage refuses a test-level load model when every load
// scenario has its own load.
const unusedTopLoadMessage = "applies to no scenario: every load scenario has its own load"

// validateLoad checks d.Load, which is non-nil.
func validateLoad(ps *problems, d *Definition) {
	var others []string
	if d.VirtualUsers != 0 {
		others = append(others, "virtual_users")
	}
	if d.Duration != "" {
		others = append(others, "duration")
	}
	if d.RampUp != "" {
		others = append(others, "ramp_up")
	}
	if len(d.Stages) > 0 {
		others = append(others, "stages")
	}
	if len(others) > 0 {
		ps.add("load", "cannot be combined with "+strings.Join(others, ", "))
	}

	l := d.Load
	if l.Type == LoadConstant || l.Type == LoadStages {
		ps.add("load.type", fmt.Sprintf("%s is only valid on a scenario's load; at the top level use virtual_users "+
			"and duration, or stages", quoteShort(string(l.Type))))
		return
	}
	ps.oneOf("load.type", string(l.Type), testLoadTypes)
	validateRateLoad(ps, "load", l)
	if l.StopGrace != "" {
		ps.add("load.stop_grace", "does not apply to the test-level load; use the top-level stop_grace")
	}
}

// validateLoadLifecycle checks the test-level initial_virtual_users,
// stop_grace and ramp_down_grace when the test-level model applies:
// stop_grace with any model, the other two with stages only.
func validateLoadLifecycle(ps *problems, d *Definition) {
	if d.InitialVirtualUsers != nil {
		if len(d.Stages) > 0 {
			ps.nonNegative("initial_virtual_users", *d.InitialVirtualUsers)
		} else {
			ps.add("initial_virtual_users", "needs stages")
		}
	}
	ps.duration("stop_grace", d.StopGrace)
	if d.RampDownGrace != "" {
		if len(d.Stages) > 0 {
			ps.duration("ramp_down_grace", d.RampDownGrace)
		} else {
			ps.add("ramp_down_grace", "needs stages")
		}
	}
}

// validateScenarioLoad checks the load of the scenario at p.
func validateScenarioLoad(ps *problems, p string, l *Load) {
	lp := p + ".load"
	ps.oneOf(lp+".type", string(l.Type), loadTypes)
	switch l.Type {
	case LoadRate, LoadRampingRate:
		validateRateLoad(ps, lp, l)
	case LoadConstant:
		switch {
		case l.VirtualUsers < 0:
			ps.add(lp+".virtual_users", "must not be negative")
		case l.VirtualUsers == 0:
			ps.add(lp+".virtual_users", "is required (at least 1) for type constant")
		}
		if l.Duration == "" {
			ps.add(lp+".duration", "is required for type constant")
		} else {
			ps.positiveDuration(lp+".duration", l.Duration, "")
		}
		notForType(ps, lp, l, "rate", "time_unit", "stages", "preallocated_virtual_users", "max_virtual_users",
			"initial_virtual_users", "ramp_down_grace")
	case LoadStages:
		if len(l.Stages) == 0 {
			ps.add(lp+".stages", "is required for type stages (at least one stage)")
		}
		if l.InitialVirtualUsers != nil {
			ps.nonNegative(lp+".initial_virtual_users", *l.InitialVirtualUsers)
		}
		// A pool that starts with virtual users may ramp to 0.
		if loadStages(ps, lp, l.Stages) && (l.InitialVirtualUsers == nil || *l.InitialVirtualUsers <= 0) {
			ps.add(lp, stagesZeroMessage)
		}
		ps.duration(lp+".ramp_down_grace", l.RampDownGrace)
		notForType(ps, lp, l, "rate", "time_unit", "duration", "virtual_users", "preallocated_virtual_users", "max_virtual_users")
	}
	ps.duration(lp+".stop_grace", l.StopGrace)
}

// validateRateLoad applies the rules of the arrival-rate types to the load
// at lp. The preallocated_virtual_users and max_virtual_users rules apply
// to any type, so the test-level load reports them next to an unknown type.
func validateRateLoad(ps *problems, lp string, l *Load) {
	if l.TimeUnit != "" {
		ps.positiveDuration(lp+".time_unit", l.TimeUnit, "")
	}
	switch {
	case l.PreallocatedVirtualUsers < 0:
		ps.add(lp+".preallocated_virtual_users", "must not be negative")
	case l.PreallocatedVirtualUsers == 0:
		ps.add(lp+".preallocated_virtual_users", "is required (at least 1)")
	}
	switch {
	case l.MaxVirtualUsers < 0:
		ps.add(lp+".max_virtual_users", "must not be negative")
	case l.MaxVirtualUsers > 0 && l.PreallocatedVirtualUsers > 0 && l.MaxVirtualUsers < l.PreallocatedVirtualUsers:
		ps.add(lp+".max_virtual_users", fmt.Sprintf("must be at least preallocated_virtual_users (%d), got %d",
			l.PreallocatedVirtualUsers, l.MaxVirtualUsers))
	}

	switch l.Type {
	case LoadRate:
		switch {
		case l.Rate < 0:
			ps.add(lp+".rate", "must not be negative")
		case l.Rate == 0:
			ps.add(lp+".rate", "is required (at least 1) for type rate")
		}
		if l.Duration == "" {
			ps.add(lp+".duration", "is required for type rate")
		} else {
			ps.positiveDuration(lp+".duration", l.Duration, "")
		}
		notForType(ps, lp, l, "stages", "virtual_users", "initial_virtual_users", "ramp_down_grace")
	case LoadRampingRate:
		if l.Rate != 0 {
			ps.add(lp+".rate", "does not apply to type ramping_rate; set each stage's target")
		}
		if l.Duration != "" {
			ps.add(lp+".duration", "does not apply to type ramping_rate; the stages set the length")
		}
		if len(l.Stages) == 0 {
			ps.add(lp+".stages", "is required for type ramping_rate (at least one stage)")
		}
		if loadStages(ps, lp, l.Stages) {
			ps.add(lp, rampingZeroMessage)
		}
		notForType(ps, lp, l, "virtual_users", "initial_virtual_users", "ramp_down_grace")
	}
}

// loadStages checks the stages of the load at lp and reports whether there
// is at least one and every target is 0. A negative target is reported on
// its own, not as all zero.
func loadStages(ps *problems, lp string, stages []RateStage) (allZero bool) {
	allZero = len(stages) > 0
	for i, s := range stages {
		p := lp + ".stages[" + strconv.Itoa(i) + "]"
		if s.Duration == "" {
			ps.add(p+".duration", "is required")
		} else {
			ps.positiveDuration(p+".duration", s.Duration, "")
		}
		ps.nonNegative(p+".target", s.Target)
		if s.Target != 0 {
			allZero = false
		}
	}
	return allZero
}

// notForType reports each of keys that the load at lp sets although its
// type takes no such key. A zero value reads as absent, except for
// initial_virtual_users, where 0 is a value.
func notForType(ps *problems, lp string, l *Load, keys ...string) {
	for _, k := range keys {
		set := false
		switch k {
		case "rate":
			set = l.Rate != 0
		case "time_unit":
			set = l.TimeUnit != ""
		case "duration":
			set = l.Duration != ""
		case "stages":
			set = len(l.Stages) > 0
		case "virtual_users":
			set = l.VirtualUsers != 0
		case "preallocated_virtual_users":
			set = l.PreallocatedVirtualUsers != 0
		case "max_virtual_users":
			set = l.MaxVirtualUsers != 0
		case "initial_virtual_users":
			set = l.InitialVirtualUsers != nil
		case "ramp_down_grace":
			set = l.RampDownGrace != ""
		}
		if set {
			ps.add(lp+"."+k, "does not apply to type "+string(l.Type))
		}
	}
}

// UsesTestLoad reports whether the test-level load model (virtual_users,
// duration, ramp_up, stages, load, initial_virtual_users, ramp_down_grace
// and stop_grace) runs any scenario: whether some load scenario has no
// Load of its own. When it is false and the definition has a load
// scenario, Parse refuses each of those keys that holds a value; a key
// holding its zero value (0, "", []) reads as absent, except
// initial_virtual_users, where 0 is a value.
func (d *Definition) UsesTestLoad() bool {
	if d == nil {
		return false
	}
	for _, sc := range d.Scenarios {
		if sc.IsLoad() && sc.Load == nil {
			return true
		}
	}
	return false
}

// ownLoadOnly reports whether there is a load scenario and every load
// scenario has its own load, so the test-level load model applies to none.
func ownLoadOnly(d *Definition) bool {
	return hasLoadScenario(d.Scenarios) && !d.UsesTestLoad()
}
