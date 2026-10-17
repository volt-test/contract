package definition

import (
	"fmt"
	"reflect"
	"strings"
)

// ReadSet is the set of loose v1 fields a reader reads, used to pick the
// V1TypeProblems that concern that reader. Build one with NewReadSet or
// MustReadSet; the zero ReadSet names no field.
type ReadSet struct {
	patterns []string
}

// NewReadSet validates field patterns and returns them as a ReadSet.
//
// A pattern names a field by its canonical key names, with "[]" for every
// array level it passes through, e.g. "weights", "region_config[].weight"
// or "scenarios[].steps[].request.headers". A pattern ends at a field
// name, never with "[]" or ".": "weights" already covers every element and
// the whole field. It must stop at a map field such as headers. Never put a
// literal index or a map key in a pattern: V1TypeProblems keeps one problem
// per field, with the path of the first mistyped value only, so a pattern
// like "weights[3]" or "headers.Accept" could miss a problem.
//
// NewReadSet returns an error for an empty pattern or segment, a trailing
// "[]" or ".", an unknown field, a v2-only field (it can never have a v1
// type problem, so naming one is a mistake), a literal index, a missing or
// extra "[]", or a segment below a map or a plain value.
func NewReadSet(patterns ...string) (ReadSet, error) {
	for _, p := range patterns {
		if err := checkPattern(p); err != nil {
			return ReadSet{}, fmt.Errorf("definition: read set: %w", err)
		}
	}
	return ReadSet{patterns: append([]string(nil), patterns...)}, nil
}

// MustReadSet is NewReadSet for package-level variables: it panics on an
// invalid pattern, so a mistake fails at program start and in every test
// of the package that declares it.
//
//	var headlessReads = definition.MustReadSet("description", "weights", ...)
func MustReadSet(patterns ...string) ReadSet {
	rs, err := NewReadSet(patterns...)
	if err != nil {
		panic(err)
	}
	return rs
}

// V1TypeProblemsIn returns the V1TypeProblems that fall under rs, so a v1
// reader can fail on exactly the fields it reads. A problem falls under a
// pattern when its path equals the pattern or continues it with "." or
// "[", with "[]" matching any index: "weights[1]" falls under "weights",
// "scenarios[0].steps[1].request.headers.A" under
// "scenarios[].steps[].request.headers", and "weightsx" under neither.
func (d *Definition) V1TypeProblemsIn(rs ReadSet) []Problem {
	var out []Problem
	for _, p := range d.V1TypeProblems {
		for _, f := range rs.patterns {
			if pathUnder(p.Path, f) {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// checkPattern checks a read-set pattern against the fields the v1 path
// reads.
func checkPattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("empty field pattern")
	}
	if strings.HasSuffix(pattern, "[]") {
		field := strings.TrimRight(pattern, "[]")
		return fmt.Errorf("pattern %q: end the pattern at the field name (%q); it already covers every element", pattern, field)
	}
	if strings.HasSuffix(pattern, ".") {
		return fmt.Errorf("pattern %q: end the pattern at a field name, not a dot (%q)", pattern, strings.TrimRight(pattern, "."))
	}
	t := reflect.TypeOf(v1Wrapper{})
	segs := strings.Split(pattern, ".")
	for n, seg := range segs {
		if seg == "" {
			return fmt.Errorf("pattern %q: empty segment", pattern)
		}
		name, levels := seg, 0
		for strings.HasSuffix(name, "[]") {
			name, levels = strings.TrimSuffix(name, "[]"), levels+1
		}
		if strings.ContainsAny(name, "[]") {
			return fmt.Errorf("pattern %q: write array indexes as [], never a literal index", pattern)
		}
		if t.Kind() != reflect.Struct {
			return fmt.Errorf("pattern %q: %q is below a field that has no fields", pattern, seg)
		}
		f, ok := fieldsOf(t).byName[name]
		if !ok {
			return fmt.Errorf("pattern %q: unknown field %q", pattern, name)
		}
		if f.v2Only {
			return fmt.Errorf("pattern %q: %q is a v2-only field and never has a v1 type problem", pattern, name)
		}
		ft, depth := derefType(f.typ), 0
		for (ft.Kind() == reflect.Slice && ft.Elem().Kind() != reflect.Uint8) || ft.Kind() == reflect.Array {
			ft, depth = derefType(ft.Elem()), depth+1
		}
		if levels > depth {
			return fmt.Errorf("pattern %q: %q has %d array levels", pattern, name, depth)
		}
		if n < len(segs)-1 {
			if ft.Kind() == reflect.Map {
				return fmt.Errorf("pattern %q: stop at the map field %q; problems with its entries fall under it", pattern, name)
			}
			if levels != depth {
				return fmt.Errorf("pattern %q: use [] for every array level of %q", pattern, name)
			}
		}
		t = ft
	}
	return nil
}

// pathUnder reports whether path equals pattern, or continues it with "."
// or "[", treating "[]" in pattern as any array index.
func pathUnder(path, pattern string) bool {
	i, j := 0, 0
	for j < len(pattern) {
		if pattern[j] == '[' && j+1 < len(pattern) && pattern[j+1] == ']' {
			if i >= len(path) || path[i] != '[' {
				return false
			}
			k := i + 1
			for k < len(path) && path[k] >= '0' && path[k] <= '9' {
				k++
			}
			if k == i+1 || k >= len(path) || path[k] != ']' {
				return false
			}
			i, j = k+1, j+2
			continue
		}
		if i >= len(path) || path[i] != pattern[j] {
			return false
		}
		i++
		j++
	}
	return i == len(path) || path[i] == '.' || path[i] == '['
}
