package definition

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var errNegativeThinkTime = errors.New("must not be negative")

// ParseThinkTime reads a v2 think_time: a duration such as "500ms", or a
// range "MIN-MAX" such as "1s-3s" for a uniformly random pause between
// the two, bounds included. MIN and MAX are durations with no sign and no
// spaces around the one hyphen. A duration gives min == max, and ""
// gives 0, 0. Neither may be negative, and min must not exceed max.
// Parse validates v2 think_time values with it; v1 never does.
func ParseThinkTime(s string) (min, max time.Duration, err error) {
	if s == "" {
		return 0, 0, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		if d < 0 {
			return 0, 0, errNegativeThinkTime
		}
		return d, d, nil
	}
	i := strings.IndexByte(s[1:], '-') + 1
	if i == 0 {
		return 0, 0, thinkTimeSyntax(s)
	}
	lo, hi := s[:i], s[i+1:]
	// Signs belong to neither half: "1s--3s" is malformed, not reversed.
	if lo[0] == '+' || hi == "" || hi[0] == '+' || hi[0] == '-' {
		return 0, 0, thinkTimeSyntax(s)
	}
	min, err1 := time.ParseDuration(lo)
	max, err2 := time.ParseDuration(hi)
	switch {
	case err1 != nil || err2 != nil:
		return 0, 0, thinkTimeSyntax(s)
	case min < 0 || lo[0] == '-':
		return 0, 0, errNegativeThinkTime
	case min > max:
		return 0, 0, fmt.Errorf("must give the shorter duration first, got %s", quoteShort(s))
	}
	return min, max, nil
}

func thinkTimeSyntax(s string) error {
	return fmt.Errorf("must be a duration such as \"500ms\" or a range such as \"1s-3s\", got %s", quoteShort(s))
}

// thinkTime reports a v2 think_time that ParseThinkTime rejects.
func (ps *problems) thinkTime(path, s string) {
	if _, _, err := ParseThinkTime(s); err != nil {
		ps.add(path, err.Error())
	}
}
