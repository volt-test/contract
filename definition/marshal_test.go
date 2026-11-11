package definition_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testdata/recorded pins json.Marshal bytes of the v1 and valid v2
// fixtures, and the V1TypeProblems of a v1 non-string header value. The
// recordings hold the v0.3.0 bytes (unchanged through v0.4.0), plus an
// explicit "auto_handle_cookies":false where a fixture writes one.
var updateRecorded = flag.Bool("update-recorded", false, "re-record testdata/recorded from the current code")

const recordedDir = "../testdata/recorded"

// marshalGolden is the recorded json.Marshal output for a fixture path
// relative to testdata, e.g. "v1/cli/header_shadow.json".
func marshalGolden(rel string) string {
	return filepath.Join(recordedDir, "marshal", rel)
}

func writeRecorded(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestMarshalUnchanged checks that json.Marshal of every parsed v1 fixture
// and every v2 fixture recorded from v0.3.0 writes the recorded bytes.
// Every v1 fixture must have a recording: older code reads them all.
func TestMarshalUnchanged(t *testing.T) {
	var rels []string
	for _, dir := range []string{"v1", "v2/valid"} {
		for _, f := range fixtures(t, dir) {
			rel, err := filepath.Rel(filepath.Join("..", "testdata"), f)
			if err != nil {
				t.Fatal(err)
			}
			rels = append(rels, filepath.ToSlash(rel))
		}
	}
	checked := 0
	for _, rel := range rels {
		golden := marshalGolden(rel)
		t.Run(rel, func(t *testing.T) {
			d := mustParse(t, readFile(t, filepath.Join("..", "testdata", rel)))
			got, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			want, err := os.ReadFile(golden)
			if os.IsNotExist(err) && strings.HasPrefix(rel, "v2/") {
				t.Skip("added after v0.3.0; TestV2RoundTrip covers it")
			}
			if *updateRecorded {
				writeRecorded(t, golden, got)
				return
			}
			if err != nil {
				t.Fatalf("%v (every v1 fixture needs a recording)", err)
			}
			checked++
			if !bytes.Equal(got, want) {
				t.Errorf("json.Marshal changed:\n got %s\nwant %s", got, want)
			}
		})
	}
	if !*updateRecorded && checked == 0 {
		t.Fatal("no recordings checked")
	}
	// A recording whose fixture is gone is a pin that no longer runs.
	err := filepath.Walk(filepath.Join(recordedDir, "marshal"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(filepath.Join(recordedDir, "marshal"), p)
		if _, err := os.Stat(filepath.Join("..", "testdata", rel)); err != nil {
			t.Errorf("recording %s has no fixture", filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil && !*updateRecorded {
		t.Fatal(err)
	}
}

// TestV1HeaderArrayStaysTypeProblem checks that a v1 header value that is
// not a string (an array included) is still the loose problem v0.3.0
// recorded, with the same path, message and count, and is dropped: the
// definition marshals to the recorded bytes.
func TestV1HeaderArrayStaysTypeProblem(t *testing.T) {
	d := mustParse(t, readFile(t, filepath.Join(recordedDir, "v1_header_array.json")))
	problems, err := json.MarshalIndent(d.V1TypeProblems, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	problems = append(problems, '\n')
	marshalled, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	marshalled = append(marshalled, '\n')
	problemsFile := filepath.Join(recordedDir, "v1_header_array.problems.json")
	marshalFile := filepath.Join(recordedDir, "v1_header_array.marshal.json")
	if *updateRecorded {
		writeRecorded(t, problemsFile, problems)
		writeRecorded(t, marshalFile, marshalled)
		return
	}
	if want := readFile(t, problemsFile); !bytes.Equal(problems, want) {
		t.Errorf("V1TypeProblems changed:\n got %s\nwant %s", problems, want)
	}
	if want := readFile(t, marshalFile); !bytes.Equal(marshalled, want) {
		t.Errorf("json.Marshal changed:\n got %s\nwant %s", marshalled, want)
	}
	if !d.IsV1() || len(d.V1TypeProblems) == 0 {
		t.Fatalf("IsV1 %v, V1TypeProblems %v", d.IsV1(), d.V1TypeProblems)
	}
}
