package definition

import (
	"maps"
	"slices"
	"strings"
)

const (
	cookieNameMessage  = "is not a valid cookie name: use letters, digits and !#$%&'*+-.^_`|~, without spaces, \"=\", \";\" or other separators"
	cookieBreakMessage = "must not contain a line break (CR or LF) or NUL"
	cookieValueMessage = `must not contain a space, '"', ",", ";", "\", a control character or non-ASCII outside ${...} references`
	varNameMessage     = `is not a valid variable name: use ASCII letters, digits, "_" and "-"`
	varReservedMessage = `is reserved for the ${env.NAME} and ${fake.NAME} built-ins; use another name`
)

// validateCookies checks the cookies at p: each name an HTTP token, and
// each value RFC 6265 cookie-octets outside ${...} references, which
// resolve when the request is sent, with no CR, LF or NUL anywhere.
func validateCookies(ps *problems, p string, cookies map[string]string) {
	for _, name := range slices.Sorted(maps.Keys(cookies)) {
		cp := joinPath(p, name)
		if !isToken(name) {
			ps.add(cp, cookieNameMessage)
		}
		v := cookies[name]
		switch {
		case strings.ContainsAny(v, "\r\n\x00"):
			ps.add(cp, cookieBreakMessage)
		case !literalCookieOctets(v):
			ps.add(cp, cookieValueMessage)
		}
	}
}

// literalCookieOctets reports whether every byte of v outside its ${...}
// references is a cookie-octet (RFC 6265): printable ASCII except space,
// '"', ',', ';' and '\'.
func literalCookieOctets(v string) bool {
	ok := true
	check := func(lit string) {
		for i := 0; i < len(lit); i++ {
			if c := lit[i]; c <= ' ' || c >= 0x7f || c == '"' || c == ',' || c == ';' || c == '\\' {
				ok = false
			}
		}
	}
	last := 0
	forEachRef(v, func(open, end int) {
		check(v[last:open])
		last = end
	})
	check(v[last:])
	return ok
}

// validateVars checks the variable names at p.
func validateVars(ps *problems, p string, vars map[string]string) {
	for _, name := range slices.Sorted(maps.Keys(vars)) {
		switch {
		case !isVarName(name):
			ps.add(joinPath(p, name), varNameMessage)
		case name == "env" || name == "fake":
			ps.add(joinPath(p, name), varReservedMessage)
		}
	}
}

// isVarName reports whether s is a vars name: one or more ASCII letters,
// digits, '_' and '-'. It has no '.', so no name looks like a namespaced
// built-in such as ${env.NAME}.
func isVarName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

// isToken reports whether s is an HTTP token (RFC 9110), the form of a
// cookie name.
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}
