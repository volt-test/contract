package definition

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
)

type scannedMember struct {
	Key, Quoted, Val string
}

// membersByDecoder lists an object's members with encoding/json's
// streaming decoder, the reference for forEachMember.
func membersByDecoder(raw []byte) ([]scannedMember, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	var out []scannedMember
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, scannedMember{Key: tok.(string), Val: string(v)})
	}
	return out, nil
}

func membersByScanner(raw []byte) ([]scannedMember, error) {
	var out []scannedMember
	err := forEachMember(raw, func(k, q, v []byte) {
		out = append(out, scannedMember{Key: string(k), Quoted: string(q), Val: string(v)})
	})
	return out, err
}

func elementsByUnmarshal(raw []byte) ([]string, error) {
	var es []json.RawMessage
	if err := json.Unmarshal(raw, &es); err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range es {
		out = append(out, string(e))
	}
	return out, nil
}

func elementsByScanner(raw []byte) ([]string, error) {
	out := []string{}
	err := forEachElement(raw, func(_ int, e []byte) { out = append(out, string(e)) })
	return out, err
}

// compareScan checks the scanners against encoding/json on one valid
// document. It returns false if raw is not a valid object or array.
func compareScan(t testing.TB, raw []byte) bool {
	t.Helper()
	if !json.Valid(raw) {
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	switch trimmed[0] {
	case '{':
		want, err := membersByDecoder(trimmed)
		if err != nil {
			t.Fatal(err)
		}
		got, err := membersByScanner(trimmed)
		if err != nil {
			t.Fatalf("forEachMember: %v on %q", err, raw)
		}
		for i := range got {
			// The quoted key must decode to the same key.
			var k string
			if err := json.Unmarshal([]byte(got[i].Quoted), &k); err != nil || k != got[i].Key {
				t.Fatalf("quoted key %q does not decode to %q", got[i].Quoted, got[i].Key)
			}
			got[i].Quoted = ""
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("members differ on %q:\n got %q\nwant %q", raw, got, want)
		}
	case '[':
		want, _ := elementsByUnmarshal(trimmed)
		got, err := elementsByScanner(trimmed)
		if err != nil {
			t.Fatalf("forEachElement: %v on %q", err, raw)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("elements differ on %q:\n got %q\nwant %q", raw, got, want)
		}
	default:
		return false
	}
	return true
}

var scanKeyPieces = []string{
	"a", "weights", "Weights", "WEIGHTS", `\"`, `\\`, `\/`, `\b`, `\f`, `\n`, `\r`, `\t`,
	`\u0077`, `\u0057`, `\uD83D\uDE00`, `\uD83D`, `\uDE00`, `\uD83Dx`, `\uD83D\u0041`, `\u0000`,
	"\xff", "\xc3", "\xc0\xaf", "\xed\xa0\x80", "\x7f", "é", "\u212a", "\u017f", " ", "]", "}", ",", ":", "{", "[",
	`\u212a`, `\u017F`,
}

var scanSpace = []string{"", " ", "\n", "\t", "\r", "  \n\t "}

func randomKey(r *rand.Rand) string {
	var b bytes.Buffer
	for n := r.Intn(4); n >= 0; n-- {
		b.WriteString(scanKeyPieces[r.Intn(len(scanKeyPieces))])
	}
	return b.String()
}

func randomValue(r *rand.Rand, depth int) string {
	sp := scanSpace[r.Intn(len(scanSpace))]
	switch k := r.Intn(9); {
	case k == 0 && depth < 4:
		var b bytes.Buffer
		b.WriteString("{" + sp)
		for i, n := 0, r.Intn(4); i < n; i++ {
			if i > 0 {
				b.WriteString(sp + "," + sp)
			}
			b.WriteString(`"` + randomKey(r) + `"` + sp + ":" + sp + randomValue(r, depth+1))
		}
		b.WriteString(sp + "}")
		return b.String()
	case k == 1 && depth < 4:
		var b bytes.Buffer
		b.WriteString("[" + sp)
		for i, n := 0, r.Intn(4); i < n; i++ {
			if i > 0 {
				b.WriteString(sp + "," + sp)
			}
			b.WriteString(randomValue(r, depth+1))
		}
		b.WriteString(sp + "]")
		return b.String()
	case k == 2:
		return `"` + randomKey(r) + `"`
	case k == 3:
		return []string{"0", "-0", "1.5", "-1e10", "1E+2", "1e-999", "1e999", "123456789012345678901234567890", "0.0e0"}[r.Intn(9)]
	case k == 4:
		return "true"
	case k == 5:
		return "false"
	case k == 6:
		return "null"
	}
	return `"x\"y\\"`
}

// TestScannersMatchEncodingJSON compares forEachMember and forEachElement
// with encoding/json on random valid documents full of escapes, odd
// whitespace and invalid UTF-8.
func TestScannersMatchEncodingJSON(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	checked := 0
	// FuzzScan covers the long tail; this keeps the race-detector run short.
	for i := 0; i < 50_000; i++ {
		var doc string
		if r.Intn(2) == 0 {
			doc = "{" + scanSpace[r.Intn(6)] + `"k":` + randomValue(r, 0) + `,"` + randomKey(r) + `":` + randomValue(r, 1) + "}"
		} else {
			doc = "[" + randomValue(r, 0) + "," + randomValue(r, 1) + "]"
		}
		if compareScan(t, []byte(scanSpace[r.Intn(6)]+doc+scanSpace[r.Intn(6)])) {
			checked++
		}
	}
	if checked < 15_000 {
		t.Fatalf("only %d valid documents checked", checked)
	}
}

// TestKeyMatchingMatchesEncodingJSON checks that lookupFold matches a key
// to a field exactly when encoding/json does, including escaped keys and
// Unicode case folding (K, the Kelvin sign, folds to k; ſ to s).
func TestKeyMatchingMatchesEncodingJSON(t *testing.T) {
	type target struct {
		Weights []int `json:"weights"`
		Skip    bool  `json:"skip_lookup"`
	}
	fields := fieldsOf(reflect.TypeOf(target{}))
	keys := []string{`weights`, `WEIGHTS`, `\u0077eights`, `\u0057EIGHTS`, "wei\u212ahts", `wei\u212Ahts`,
		"\u017fkip_lookup", `\u017Fkip_lookup`, "s\u212aip_lookup", "weights\xff", `weights\u0000`, "weight\xc5\xbf"}
	for _, k := range keys {
		var a, b target
		_ = json.Unmarshal([]byte(`{"`+k+`": [1]}`), &a)
		_ = json.Unmarshal([]byte(`{"`+k+`": true}`), &b)
		byJSON := a.Weights != nil || b.Skip

		var ours bool
		if err := forEachMember([]byte(`{"`+k+`": 1}`), func(key, _, _ []byte) { _, _, ours = fields.lookupFold(key) }); err != nil {
			t.Fatalf("%q: %v", k, err)
		}
		if byJSON != ours {
			t.Errorf("key %q: encoding/json matched=%v, lookupFold=%v", k, byJSON, ours)
		}
	}
}

func TestScannersNeverPanic(t *testing.T) {
	inputs := []string{``, `"`, `"\`, `"\"`, `[`, `{`, `[1,`, `{"a"`, `{"a":`, `{"a":1`, `[1 2]`, `{"a" 1}`,
		`{1:2}`, `[,]`, `[1,]`, `]`, `}`, `{"a":1,}`, `{"`, `{""`, `{"":`, `[{"a":[}]`, `["\`}
	for _, in := range inputs {
		for i := 0; i <= len(in); i++ {
			_, _ = skipValue([]byte(in), i)
		}
		_ = forEachElement([]byte(in), func(int, []byte) {})
		_ = forEachMember([]byte(in), func(_, _, _ []byte) {})
	}
}

// FuzzScan compares the scanners with encoding/json on any valid object
// or array: keys, quoted keys and value bytes must match, and malformed
// input must not panic.
func FuzzScan(f *testing.F) {
	r := rand.New(rand.NewSource(2))
	for i := 0; i < 50; i++ {
		f.Add([]byte("{" + `"k":` + randomValue(r, 0) + "}"))
		f.Add([]byte("[" + randomValue(r, 0) + "]"))
	}
	f.Add([]byte(`{"a\u0062": [1, {"c": "\"]"}], "a": null}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if !compareScan(t, data) {
			_ = forEachElement(data, func(int, []byte) {})
			_ = forEachMember(data, func(_, _, _ []byte) {})
		}
	})
}
