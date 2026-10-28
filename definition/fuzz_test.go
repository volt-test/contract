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
		`{"schema_version":"2.0","name":"x","load":{"type":"rate","rate":5,"time_unit":"1m","duration":"1m","preallocated_vus":1,"max_vus":2},"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"}}]}]}`,
		`{"schema_version":"2.0","name":"x","virtual_users":0,"duration":"","stages":[],"load":{"type":"ramping_rate","rate":0,"stages":[{"duration":"1m","target":0},{"duration":"1s"}],"preallocated_vus":1,"max_vus":0},"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"}}]}]}`,
		`{"schema_version":"2.0","name":"x","virtual_users":3,"load":{"type":"","rate":-1,"time_unit":"0s","stages":[{"target":-1}],"preallocated_vus":0,"max_vus":-1},"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"}}]}]}`,
		// A number out of float64 range, which Parse rejects.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"},"checks":[{"type":"json","selector":"$.a","equals":1e999}]}]}]}`,
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
