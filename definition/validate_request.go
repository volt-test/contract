package definition

import (
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"mime"
	"slices"
	"strings"
)

// MaxInlineBinaryBytes bounds the decoded size of every content_base64 and
// body_base64 of a definition, together (10 MiB).
const MaxInlineBinaryBytes = 10 << 20

var responseTypes = []string{string(ResponseText), string(ResponseBinary)}

// inlineBudget sums the decoded size of the inline binary values of a
// definition, in definition order, and reports the value that crosses
// MaxInlineBinaryBytes, once.
type inlineBudget struct {
	used int64
	over bool
}

// add checks one base64 value at path and counts its decoded size.
func (b *inlineBudget) add(ps *problems, path, v string) {
	if v == "" {
		return
	}
	n, msg := base64Size(v)
	if msg != "" {
		ps.add(path, msg)
		return
	}
	b.used += n
	if b.used > MaxInlineBinaryBytes && !b.over {
		b.over = true
		ps.add(path, fmt.Sprintf("brings the inline binary content of the definition (every content_base64 and "+
			"body_base64, decoded) over %d bytes (10 MiB); send large files by path", MaxInlineBinaryBytes))
	}
}

// base64Size checks that v is standard base64 with padding and no line
// breaks, and returns its decoded size. The size comes from the length;
// the check decodes v in small chunks, so it never holds the decoded value.
// Messages never repeat the value.
func base64Size(v string) (int64, string) {
	const prefix = "must be standard base64 with padding"
	if strings.ContainsAny(v, "\r\n") {
		// The decoder skips line breaks, which would make the size wrong.
		return 0, prefix + " and no line breaks"
	}
	if len(v)%4 != 0 {
		return 0, prefix + ": the length must be a multiple of 4"
	}
	// Each chunk is decoded on its own, so padding inside the value is
	// caught here rather than by the decoder.
	pad := len(v) - len(strings.TrimRight(v, "="))
	if i := strings.IndexByte(v, '='); pad > 2 || (i >= 0 && i < len(v)-pad) {
		return 0, prefix + ": = may only end the value"
	}
	enc := base64.StdEncoding.Strict()
	var src [1024]byte // a multiple of 4
	var dst [768]byte
	for i := 0; i < len(v); i += len(src) {
		n := copy(src[:], v[i:])
		if _, err := enc.Decode(dst[:], src[:n]); err != nil {
			var ce base64.CorruptInputError
			if errors.As(err, &ce) {
				return 0, fmt.Sprintf("%s: invalid data at offset %d", prefix, int64(i)+int64(ce))
			}
			return 0, prefix
		}
	}
	return int64(len(v)/4*3 - pad), ""
}

// validateRequestRules applies the rules for headers, multipart files,
// binary bodies and the request timeout to the request at rp.
func validateRequestRules(ps *problems, rp string, r Request, budget *inlineBudget) {
	if r.Timeout != "" {
		ps.positiveDuration(rp+".timeout", r.Timeout, "")
	}
	validateHeaders(ps, rp, r)
	budget.add(ps, rp+".body_base64", r.BodyBase64)
	multipart := r.BodyType == BodyTypeMultipart
	var fields map[string]bool
	if multipart {
		fields = make(map[string]bool, len(r.Fields))
		for _, f := range r.Fields {
			fields[f.Name] = true
			if hasHeaderBreak(f.Name) {
				ps.add(joinPath(rp+".fields", f.Name), partNameBreak)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(r.Files)) {
		fp := joinPath(rp+".files", name)
		if multipart {
			if hasHeaderBreak(name) {
				ps.add(fp, partNameBreak)
			}
			if fields[name] {
				ps.add(fp, "is also a field name; a name is either a field or a file")
			}
		}
		validateFilePart(ps, fp, r.Files[name], budget)
	}
}

// partNameBreak is the problem with a multipart field or file name that
// would break the part's Content-Disposition header.
const partNameBreak = "must not have a line break (CR or LF) or NUL in its name"

// validateHeaders refuses a Host header with other than one value, two
// keys that both name Host, and Content-Type with body_type "multipart".
// Header names match case-insensitively, as in HTTP.
func validateHeaders(ps *problems, rp string, r Request) {
	host := ""
	for _, name := range slices.Sorted(maps.Keys(r.Headers)) {
		hp := joinPath(rp+".headers", name)
		switch {
		case strings.EqualFold(name, "Host"):
			switch {
			case host != "":
				ps.add(hp, fmt.Sprintf("names the Host header again (as %s); a request has one Host", quoteShort(host)))
			case len(r.Headers[name]) != 1:
				host = name
				ps.add(hp, "must have exactly one value: a request has one Host")
			default:
				host = name
			}
		case strings.EqualFold(name, "Content-Type") && r.BodyType == BodyTypeMultipart:
			ps.add(hp, `cannot be set with body_type "multipart": the Content-Type carries the part boundary and is set for you`)
		}
	}
}

// validateFilePart checks one multipart file at fp.
func validateFilePart(ps *problems, fp string, f FilePart, budget *inlineBudget) {
	switch {
	case f.Path == "" && f.ContentBase64 == "":
		ps.add(fp, "needs path or content_base64")
	case f.Path != "" && f.ContentBase64 != "":
		ps.add(fp, "cannot have both path and content_base64; use one of them")
	}
	switch {
	case f.Path == "":
	case strings.TrimSpace(f.Path) == "":
		ps.add(fp+".path", "must not be blank")
	case hasHeaderBreak(f.Path):
		ps.add(fp+".path", "must not contain a line break (CR or LF) or NUL")
	}
	budget.add(ps, fp+".content_base64", f.ContentBase64)
	if hasHeaderBreak(f.Filename) {
		ps.add(fp+".filename", "must not contain a line break (CR or LF) or NUL")
	}
	switch {
	case f.ContentType == "":
	case hasHeaderBreak(f.ContentType):
		ps.add(fp+".content_type", "must not contain a line break (CR or LF) or NUL")
	default:
		// ParseMediaType also accepts a type without a subtype.
		mt, _, err := mime.ParseMediaType(f.ContentType)
		if err != nil || !strings.Contains(mt, "/") {
			ps.add(fp+".content_type", fmt.Sprintf(`must be a media type such as "image/png", got %s`, quoteShort(f.ContentType)))
		}
	}
}

// hasHeaderBreak reports whether s holds a byte that would end or corrupt
// a header line.
func hasHeaderBreak(s string) bool { return strings.ContainsAny(s, "\r\n\x00") }

// validateResponseReading applies response_type and discard_body to the
// step at sp: a step that drops its body has no check or extractor that
// reads it, and a binary step has no json or html check or extractor.
func validateResponseReading(ps *problems, sp string, st Step) {
	if st.ResponseType != "" {
		ps.oneOf(sp+".response_type", string(st.ResponseType), responseTypes)
	}
	discard := st.DiscardBody != nil && *st.DiscardBody
	binary := st.ResponseType == ResponseBinary
	if !discard && !binary {
		return
	}
	for k, c := range st.Checks {
		what := fmt.Sprintf("checks[%d] (%s check", k, article(string(c.Type)))
		if op, reads := bodyReadingOp(c); discard && reads {
			if op != "" {
				what += " with " + op
			}
			ps.add(sp+".discard_body", "cannot be combined with "+what+"), which reads the response body")
		}
		if binary && (c.Type == CheckJSON || c.Type == CheckHTML) {
			ps.add(sp+".response_type", `"binary" cannot be combined with `+what+"); json and html read the body as text")
		}
	}
	for k, ex := range st.Extract {
		what := fmt.Sprintf("extract[%d] (%s extractor)", k, article(ex.Type))
		if discard && extractorReadsBody(ex) {
			ps.add(sp+".discard_body", "cannot be combined with "+what+", which reads the response body")
		}
		if binary && (ex.Type == "json" || ex.Type == "html") {
			ps.add(sp+".response_type", `"binary" cannot be combined with `+what+"; json and html read the body as text")
		}
	}
}

// ReadsBody reports whether a check or extractor of the step reads the
// response body's content: a body check with equals, contains,
// not_contains or matches, a json, html or assert check (an assert's
// expression may read the body), or a json, regex or html extractor. Such
// a step keeps its body under http.discard_bodies, and Parse refuses
// discard_body: true on it.
func (s Step) ReadsBody() bool {
	for _, c := range s.Checks {
		if _, reads := bodyReadingOp(c); reads {
			return true
		}
	}
	for _, ex := range s.Extract {
		if extractorReadsBody(ex) {
			return true
		}
	}
	return false
}

// extractorReadsBody reports whether an extractor reads the response
// body's content.
func extractorReadsBody(ex Extract) bool {
	return ex.Type == "json" || ex.Type == "regex" || ex.Type == "html"
}

// article puts "a" or "an" before a check or extractor type.
func article(typ string) string {
	if typ == "html" || (typ != "" && strings.IndexByte("aeiou", typ[0]) >= 0) {
		return "an " + typ
	}
	return "a " + typ
}

// bodyReadingOp reports whether a check reads the response body's content,
// and the operator that does for a body check ("" for json, html and
// assert, which always count as reading it). size and empty need only the
// length.
func bodyReadingOp(c Check) (string, bool) {
	switch c.Type {
	case CheckJSON, CheckHTML, CheckAssert:
		return "", true
	case CheckBody:
		for _, op := range c.operators() {
			switch op.key {
			case "equals", "contains", "not_contains", "matches":
				return op.key, true
			}
		}
	}
	return "", false
}
