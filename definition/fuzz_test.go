package definition_test

import (
	"encoding/json"
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
		`{"Schema_Version":"x","scenarios":[{"steps":[{"validate":[{"expected":{"a":[1,null]}}]}]}]}`,
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
	})
}
