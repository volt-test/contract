package definition_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/volt-test/contract/definition"
)

func FuzzParse(f *testing.F) {
	for _, dir := range []string{"v1", "v2"} {
		for _, file := range fixtures(f, dir) {
			f.Add(readFile(f, file))
		}
	}
	for _, s := range []string{
		``, `null`, `{}`, `[]`, `{"schema_version":"2.0"}`, `{"schema_version":null}`,
		`{"schema_version":"2.0","scenarios":[null,{"steps":[{"request":null}]}]}`,
		`{"schema_version":"2.0","name":"x","scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"/x"}}]}],"target":{"base_url":"${b}"}}`,
		`{"schema_version":"2.0","name":"x","virtual_users":1,"thresholds":["p95<1s",{"expr":"iterations>1","abort_on_fail":true,"delay_abort":"1s"},{"expr":"rps>1","delay_abort":""}],"scenarios":[{"name":"a","thresholds":["iterations>1"],"steps":[{"name":"b","thresholds":["error_rate<1%"],"request":{"method":"GET","url":"https://x/"}}]}]}`,
		`{"Schema_Version":"x","scenarios":[{"steps":[{"validate":[{"expected":{"a":[1,null]}}]}]}]}`,
		`{"schema_version":"2.0","name":"x","virtual_users":1,"expected_statuses":[200,404],"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"},"extract":[{"variable_name":"t","selector":"$.t","type":"json","required":true,"on_fail":"record","pick":"all"},{"variable_name":"u","selector":"$.u[*]","type":"json","pick":"random"}],"checks":[{"type":"status","in":[],"between":[200,299],"on_fail":""},{"type":"json","selector":"$.id","equals":1234567890123456789,"name":""},{"type":"html","selector":"h1","equals":"\u003cb\u003e \u2028"},{"type":"timing","duration":"< 500ms"}]}]}]}`,
		// run_once and teardown: setup, load and teardown, both flags on one scenario, a weight, and false.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"s","run_once":true,"weight":0,"steps":[{"name":"b","request":{"method":"GET","url":"https://x/"},"extract":[{"variable_name":"t","selector":"$.t","type":"json","required":true}]}]},{"name":"l","run_once":false,"teardown":false,"steps":[{"name":"b","request":{"method":"GET","url":"https://x/${t}"},"extract":[{"variable_name":"t","selector":"$.t","type":"json"}]}]},{"name":"d","teardown":true,"run_once":true,"weight":3,"steps":[{"name":"b","request":{"method":"DELETE","url":"https://x/${t}"}}]}]}`,
		// once_per_vu: on and off in a load scenario, on every step of one, and in run_once and teardown scenarios.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"s","run_once":true,"steps":[{"name":"b","once_per_vu":true,"request":{"method":"GET","url":"https://x/"}}]},{"name":"l","steps":[{"name":"b","once_per_vu":true,"request":{"method":"POST","url":"https://x/"},"extract":[{"variable_name":"t","selector":"$.t","type":"json","required":true,"on_fail":"abort"}]},{"name":"c","once_per_vu":false,"request":{"method":"GET","url":"https://x/${t}"}}]},{"name":"m","weight":2,"steps":[{"name":"b","once_per_vu":true,"request":{"method":"GET","url":"https://x/"}}]},{"name":"d","teardown":true,"steps":[{"name":"b","once_per_vu":"true","request":{"method":"DELETE","url":"https://x/"}}]}]}`,
		// Every optional check key with its zero value, which reads as absent.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"expected_statuses":[],"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"},"extract":[{"variable_name":"t","selector":"t","type":"header","required":false,"on_fail":"","pick":""}],"checks":[{"name":"","type":"status","selector":"","in":[],"not_in":[],"between":[500,599],"not_contains":"","matches":"","is":"","count":"","size":"","duration":"","on_fail":""},{"type":"status","between":[],"in":[200]},{"type":"body","equals":""},{"type":"json","selector":"$.a","contains":""},{"type":"html","selector":"p","equals":""}]}]}]}`,
		// exists on every type that takes it, and the other flags.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"},"checks":[{"type":"header","selector":"A","exists":true},{"type":"json","selector":"$.a","exists":true},{"type":"html","selector":"a","exists":true},{"type":"cookie","selector":"c","exists":true},{"type":"json","selector":"$.b","missing":true},{"type":"body","empty":true}]}]}]}`,
		// Every http, tls and step redirect key, with explicit false and an inline certificate.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"http":{"follow_redirects":false,"max_redirects":5,"version":"1.1"},"tls":{"insecure_skip_verify":true,"min_version":"1.0","client_cert":"c.pem","client_key":"k.pem","ca_cert":"-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"},"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"},"follow_redirects":true,"max_redirects":2},{"name":"c","request":{"method":"GET","url":"https://x/"},"follow_redirects":false}]}]}`,
		// The same keys with zero values, which read as absent, and empty objects.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"http":{"max_redirects":0,"version":""},"tls":{"insecure_skip_verify":false,"min_version":"","client_cert":"","client_key":"","ca_cert":""},"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"},"follow_redirects":false,"max_redirects":0}]}]}`,
		`{"schema_version":"2.0","name":"x","virtual_users":1,"http":{},"tls":{},"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"},"max_redirects":1}]}]}`,
		// A comment line before the block, and a path with a line break, which Parse rejects.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"tls":{"ca_cert":"## bundle\n-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n","client_cert":"a\nb","client_key":" "},"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"}}]}]}`,
		// Both load types, then zero values next to load, and a load Parse rejects.
		`{"schema_version":"2.0","name":"x","load":{"type":"rate","rate":5,"time_unit":"1m","duration":"1m","preallocated_virtual_users":1,"max_virtual_users":2},"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"}}]}]}`,
		`{"schema_version":"2.0","name":"x","virtual_users":0,"duration":"","stages":[],"load":{"type":"ramping_rate","rate":0,"stages":[{"duration":"1m","target":0},{"duration":"1s"}],"preallocated_virtual_users":1,"max_virtual_users":0},"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"}}]}]}`,
		`{"schema_version":"2.0","name":"x","virtual_users":3,"load":{"type":"","rate":-1,"time_unit":"0s","stages":[{"target":-1}],"preallocated_virtual_users":0,"max_virtual_users":-1},"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"}}]}]}`,
		// Ramp start and graceful stop: an explicit 0, a pool ramping from initial_virtual_users to 0,
		// then the keys where Parse rejects them.
		`{"schema_version":"2.0","name":"x","stages":[{"duration":"1m","target":2}],"initial_virtual_users":0,"stop_grace":"0s","ramp_down_grace":"1s","thresholds":["interrupted_iterations == 0"],"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"}}]},{"name":"p","load":{"type":"stages","initial_virtual_users":3,"stages":[{"duration":"1m"}],"ramp_down_grace":"0s","stop_grace":"2s"},"steps":[{"name":"b","request":{"method":"GET","url":"https://x/"}}]}]}`,
		`{"schema_version":"2.0","name":"x","virtual_users":1,"initial_virtual_users":0,"ramp_down_grace":"-1s","stop_grace":"x","load":{"type":"rate","stop_grace":"1s","initial_virtual_users":1},"scenarios":[{"name":"a","load":{"type":"constant","virtual_users":1,"duration":"1m","initial_virtual_users":0,"ramp_down_grace":"1s"},"steps":[{"name":"b","request":{"method":"GET","url":"https://x/"}}]}]}`,
		// A number out of float64 range, which Parse rejects.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"},"checks":[{"type":"json","selector":"$.a","equals":1e999}]}]}]}`,
		// Request bodies: every body_type, literals and key order kept, field order kept, zero values,
		// and the combinations Parse rejects (a duplicate field name, a scalar body_json, body next to json).
		`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"a","steps":[{"name":"j","request":{"method":"POST","url":"https://x/","body_type":"json","body_json":{"z":19.90,"a":["<&>","${random_int(1,3)}",{"n":null}],"b":1e999}}},{"name":"k","request":{"method":"POST","url":"https://x/","body_type":"json","body_json":[]}},{"name":"f","request":{"method":"POST","url":"https://x/","body_type":"form","fields":{"z":"a b&c","a b":"${uuid}","":""}}},{"name":"e","request":{"method":"POST","url":"https://x/","body_type":"form","fields":{}}},{"name":"r","request":{"method":"GET","url":"https://x/","body_type":"","body":"x","fields":{}}}]}]}`,
		`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"a","steps":[{"name":"f","request":{"method":"POST","url":"https://x/","body_type":"form","fields":{"a":"1","a":"2"}}},{"name":"j","request":{"method":"POST","url":"https://x/","body_type":"json","body_json":"{}","body":"x"}},{"name":"r","request":{"method":"POST","url":"https://x/","body_type":"xml","body_json":{},"fields":{"a":1}}}]}]}`,
		`{"name":"v1","virtual_users":1,"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"POST","url":"https://x/","body":"x","body_type":"json","body_json":5,"fields":{"a":1,"a":2}}}]}]}`,
		// Header arrays, multipart files, binary bodies, response reading and timeouts, valid and not.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"http":{"max_body_size":"1mib","discard_bodies":true,"connect_timeout":"1s","tls_handshake_timeout":"1s","response_header_timeout":"1s"},"scenarios":[{"name":"a","steps":[{"name":"m","request":{"method":"POST","url":"https://x/","timeout":"1s","headers":{"Host":"h","A":["1","2"]},"body_type":"multipart","fields":{"t":"${x}"},"files":{"f":{"path":"a"},"g":{"content_base64":"AAAA","filename":"${n}","content_type":"image/png"}}},"discard_body":true,"response_type":"binary"},{"name":"b","request":{"method":"PUT","url":"https://x/","body_base64":"AAECAwQ="}}]}]}`,
		`{"schema_version":"2.0","name":"x","virtual_users":1,"http":{"max_body_size":"0.5b","connect_timeout":"0s"},"scenarios":[{"name":"a","steps":[{"name":"m","request":{"method":"POST","url":"https://x/","timeout":"-1s","headers":{"Host":["a","b"],"host":"c","Content-Type":"x","E":[],"N":[1,null]},"body_type":"multipart","body":"x","files":{"f":{},"g":{"path":"a","content_base64":"A=A=","filename":"a\r\n","content_type":"x"}}},"discard_body":true,"response_type":"binary","checks":[{"type":"json","selector":"$.a","exists":true}],"extract":[{"variable_name":"r","selector":"(a)","type":"regex"}]}]}]}`,
		`{"name":"v1","virtual_users":1,"http":{"max_body_size":5},"scenarios":[{"name":"a","steps":[{"name":"b","discard_body":"x","request":{"method":"POST","url":"https://x/","headers":{"A":["x"],"B":null,"C":5},"files":"x","body_base64":1,"timeout":[]}}]}]}`,
		// Dynamic steps: a group, loops, conditions, assert and ${expr: ...} templates, valid and not.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"vars":{"v":"${expr: {\"a\": '}'}}"},"scenarios":[{"name":"a","steps":[{"name":"l","request":{"method":"GET","url":"https://x/${expr: n % 2}"},"skip_if":"a","extract":[{"variable_name":"ids","selector":"$.ids","type":"json","pick":"all"}],"checks":[{"type":"assert","name":"n","expr":"status == 200"}]},{"name":"f","request":{"method":"GET","url":"https://x/${id}"},"only_if":"b","foreach":{"var":"id","in":"${ids}"}},{"name":"g","repeat":{"until":"done","max":3},"steps":[{"name":"i","request":{"method":"GET","url":"https://x/"}}]}]}]}`,
		`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"a","run_once":true,"steps":[{"name":"g","repeat":{"until":"","max":0},"request":{"method":"GET","url":"https://x/${expr: {"},"steps":[{"name":"g","steps":[{"name":"h"}],"repeat":null}],"foreach":{"var":"env","in":"${a}${b}"},"skip_if":"x","only_if":"${y}"}]}]}`,
		// Number literals at the edges, and labels cut at 128 bytes.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"},"checks":[{"type":"json","selector":"$.a","equals":1e308},{"type":"json","selector":"$.b","equals":-0.0},{"type":"json","selector":"$.c","contains":1E+2},{"type":"url","contains":"\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9"}]}]}]}`,
	} {
		f.Add([]byte(s))
	}
	schema := compileSchema(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := definition.Parse(data)
		if err != nil {
			return
		}
		if d.IsV1() {
			return
		}
		// The schema never rejects what Parse accepts as v2.
		requireSchemaAccepts(t, schema, data)
		// A valid v2 definition marshals to a valid v2 definition.
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		d2, err := definition.Parse(b)
		if err != nil {
			t.Fatalf("re-parse of marshalled v2 definition: %v\ninput: %s\nmarshalled: %s", err, data, b)
		}
		b2, _ := json.Marshal(d2)
		if string(b) != string(b2) {
			t.Fatalf("marshal is not a fixed point:\n%s\n%s", b, b2)
		}
		// Results key on check labels, so they survive Marshal and Parse.
		if l1, l2 := checkLabels(d), checkLabels(d2); !reflect.DeepEqual(l1, l2) {
			t.Fatalf("check labels changed on Marshal and Parse:\n%q\n%q\ninput: %s", l1, l2, data)
		}
	})
}

// FuzzParseThinkTime checks that ParseThinkTime never panics and that an
// accepted value gives 0 <= min <= max.
func FuzzParseThinkTime(f *testing.F) {
	for _, s := range []string{"", "-", "0", "500ms", "-1s", "1s-3s", "0-3s", "3s-1s", "1s--3s", "1s-", "-1s-3s", "1s–3s"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		min, max, err := definition.ParseThinkTime(s)
		if err != nil {
			return
		}
		if min < 0 || min > max {
			t.Fatalf("ParseThinkTime(%q) = %v, %v", s, min, max)
		}
	})
}
