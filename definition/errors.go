package definition

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CompatibilityURL is the README section that lists, for each
// schema_version, the first engine release that reads it. Error messages
// about versions and unknown fields point here.
const CompatibilityURL = "https://github.com/volt-test/contract#compatibility"

// ErrInvalidDefinition matches every error returned by Parse:
//
//	if errors.Is(err, definition.ErrInvalidDefinition) { ... }
//
// Use errors.As with one of the concrete error types for details.
var ErrInvalidDefinition = errors.New("invalid test definition")

// SyntaxError reports input that is not a single JSON object.
type SyntaxError struct {
	Err error
}

func (e *SyntaxError) Error() string {
	return "definition: invalid JSON: " + e.Err.Error()
}

// Unwrap returns the underlying decoding error.
func (e *SyntaxError) Unwrap() error { return e.Err }

// Is reports whether target is ErrInvalidDefinition.
func (e *SyntaxError) Is(target error) bool { return target == ErrInvalidDefinition }

// InvalidVersionError reports a schema_version value that is not a valid
// version: not 1, "1", "1.0" or a "MAJOR.MINOR" string.
type InvalidVersionError struct {
	// Value is the schema_version value as it appeared in the input (raw
	// JSON, so strings keep their quotes).
	Value string
}

func (e *InvalidVersionError) Error() string {
	return fmt.Sprintf("definition: invalid schema_version %s: use a \"MAJOR.MINOR\" string such as \"2.0\" (or 1 for v1)", truncate(e.Value, 64))
}

// truncate shortens s to at most n bytes, on a UTF-8 boundary, marking the
// cut with "...".
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// Is reports whether target is ErrInvalidDefinition.
func (e *InvalidVersionError) Is(target error) bool { return target == ErrInvalidDefinition }

// UnsupportedVersionError reports a valid schema_version that the reader
// does not support.
type UnsupportedVersionError struct {
	Version   SchemaVersion
	Supported []string
}

func (e *UnsupportedVersionError) Error() string {
	quoted := make([]string, len(e.Supported))
	for i, s := range e.Supported {
		quoted[i] = strconv.Quote(s)
	}
	list := strings.Join(quoted, ", ")
	if list == "" {
		list = "none"
	}
	need := "it needs a newer engine release"
	if rel, ok := MinEngineRelease(e.Version); ok {
		need = "minimum engine release: " + rel
	}
	return fmt.Sprintf("definition: schema_version %q is not supported (supported: %s); %s; see %s",
		e.Version.String(), list, need, CompatibilityURL)
}

// Is reports whether target is ErrInvalidDefinition.
func (e *UnsupportedVersionError) Is(target error) bool { return target == ErrInvalidDefinition }

// UnknownFieldError reports keys that the definition's schema_version does
// not define. That covers misspelt or wrongly cased keys, fields from a
// newer schema_version, and v1-only fields used in a v2 definition.
type UnknownFieldError struct {
	// Paths lists the unknown keys, e.g. "scenarios[1].steps[0].request.retries",
	// in document-walk order: the keys of each object alphabetically, array
	// elements by index. At most 1000 paths are kept.
	Paths []string
	// Omitted counts unknown keys found beyond the first 1000.
	Omitted int
	// Version is the schema_version the definition declared.
	Version SchemaVersion
}

func (e *UnknownFieldError) Error() string {
	noun := "fields"
	if len(e.Paths) == 1 && e.Omitted == 0 {
		noun = "field"
	}
	return fmt.Sprintf("definition: unknown %s for schema_version %q: %s (check spelling and case; v1-only fields are not allowed in v2; a field from a newer schema_version needs a newer reader, see %s)",
		noun, e.Version.String(), listForError(e.Paths, e.Omitted, ", "), CompatibilityURL)
}

// Is reports whether target is ErrInvalidDefinition.
func (e *UnknownFieldError) Is(target error) bool { return target == ErrInvalidDefinition }

// maxKept (1000) bounds the paths and problems an error keeps, so a huge
// malformed input cannot produce a huge error. Error messages show at most
// maxShown (100) of them.
const maxKept = 1000

const maxShown = 100

// listForError joins at most maxShown items and notes how many more exist.
func listForError(items []string, omitted int, sep string) string {
	more := omitted
	if len(items) > maxShown {
		more += len(items) - maxShown
		items = items[:maxShown]
	}
	out := strings.Join(items, sep)
	if more > 0 {
		out += fmt.Sprintf(" (+%d more)", more)
	}
	return out
}

// Problem is one reason a definition is invalid.
type Problem struct {
	// Path locates the value, e.g. "scenarios[0].steps[2].request.url".
	// Empty means the definition as a whole.
	Path    string
	Message string
	// Count is how many values the problem stands for. In V1TypeProblems
	// one problem is kept per field, with Path naming the first value and
	// Count the number of values of that field with the problem. Zero or
	// one means just Path.
	Count int
}

func (p Problem) String() string {
	s := p.Message
	if p.Path != "" {
		s = p.Path + ": " + p.Message
	}
	if p.Count > 1 {
		s += fmt.Sprintf(" (%d values)", p.Count)
	}
	return s
}

// ValidationError lists the problems found in a definition whose keys are
// all known: wrong JSON types, missing required values, values out of
// range. At most 1000 problems are kept.
type ValidationError struct {
	Problems []Problem
	// Omitted counts problems found beyond the first 1000.
	Omitted int
}

func (e *ValidationError) Error() string {
	n := len(e.Problems)
	if n > maxShown {
		n = maxShown
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = e.Problems[i].String()
	}
	return "definition: " + listForError(parts, e.Omitted+len(e.Problems)-n, "; ")
}

// Is reports whether target is ErrInvalidDefinition.
func (e *ValidationError) Is(target error) bool { return target == ErrInvalidDefinition }

// problemList collects problems, keeping the first maxKept.
type problemList struct {
	items   []Problem
	omitted int
}

func (l *problemList) full() bool { return len(l.items) >= maxKept }

func (l *problemList) add(path, msg string) {
	if l.full() {
		l.omitted++
		return
	}
	l.items = append(l.items, Problem{Path: path, Message: msg})
}

func (l *problemList) err() *ValidationError {
	if len(l.items) == 0 {
		return nil
	}
	return &ValidationError{Problems: l.items, Omitted: l.omitted}
}
