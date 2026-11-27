package definition_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

const (
	cloudPEMPath  = "must be inline PEM on VoltTest Cloud (a file path cannot be read on a cloud node)"
	cloudRunOnce  = "run_once scenarios are not supported on VoltTest Cloud yet; run them with the headless engine"
	cloudTeardown = "teardown scenarios are not supported on VoltTest Cloud yet; run them with the headless engine"
)

// missingEnv is the problem for an ${env.*} reference ref at path that the
// run supplies no value for.
func missingEnv(path, ref string) definition.Problem {
	return definition.Problem{Path: path, Message: ref + " has no value; supply it in the run's env"}
}

// wantMissingEnv checks MissingEnvProblems(d, have) against want, and that
// CloudProblems reports no ${env.*} problem for the same definition.
func wantMissingEnv(t *testing.T, data []byte, have map[string]bool, want []definition.Problem) {
	t.Helper()
	d := mustParse(t, data)
	if got := definition.MissingEnvProblems(d, have); !reflect.DeepEqual(got, want) {
		t.Errorf("MissingEnvProblems =\n%s\nwant\n%s", cloudProblemLines(got), cloudProblemLines(want))
	}
	for _, cp := range definition.CloudProblems(d, 1) {
		if strings.Contains(cp.Message, "${env.") {
			t.Errorf("CloudProblems still reports an ${env.*} reference: %s", cp)
		}
	}
}

// cloudStepsDoc is a 2.0 definition with target and one scenario of steps.
func cloudStepsDoc(target, steps string) []byte {
	return []byte(`{"schema_version": "2.0", "name": "cloud", "virtual_users": 1, "target": ` + target + `,
		"scenarios": [{"name": "s", "steps": [` + steps + `]}]}`)
}

func wantCloudProblems(t *testing.T, data []byte, nodes int, want []definition.Problem) {
	t.Helper()
	got := definition.CloudProblems(mustParse(t, data), nodes)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CloudProblems(%d) =\n%s\nwant\n%s", nodes, cloudProblemLines(got), cloudProblemLines(want))
	}
}

func cloudProblemLines(ps []definition.Problem) string {
	if ps == nil {
		return "  nil"
	}
	var b strings.Builder
	for _, p := range ps {
		b.WriteString("  " + p.Path + ": " + p.Message + "\n")
	}
	return b.String()
}

// Every ${env.*} reference with no value is reported at each path where it
// is written: one problem per distinct reference per path, an invalid name
// included, none for an unterminated ${env.X or for ${envA} and ${env}.
// Headers are scanned by sorted name.
func TestMissingEnvProblems(t *testing.T) {
	data := cloudStepsDoc(`{"base_url": "https://api.test/${env.A}"}`, `
		{"name": "a", "request": {"method": "POST", "url": "/a/${env.A}/${uuid}", "body": "${env.B}${env.B}${env.-}${env.",
			"headers": {"Content-Type": "text/plain", "Z": "${env.}", "A": "${env.A B}x${env.C"}}},
		{"name": "b", "request": {"method": "GET", "url": "/b/${env.A}"}},
		{"name": "c", "request": {"method": "GET", "url": "/c/${env.X", "headers": {"X": "${envA} ${env}"}}}`)
	wantMissingEnv(t, data, nil, []definition.Problem{
		missingEnv("target.base_url", "${env.A}"),
		missingEnv("scenarios[0].steps[0].request.url", "${env.A}"),
		missingEnv("scenarios[0].steps[0].request.body", "${env.B}"),
		missingEnv("scenarios[0].steps[0].request.body", "${env.-}"),
		missingEnv("scenarios[0].steps[0].request.headers.A", "${env.A B}"),
		missingEnv("scenarios[0].steps[0].request.headers.Z", "${env.}"),
		missingEnv("scenarios[0].steps[1].request.url", "${env.A}"),
	})
}

// A reference with filters is reported as written, filters and all, and a
// filtered reference is distinct from the plain one.
func TestMissingEnvProblemsFilters(t *testing.T) {
	data := cloudStepsDoc(`{"base_url": "https://api.test/${env.B|upper}"}`, `
		{"name": "a", "request": {"method": "POST", "url": "/a?x=${env.X|nope}", "body": "${env.X|default:\"y\"}${env.X}${env.X|default:\"y\"}",
			"headers": {"Content-Type": "text/plain", "X-U": "${env.U|upper}"}}}`)
	wantMissingEnv(t, data, nil, []definition.Problem{
		missingEnv("target.base_url", "${env.B|upper}"),
		missingEnv("scenarios[0].steps[0].request.url", "${env.X|nope}"),
		missingEnv("scenarios[0].steps[0].request.body", `${env.X|default:"y"}`),
		missingEnv("scenarios[0].steps[0].request.body", "${env.X}"),
		missingEnv("scenarios[0].steps[0].request.headers.X-U", "${env.U|upper}"),
	})
}

// In a typed body each body_json string value and each fields value is
// reported at its own path. Keys and field names are sent as written, so a
// reference in one is not reported.
func TestMissingEnvProblemsTypedBodies(t *testing.T) {
	data := cloudStepsDoc(`{"base_url": "https://api.test"}`, `
		{"name": "j", "request": {"method": "POST", "url": "/j", "body_type": "json",
			"body_json": {"t": "${env.TOKEN}", "n": {"${env.KEY}": "x ${env.TOKEN|upper}"}, "k": 1, "u": "${env.ESC}"}}},
		{"name": "f", "request": {"method": "POST", "url": "/f", "body_type": "form",
			"fields": {"${env.NAME}": "1", "a b": "${env.FORM}", "plain": "${env.FORM}"}}},
		{"name": "l", "request": {"method": "POST", "url": "/l", "body_type": "json",
			"body_json": ["${env.A}", 1, true, null, {"x": ["${env.B}", "${env.B}"]}, ["${env.C}"]]}}`)
	wantMissingEnv(t, data, nil, []definition.Problem{
		missingEnv("scenarios[0].steps[0].request.body_json.t", "${env.TOKEN}"),
		missingEnv(`scenarios[0].steps[0].request.body_json.n["${env.KEY}"]`, "${env.TOKEN|upper}"),
		missingEnv("scenarios[0].steps[0].request.body_json.u", "${env.ESC}"),
		missingEnv(`scenarios[0].steps[1].request.fields["a b"]`, "${env.FORM}"),
		missingEnv("scenarios[0].steps[1].request.fields.plain", "${env.FORM}"),
		missingEnv("scenarios[0].steps[2].request.body_json[0]", "${env.A}"),
		missingEnv("scenarios[0].steps[2].request.body_json[4].x[0]", "${env.B}"),
		missingEnv("scenarios[0].steps[2].request.body_json[4].x[1]", "${env.B}"),
		missingEnv("scenarios[0].steps[2].request.body_json[5][0]", "${env.C}"),
	})
}

// Paths use the contract's key style: a name that is not a plain key, or
// is longer than 128 bytes, is quoted and cut. Plain names give the same
// path as a dotted join. Headers are sorted by name as bytes.
func TestMissingEnvProblemsKeyPaths(t *testing.T) {
	long := strings.Repeat("h", 129)
	cut := `["` + strings.Repeat("h", 128) + `..."]`
	data := cloudStepsDoc(`{"base_url": "https://api.test"}`, `
		{"name": "h", "request": {"method": "GET", "url": "/h",
			"headers": {"`+long+`": "${env.LONG}", "X Y": "${env.SPACE}", "X-Plain_1": "${env.PLAIN}"}}},
		{"name": "j", "request": {"method": "POST", "url": "/j", "body_type": "json",
			"body_json": {"a.b": "${env.DOT}", "": "${env.EMPTY}", "`+long+`": {"ok": "${env.DEEP}"}}}},
		{"name": "f", "request": {"method": "POST", "url": "/f", "body_type": "form",
			"fields": {"q\"x": "${env.QUOTE}"}}}`)
	wantMissingEnv(t, data, nil, []definition.Problem{
		missingEnv(`scenarios[0].steps[0].request.headers["X Y"]`, "${env.SPACE}"),
		missingEnv("scenarios[0].steps[0].request.headers.X-Plain_1", "${env.PLAIN}"),
		missingEnv("scenarios[0].steps[0].request.headers"+cut, "${env.LONG}"),
		missingEnv(`scenarios[0].steps[1].request.body_json["a.b"]`, "${env.DOT}"),
		missingEnv(`scenarios[0].steps[1].request.body_json[""]`, "${env.EMPTY}"),
		missingEnv("scenarios[0].steps[1].request.body_json"+cut+".ok", "${env.DEEP}"),
		missingEnv(`scenarios[0].steps[2].request.fields["q\"x"]`, "${env.QUOTE}"),
	})
}

// A tls value given as a path is refused at its key, without the value;
// inline PEM is accepted.
func TestCloudProblemsTLSPaths(t *testing.T) {
	const certPath, keyPath, caPath = "certs/client-cert-path.pem", "keys/SECRETKEYPATHVALUE.pem", "certs/ca-path.pem"
	for _, tc := range []struct {
		name  string
		tls   map[string]any
		paths []string
	}{
		{"all paths", map[string]any{"client_cert": certPath, "client_key": keyPath, "ca_cert": caPath}, []string{"tls.client_cert", "tls.client_key", "tls.ca_cert"}},
		{"key path only", map[string]any{"client_cert": certPEM, "client_key": keyPath}, []string{"tls.client_key"}},
		{"ca path only", map[string]any{"ca_cert": caPath, "insecure_skip_verify": true}, []string{"tls.ca_cert"}},
		{"inline", map[string]any{"client_cert": certPEM, "client_key": pkcs8PEM, "ca_cert": certPEM}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var want []definition.Problem
			for _, p := range tc.paths {
				want = append(want, definition.Problem{Path: p, Message: cloudPEMPath})
			}
			got := definition.CloudProblems(mustParse(t, withTLS(t, tc.tls)), 1)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("CloudProblems =\n%s\nwant\n%s", cloudProblemLines(got), cloudProblemLines(want))
			}
			for _, p := range got {
				if strings.Contains(p.String(), "SECRETKEYPATHVALUE") {
					t.Errorf("problem echoes the client_key value: %s", p)
				}
			}
		})
	}
}

// run_once and teardown scenarios are refused at the flag, in definition
// order; a 2.0 definition without them has no problem.
func TestCloudProblemsSetupTeardown(t *testing.T) {
	const step = `"steps": [{"name": "a", "request": {"method": "GET", "url": "https://x.test/a"}}]`
	doc := func(scenarios ...string) []byte {
		return []byte(`{"schema_version": "2.0", "name": "t", "virtual_users": 1, "duration": "1s", "scenarios": [` +
			strings.Join(scenarios, ",") + `]}`)
	}
	wantCloudProblems(t, doc(
		`{"name": "setup", "run_once": true, `+step+`}`,
		`{"name": "load", `+step+`}`,
		`{"name": "cleanup", "teardown": true, `+step+`}`,
	), 1, []definition.Problem{
		{Path: "scenarios[0].run_once", Message: cloudRunOnce},
		{Path: "scenarios[2].teardown", Message: cloudTeardown},
	})
	for _, tc := range []struct{ flag, path, msg string }{
		{`"run_once": true`, "scenarios[0].run_once", cloudRunOnce},
		{`"teardown": true`, "scenarios[0].teardown", cloudTeardown},
	} {
		wantCloudProblems(t, doc(`{"name": "x", `+tc.flag+`, `+step+`}`, `{"name": "s", `+step+`}`), 1,
			[]definition.Problem{{Path: tc.path, Message: tc.msg}})
	}
	wantCloudProblems(t, doc(`{"name": "s", `+step+`}`), 1, nil)
}

// No load model is refused on any node count.
func TestCloudProblemsNodes(t *testing.T) {
	const scenarios = `"scenarios": [{"name": "s", "steps": [{"name": "a", "request": {"method": "GET", "url": "https://x.test"}}]}]`
	rate := []byte(`{"schema_version": "2.0", "name": "t", "load": {"type": "rate", "rate": 5, "duration": "1s", "preallocated_virtual_users": 1}, ` + scenarios + `}`)
	ramping := []byte(`{"schema_version": "2.0", "name": "t", "load": {"type": "ramping_rate", "stages": [{"duration": "1s", "target": 5}], "preallocated_virtual_users": 1}, ` + scenarios + `}`)
	constant := []byte(`{"schema_version": "2.0", "name": "t", "virtual_users": 1, "duration": "1s", ` + scenarios + `}`)
	staged := []byte(`{"schema_version": "2.0", "name": "t", "stages": [{"duration": "1s", "target": 2}], ` + scenarios + `}`)
	for _, n := range []int{-1, 0, 1, 2, 3, 100} {
		wantCloudProblems(t, rate, n, nil)
		wantCloudProblems(t, ramping, n, nil)
		wantCloudProblems(t, constant, n, nil)
		wantCloudProblems(t, staged, n, nil)
	}
}

// Thresholds are not cloud policy: bytes_sent and bytes_received are
// accepted here, whatever a given engine release measures.
func TestCloudProblemsThresholds(t *testing.T) {
	data := []byte(`{"schema_version": "2.0", "name": "t", "virtual_users": 1, "duration": "1s",
		"thresholds": ["bytes_sent < 1mb", {"expr": "bytes_received < 10mb", "abort_on_fail": true}, "p95 < 500ms"],
		"scenarios": [{"name": "s", "steps": [{"name": "a", "request": {"method": "GET", "url": "https://x.test"}}]}]}`)
	wantCloudProblems(t, data, 3, nil)
}

// The refusals come in a fixed order: tls, then run_once and teardown.
// ${env.*} references are MissingEnvProblems', in their own order: the
// base_url first, then the steps.
func TestCloudProblemsOrder(t *testing.T) {
	data := []byte(`{"schema_version": "2.0", "name": "t",
		"load": {"type": "rate", "rate": 5, "duration": "1s", "preallocated_virtual_users": 1},
		"tls": {"client_cert": "c.pem", "client_key": "k.pem"},
		"target": {"base_url": "https://x.test/${env.BASE}"},
		"scenarios": [
			{"name": "setup", "run_once": true, "steps": [{"name": "a", "request": {"method": "GET", "url": "/s?k=${env.S}"}}]},
			{"name": "load", "steps": [{"name": "a", "request": {"method": "GET", "url": "/l",
				"headers": {"B": "${env.HB}", "A": "${env.HA}"}}}]},
			{"name": "cleanup", "teardown": true, "steps": [{"name": "a", "request": {"method": "GET", "url": "/t"}}]}]}`)
	wantCloudProblems(t, data, 2, []definition.Problem{
		{Path: "tls.client_cert", Message: cloudPEMPath},
		{Path: "tls.client_key", Message: cloudPEMPath},
		{Path: "scenarios[0].run_once", Message: cloudRunOnce},
		{Path: "scenarios[2].teardown", Message: cloudTeardown},
	})
	wantMissingEnv(t, data, nil, []definition.Problem{
		missingEnv("target.base_url", "${env.BASE}"),
		missingEnv("scenarios[0].steps[0].request.url", "${env.S}"),
		missingEnv("scenarios[1].steps[0].request.headers.A", "${env.HA}"),
		missingEnv("scenarios[1].steps[0].request.headers.B", "${env.HB}"),
	})
}

// v1 has no cloud refusals and no env problems: nil for a v1 definition
// with ${env.X}, tls-like keys, run_once and load (both dropped unread),
// on any node count, and for a nil definition.
func TestCloudProblemsV1(t *testing.T) {
	for _, version := range []string{``, `"schema_version": 1, `, `"schema_version": "1", `} {
		data := []byte(`{` + version + `"name": "t", "virtual_users": 1, "duration": "1s",
			"target": {"url": "https://x.test/${env.X}", "idle_timeout": "1s"},
			"load": {"type": "rate", "rate": 5, "duration": "1s", "preallocated_virtual_users": 1},
			"scenarios": [{"name": "s", "run_once": true, "steps": [{"name": "a", "request": {"method": "POST",
				"url": "https://x.test/${env.X}", "body": "${env.Y}", "header": {"A": "${env.Z}"}, "headers": {"B": "${env.Z}"}}}]}]}`)
		d := mustParse(t, data)
		if !d.IsV1() {
			t.Fatalf("%q: not v1", version)
		}
		for _, n := range []int{0, 1, 2} {
			if got := definition.CloudProblems(d, n); got != nil {
				t.Errorf("%q: CloudProblems(v1, %d) = %v, want nil", version, n, got)
			}
		}
		if got := definition.MissingEnvProblems(d, nil); got != nil {
			t.Errorf("%q: MissingEnvProblems(v1) = %v, want nil", version, got)
		}
	}
	if got := definition.CloudProblems(nil, 2); got != nil {
		t.Errorf("CloudProblems(nil) = %v, want nil", got)
	}
	if got := definition.MissingEnvProblems(nil, nil); got != nil {
		t.Errorf("MissingEnvProblems(nil) = %v, want nil", got)
	}
}

// A hand-built definition is scanned the same way, and a body_json that is
// not an object or array is skipped, not reported or walked.
func TestMissingEnvProblemsBuilt(t *testing.T) {
	v2 := definition.SchemaVersion{Major: 2}
	d := &definition.Definition{
		SchemaVersion: &v2,
		Scenarios: []definition.Scenario{{Steps: []definition.Step{
			{Request: definition.Request{URL: "${env.A}", BodyJSON: definition.JSONBody(`"${env.B}"`)}},
			{Request: definition.Request{BodyJSON: definition.JSONBody(`{"a": "${env.C}"`)}},
		}}},
	}
	want := []definition.Problem{
		missingEnv("scenarios[0].steps[0].request.url", "${env.A}"),
		missingEnv("scenarios[0].steps[1].request.body_json.a", "${env.C}"),
	}
	if got := definition.MissingEnvProblems(d, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("MissingEnvProblems =\n%s\nwant\n%s", cloudProblemLines(got), cloudProblemLines(want))
	}
	if got := definition.CloudProblems(d, 1); got != nil {
		t.Errorf("CloudProblems =\n%s\nwant nil", cloudProblemLines(got))
	}
}

// Many references in one value cost one problem each, found in one pass.
func TestMissingEnvProblemsManyRefs(t *testing.T) {
	var b strings.Builder
	const n = 5000
	for i := range n {
		b.WriteString("${env.V" + strconv.Itoa(i%(n/2)) + "}")
	}
	data := cloudStepsDoc(`{"base_url": "https://api.test"}`,
		`{"name": "a", "request": {"method": "POST", "url": "/a", "body": "`+b.String()+`"}}`)
	got := definition.MissingEnvProblems(mustParse(t, data), nil)
	if len(got) != n/2 {
		t.Fatalf("%d problems, want %d (one per distinct reference)", len(got), n/2)
	}
	if got[n/2-1].Message != "${env.V2499} has no value; supply it in the run's env" {
		t.Errorf("last problem %s", got[n/2-1])
	}
}

const cloudBinary = "is not available on VoltTest Cloud yet; run this test headless"

// Binary request content is refused on VoltTest Cloud at each value: the
// request body_base64, then each file's path or content_base64 by field
// name. Multipart fields alone are allowed. The message does not point to
// content_base64 as a way around a path.
func TestCloudProblemsInlineBinary(t *testing.T) {
	const rp = "scenarios[0].steps[0].request"
	for _, tc := range []struct {
		fixture string
		want    []definition.Problem
	}{
		{"cloud_multipart_path.json", []definition.Problem{{Path: rp + ".files.doc.path", Message: cloudBinary}}},
		{"cloud_multipart_content.json", []definition.Problem{{Path: rp + ".files.doc.content_base64", Message: cloudBinary}}},
		{"cloud_body_base64.json", []definition.Problem{{Path: rp + ".body_base64", Message: cloudBinary}}},
		{"cloud_multipart_fields.json", nil},
		{"multipart.json", []definition.Problem{
			{Path: rp + ".files.avatar.content_base64", Message: cloudBinary},
			{Path: rp + ".files.report.path", Message: cloudBinary},
			{Path: "scenarios[0].steps[1].request.body_base64", Message: cloudBinary},
		}},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			wantCloudProblems(t, readFile(t, "../testdata/v2/valid/"+tc.fixture), 1, tc.want)
		})
	}
	for _, p := range definition.CloudProblems(mustParse(t, readFile(t, "../testdata/v2/valid/multipart.json")), 1) {
		if strings.Contains(p.Message, "content_base64") || strings.Contains(p.Message, "PEM") {
			t.Errorf("%s: the message suggests a cloud form", p)
		}
	}
}

// Binary content is refused step by step, and file names are quoted like
// other keys. A file's filename is scanned for ${env.*} after the fields
// and before the headers, by field name.
func TestCloudProblemsInlineBinaryOrder(t *testing.T) {
	data := cloudStepsDoc(`{"base_url": "https://api.test"}`, `
		{"name": "a", "request": {"method": "POST", "url": "/a/${env.A}", "body_base64": "AAAA"}},
		{"name": "b", "request": {"method": "POST", "url": "/b", "body_type": "multipart", "headers": {"X": "${env.H}"},
			"fields": {"t": "${env.T}"}, "files": {"z": {"path": "z.bin", "filename": "${env.Z}"}, "a b": {"content_base64": "AAAA", "filename": "${env.F}"}}}}`)
	wantCloudProblems(t, data, 1, []definition.Problem{
		{Path: "scenarios[0].steps[0].request.body_base64", Message: cloudBinary},
		{Path: `scenarios[0].steps[1].request.files["a b"].content_base64`, Message: cloudBinary},
		{Path: "scenarios[0].steps[1].request.files.z.path", Message: cloudBinary},
	})
	wantMissingEnv(t, data, nil, []definition.Problem{
		missingEnv("scenarios[0].steps[0].request.url", "${env.A}"),
		missingEnv("scenarios[0].steps[1].request.fields.t", "${env.T}"),
		missingEnv(`scenarios[0].steps[1].request.files["a b"].filename`, "${env.F}"),
		missingEnv("scenarios[0].steps[1].request.files.z.filename", "${env.Z}"),
		missingEnv("scenarios[0].steps[1].request.headers.X", "${env.H}"),
	})
}

// Every value of a header is scanned for ${env.*}: one value at the
// header's path, several at their index.
func TestMissingEnvProblemsHeaderValues(t *testing.T) {
	data := cloudStepsDoc(`{"base_url": "https://api.test"}`, `
		{"name": "a", "request": {"method": "GET", "url": "/a",
			"headers": {"Host": ["${env.HOST}"], "X-A": ["ok", "${env.A}", "${env.A}"], "X-B": "${env.B}"}}}`)
	wantMissingEnv(t, data, nil, []definition.Problem{
		missingEnv("scenarios[0].steps[0].request.headers.Host", "${env.HOST}"),
		missingEnv("scenarios[0].steps[0].request.headers.X-A[1]", "${env.A}"),
		missingEnv("scenarios[0].steps[0].request.headers.X-A[2]", "${env.A}"),
		missingEnv("scenarios[0].steps[0].request.headers.X-B", "${env.B}"),
	})
}

// have holds names: a reference is reported unless its name, the text
// after "env." up to its first filter "|", is in have. A nil or empty have
// reports every reference, and an entry set to false counts as missing.
func TestMissingEnvProblemsHave(t *testing.T) {
	data := cloudStepsDoc(`{"base_url": "https://api.test/${env.A}"}`, `
		{"name": "a", "request": {"method": "POST", "url": "/a/${env.B}", "body": "${env.B|upper}${env.C}",
			"headers": {"Content-Type": "text/plain"}}}`)
	const rp = "scenarios[0].steps[0].request"
	all := []definition.Problem{
		missingEnv("target.base_url", "${env.A}"),
		missingEnv(rp+".url", "${env.B}"),
		missingEnv(rp+".body", "${env.B|upper}"),
		missingEnv(rp+".body", "${env.C}"),
	}
	for _, tc := range []struct {
		name string
		have map[string]bool
		want []definition.Problem
	}{
		{"nil", nil, all},
		{"empty", map[string]bool{}, all},
		{"false is no value", map[string]bool{"A": false, "B": false, "C": false}, all},
		{"another name", map[string]bool{"D": true}, all},
		{"a name covers its filtered form", map[string]bool{"B": true}, []definition.Problem{all[0], all[3]}},
		{"the filters are not part of the name", map[string]bool{"B|upper": true}, all},
		{"partial", map[string]bool{"A": true, "C": true}, []definition.Problem{all[1], all[2]}},
		{"all", map[string]bool{"A": true, "B": true, "C": true}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) { wantMissingEnv(t, data, tc.have, tc.want) })
	}
}

// A problem carries the reference as written and nothing else: the run's
// values never reach this package, and the template around a reference is
// not echoed either.
func TestMissingEnvProblemsCarryNoValue(t *testing.T) {
	const around = "TEMPLATETEXT-NOT-A-VALUE"
	data := cloudStepsDoc(`{"base_url": "https://api.test/`+around+`/${env.A}"}`, `
		{"name": "a", "request": {"method": "POST", "url": "/a?k=`+around+`&v=${env.B}", "body": "`+around+`${env.C}",
			"headers": {"Content-Type": "text/plain", "X-K": "`+around+` ${env.A}"}}}`)
	d := mustParse(t, data)
	refs := map[string]bool{"${env.A}": true, "${env.B}": true, "${env.C}": true}
	for _, have := range []map[string]bool{nil, {"A": true}, {"A": true, "B": true, "C": true}} {
		ps := definition.MissingEnvProblems(d, have)
		if len(have) == 0 && len(ps) != 4 {
			t.Fatalf("%d problems, want 4", len(ps))
		}
		for _, p := range ps {
			if strings.Contains(p.String(), around) {
				t.Errorf("the problem echoes the template around the reference: %s", p)
			}
			ref, _, _ := strings.Cut(p.Message, " ")
			if !refs[ref] || p.Message != ref+" has no value; supply it in the run's env" {
				t.Errorf("the message is not the reference and the remedy: %s", p)
			}
		}
	}
}

// A ${env.*} in a condition or a check value is literal text (Check and
// the expression rules keep it as written), so it needs no value and
// neither function reports it.
func TestMissingEnvProblemsLiteralInConditions(t *testing.T) {
	d := mustParse(t, readFile(t, "../testdata/v2/valid/cloud_env_literal.json"))
	if got := definition.MissingEnvProblems(d, nil); got != nil {
		t.Errorf("MissingEnvProblems =\n%s\nwant nil", cloudProblemLines(got))
	}
	if got := definition.CloudProblems(d, 1); got != nil {
		t.Errorf("CloudProblems =\n%s\nwant nil", cloudProblemLines(got))
	}
}
