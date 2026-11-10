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
//   - binary request content, per step: the request body_base64, then each
//     multipart file's path or content_base64, by field name. Cloud
//     nodes cannot receive file content yet;
//   - each distinct ${env.*} reference at each path where it is written:
//     target.base_url, then per step the request url, body, each body_json
//     string value, each fields value, each multipart file's filename (by
//     field name) and each header (sorted by name).
//     Keys and field names are sent as written, so they are not scanned;
//   - each run_once and each teardown scenario.
//
// No current refusal depends on nodes; it is kept for callers.
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
		for j, st := range sc.Steps {
			rp := indexPath(indexPath("scenarios", i)+".steps", j) + ".request"
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
		}
	}
	ps = cloudEnvProblems(ps, d)
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

func cloudEnvProblems(ps []Problem, d *Definition) []Problem {
	if d.Target != nil {
		ps = appendEnvRefs(ps, "target.base_url", d.Target.BaseURL)
	}
	for i, sc := range d.Scenarios {
		for j, st := range sc.Steps {
			rp := indexPath(indexPath("scenarios", i)+".steps", j) + ".request"
			r := st.Request
			ps = appendEnvRefs(ps, rp+".url", r.URL)
			ps = appendEnvRefs(ps, rp+".body", r.Body)
			if len(r.BodyJSON) > 0 {
				// Parse has checked body_json; a decode error ends the walk.
				_ = forEachJSONString(r.BodyJSON, rp+".body_json", func(path, s string) {
					ps = appendEnvRefs(ps, path, s)
				})
			}
			for _, f := range r.Fields {
				ps = appendEnvRefs(ps, joinPath(rp+".fields", f.Name), f.Value)
			}
			for _, name := range slices.Sorted(maps.Keys(r.Files)) {
				ps = appendEnvRefs(ps, joinPath(rp+".files", name)+".filename", r.Files[name].Filename)
			}
			for _, name := range slices.Sorted(maps.Keys(r.Headers)) {
				// One value is reported at the header, as it marshals;
				// several at their index.
				hp, vs := joinPath(rp+".headers", name), r.Headers[name]
				if len(vs) == 1 {
					ps = appendEnvRefs(ps, hp, vs[0])
					continue
				}
				for k, v := range vs {
					ps = appendEnvRefs(ps, indexPath(hp, k), v)
				}
			}
		}
	}
	return ps
}

// appendEnvRefs appends a problem at path for each distinct ${env.*}
// reference in src, as written and in source order. A reference runs from
// ${ to the first }; without a } the rest of src is literal.
func appendEnvRefs(ps []Problem, path, src string) []Problem {
	var seen map[string]bool
	for i := 0; i < len(src); {
		open := strings.Index(src[i:], "${")
		if open < 0 {
			break
		}
		open += i
		end := strings.IndexByte(src[open+2:], '}')
		if end < 0 {
			break
		}
		end += open + 2
		if ref := src[open : end+1]; strings.HasPrefix(ref[2:], "env.") && !seen[ref] {
			if seen == nil {
				seen = map[string]bool{}
			}
			seen[ref] = true
			ps = append(ps, Problem{Path: path, Message: ref + cloudEnvMessage})
		}
		i = end + 1
	}
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
