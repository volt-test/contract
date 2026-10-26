package definition_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

// httpTLSFixtureProblems maps each invalid fixture for the http and tls
// options and the step redirect keys to the one problem Parse reports.
var httpTLSFixtureProblems = map[string]string{
	"structure_http_bad_version.json":                `http.version: must be one of "1.1", got "2"`,
	"structure_http_negative_max_redirects.json":     `http.max_redirects: must not be negative; to stop following redirects, set follow_redirects: false`,
	"structure_step_negative_max_redirects.json":     `scenarios[0].steps[0].max_redirects: must not be negative; to stop following redirects, set follow_redirects: false`,
	"structure_step_max_redirects_not_followed.json": `scenarios[0].steps[0].max_redirects: ` + noEffect,
	"structure_step_max_redirects_inherits_off.json": `scenarios[0].steps[0].max_redirects: ` + noEffect,
	"structure_tls_bad_min_version.json":             `tls.min_version: must be one of "1.0", "1.1", "1.2", "1.3", got "1.4"`,
	"structure_tls_key_without_cert.json":            `tls.client_cert: is required with client_key`,
	"structure_tls_cert_without_key.json":            `tls.client_key: is required with client_cert`,
	"structure_tls_cert_not_pem.json":                `tls.client_cert: ` + notPEM,
	"structure_tls_ca_wrong_block.json":              `tls.ca_cert: must contain a PEM CERTIFICATE block`,
	"structure_tls_blank_path.json":                  `tls.ca_cert: must not be blank`,
	"structure_tls_path_with_newline.json":           `tls.ca_cert: ` + pathOrPEM,
	"type_http_string.json":                          `http: must be an object`,
	"type_http_follow_redirects_string.json":         `http.follow_redirects: must be true or false`,
	"type_http_debug_string.json":                    `http_debug: must be true or false`,
	"type_tls_insecure_skip_verify_string.json":      `tls.insecure_skip_verify: must be true or false`,
	"type_tls_client_key_number.json":                `tls.client_key: must be a string`,
	"type_step_follow_redirects_null.json":           `scenarios[0].steps[0].follow_redirects: must not be null`,
}

const (
	noEffect  = "has no effect: redirects are not followed for this step; set follow_redirects: true on the step"
	notPEM    = "must be valid PEM: no complete -----BEGIN/-----END block found"
	pathOrPEM = "must be a file path or inline PEM with a -----BEGIN line"
	encrypted = "encrypted keys are not supported"
	needKey   = "must contain a PEM private key block"
	needCert  = "must contain a PEM CERTIFICATE block"
)

// httpTLSFixturePrefixes are the fixture name prefixes this file owns.
var httpTLSFixturePrefixes = []string{"structure_http_", "type_http_", "structure_tls_", "type_tls_", "structure_step_", "type_step_"}

// TestHTTPTLSFixtures checks that each invalid fixture for the http and
// tls options fails with exactly the problem it is named after, and that
// every such fixture is listed.
func TestHTTPTLSFixtures(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures(t, "v2/invalid") {
		name := filepath.Base(f)
		want, ok := httpTLSFixtureProblems[name]
		if !ok {
			for _, p := range httpTLSFixturePrefixes {
				if strings.HasPrefix(name, p) {
					t.Errorf("%s: no expected problem in httpTLSFixtureProblems", name)
				}
			}
			continue
		}
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			got := problemStrings(t, readFile(t, f))
			if len(got) != 1 || got[0] != want {
				t.Errorf("problems = %q, want [%q]", got, want)
			}
		})
	}
	for name := range httpTLSFixtureProblems {
		if !seen[name] {
			t.Errorf("missing fixture testdata/v2/invalid/%s", name)
		}
	}
}

// PEM test values. Private-key block types are built by concatenation and
// every body is a few bytes of base64, so no key, real or realistic, is in
// this public repository.
var privateKey = privateKeyType

func pemBlock(typ, headers, body string) string {
	return "-----BEGIN " + typ + "-----\n" + headers + body + "\n-----END " + typ + "-----\n"
}

var (
	certPEM     = pemBlock("CERTIFICATE", "", "AAAA")
	pkcs8PEM    = pemBlock(privateKey, "", "AAAA")
	rsaPEM      = pemBlock("RSA "+privateKey, "", "AAAA")
	ecPEM       = pemBlock("EC "+privateKey, "", "AAAA")
	ecParamsPEM = pemBlock("EC PARAMETERS", "", "AAAA") + ecPEM
	encPKCS8PEM = pemBlock("ENCRYPTED "+privateKey, "", "AAAA")
	// A legacy encrypted key: the block type is unchanged and the headers
	// say it is encrypted. pem.Decode reads headers up to a blank line.
	procTypePEM = pemBlock("RSA "+privateKey, "Proc-Type: 4,ENCRYPTED\nDEK-Info: AES-128-CBC,00\n\n", "AAAA")
	publicPEM   = pemBlock("PUBLIC KEY", "", "AAAA")
)

func TestIsInlinePEM(t *testing.T) {
	cases := map[string]bool{
		"":                             false,
		"certs/client.pem":             false,
		"/etc/ssl/ca.pem":              false,
		`C:\certs\ca.pem`:              false,
		"BEGIN CERTIFICATE":            false,
		certPEM:                        true,
		"   " + certPEM:                true,
		"\n\n" + certPEM:               true,
		"\ufeff" + certPEM:             true,
		"## CA bundle\n##\n" + certPEM: true,
		"-----BEGIN junk":              true,
	}
	for in, want := range cases {
		if got := definition.IsInlinePEM(in); got != want {
			t.Errorf("IsInlinePEM(%q) = %v, want %v", in, got, want)
		}
	}
}

// withTLS returns minimal.json with the given tls object.
func withTLS(t *testing.T, tls map[string]any) []byte {
	t.Helper()
	return v2doc(t, func(m map[string]any) { m["tls"] = tls })
}

// TestTLSPEMValues checks the path or inline PEM rules of client_cert,
// client_key and ca_cert. Each case sets one field; client_cert and
// client_key get a path partner so the pairing rule holds.
func TestTLSPEMValues(t *testing.T) {
	schema := compileSchema(t)
	cases := []struct {
		name, field, value string
		want               string // the message; "" when Parse accepts it
	}{
		{"key path", "client_key", "certs/client-key.pem", ""},
		{"key path with spaces", "client_key", "my certs/client key.pem", ""},
		{"key pkcs8", "client_key", pkcs8PEM, ""},
		{"key rsa", "client_key", rsaPEM, ""},
		{"key ec", "client_key", ecPEM, ""},
		{"key ec after parameters", "client_key", ecParamsPEM, ""},
		{"key with leading blank lines", "client_key", "\n  \n" + rsaPEM, ""},
		{"key is a certificate", "client_key", certPEM, needKey},
		{"key is a public key", "client_key", publicPEM, needKey},
		{"key encrypted pkcs8", "client_key", encPKCS8PEM, encrypted},
		{"key legacy encrypted", "client_key", procTypePEM, encrypted},
		{"key not pem", "client_key", "-----BEGIN junk", notPEM},
		{"key blank", "client_key", " \t", "must not be blank"},
		{"key path with newline", "client_key", "certs/client-key.pem\n", pathOrPEM},

		{"cert path", "client_cert", "certs/client.pem", ""},
		{"cert inline", "client_cert", certPEM, ""},
		{"cert chain", "client_cert", certPEM + certPEM, ""},
		{"cert with key", "client_cert", certPEM + rsaPEM, "must not contain a private key; put the key in client_key"},
		{"cert is a key", "client_cert", pkcs8PEM, "must not contain a private key; put the key in client_key"},
		{"cert is a public key", "client_cert", publicPEM, needCert},
		{"cert bad base64", "client_cert", "-----BEGIN CERTIFICATE-----\n!!\n-----END CERTIFICATE-----\n", notPEM},
		{"cert path with carriage return", "client_cert", "certs/client.pem\r", pathOrPEM},

		{"ca path", "ca_cert", "certs/ca.pem", ""},
		{"ca bundle with byte order mark", "ca_cert", "\ufeff\n" + certPEM, ""},
		// A byte order mark on the -----BEGIN line hides the block from the
		// PEM decoder: the value is inline (not a path) but not valid PEM.
		{"ca byte order mark on the begin line", "ca_cert", "\ufeff" + certPEM, notPEM},
		{"ca bundle with comment lines", "ca_cert", "##\n## Bundle of CA Root Certificates\n##\n\n" + certPEM + certPEM, ""},
		{"ca with key", "ca_cert", certPEM + ecPEM, "must not contain a private key"},
		{"ca public key", "ca_cert", publicPEM, needCert},
		{"ca two lines", "ca_cert", "certs/a.pem\ncerts/b.pem", pathOrPEM},
		{"ca blank", "ca_cert", "\n", "must not be blank"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tls := map[string]any{tc.field: tc.value}
			switch tc.field {
			case "client_key":
				tls["client_cert"] = "certs/client.pem"
			case "client_cert":
				tls["client_key"] = "certs/client-key.pem"
			}
			doc := withTLS(t, tls)
			got := problemStrings(t, doc)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("problems = %q, want none", got)
				}
				requireSchemaAccepts(t, schema, doc)
				return
			}
			if want := "tls." + tc.field + ": " + tc.want; len(got) != 1 || got[0] != want {
				t.Errorf("problems = %q, want [%q]", got, want)
			}
		})
	}
}

// TestTLSProblemsDoNotEchoValues checks that no problem about client_cert,
// client_key or ca_cert repeats the value or a part of it: the value can be
// a private key. Each value carries a marker that must not appear in the
// error.
func TestTLSProblemsDoNotEchoValues(t *testing.T) {
	const marker = "ZQX9"
	bad := []string{
		"certs/" + marker + ".pem\n",
		"certs/" + marker + ".pem\r\n",
		"-----BEGIN " + marker + "\n",
		"-----BEGIN CERTIFICATE-----\n" + marker + "!!\n-----END CERTIFICATE-----\n",
		pemBlock("CERTIFICATE", "", marker+"AAAA"),
		pemBlock(marker+" "+privateKey, "", marker+"AAAA"),
		pemBlock("RSA "+privateKey, "Proc-Type: 4,ENCRYPTED\nDEK-Info: AES-128-CBC,"+marker+"\n\n", marker+"AAAA"),
		pemBlock("ENCRYPTED "+privateKey, "", marker+"AAAA"),
		pemBlock(marker, "", marker+"AAAA"),
		pemBlock("PUBLIC KEY", "", marker+"AAAA"),
		certPEM + pemBlock("EC "+privateKey, "", marker+"AAAA"),
	}
	problemsSeen := 0
	for _, field := range []string{"client_cert", "client_key", "ca_cert"} {
		for _, v := range bad {
			// Alone, so cert/key pairing problems are covered too.
			for _, tls := range []map[string]any{{field: v}, {field: v, "client_cert": "c.pem", "client_key": "k.pem"}} {
				tls[field] = v
				_, err := definition.Parse(withTLS(t, tls))
				var ve *definition.ValidationError
				if !errors.As(err, &ve) {
					continue
				}
				texts := []string{err.Error()}
				for _, p := range ve.Problems {
					texts = append(texts, p.String(), p.Message)
					if strings.HasPrefix(p.Path, "tls.") {
						problemsSeen++
					}
				}
				for _, s := range texts {
					if strings.Contains(s, marker) || strings.Contains(s, v) {
						t.Errorf("%s = %q: problem repeats the value: %s", field, v, s)
					}
				}
			}
		}
	}
	if problemsSeen == 0 {
		t.Fatal("no problems reported; the test is not exercising the rules")
	}
	// Every message the PEM rules can produce was seen in a problem above
	// or in TestTLSPEMValues; spot-check the newline one here.
	got := problemStrings(t, withTLS(t, map[string]any{"ca_cert": "certs/" + marker + ".pem\n"}))
	if want := []string{"tls.ca_cert: " + pathOrPEM}; !reflect.DeepEqual(got, want) {
		t.Errorf("problems = %q, want %q", got, want)
	}
}

// TestRedirectRules checks the test-level and step-level redirect keys
// beyond the one-problem fixtures.
func TestRedirectRules(t *testing.T) {
	schema := compileSchema(t)
	const sp = "scenarios[0].steps[0]"
	cases := []struct {
		name string
		http map[string]any // nil: no http object
		step map[string]any
		want []string
	}{
		{"defaults", nil, nil, nil},
		{"empty http object", map[string]any{}, nil, nil},
		{"step max with the default", nil, map[string]any{"max_redirects": 3}, nil},
		{"step max with test-level follow true", map[string]any{"follow_redirects": true}, map[string]any{"max_redirects": 3}, nil},
		{"step opts back in", map[string]any{"follow_redirects": false}, map[string]any{"follow_redirects": true, "max_redirects": 3}, nil},
		{"test-level max with follow off", map[string]any{"follow_redirects": false, "max_redirects": 4}, nil, nil},
		{"step follow off, zero max", nil, map[string]any{"follow_redirects": false, "max_redirects": 0}, nil},
		{"zero test-level max", map[string]any{"max_redirects": 0, "version": ""}, nil, nil},
		{"step follow off, max", nil, map[string]any{"follow_redirects": false, "max_redirects": 1},
			[]string{sp + ".max_redirects: " + noEffect}},
		{"step inherits test-level off", map[string]any{"follow_redirects": false}, map[string]any{"max_redirects": 2},
			[]string{sp + ".max_redirects: " + noEffect}},
		{"negative step max with follow off: one problem", nil, map[string]any{"follow_redirects": false, "max_redirects": -1},
			[]string{sp + ".max_redirects: must not be negative; to stop following redirects, set follow_redirects: false"}},
		{"negative at both levels", map[string]any{"max_redirects": -1}, map[string]any{"max_redirects": -1}, []string{
			"http.max_redirects: must not be negative; to stop following redirects, set follow_redirects: false",
			sp + ".max_redirects: must not be negative; to stop following redirects, set follow_redirects: false",
		}},
		{"version 2", map[string]any{"version": "2"}, nil, []string{`http.version: must be one of "1.1", got "2"`}},
		{"version 1.0", map[string]any{"version": "1.0"}, nil, []string{`http.version: must be one of "1.1", got "1.0"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := v2doc(t, func(m map[string]any) {
				if tc.http != nil {
					m["http"] = tc.http
				}
				for k, v := range tc.step {
					step0(m)[k] = v
				}
			})
			got := problemStrings(t, doc)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("problems:\n got %q\nwant %q", got, tc.want)
			}
			if tc.want == nil {
				requireSchemaAccepts(t, schema, doc)
			}
		})
	}
}

// TestHTTPTLSEmptyObjects: "http": {} and "tls": {} are accepted, set
// nothing, and are written back as empty objects.
func TestHTTPTLSEmptyObjects(t *testing.T) {
	doc := v2doc(t, func(m map[string]any) { m["http"] = map[string]any{}; m["tls"] = map[string]any{} })
	requireSchemaAccepts(t, compileSchema(t), doc)
	d := mustParse(t, doc)
	if d.HTTP == nil || *d.HTTP != (definition.HTTPOptions{}) || d.TLS == nil || *d.TLS != (definition.TLSOptions{}) {
		t.Fatalf("HTTP %+v, TLS %+v; want both empty and non-nil", d.HTTP, d.TLS)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"http":{}`) || !strings.Contains(string(b), `"tls":{}`) {
		t.Errorf("Marshal = %s, want empty http and tls objects", b)
	}
	if d2 := mustParse(t, b); !reflect.DeepEqual(d, d2) {
		t.Errorf("Parse(Marshal(d)) != d")
	}
}

// TestFollowRedirectsFalseSurvives: an explicit false at either level is
// kept apart from absent through Marshal and Parse.
func TestFollowRedirectsFalseSurvives(t *testing.T) {
	d := mustParse(t, readFile(t, "../testdata/v2/valid/http_tls.json"))
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	d2 := mustParse(t, b)
	h, steps := d2.HTTP, d2.Scenarios[0].Steps
	if h == nil || h.FollowRedirects == nil || *h.FollowRedirects {
		t.Errorf("http.follow_redirects = %v, want explicit false", h)
	}
	if f := steps[0].FollowRedirects; f == nil || !*f || steps[0].MaxRedirects != 3 {
		t.Errorf("steps[0] = %+v, want follow_redirects true, max_redirects 3", steps[0])
	}
	if f := steps[1].FollowRedirects; f == nil || *f {
		t.Errorf("steps[1].follow_redirects = %v, want explicit false", f)
	}
	if steps[2].FollowRedirects != nil || steps[2].MaxRedirects != 0 {
		t.Errorf("steps[2] = %+v, want no redirect overrides", steps[2])
	}
	tls := d2.TLS
	if tls == nil || !tls.InsecureSkipVerify || tls.MinVersion != "1.3" || tls.ClientCert != "certs/client.pem" ||
		tls.ClientKey != "certs/client-key.pem" || !definition.IsInlinePEM(tls.CACert) {
		t.Errorf("tls = %+v", tls)
	}
	if h.MaxRedirects != 5 || h.Version != "1.1" {
		t.Errorf("http = %+v", h)
	}
}

// TestV1IgnoresHTTPTLS: a v1 definition carrying the v2 http, tls and step
// redirect keys, some of the wrong JSON type, parses exactly as it does
// without them.
func TestV1IgnoresHTTPTLS(t *testing.T) {
	plain := `{"name":"v1","virtual_users":1%s,"scenarios":[{"name":"s","steps":[{"name":"a",` +
		`"request":{"method":"GET","url":"https://shop.example.com/"}%s}]}]}`
	without := []byte(strings.ReplaceAll(plain, "%s", ""))
	for _, extra := range [][2]string{
		{`,"http":{"follow_redirects":false,"max_redirects":5,"version":"1.1"},"tls":{"client_key":"k.pem"}`, `,"follow_redirects":false,"max_redirects":3`},
		{`,"http":"x","tls":["a"]`, `,"follow_redirects":"no","max_redirects":-1.5`},
		{`,"HTTP":{"version":2},"Tls":{"insecure_skip_verify":"yes","client_cert":5}`, `,"Follow_Redirects":null,"MAX_REDIRECTS":{}`},
		{`,"http":null,"tls":null`, `,"follow_redirects":null,"max_redirects":1e999`},
	} {
		with := []byte(strings.Replace(strings.Replace(plain, "%s", extra[0], 1), "%s", extra[1], 1))
		got, err := definition.Parse(with)
		if err != nil {
			t.Fatalf("Parse: %v\n%s", err, with)
		}
		if !got.IsV1() || len(got.V1TypeProblems) != 0 {
			t.Errorf("IsV1 %v, V1TypeProblems %v", got.IsV1(), got.V1TypeProblems)
		}
		if want := mustParse(t, without); !reflect.DeepEqual(got, want) {
			t.Errorf("v1 with http and tls keys:\n got %+v\nwant %+v\n%s", got, want, with)
		}
	}
}
