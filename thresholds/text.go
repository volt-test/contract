package thresholds

import (
	"errors"
	"strconv"
)

// The enums encode as their names in JSON and other text formats, so an
// encoded Bound is readable and does not depend on the order of the
// constants: Kind as "duration", Op as "<=", ScopeKind as "step".
// Encoding a value outside the declared constants is an error.

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
	for _, c := range all {
		if c == v {
			return []byte(v.String()), nil
		}
	}
	return nil, errors.New("thresholds: invalid " + typ + " " + strconv.Itoa(int(v)))
}

func unmarshalEnum[T textEnum](text []byte, all []T, typ string) (T, error) {
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
