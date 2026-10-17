package definition_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"testing"
)

// fixtures returns the JSON files under testdata/<dir>, relative to the
// repository root.
func fixtures(t testing.TB, dir string) []string {
	t.Helper()
	var files []string
	root := filepath.Join("..", "testdata", dir)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".json" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no fixtures under %s", root)
	}
	sort.Strings(files)
	return files
}

func readFile(t testing.TB, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decodeMap(t testing.TB, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// leafSet maps each meaningful leaf of a decoded JSON document to its path,
// e.g. "scenarios[0].steps[1].request.url".
//
// A typed decoder cannot tell a zero value from a missing key, so zero
// leaves ("", false, 0, null) and empty containers are not meaningful and
// are left out. One exception: "header": {} next to "headers" is kept,
// because a v1 cloud engine uses a non-nil header map in place of headers.
// Values keep their JSON types, so a number that came back as a string is
// a difference.
type leafSet map[string]any

func leaves(doc map[string]any) leafSet {
	out := leafSet{}
	out.collect("", doc)
	return out
}

func (ls leafSet) collect(path string, v any) {
	switch x := v.(type) {
	case map[string]any:
		_, hasHeaders := x["headers"]
		for k, child := range x {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if m, ok := child.(map[string]any); ok && k == "header" && len(m) == 0 && hasHeaders && x["headers"] != nil {
				ls[p] = map[string]any{}
				continue
			}
			ls.collect(p, child)
		}
	case []any:
		for i, child := range x {
			ls.collect(path+"["+strconv.Itoa(i)+"]", child)
		}
	default:
		if !isZeroLeaf(x) {
			ls[path] = x
		}
	}
}

func isZeroLeaf(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case float64:
		return x == 0
	}
	return false
}

// leafChange is one way two documents differ at a leaf path.
type leafChange struct {
	Path          string
	Before, After any  // nil when the leaf is missing on that side
	InBefore      bool // the leaf exists in the first document
	InAfter       bool // the leaf exists in the second document
}

func (c leafChange) String() string {
	switch {
	case !c.InAfter:
		return fmt.Sprintf("%s lost (was %#v)", c.Path, c.Before)
	case !c.InBefore:
		return fmt.Sprintf("%s appeared (%#v)", c.Path, c.After)
	}
	return fmt.Sprintf("%s became %#v (was %#v)", c.Path, c.After, c.Before)
}

// changesBetween walks the union of leaf paths of two decoded documents in
// sorted order and reports every path whose value or JSON type differs.
func changesBetween(before, after map[string]any) []leafChange {
	b, a := leaves(before), leaves(after)
	union := make(map[string]struct{}, len(b)+len(a))
	for p := range b {
		union[p] = struct{}{}
	}
	for p := range a {
		union[p] = struct{}{}
	}
	paths := make([]string, 0, len(union))
	for p := range union {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var out []leafChange
	for _, p := range paths {
		bv, inB := b[p]
		av, inA := a[p]
		if inB && inA && reflect.DeepEqual(bv, av) {
			continue
		}
		out = append(out, leafChange{Path: p, Before: bv, After: av, InBefore: inB, InAfter: inA})
	}
	return out
}

var indexRE = regexp.MustCompile(`\[\d+\]`)

// pattern replaces array indexes with [] so paths from different fixtures
// can be compared.
func pattern(path string) string { return indexRE.ReplaceAllString(path, "[]") }
