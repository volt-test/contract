package definition

import (
	"strconv"
	"strings"
)

const exprOpen = "${expr:"

// TemplateRef is one ${...} reference in a template: src[Start:End], from
// its "${" to its closing "}" included.
type TemplateRef struct {
	Start, End int
	// Expr is set for a ${expr: ...} expression.
	Expr bool
}

// Body returns the text inside the reference: the expression after
// "${expr:" for an expression, else the text between "${" and "}".
func (r TemplateRef) Body(src string) string {
	if r.Expr {
		return src[r.Start+len(exprOpen) : r.End-1]
	}
	return src[r.Start+2 : r.End-1]
}

// TemplateError is a ${expr: ...} expression that ScanTemplate cannot read.
type TemplateError struct {
	// Offset is the byte offset of the expression's "${".
	Offset int
	// Empty is set for an expression of white space only, such as
	// ${expr:}, and Comment for one with "//" or "/*" outside its quoted
	// literals; otherwise the expression is not closed.
	Empty, Comment bool
}

func (e *TemplateError) Error() string {
	return "definition: " + e.message()
}

// message is the error text without the package prefix, as a Problem
// carries it.
func (e *TemplateError) message() string {
	at := strconv.Itoa(e.Offset)
	switch {
	case e.Empty:
		return "${expr:} at byte " + at + " has no expression"
	case e.Comment:
		return "${expr: ...} at byte " + at + ` has "//" or "/*" outside a quoted string; expressions cannot hold comments`
	}
	return "${expr: ...} at byte " + at + ` is not closed: no "}" balances its braces outside quoted text, ` +
		"or a quoted string runs into a line break or the end"
}

// ScanTemplate returns the ${...} references in a v2 template, in source
// order.
//
// A reference that starts with exactly "${expr:" is an expression and
// ends at the "}" that balances its braces. Braces inside '...', "..."
// and `...` literals do not count, a backslash inside '...' and "..."
// escapes the next byte, a line feed (escaped or not) ends a '...' or
// "..." literal as unterminated, and "${" inside an expression is plain
// text. Every other reference runs from "${" to the first "}"; without a
// "}" the rest of src is literal. There is no escape for "${".
//
// An expression that is not closed, that holds only white space, or that
// has "//" or "/*" outside its literals (expr-lang reads them as comments,
// which would hide the closing "}") is a *TemplateError; the references
// before it are returned with it.
func ScanTemplate(src string) ([]TemplateRef, error) {
	var refs []TemplateRef
	err := scanTemplate(src, func(r TemplateRef) { refs = append(refs, r) })
	return refs, err
}

// scanTemplate calls fn with each reference ScanTemplate returns, and
// returns its error.
func scanTemplate(src string, fn func(TemplateRef)) error {
	for i := 0; i < len(src); {
		open := strings.Index(src[i:], "${")
		if open < 0 {
			return nil
		}
		open += i
		if strings.HasPrefix(src[open:], exprOpen) {
			end, err := exprEnd(src, open)
			if err != nil {
				return err
			}
			fn(TemplateRef{Start: open, End: end, Expr: true})
			i = end
			continue
		}
		end := strings.IndexByte(src[open+2:], '}')
		if end < 0 {
			return nil
		}
		end += open + 3
		fn(TemplateRef{Start: open, End: end})
		i = end
	}
	return nil
}

// exprEnd returns the end of the expression whose "${expr:" is at
// src[open:]. The quoting rules are expr-lang's: the engine compiles the
// body with it, so both must agree on where a literal ends.
func exprEnd(src string, open int) (int, error) {
	body := open + len(exprOpen)
	depth := 1
	for i := body; i < len(src); i++ {
		switch c := src[i]; c {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				if strings.TrimSpace(src[body:i]) == "" {
					return 0, &TemplateError{Offset: open, Empty: true}
				}
				return i + 1, nil
			}
		case '\'', '"', '`':
			if i = literalEnd(src, i); i < 0 {
				return 0, &TemplateError{Offset: open}
			}
		case '/':
			if isCommentStart(src, i) {
				return 0, &TemplateError{Offset: open, Comment: true}
			}
		}
	}
	return 0, &TemplateError{Offset: open}
}

// literalEnd returns the index of the quote that closes the literal
// opening at s[i], or -1 when it is not closed. A doubled backtick inside
// a raw literal closes it and opens another, which leaves the same text
// inside literals.
func literalEnd(s string, i int) int {
	q := s[i]
	if q == '`' {
		if n := strings.IndexByte(s[i+1:], '`'); n >= 0 {
			return i + 1 + n
		}
		return -1
	}
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case q:
			return j
		case '\n':
			return -1
		case '\\':
			if j++; j < len(s) && s[j] == '\n' {
				return -1
			}
		}
	}
	return -1
}

func isCommentStart(s string, i int) bool {
	return s[i] == '/' && i+1 < len(s) && (s[i+1] == '/' || s[i+1] == '*')
}

// hasComment reports whether a bare expression has "//" or "/*" outside
// its quoted literals.
func hasComment(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'', '"', '`':
			if i = literalEnd(s, i); i < 0 {
				return false
			}
		case '/':
			if isCommentStart(s, i) {
				return true
			}
		}
	}
	return false
}
