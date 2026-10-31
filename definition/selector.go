package definition

import (
	"strconv"
	"strings"
	"unicode"
)

const (
	jsonCheckSelectorMessage   = " is not a supported JSON path: use $.key, $.key[0] or $.a.b[1].c, where a key before [N] uses only letters, digits, _ and - (no white space, wildcards, filters, bracket keys, negative or chained indexes)"
	jsonExtractSelectorMessage = " is not a supported JSON path: use $.key, $.key[0], $.a.b[1].c, $.items[*].id, $[0] or $[*], where a key before [N] or [*] uses only letters, digits, _ and - (no white space, filters, bracket keys, negative or chained indexes)"
)

// JSONSelectorProblems reports the selectors of json extractors and json
// checks in a parsed v2 definition that the engine does not read. Per
// step, each extract[k].selector comes first, then each
// checks[k].selector. Other types' selectors are not scanned.
//
// A selector is an optional "$." prefix, then dot-separated keys. A key
// is any non-empty text without '.', '[', ']' or white space, other than
// "*"; key[N] takes a decimal index and a key of only letters, digits, _
// and -. Extractors also take key[*], and $[N] or $[*] on the document
// itself. White space is anything unicode.IsSpace matches.
//
// It returns nil for a nil or v1 definition.
func JSONSelectorProblems(d *Definition) []Problem {
	if d == nil || d.IsV1() {
		return nil
	}
	var ps []Problem
	for i, sc := range d.Scenarios {
		for j, st := range sc.Steps {
			sp := indexPath(indexPath("scenarios", i)+".steps", j)
			for k, e := range st.Extract {
				if e.Type == "json" && !jsonSelectorOK(e.Selector, true) {
					ps = append(ps, Problem{Path: indexPath(sp+".extract", k) + ".selector", Message: quoteShort(e.Selector) + jsonExtractSelectorMessage})
				}
			}
			for k, c := range st.Checks {
				if c.Type == CheckJSON && !jsonSelectorOK(c.Selector, false) {
					ps = append(ps, Problem{Path: indexPath(sp+".checks", k) + ".selector", Message: quoteShort(c.Selector) + jsonCheckSelectorMessage})
				}
			}
		}
	}
	return ps
}

// jsonSelectorOK reports whether selector is in the check grammar or,
// with extract, the extractor grammar.
func jsonSelectorOK(selector string, extract bool) bool {
	if selector == "$" || strings.IndexFunc(selector, unicode.IsSpace) >= 0 {
		return false
	}
	// $[N] and $[*] index the document: the first part has no key.
	root := extract && strings.HasPrefix(selector, "$[")
	rest := strings.TrimPrefix(selector, "$.")
	if root {
		rest = selector[1:]
	}
	if rest == "" {
		return false
	}
	for i, part := range strings.Split(rest, ".") {
		if !jsonSelectorPartOK(part, extract, root && i == 0) {
			return false
		}
	}
	return true
}

// jsonSelectorPartOK checks one dot-separated part: key, key[N] or, with
// wild, key[*]. With rootKey the part is [N] or [*] with no key.
func jsonSelectorPartOK(part string, wild, rootKey bool) bool {
	if part == "" || part == "*" {
		return false
	}
	open := strings.IndexByte(part, '[')
	if open < 0 && strings.IndexByte(part, ']') < 0 {
		return !rootKey
	}
	if open < 0 || (open == 0) != rootKey || part[len(part)-1] != ']' {
		return false
	}
	key, num := part[:open], part[open+1:len(part)-1]
	for i := 0; i < len(key); i++ {
		c := key[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	if wild && num == "*" {
		return true
	}
	if num == "" {
		return false
	}
	for i := 0; i < len(num); i++ {
		if num[i] < '0' || num[i] > '9' {
			return false
		}
	}
	_, err := strconv.Atoi(num) // digits only: an error is overflow
	return err == nil
}
