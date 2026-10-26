package definition_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// privateKeyType ends the PEM block type of every private key. It is built
// by concatenation so this file holds no private-key text.
const privateKeyType = "PRIVATE " + "KEY"

// TestFixturesCarryNoSecrets keeps credentials out of the public fixtures,
// including CSV files embedded as base64 in data_content.
func TestFixturesCarryNoSecrets(t *testing.T) {
	csvs := 0
	for _, dir := range []string{"v1", "v2"} {
		for _, f := range fixtures(t, dir) {
			raw := readFile(t, f)
			if bytes.Contains(bytes.ToLower(raw), []byte("secret")) {
				t.Errorf("%s contains \"secret\"", filepath.ToSlash(f))
			}
			// No private key, even a placeholder: secret scanners match the
			// PEM header whatever the body. Fixtures give client_key as a path.
			if bytes.Contains(bytes.ToUpper(raw), []byte(privateKeyType)) {
				t.Errorf("%s contains %q", filepath.ToSlash(f), privateKeyType)
			}
			// UseNumber: invalid fixtures may hold numbers out of float64 range.
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			var doc any
			if err := dec.Decode(&doc); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			for _, content := range dataContents(doc) {
				csv, err := base64.StdEncoding.DecodeString(content)
				if err != nil {
					t.Errorf("%s: data_content is not base64: %v", f, err)
					continue
				}
				csvs++
				if strings.Contains(strings.ToLower(string(csv)), "secret") {
					t.Errorf("%s: embedded CSV contains \"secret\": %q", filepath.ToSlash(f), csv)
				}
			}
		}
	}
	if csvs == 0 {
		t.Fatal("no data_content found; the check is not looking in the right place")
	}
}

// dataContents returns every string value of a "data_content" key in doc.
func dataContents(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if s, ok := val.(string); ok && k == "data_content" {
				out = append(out, s)
				continue
			}
			out = append(out, dataContents(val)...)
		}
	case []any:
		for _, val := range x {
			out = append(out, dataContents(val)...)
		}
	}
	return out
}
