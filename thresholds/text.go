package thresholds

import (
	"errors"
	"strconv"
)

// The enums encode as their names in JSON and other text formats, so an
// encoded Bound is readable and does not depend on the order of the
// constants: Kind as "duration", Op as "<=", ScopeKind as "step". The zero
// value encodes as "" and "" decodes to it, so an unset Bound encodes.
// Encoding any other value outside the declared constants is an error.
// Metric encodes as its name, e.g. "p95".

var (
	kinds      = []Kind{KindDuration, KindRate, KindCount, KindSize, KindThroughput}
	allOps     = []Op{OpLT, OpLE, OpGT, OpGE, OpEQ}
	scopeKinds = []ScopeKind{ScopeTest, ScopeScenario, ScopeStep}
)

type textEnum interface {
	~int
	String() string
}

func marshalEnum[T textEnum](v T, all []T, typ string) ([]byte, error) {
	if v == 0 {
		return []byte{}, nil
	}
	for _, c := range all {
		if c == v {
			return []byte(v.String()), nil
		}
	}
	return nil, errors.New("thresholds: invalid " + typ + " " + strconv.Itoa(int(v)))
}

func unmarshalEnum[T textEnum](text []byte, all []T, typ string) (T, error) {
	if len(text) == 0 {
		return 0, nil
	}
	for _, c := range all {
		if c.String() == string(text) {
			return c, nil
		}
	}
	return 0, errors.New("thresholds: unknown " + typ + " " + quoteShort(string(text)))
}

// MarshalText returns the kind's name, e.g. "duration".
func (k Kind) MarshalText() ([]byte, error) { return marshalEnum(k, kinds, "Kind") }

// UnmarshalText reads a kind's name. On error k is unchanged.
func (k *Kind) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum(text, kinds, "Kind")
	if err == nil {
		*k = v
	}
	return err
}

// MarshalText returns the operator, e.g. "<=".
func (o Op) MarshalText() ([]byte, error) { return marshalEnum(o, allOps, "Op") }

// UnmarshalText reads an operator. On error o is unchanged.
func (o *Op) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum(text, allOps, "Op")
	if err == nil {
		*o = v
	}
	return err
}

// MarshalText returns the scope's name, e.g. "step".
func (k ScopeKind) MarshalText() ([]byte, error) { return marshalEnum(k, scopeKinds, "ScopeKind") }

// UnmarshalText reads a scope's name. On error k is unchanged.
func (k *ScopeKind) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum(text, scopeKinds, "ScopeKind")
	if err == nil {
		*k = v
	}
	return err
}

// MarshalText returns the metric's name, e.g. "p95", or "" for the zero
// Metric. A Metric that is not a catalog entry (one built by hand rather
// than by Parse) is an error.
func (m Metric) MarshalText() ([]byte, error) {
	if m == (Metric{}) {
		return []byte{}, nil
	}
	if c, ok := lookupMetric(m.Name); !ok || c != m {
		return nil, errors.New("thresholds: metric " + quoteShort(m.Name) + " is not a catalog entry; build it with Parse")
	}
	return []byte(m.Name), nil
}

// UnmarshalText reads a metric name and looks it up in the catalog; ""
// gives the zero Metric. An unknown name is an error and leaves m
// unchanged.
func (m *Metric) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*m = Metric{}
		return nil
	}
	c, ok := lookupMetric(string(text))
	if !ok {
		return errors.New("thresholds: unknown metric " + quoteShort(string(text)))
	}
	*m = c
	return nil
}
