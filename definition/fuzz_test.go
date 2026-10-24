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
		`{"schema_version":"2.0","name":"x","virtual_users":1,"expected_statuses":[200,404],"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"},"extract":[{"variable_name":"t","selector":"$.t","type":"json","required":true,"on_fail":"record"}],"checks":[{"type":"status","in":[],"between":[200,299],"on_fail":""},{"type":"json","selector":"$.id","equals":1234567890123456789,"name":""},{"type":"html","selector":"h1","equals":"\u003cb\u003e \u2028"},{"type":"timing","duration":"< 500ms"}]}]}]}`,
		// Every optional check key with its zero value, which reads as absent.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"expected_statuses":[],"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"},"extract":[{"variable_name":"t","selector":"t","type":"header","required":false,"on_fail":""}],"checks":[{"name":"","type":"status","selector":"","in":[],"not_in":[],"between":[500,599],"not_contains":"","matches":"","is":"","count":"","size":"","duration":"","on_fail":""},{"type":"status","between":[],"in":[200]},{"type":"body","equals":""},{"type":"json","selector":"$.a","contains":""},{"type":"html","selector":"p","equals":""}]}]}]}`,
		// exists on every type that takes it, and the other flags.
		`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"a","steps":[{"name":"b","request":{"method":"GET","url":"https://x/"},"checks":[{"type":"header","selector":"A","exists":true},{"type":"json","selector":"$.a","exists":true},{"type":"html","selector":"a","exists":true},{"type":"cookie","selector":"c","exists":true},{"type":"json","selector":"$.b","missing":true},{"type":"body","empty":true}]}]}]}`,
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
