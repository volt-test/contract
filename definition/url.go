package definition

import "strings"

// ResolveURL returns the URL a v2 step requests. A relative ref (a path
// starting with a single "/") is joined to base by plain string
// concatenation, after trimming trailing slashes from base, so ${...}
// references in either part stay as written. Any other ref (absolute, or
// starting with ${...}) and any ref with an empty base is returned
// unchanged.
//
// Parse never resolves URLs; engines call ResolveURL with
// Target.BaseURL and each Request.URL.
func ResolveURL(base, ref string) string {
	if base == "" || !isRelativeRef(ref) {
		return ref
	}
	return strings.TrimRight(base, "/") + ref
}

// isRelativeRef reports whether ref is a path to join to target.base_url:
// a leading "/" but not "//", which would read as a host.
func isRelativeRef(ref string) bool {
	return strings.HasPrefix(ref, "/") && !strings.HasPrefix(ref, "//")
}

// isAbsoluteHTTP reports whether u starts with http:// or https:// (any
// case) followed by something. net/url is not used: it rejects ${...} in
// the host.
func isAbsoluteHTTP(u string) bool {
	lower := strings.ToLower(u)
	for _, scheme := range []string{"http://", "https://"} {
		if strings.HasPrefix(lower, scheme) && len(u) > len(scheme) {
			return true
		}
	}
	return false
}

// isTemplated reports whether u starts with a ${...} reference, which v1
// and v2 treat as an absolute URL.
func isTemplated(u string) bool {
	return strings.HasPrefix(u, "${")
}
