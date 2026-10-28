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
// spaces around the one hyphen. A duration gives lo == hi, and ""
// gives 0, 0. Neither may be negative, and lo must not exceed hi.
// Parse validates v2 think_time values with it; v1 never does.
func ParseThinkTime(s string) (lo, hi time.Duration, err error) {
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
	a, b := s[:i], s[i+1:]
	// Signs belong to neither half: "1s--3s" is malformed, not reversed.
	if a[0] == '+' || b == "" || b[0] == '+' || b[0] == '-' {
		return 0, 0, thinkTimeSyntax(s)
	}
	lo, err1 := time.ParseDuration(a)
	hi, err2 := time.ParseDuration(b)
	switch {
	case err1 != nil || err2 != nil:
		return 0, 0, thinkTimeSyntax(s)
	case lo < 0 || a[0] == '-':
		return 0, 0, errNegativeThinkTime
	case lo > hi:
		return 0, 0, fmt.Errorf("must give the shorter duration first, got %s", quoteShort(s))
	}
	return lo, hi, nil
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
