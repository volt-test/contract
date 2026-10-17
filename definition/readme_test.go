package definition_test

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/volt-test/contract/definition"
)

var compatRow = regexp.MustCompile("^\\| `([^`]+)` \\| ([^|]+?) \\|$")

// TestREADMECompatibilityTable checks that the README table agrees with the
// minimum engine releases behind UnsupportedVersionError.
func TestREADMECompatibilityTable(t *testing.T) {
	readme := string(readFile(t, "../README.md"))
	_, section, ok := strings.Cut(readme, "\n## Compatibility\n")
	if !ok {
		t.Fatal("README has no \"## Compatibility\" section")
	}
	if i := strings.Index(section, "\n## "); i >= 0 {
		section = section[:i]
	}
	rows := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		if m := compatRow.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if _, dup := rows[m[1]]; dup {
				t.Errorf("README lists schema_version %s twice", m[1])
			}
			rows[m[1]] = m[2]
		}
	}
	if !reflect.DeepEqual(rows, definition.MinEngineReleaseForTest) {
		t.Errorf("README compatibility rows %v\n!= minEngineRelease %v", rows, definition.MinEngineReleaseForTest)
	}
	for _, s := range definition.SupportedSchemaVersions() {
		if _, ok := rows[s]; !ok {
			t.Errorf("README has no row for supported schema_version %s", s)
		}
	}
}
