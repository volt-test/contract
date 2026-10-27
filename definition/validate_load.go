package definition

import (
	"fmt"
	"strconv"
	"strings"
)

// LoadType is the type of a Load: how the arrival rate is set over the
// test.
type LoadType string

// The load types.
const (
	LoadRate        LoadType = "rate"
	LoadRampingRate LoadType = "ramping_rate"
)

var loadTypes = []string{string(LoadRate), string(LoadRampingRate)}

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
	ps.oneOf("load.type", string(l.Type), loadTypes)
	if l.TimeUnit != "" {
		ps.positiveDuration("load.time_unit", l.TimeUnit, "")
	}
	switch {
	case l.PreallocatedVUs < 0:
		ps.add("load.preallocated_vus", "must not be negative")
	case l.PreallocatedVUs == 0:
		ps.add("load.preallocated_vus", "is required (at least 1)")
	}
	switch {
	case l.MaxVUs < 0:
		ps.add("load.max_vus", "must not be negative")
	case l.MaxVUs > 0 && l.PreallocatedVUs > 0 && l.MaxVUs < l.PreallocatedVUs:
		ps.add("load.max_vus", fmt.Sprintf("must be at least preallocated_vus (%d), got %d", l.PreallocatedVUs, l.MaxVUs))
	}

	switch l.Type {
	case LoadRate:
		switch {
		case l.Rate < 0:
			ps.add("load.rate", "must not be negative")
		case l.Rate == 0:
			ps.add("load.rate", "is required (at least 1) for type rate")
		}
		if l.Duration == "" {
			ps.add("load.duration", "is required for type rate")
		} else {
			ps.positiveDuration("load.duration", l.Duration, "")
		}
		if len(l.Stages) > 0 {
			ps.add("load.stages", "does not apply to type rate")
		}
	case LoadRampingRate:
		if l.Rate != 0 {
			ps.add("load.rate", "does not apply to type ramping_rate; set each stage's target")
		}
		if l.Duration != "" {
			ps.add("load.duration", "does not apply to type ramping_rate; the stages set the length")
		}
		if len(l.Stages) == 0 {
			ps.add("load.stages", "is required for type ramping_rate (at least one stage)")
		}
		for i, s := range l.Stages {
			p := "load.stages[" + strconv.Itoa(i) + "]"
			if s.Duration == "" {
				ps.add(p+".duration", "is required")
			} else {
				ps.positiveDuration(p+".duration", s.Duration, "")
			}
			ps.nonNegative(p+".target", s.Target)
		}
	}
}
