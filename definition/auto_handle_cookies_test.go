package definition_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

// v1CookieCase is one v1 document of testdata/recorded/v1_auto_handle_cookies.json.
type v1CookieCase struct {
	Name string          `json:"name"`
	Doc  json.RawMessage `json:"doc"`
}

// v1CookieResult is what Parse made of a case: the problems of a
// *ValidationError, or the V1TypeProblems and json.Marshal output.
type v1CookieResult struct {
	Name           string               `json:"name"`
	Problems       []definition.Problem `json:"problems,omitempty"`
	V1TypeProblems []definition.Problem `json:"v1_type_problems,omitempty"`
	Marshal        string               `json:"marshal,omitempty"`
}

// explicitFalse is the one intended change to v1 json.Marshal output since
// v0.4.0: an explicit false auto_handle_cookies is kept.
const explicitFalse = `"auto_handle_cookies":false,`

// TestV1AutoHandleCookiesRecorded checks v1 auto_handle_cookies values of
// every JSON type, null, case variants and duplicate keys against the
// results v0.4.0 recorded, when the field was a bool. The marshal output
// is compared without explicitFalse.
func TestV1AutoHandleCookiesRecorded(t *testing.T) {
	var cases []v1CookieCase
	if err := json.Unmarshal(readFile(t, filepath.Join(recordedDir, "v1_auto_handle_cookies.json")), &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	var got []v1CookieResult
	for _, c := range cases {
		r := v1CookieResult{Name: c.Name}
		d, err := definition.Parse(c.Doc)
		var ve *definition.ValidationError
		switch {
		case errors.As(err, &ve):
			r.Problems = ve.Problems
		case err != nil:
			t.Fatalf("%s: %v", c.Name, err)
		default:
			if !d.IsV1() {
				t.Fatalf("%s: not v1", c.Name)
			}
			r.V1TypeProblems = d.V1TypeProblems
			b, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			r.Marshal = strings.ReplaceAll(string(b), explicitFalse, "")
		}
		got = append(got, r)
	}
	out, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, '\n')
	file := filepath.Join(recordedDir, "v1_auto_handle_cookies.results.json")
	if *updateRecorded {
		writeRecorded(t, file, out)
		return
	}
	if want := readFile(t, file); !bytes.Equal(out, want) {
		t.Errorf("results changed:\n got %s\nwant %s", out, want)
	}
}
