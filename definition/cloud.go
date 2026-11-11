package definition

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"
)

const (
	cloudPathMessage   = "must be inline PEM on VoltTest Cloud (a file path cannot be read on a cloud node)"
	cloudEnvMessage    = " is not available on VoltTest Cloud"
	cloudSetupMessage  = " scenarios are not supported on VoltTest Cloud yet; run them with the headless engine"
	cloudBinaryMessage = "is not available on VoltTest Cloud yet; run this test headless"
)

// CloudProblems reports what VoltTest Cloud refuses in a parsed v2
// definition run on nodes nodes, in this order:
//
//   - a tls client_cert, client_key or ca_cert that is a file path, not
//     inline PEM;
//   - binary request content, per leaf step (steps inside step groups
//     included, at their own paths): the request body_base64, then each
//     multipart file's path or content_base64, by field name. Cloud
//     nodes cannot receive file content yet;
//   - each distinct ${env.*} reference at each path where it is written:
//     target.base_url, each test-level cookie and then var (each sorted by
//     name), then per scenario its cookies and vars, and per leaf step the
//     request url, body, each body_json string value, each fields value,
//     each multipart file's filename (by field name), each header (sorted
//     by name) and the step's cookies.
//     Keys, field names and cookie and var names are not scanned, and
//     neither are ${expr: ...} bodies (see ScanTemplate);
//   - each run_once and each teardown scenario.
//
// A scenario's own load and start_at are not refused here. No current
// refusal depends on nodes; it is kept for callers.
//
// It returns nil for a nil or v1 definition. The result is not capped;
// callers that build a ValidationError bound it.
func CloudProblems(d *Definition, nodes int) []Problem {
	if d == nil || d.IsV1() {
		return nil
	}
	var ps []Problem
	if t := d.TLS; t != nil {
		for _, f := range [...]struct{ path, v string }{
			{"tls.client_cert", t.ClientCert},
			{"tls.client_key", t.ClientKey},
			{"tls.ca_cert", t.CACert},
		} {
			if f.v != "" && !IsInlinePEM(f.v) {
				ps = append(ps, Problem{Path: f.path, Message: cloudPathMessage})
			}
		}
	}
	for i, sc := range d.Scenarios {
		forEachLeaf(sc.Steps, indexPath("scenarios", i), func(sp string, st *Step) {
			rp := sp + ".request"
			r := st.Request
			if r.BodyBase64 != "" {
				ps = append(ps, Problem{Path: rp + ".body_base64", Message: cloudBinaryMessage})
			}
			for _, name := range slices.Sorted(maps.Keys(r.Files)) {
				fp, f := joinPath(rp+".files", name), r.Files[name]
				if f.Path != "" {
					ps = append(ps, Problem{Path: fp + ".path", Message: cloudBinaryMessage})
				}
				if f.ContentBase64 != "" {
					ps = append(ps, Problem{Path: fp + ".content_base64", Message: cloudBinaryMessage})
				}
			}
		})
	}
	forEachTemplate(d, func(path, src string) { ps = appendEnvRefs(ps, path, src) })
	for i, sc := range d.Scenarios {
		sp := indexPath("scenarios", i)
		if sc.RunOnce {
			ps = append(ps, Problem{Path: sp + ".run_once", Message: "run_once" + cloudSetupMessage})
		}
		if sc.Teardown {
			ps = append(ps, Problem{Path: sp + ".teardown", Message: "teardown" + cloudSetupMessage})
		}
	}
	return ps
}

// forEachTemplate calls fn with each template value of d and its path, in
// the order CloudProblems lists ${env.*} references.
func forEachTemplate(d *Definition, fn func(path, src string)) {
	if d.Target != nil {
		fn("target.base_url", d.Target.BaseURL)
	}
	forEachMapValue("cookies", d.Cookies, fn)
	forEachMapValue("vars", d.Vars, fn)
	for i, sc := range d.Scenarios {
		sp := indexPath("scenarios", i)
		forEachMapValue(sp+".cookies", sc.Cookies, fn)
		forEachMapValue(sp+".vars", sc.Vars, fn)
		forEachLeaf(sc.Steps, sp, func(stp string, st *Step) {
			rp := stp + ".request"
			r := st.Request
			fn(rp+".url", r.URL)
			fn(rp+".body", r.Body)
			if len(r.BodyJSON) > 0 {
				// Parse has checked body_json; a decode error ends the walk.
				_ = forEachJSONString(r.BodyJSON, rp+".body_json", fn)
			}
			for _, f := range r.Fields {
				fn(joinPath(rp+".fields", f.Name), f.Value)
			}
			for _, name := range slices.Sorted(maps.Keys(r.Files)) {
				fn(joinPath(rp+".files", name)+".filename", r.Files[name].Filename)
			}
			for _, name := range slices.Sorted(maps.Keys(r.Headers)) {
				hp, vs := joinPath(rp+".headers", name), r.Headers[name]
				if len(vs) == 1 {
					fn(hp, vs[0])
					continue
				}
				for k, v := range vs {
					fn(indexPath(hp, k), v)
				}
			}
			forEachMapValue(stp+".cookies", st.Cookies, fn)
		})
	}
}

// forEachMapValue calls fn with each value of m and its path under p, by
// sorted key.
func forEachMapValue(p string, m map[string]string, fn func(path, src string)) {
	for _, k := range slices.Sorted(maps.Keys(m)) {
		fn(joinPath(p, k), m[k])
	}
}

// appendEnvRefs appends a problem at path for each distinct ${env.*}
// reference in src, as written and in source order. ${expr: ...} bodies
// are not scanned; after an expression ScanTemplate cannot read, the rest
// of src is not scanned either.
func appendEnvRefs(ps []Problem, path, src string) []Problem {
	var seen map[string]bool
	_ = scanTemplate(src, func(r TemplateRef) {
		if ref := src[r.Start:r.End]; !r.Expr && strings.HasPrefix(ref[2:], "env.") && !seen[ref] {
			if seen == nil {
				seen = map[string]bool{}
			}
			seen[ref] = true
			ps = append(ps, Problem{Path: path, Message: ref + cloudEnvMessage})
		}
	})
	return ps
}

// forEachJSONString calls fn with each string value in body, a JSON object
// or array, and its path under root (root.a, root.list[0], root["a b"]), in
// document order. Object keys are not visited. It stops at the end of the
// top-level value.
func forEachJSONString(body []byte, root string, fn func(path, s string)) error {
	type frame struct {
		obj  bool
		n    int    // elements so far (arrays)
		path string // the container's path
		cur  string // the current member's path (objects)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var stack []frame
	wantKey := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok && (d == '}' || d == ']') {
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return nil
			}
			wantKey = stack[len(stack)-1].obj
			continue
		}
		path := root
		if len(stack) == 0 {
			if d, ok := tok.(json.Delim); !ok || (d != '{' && d != '[') {
				return errJSONBody
			}
		} else {
			top := &stack[len(stack)-1]
			switch {
			case top.obj && wantKey:
				k, _ := tok.(string)
				top.cur = joinPath(top.path, k)
				wantKey = false
				continue
			case top.obj:
				path = top.cur
			default:
				path = indexPath(top.path, top.n)
				top.n++
			}
		}
		if d, ok := tok.(json.Delim); ok {
			stack = append(stack, frame{obj: d == '{', path: path})
			wantKey = d == '{'
			continue
		}
		if s, ok := tok.(string); ok {
			fn(path, s)
		}
		wantKey = stack[len(stack)-1].obj
	}
}
