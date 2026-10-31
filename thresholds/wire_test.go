package thresholds_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"

	"github.com/volt-test/contract/thresholds"
)

// measure runs fn and returns its wall time, bytes allocated and number of
// allocations.
func measure(fn func()) (time.Duration, uint64, uint64) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	fn()
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	return elapsed, after.TotalAlloc - before.TotalAlloc, after.Mallocs - before.Mallocs
}

// wireSnapshot is a snapshot that uses every part of the wire form.
func wireSnapshot(t testing.TB) *thresholds.Snapshot {
	t.Helper()
	zone := time.FixedZone("x", 5*3600+30*60)
	home := thresholds.Scope{
		Requests: 40, Failed: 1, BytesSent: 4000, BytesReceived: 90000,
		Checks: ptr(80), ChecksPassed: ptr(79),
		Duration: dist(t, values(1, 40)...), TTFB: dist(t, values(2, 40)...),
	}
	// Values but no histogram: the percentiles are unknown.
	search := thresholds.Scope{Requests: 2, Duration: thresholds.Distribution{Count: 2, SumUs: 3000, MinUs: 1000, MaxUs: 2000}}
	browse := thresholds.Scope{
		Requests: 42, Failed: 1, BytesSent: 4100, BytesReceived: 91000,
		Checks: ptr(80), ChecksPassed: ptr(79), Iterations: ptr(20), DroppedIterations: ptr(0),
		Duration: dist(t, values(3, 42)...), IterationDuration: dist(t, values(4, 20)...),
	}
	// Checks measured as zero, iterations not measured.
	buy := thresholds.Scope{Requests: 0, Checks: ptr(0), ChecksPassed: ptr(0)}
	test := browse
	test.Checks, test.ChecksPassed = ptr(80), ptr(79)
	test.Iterations, test.DroppedIterations = nil, ptr(3)
	return &thresholds.Snapshot{
		Start:     time.Date(2026, 10, 16, 12, 0, 0, 123456789, time.UTC),
		End:       time.Date(2026, 10, 16, 17, 31, 0, 987654321, zone),
		Responses: 41,
		Test:      test,
		Scenarios: map[string]*thresholds.ScenarioScope{
			"browse":  {Scope: browse, Steps: map[string]*thresholds.Scope{"home": &home, "search \"q\" <b>": &search, "gone": nil}},
			"buy":     {Scope: buy},
			"unicode": {Scope: thresholds.Scope{}, Steps: map[string]*thresholds.Scope{"ステップ": {}}},
			"absent":  nil,
		},
	}
}

func mustEncode(t testing.TB, s *thresholds.Snapshot) []byte {
	t.Helper()
	b, err := thresholds.EncodeSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustDecode(t testing.TB, b []byte) *thresholds.Snapshot {
	t.Helper()
	s, err := thresholds.DecodeSnapshot(b, thresholds.SnapshotLimits{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// everyBound is every metric of the catalog at every scope of s, each
// with a bound that parses for its kind.
func everyBound(t testing.TB, refs []thresholds.Ref) []thresholds.Bound {
	t.Helper()
	var out []thresholds.Bound
	for _, ref := range refs {
		for _, m := range thresholds.Metrics() {
			var th thresholds.Threshold
			var err error
			for _, v := range []string{"< 50ms", "< 50%", "< 100"} {
				if th, err = thresholds.Parse(m + " " + v); err == nil {
					break
				}
			}
			if err != nil {
				t.Fatalf("no bound parses for %s: %v", m, err)
			}
			out = append(out, thresholds.Bound{Ref: ref, Threshold: th})
		}
	}
	return out
}

func refsOf(s *thresholds.Snapshot) []thresholds.Ref {
	refs := []thresholds.Ref{{}}
	for name, sc := range s.Scenarios {
		if sc == nil {
			continue
		}
		refs = append(refs, thresholds.Ref{Scenario: name})
		for step, st := range sc.Steps {
			if st != nil {
				refs = append(refs, thresholds.Ref{Scenario: name, Step: step})
			}
		}
	}
	return refs
}

func requireSameResults(t testing.TB, got, want []thresholds.Result) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d results, want %d", len(got), len(want))
	}
	for i := range got {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("%s %s/%s: got %s %v, want %s %v", got[i].Expr, got[i].Scenario, got[i].Step,
				got[i].Status, deref(got[i].Actual), want[i].Status, deref(want[i].Actual))
		}
	}
}

func TestSnapshotWireRoundTrip(t *testing.T) {
	s := wireSnapshot(t)
	before := deepCopy(s)
	b := mustEncode(t, s)
	if !equalSnapshots(s, before) {
		t.Fatal("EncodeSnapshot modified its input")
	}
	got := mustDecode(t, b)
	if !equalSnapshots(got, s) {
		t.Fatalf("round trip changed the snapshot:\n%s", b)
	}
	if !got.Start.Equal(s.Start) || !got.End.Equal(s.End) || got.Responses != 41 {
		t.Errorf("window %v..%v, responses %d", got.Start, got.End, got.Responses)
	}
	// nil and &0 stay apart.
	if got.Test.Iterations != nil || got.Scenarios["buy"].Checks == nil || *got.Scenarios["buy"].Checks != 0 {
		t.Errorf("optional counters: test iterations %v, buy checks %v", got.Test.Iterations, got.Scenarios["buy"].Checks)
	}
	home := got.Scenarios["browse"].Steps["home"]
	if home.Duration.Hist == nil || home.Duration.Hist.TotalCount() != 40 || !home.Duration.Hist.Equals(s.Scenarios["browse"].Steps["home"].Duration.Hist) {
		t.Errorf("home histogram did not round-trip")
	}
	if h := got.Scenarios["browse"].Steps["search \"q\" <b>"].Duration.Hist; h != nil {
		t.Errorf("a distribution without a histogram decoded with one")
	}
	if _, ok := got.Scenarios["absent"]; ok {
		t.Error("a nil scenario was encoded")
	}
	if _, ok := got.Scenarios["browse"].Steps["gone"]; ok {
		t.Error("a nil step was encoded")
	}
	bounds := everyBound(t, refsOf(s))
	requireSameResults(t, thresholds.Evaluate(got, bounds), thresholds.Evaluate(s, bounds))

	// The shape: lower-case keys, hdr as the EncodeHistogram text once,
	// empty distributions and unmeasured counters left out.
	var doc struct {
		Start     string
		Responses int64
		Test      map[string]json.RawMessage
		Scenarios map[string]map[string]json.RawMessage
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Start != "2026-10-16T12:00:00.123456789Z" {
		t.Errorf("start = %q", doc.Start)
	}
	if _, ok := doc.Test["iterations"]; ok {
		t.Error("an unmeasured counter was written")
	}
	if _, ok := doc.Test["ttfb"]; ok {
		t.Error("an empty distribution was written")
	}
	var d struct {
		HDR string `json:"hdr"`
	}
	if err := json.Unmarshal(doc.Test["duration"], &d); err != nil {
		t.Fatal(err)
	}
	want, err := thresholds.EncodeHistogram(s.Test.Duration.Hist)
	if err != nil {
		t.Fatal(err)
	}
	if d.HDR != string(want) {
		t.Errorf("hdr is not the EncodeHistogram text")
	}
	for _, key := range []string{"start", "end", "responses", "test", "scenarios", "requests", "failed", "bytes_sent",
		"bytes_received", "checks", "checks_passed", "dropped_iterations", "duration", "iteration_duration", "steps",
		"count", "sum_us", "min_us", "max_us", "hdr"} {
		if !strings.Contains(string(b), `"`+key+`":`) {
			t.Errorf("key %q not written", key)
		}
	}
	// Encoding is deterministic.
	if b2 := mustEncode(t, got); string(b2) != string(mustEncode(t, s)) {
		t.Error("re-encoding differs")
	}
}

// DecodeSnapshot refuses input that is not valid UTF-8, so EncodeSnapshot
// must never write any: a name with invalid bytes is written with U+FFFD.
func TestEncodeSnapshotInvalidUTF8Names(t *testing.T) {
	s := &thresholds.Snapshot{
		Start: t0, End: t0.Add(time.Minute),
		Scenarios: map[string]*thresholds.ScenarioScope{
			"a\xffb": {Steps: map[string]*thresholds.Scope{"c\xfe": {}}},
		},
	}
	b := mustEncode(t, s)
	if !utf8.Valid(b) {
		t.Fatalf("encoded snapshot is not valid UTF-8:\n%q", b)
	}
	got := mustDecode(t, b)
	sc := got.Scenarios["a�b"]
	if sc == nil || sc.Steps["c�"] == nil || len(got.Scenarios) != 1 || len(sc.Steps) != 1 {
		t.Errorf("names decoded as %v", got.Scenarios)
	}
}

func TestEncodeSnapshotErrors(t *testing.T) {
	if _, err := thresholds.EncodeSnapshot(nil); !errors.Is(err, thresholds.ErrInvalidScope) {
		t.Errorf("nil: %v", err)
	}
	s := wireSnapshot(t)
	s.Scenarios["browse"].Steps["home"].TTFB.Hist = hdrhistogram.New(1, 1000, 2)
	if _, err := thresholds.EncodeSnapshot(s); !errors.Is(err, thresholds.ErrHistogramConfig) ||
		!strings.Contains(err.Error(), `scenario "browse", step "home", ttfb`) {
		t.Errorf("other configuration: %v", err)
	}
	s = wireSnapshot(t)
	s.End = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := thresholds.EncodeSnapshot(s); err == nil {
		t.Error("year 10000 encoded")
	}
	// A broken scope is written as it is and refused by the reader.
	s = wireSnapshot(t)
	s.Test.Failed = s.Test.Requests + 1
	if _, err := thresholds.DecodeSnapshot(mustEncode(t, s), thresholds.SnapshotLimits{}); !errors.Is(err, thresholds.ErrInvalidScope) {
		t.Errorf("broken scope: %v", err)
	}
	s = wireSnapshot(t)
	s.Test.TTFB = thresholds.Distribution{SumUs: 5}
	if _, err := thresholds.DecodeSnapshot(mustEncode(t, s), thresholds.SnapshotLimits{}); !errors.Is(err, thresholds.ErrMalformedSnapshot) {
		t.Errorf("count 0 with a sum: %v", err)
	}
}

const (
	wireStart = `"start":"2026-10-16T12:00:00Z","end":"2026-10-16T12:01:00Z","responses":3`
	scopeKeys = `"requests":3,"failed":0,"bytes_sent":0,"bytes_received":0`
	distKeys  = `"count":1,"sum_us":5,"min_us":5,"max_us":5`
)

// validDoc is a small valid snapshot as a generic JSON value, for tests
// that remove or change one key.
func validDoc(t testing.TB) map[string]any {
	t.Helper()
	var m map[string]any
	doc := `{` + wireStart + `,"test":{` + scopeKeys + `,"duration":{` + distKeys + `}},
		"scenarios":{"a":{` + scopeKeys + `,"duration":{` + distKeys + `},"steps":{"b":{` + scopeKeys + `,"ttfb":{` + distKeys + `}}}}}}`
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// at walks path in m and returns the object holding its last key.
func at(m map[string]any, path []string) map[string]any {
	for _, k := range path[:len(path)-1] {
		m = m[k].(map[string]any)
	}
	return m
}

func marshal(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecodeSnapshotRequiredKeys(t *testing.T) {
	mustDecode(t, marshal(t, validDoc(t)))
	var paths [][]string
	for _, k := range []string{"start", "end", "responses", "test"} {
		paths = append(paths, []string{k})
	}
	for _, scope := range [][]string{{"test"}, {"scenarios", "a"}, {"scenarios", "a", "steps", "b"}} {
		for _, k := range []string{"requests", "failed", "bytes_sent", "bytes_received"} {
			paths = append(paths, append(append([]string{}, scope...), k))
		}
	}
	for _, d := range [][]string{{"test", "duration"}, {"scenarios", "a", "duration"}, {"scenarios", "a", "steps", "b", "ttfb"}} {
		for _, k := range []string{"count", "sum_us", "min_us", "max_us"} {
			paths = append(paths, append(append([]string{}, d...), k))
		}
	}
	for _, p := range paths {
		name := strings.Join(p, ".")
		for _, null := range []bool{false, true} {
			m := validDoc(t)
			if null {
				at(m, p)[p[len(p)-1]] = nil
			} else {
				delete(at(m, p), p[len(p)-1])
			}
			_, err := thresholds.DecodeSnapshot(marshal(t, m), thresholds.SnapshotLimits{})
			if !errors.Is(err, thresholds.ErrMalformedSnapshot) || !strings.Contains(err.Error(), "missing") {
				t.Errorf("%s (null %v): %v", name, null, err)
			}
		}
	}
}

func TestDecodeSnapshotRefuses(t *testing.T) {
	head := `{` + wireStart + `,"test":{` + scopeKeys + `}`
	scope := `{` + scopeKeys + `}`
	hdr := func(t testing.TB, us ...int64) string {
		t.Helper()
		b, err := thresholds.EncodeHistogram(dist(t, us...).Hist)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	other, err := hdrhistogram.New(1, 1000, 2).Encode(hdrhistogram.V2CompressedEncodingCookieBase)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, doc string
		want      error
		msg       string
	}{
		{"null test", `{` + wireStart + `,"test":null}`, thresholds.ErrMalformedSnapshot, `missing "test"`},
		{"null scenario", head + `,"scenarios":{"a":null}}`, thresholds.ErrMalformedSnapshot, `"a" is not an object`},
		{"null step", head + `,"scenarios":{"a":{` + scopeKeys + `,"steps":{"b":null}}}}`, thresholds.ErrMalformedSnapshot, `"b" is not an object`},
		{"null scenarios", head + `,"scenarios":null}`, thresholds.ErrMalformedSnapshot, "scenarios is not an object"},
		{"null steps", head + `,"scenarios":{"a":{` + scopeKeys + `,"steps":null}}}`, thresholds.ErrMalformedSnapshot, "steps is not an object"},
		{"scenarios array", head + `,"scenarios":[]}`, thresholds.ErrMalformedSnapshot, "scenarios is not an object"},
		{"scenario array", head + `,"scenarios":{"a":[]}}`, thresholds.ErrMalformedSnapshot, "not an object"},
		{"count 0", head[:len(head)-1] + `,"ttfb":{"count":0,"sum_us":0,"min_us":0,"max_us":0}}}`, thresholds.ErrMalformedSnapshot, "test, ttfb: count is 0"},
		{"count 0 with an hdr", head[:len(head)-1] + `,"duration":{"count":0,"sum_us":0,"min_us":0,"max_us":0,"hdr":"!!!"}}}`, thresholds.ErrMalformedSnapshot, "test, duration: count is 0"},
		{"long start", strings.Replace(head, `12:00:00Z`, `12:00:00.`+strings.Repeat("1", 60)+`Z`, 1) + `}`, thresholds.ErrMalformedSnapshot, `start: not an RFC 3339 time`},
		{"duplicate scenario", head + `,"scenarios":{"a":` + scope + `,"a":` + scope + `}}`, thresholds.ErrMalformedSnapshot, `"a" appears twice`},
		{"duplicate scenario, escaped", head + `,"scenarios":{"a":` + scope + `,"` + esc("0061") + `":` + scope + `}}`, thresholds.ErrMalformedSnapshot, "appears twice"},
		{"duplicate scenario, lone surrogates", head + `,"scenarios":{"` + esc("d800") + `":` + scope + `,"` + esc("dc00") + `":` + scope + `}}`, thresholds.ErrMalformedSnapshot, "appears twice"},
		{"invalid UTF-8 name", head + `,"scenarios":{"` + "\xff" + `":` + scope + `}}`, thresholds.ErrMalformedSnapshot, "not valid UTF-8"},
		{"invalid UTF-8 unknown key", head + `,"` + "\xff" + `":0}`, thresholds.ErrMalformedSnapshot, "not valid UTF-8"},
		{"invalid UTF-8 hdr", head[:len(head)-1] + `,"ttfb":{` + distKeys + `,"hdr":"` + "\xff" + `"}}}`, thresholds.ErrMalformedSnapshot, "not valid UTF-8"},
		{"duplicate step", head + `,"scenarios":{"a":{` + scopeKeys + `,"steps":{"b":` + scope + `,"b":` + scope + `}}}}`, thresholds.ErrMalformedSnapshot, `steps: "b" appears twice`},
		{"negative responses", strings.Replace(head, `"responses":3`, `"responses":-1`, 1) + `}`, thresholds.ErrInvalidScope, "negative responses"},
		{"responses overflow", strings.Replace(head, `"responses":3`, `"responses":9223372036854775808`, 1) + `}`, thresholds.ErrMalformedSnapshot, "responses: wrong type"},
		{"responses float", strings.Replace(head, `"responses":3`, `"responses":1.5`, 1) + `}`, thresholds.ErrMalformedSnapshot, "responses: wrong type"},
		{"zero start", strings.Replace(head, `2026-10-16T12:00:00Z`, `0001-01-01T00:00:00Z`, 1) + `}`, thresholds.ErrInvalidScope, "no start"},
		{"end before start", strings.Replace(head, `12:01:00Z`, `11:59:00Z`, 1) + `}`, thresholds.ErrInvalidScope, "ends before it starts"},
		{"bad start", strings.Replace(head, `2026-10-16T12:00:00Z`, `yesterday`, 1) + `}`, thresholds.ErrMalformedSnapshot, `start: not an RFC 3339 time`},
		{"start number", strings.Replace(head, `"2026-10-16T12:00:00Z"`, `5`, 1) + `}`, thresholds.ErrMalformedSnapshot, `start: not an RFC 3339 time`},
		{"negative counter", head + `,"scenarios":{"a":{` + strings.Replace(scopeKeys, `"failed":0`, `"failed":-1`, 1) + `}}}`, thresholds.ErrInvalidScope, `scenario "a": invalid scope: negative counter`},
		{"failed over requests", head + `,"scenarios":{"a":{"steps":{"b":{` + strings.Replace(scopeKeys, `"failed":0`, `"failed":4`, 1) + `}},` + scopeKeys + `}}}`, thresholds.ErrInvalidScope, `step "b": invalid scope`},
		{"bad hdr", head[:len(head)-1] + `,"duration":{` + distKeys + `,"hdr":"!!!"}}}`, thresholds.ErrMalformedHistogram, "test, duration: malformed histogram"},
		{"hdr configuration", head[:len(head)-1] + `,"duration":{` + distKeys + `,"hdr":"` + string(other) + `"}}}`, thresholds.ErrHistogramConfig, "test, duration"},
		{"hdr over count", head[:len(head)-1] + `,"duration":{` + distKeys + `,"hdr":"` + hdr(t, 5, 6) + `"}}}`, thresholds.ErrInvalidScope, "histogram holds more values"},
		{"top array", `[]`, thresholds.ErrMalformedSnapshot, "not an object"},
		{"top null", `null`, thresholds.ErrMalformedSnapshot, `missing "start"`},
		{"invalid JSON", head, thresholds.ErrMalformedSnapshot, "invalid JSON"},
		{"trailing data", head + `}{}`, thresholds.ErrMalformedSnapshot, "invalid JSON"},
		{"counter string", head + `,"scenarios":{"a":{` + strings.Replace(scopeKeys, `"failed":0`, `"failed":"0"`, 1) + `}}}`, thresholds.ErrMalformedSnapshot, `scenario "a": failed: wrong type`},
		{"empty", ``, thresholds.ErrMalformedSnapshot, "invalid JSON"},
		{"top-level key over 32 bytes", head + `,"` + strings.Repeat("x", 33) + `":0}`, thresholds.ErrMalformedSnapshot, "malformed snapshot: a key is longer than 32 bytes"},
		{"top-level escaped key", strings.Replace(head, `"test"`, `"`+esc("0074")+`est"`, 1) + `}`, thresholds.ErrMalformedSnapshot, "a key is longer than 32 bytes, has an escape"},
		{"top-level non-ASCII key", head + `,"ſcenarios":{}}`, thresholds.ErrMalformedSnapshot, "is not ASCII"},
		{"test key", head[:len(head)-1] + `,"é":0}}`, thresholds.ErrMalformedSnapshot, "test: a key is"},
		{"test key, folded", strings.Replace(head, `"test"`, `"tEsT"`, 1)[:len(head)-1] + `,"é":0}}`, thresholds.ErrMalformedSnapshot, "test: a key is"},
		{"duplicate test", head + `,"test":` + scope + `}`, thresholds.ErrMalformedSnapshot, `"test" appears twice`},
		{"duplicate test, folded", head + `,"Test":null}`, thresholds.ErrMalformedSnapshot, `"test" appears twice`},
		{"duplicate distribution", head[:len(head)-1] + `,"ttfb":null,"ttfb":{` + distKeys + `}}}`, thresholds.ErrMalformedSnapshot, `test: "ttfb" appears twice`},
		{"duplicate distribution, folded", head + `,"scenarios":{"a":{` + scopeKeys + `,"Iteration_Duration":{` + distKeys + `},"iteration_duration":{` + distKeys + `}}}}`, thresholds.ErrMalformedSnapshot, `scenario "a": "iteration_duration" appears twice`},
		{"duplicate step distribution", head + `,"scenarios":{"a":{` + scopeKeys + `,"steps":{"b":{` + scopeKeys + `,"duration":{` + distKeys + `},"DURATION":null}}}}}`, thresholds.ErrMalformedSnapshot, `scenario "a", step "b": "duration" appears twice`},
		{"duplicate start", head + `,"start":"2026-10-16T12:00:00Z"}`, thresholds.ErrMalformedSnapshot, `malformed snapshot: "start" appears twice`},
		{"duplicate end, folded, offset", head + `,"End":"2026-10-16T12:01:00+00:01"}`, thresholds.ErrMalformedSnapshot, `malformed snapshot: "end" appears twice`},
		{"duplicate responses", strings.Replace(head, `"responses":3`, `"responses":-1,"future":[{"x":1}],"responses":3`, 1) + `}`, thresholds.ErrMalformedSnapshot, `malformed snapshot: "responses" appears twice`},
		{"duplicate responses, null", strings.Replace(head, `"responses":3`, `"responses":null,"RESPONSES":3`, 1) + `}`, thresholds.ErrMalformedSnapshot, `"responses" appears twice`},
		{"duplicate scenarios", head + `,"scenarios":{},"scenarios":{"a":` + scope + `}}`, thresholds.ErrMalformedSnapshot, `malformed snapshot: "scenarios" appears twice`},
		{"duplicate counter", head[:len(head)-1] + `,"requests":3}}`, thresholds.ErrMalformedSnapshot, `test: "requests" appears twice`},
		{"duplicate optional counter, null first", head[:len(head)-1] + `,"checks":null,"checks":5}}`, thresholds.ErrMalformedSnapshot, `test: "checks" appears twice`},
		{"duplicate scenario counter", head + `,"scenarios":{"a":{` + scopeKeys + `,"Bytes_Sent":0}}}`, thresholds.ErrMalformedSnapshot, `scenario "a": "bytes_sent" appears twice`},
		{"duplicate steps", head + `,"scenarios":{"a":{` + scopeKeys + `,"steps":{},"steps":{"b":` + scope + `}}}}`, thresholds.ErrMalformedSnapshot, `scenario "a": "steps" appears twice`},
		{"duplicate step counter", head + `,"scenarios":{"a":{` + scopeKeys + `,"steps":{"b":{` + scopeKeys + `,"dropped_iterations":1,"dropped_iterations":1}}}}}`, thresholds.ErrMalformedSnapshot, `scenario "a", step "b": "dropped_iterations" appears twice`},
		{"duplicate count", head[:len(head)-1] + `,"ttfb":{` + distKeys + `,"count":1}}}`, thresholds.ErrMalformedSnapshot, `test, ttfb: "count" appears twice`},
		{"duplicate hdr, folded", head[:len(head)-1] + `,"duration":{` + distKeys + `,"hdr":"x","HDR":"y"}}}`, thresholds.ErrMalformedSnapshot, `test, duration: "hdr" appears twice`},
		{"duplicate step hdr", head + `,"scenarios":{"a":{` + scopeKeys + `,"steps":{"b":{` + scopeKeys + `,"ttfb":{` + distKeys + `,"hdr":null,"hdr":"x"}}}}}}`, thresholds.ErrMalformedSnapshot, `scenario "a", step "b", ttfb: "hdr" appears twice`},
		{"distribution key", head[:len(head)-1] + `,"TTFB":{` + distKeys + `,"` + esc("0078") + `":0}}}`, thresholds.ErrMalformedSnapshot, "test, ttfb: a key is"},
		{"scenario key", head + `,"scenarios":{"a":{` + scopeKeys + `,"` + strings.Repeat("x", 33) + `":0}}}`, thresholds.ErrMalformedSnapshot, `scenario "a": a key is`},
		{"step key", head + `,"scenarios":{"a":{` + scopeKeys + `,"steps":{"b":{` + scopeKeys + `,"é":0}}}}}`, thresholds.ErrMalformedSnapshot, `scenario "a", step "b": a key is`},
		{"step distribution key", head + `,"scenarios":{"a":{` + scopeKeys + `,"steps":{"b":{` + scopeKeys + `,"iteration_duration":{` + distKeys + `,"é":0}}}}}}`, thresholds.ErrMalformedSnapshot, `scenario "a", step "b", iteration_duration: a key is`},
		{"escaped steps key", head + `,"scenarios":{"a":{` + scopeKeys + `,"` + esc("0073") + `teps":{}}}}`, thresholds.ErrMalformedSnapshot, `scenario "a": a key is`},
		{"escaped hdr", head[:len(head)-1] + `,"duration":{` + distKeys + `,"hdr":"` + esc("0041") + `"}}}`, thresholds.ErrMalformedSnapshot, "hdr: not a string of at most 524288 bytes without escapes"},
		{"hdr number", head[:len(head)-1] + `,"duration":{` + distKeys + `,"hdr":5}}}`, thresholds.ErrMalformedSnapshot, "hdr: not a string"},
		{"scenario hdr object", head + `,"scenarios":{"a":{` + scopeKeys + `,"ttfb":{` + distKeys + `,"hdr":{}}}}}`, thresholds.ErrMalformedSnapshot, `scenario "a": ttfb.hdr: not a string`},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := thresholds.DecodeSnapshot([]byte(c.doc), thresholds.SnapshotLimits{})
			if !errors.Is(err, c.want) || !strings.Contains(err.Error(), c.msg) {
				t.Errorf("got %v, want %v containing %q", err, c.want, c.msg)
			}
			if err != nil && !strings.HasPrefix(err.Error(), "thresholds: ") {
				t.Errorf("message %q lacks the package prefix", err)
			}
		})
	}
	// Unknown keys are ignored, and may repeat; steps is unknown outside
	// a scenario.
	s := mustDecode(t, []byte(strings.Replace(head, `"responses":3`, `"future":[{"x":1}],"responses":3,"FUTURE":2,"future":null`, 1)+
		`,"scenarios":{"a":{`+scopeKeys+`,"steps":{"b":{`+scopeKeys+`,"steps":1,"steps":2}}}}}`))
	if s.Responses != 3 || s.Scenarios["a"].Steps["b"] == nil {
		t.Errorf("unknown keys: %+v", s)
	}
	s = mustDecode(t, []byte(head[:len(head)-1]+`,"steps":1,"steps":2}}`))
	if s.Test.Requests != 3 {
		t.Errorf("steps in test: %+v", s.Test)
	}
	// Optional counters may be null: not measured.
	s = mustDecode(t, []byte(head[:len(head)-1]+`,"checks":null}}`))
	if s.Test.Checks != nil {
		t.Errorf("null checks = %v", *s.Test.Checks)
	}
	// Keys match ignoring ASCII case, and an unknown key of 32 bytes is
	// ignored.
	s = mustDecode(t, []byte(`{"START":"2026-10-16T12:00:00Z","End":"2026-10-16T12:01:00Z","responses":3,"Test":{"REQUESTS":3,"failed":0,"bytes_sent":0,"bytes_received":0,"Duration":{`+
		distKeys+`,"`+strings.Repeat("x", 32)+`":0}},"`+strings.Repeat("x", 32)+`":0,"Scenarios":{"a":{`+scopeKeys+`,"STEPS":{"b":`+scope+`}}}}`))
	if s.Test.Requests != 3 || s.Test.Duration.Count != 1 || s.Scenarios["a"] == nil || s.Scenarios["a"].Steps["b"] == nil {
		t.Errorf("case-folded keys: %+v", s)
	}
	// A null distribution is no distribution.
	s = mustDecode(t, []byte(head[:len(head)-1]+`,"duration":null}}`))
	if s.Test.Duration.Count != 0 {
		t.Errorf("null duration: %+v", s.Test.Duration)
	}
	// A null hdr is no histogram.
	s = mustDecode(t, []byte(head[:len(head)-1]+`,"duration":{`+distKeys+`,"hdr":null}}}`))
	if s.Test.Duration.Count != 1 || s.Test.Duration.Hist != nil {
		t.Errorf("null hdr: %+v", s.Test.Duration)
	}
}

// Messages stay short whatever the input repeats.
func TestDecodeSnapshotMessagesBounded(t *testing.T) {
	long := strings.Repeat("9", 100_000)
	name := strings.Repeat("n", 5000)
	for _, doc := range []string{
		`{` + wireStart + `,"test":{"requests":` + long + `}}`,
		`{` + strings.Replace(wireStart, "12:00:00Z", "12:00:00Z"+long, 1) + `,"test":{` + scopeKeys + `}}`,
		`{` + wireStart + `,"test":{` + scopeKeys + `},"scenarios":{"` + name + `":{}}}`,
		`{` + wireStart + `,"test":{` + scopeKeys + `},"scenarios":{"` + name[:4000] + `":{}}}`,
		`{` + wireStart + `,"test":{` + scopeKeys + `},"scenarios":{"` + name[:4000] + `":{` + scopeKeys + `,"failed":9}}}`,
	} {
		_, err := thresholds.DecodeSnapshot([]byte(doc), thresholds.SnapshotLimits{})
		if err == nil {
			t.Fatal("accepted")
		}
		if len(err.Error()) > 400 {
			t.Errorf("message of %d bytes: %.200s", len(err.Error()), err)
		}
	}
}

// docWithScopes is a valid snapshot with n scopes in all (the test, one
// scenario and n-2 steps under it, n >= 2), the first hists of them with
// a histogram.
func docWithScopes(t testing.TB, n, hists int) string {
	t.Helper()
	h, err := thresholds.EncodeHistogram(dist(t, 5).Hist)
	if err != nil {
		t.Fatal(err)
	}
	withHist := func() string {
		if hists == 0 {
			return ""
		}
		hists--
		return `,"duration":{` + distKeys + `,"hdr":"` + string(h) + `"}`
	}
	var b strings.Builder
	b.WriteString(`{` + wireStart + `,"test":{` + scopeKeys + withHist() + `},"scenarios":{"s":{` + scopeKeys + withHist() + `,"steps":{`)
	for i := 0; i < n-2; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"%d":{%s%s}`, i, scopeKeys, withHist())
	}
	b.WriteString(`}}}}`)
	if hists > 0 {
		t.Fatalf("%d histograms left over", hists)
	}
	return b.String()
}

func TestDecodeSnapshotLimits(t *testing.T) {
	type lim = thresholds.SnapshotLimits
	ok := func(t *testing.T, doc string, l lim) {
		t.Helper()
		if _, err := thresholds.DecodeSnapshot([]byte(doc), l); err != nil {
			t.Errorf("at the cap: %v", err)
		}
	}
	over := func(t *testing.T, doc string, l lim, msg string) {
		t.Helper()
		_, err := thresholds.DecodeSnapshot([]byte(doc), l)
		if !errors.Is(err, thresholds.ErrMalformedSnapshot) || !strings.Contains(err.Error(), msg) {
			t.Errorf("over the cap: %v, want %q", err, msg)
		}
	}
	t.Run("MaxBytes", func(t *testing.T) {
		doc := docWithScopes(t, 3, 0)
		l := lim{MaxBytes: len(doc) + 10}
		ok(t, doc+strings.Repeat(" ", 10), l)
		over(t, doc+strings.Repeat(" ", 11), l, "over the limit of")
	})
	t.Run("MaxScopes", func(t *testing.T) {
		l := lim{MaxScopes: 7}
		ok(t, docWithScopes(t, 7, 0), l)
		over(t, docWithScopes(t, 8, 0), l, "more than 7 scopes")
		// Scenarios and steps count together.
		doc := `{` + wireStart + `,"test":{` + scopeKeys + `},"scenarios":{"a":{` + scopeKeys + `,"steps":{"1":{` + scopeKeys + `}}},"b":{` + scopeKeys + `,"steps":{"1":{` + scopeKeys + `}}}}}`
		ok(t, doc, lim{MaxScopes: 5})
		over(t, doc, lim{MaxScopes: 4}, "more than 4 scopes")
	})
	t.Run("MaxHistograms", func(t *testing.T) {
		l := lim{MaxHistograms: 4}
		ok(t, docWithScopes(t, 6, 4), l)
		over(t, docWithScopes(t, 6, 5), l, "more than 4 histograms")
	})
	t.Run("MaxNameBytes", func(t *testing.T) {
		l := lim{MaxNameBytes: 12}
		named := func(raw string) string {
			return `{` + wireStart + `,"test":{` + scopeKeys + `},"scenarios":{"` + raw + `":{` + scopeKeys + `}}}`
		}
		ok(t, named(strings.Repeat("n", 12)), l)
		over(t, named(strings.Repeat("n", 13)), l, "longer than 12 bytes")
		// Measured once decoded.
		ok(t, named(strings.Repeat(esc("00e9"), 6)), l)
		over(t, named(strings.Repeat(esc("00e9"), 6)+"x"), l, "longer than 12 bytes")
		ok(t, named(strings.Repeat("é", 6)), l)
		over(t, named(strings.Repeat("é", 6)+"x"), l, "longer than 12 bytes")
		// Each lone surrogate becomes U+FFFD, three bytes.
		ok(t, named(strings.Repeat(esc("d800"), 4)), l)
		over(t, named(strings.Repeat(esc("d800"), 5)), l, "longer than 12 bytes")
		// Raw invalid UTF-8 is refused before any name is read.
		over(t, named("\xff"), l, "not valid UTF-8")
		// Steps too.
		step := func(name string) string {
			return `{` + wireStart + `,"test":{` + scopeKeys + `},"scenarios":{"a":{` + scopeKeys + `,"steps":{"` + name + `":{` + scopeKeys + `}}}}}`
		}
		ok(t, step(strings.Repeat("n", 12)), l)
		over(t, step(strings.Repeat("n", 13)), l, "longer than 12 bytes")
	})
	t.Run("hdr length", func(t *testing.T) {
		// 512 KiB of base64 reaches DecodeHistogram, which refuses the
		// text itself; one more byte is refused unread.
		doc := func(n int) string {
			return `{` + wireStart + `,"test":{` + scopeKeys + `,"duration":{` + distKeys + `,"hdr":"` + strings.Repeat("A", n) + `"}}}`
		}
		_, err := thresholds.DecodeSnapshot([]byte(doc(512<<10)), lim{})
		if !errors.Is(err, thresholds.ErrMalformedHistogram) {
			t.Errorf("at the cap: %v, want ErrMalformedHistogram", err)
		}
		over(t, doc(512<<10+1), lim{}, "test.duration.hdr: not a string of at most 524288 bytes")
	})
	t.Run("defaults", func(t *testing.T) {
		if _, err := thresholds.DecodeSnapshot([]byte(docWithScopes(t, 1024, 256)), lim{}); err != nil {
			t.Errorf("1024 scopes, 256 histograms: %v", err)
		}
		over(t, docWithScopes(t, 1025, 0), lim{}, "more than 1024 scopes")
		over(t, docWithScopes(t, 300, 257), lim{}, "more than 256 histograms")
		over(t, docWithScopes(t, 3, 0)+strings.Repeat(" ", 32<<20), lim{}, "over the limit of 33554432")
		name := strings.Repeat("n", 4096)
		ok(t, `{`+wireStart+`,"test":{`+scopeKeys+`},"scenarios":{"`+name+`":{`+scopeKeys+`}}}`, lim{})
		over(t, `{`+wireStart+`,"test":{`+scopeKeys+`},"scenarios":{"`+name+`n":{`+scopeKeys+`}}}`, lim{}, "longer than 4096 bytes")
	})
}

// flood builds a payload of exactly size bytes: head, then items
// separated by commas until the next would not fit, then tail, padded
// with spaces before tail.
func flood(size int, head string, item func(i int) string, tail string) []byte {
	b := make([]byte, 0, size)
	b = append(b, head...)
	for i := 0; ; i++ {
		it := item(i)
		if i > 0 {
			it = "," + it
		}
		if len(b)+len(it)+len(tail) > size {
			break
		}
		b = append(b, it...)
	}
	for len(b)+len(tail) < size {
		b = append(b, ' ')
	}
	return append(b, tail...)
}

// filler is an unknown key holding an array of numbers, about n bytes.
func filler(n int) string {
	return `"x":[` + strings.TrimSuffix(strings.Repeat("1.5,", max(n/4, 1)), ",") + `]`
}

// longKey repeats r to about n bytes.
func longKey(r string, n int) string { return strings.Repeat(r, n/len(r)) }

func TestDecodeSnapshotAllocBound(t *testing.T) {
	size := 32 << 20
	if raceEnabled {
		size = 4 << 20
	}
	lim := thresholds.SnapshotLimits{MaxBytes: size}
	// The test scope carries an invalid hdr: if a histogram were decoded
	// before the limits are checked, the error would be
	// ErrMalformedHistogram.
	head := `{` + wireStart + `,"test":{` + scopeKeys + `,"duration":{` + distKeys + `,"hdr":"!!!"}}`
	scope := `{` + scopeKeys + `}`
	num := func(s string) func(int) string {
		return func(i int) string { return `"` + strconv.Itoa(i) + `":` + s }
	}
	threeHists := `{` + scopeKeys + `,"duration":{` + distKeys + `,"hdr":"x"},"ttfb":{` + distKeys + `,"hdr":"x"},"iteration_duration":{` + distKeys + `,"hdr":"x"}}`
	steps := func(key string, k int) func(int) string {
		return func(i int) string {
			var b strings.Builder
			fmt.Fprintf(&b, `"%d":{%s,%q:{`, i, scopeKeys, key)
			for j := 0; j < k; j++ {
				if j > 0 {
					b.WriteByte(',')
				}
				fmt.Fprintf(&b, `"%d":%s`, j, scope)
			}
			b.WriteString(`}}`)
			return b.String()
		}
	}
	type payload struct {
		name string
		doc  []byte
		// linear: encoding/json copies something per key, so allocations
		// grow with the input; only the bytes are bounded.
		linear bool
		// copies of the input allocated at most, 3 when zero.
		copies uint64
		// want is the error, ErrMalformedSnapshot when nil.
		want error
	}
	third := size/3 - 200
	payloads := []payload{
		{name: "(a) empty scopes", doc: flood(size, head+`,"scenarios":{`, num(`{}`), `}}`)},
		{name: "(a) valid scopes", doc: flood(size, head+`,"scenarios":{`, num(scope), `}}`)},
		{name: "(b) steps under one scenario", doc: flood(size, head+`,"scenarios":{"s":{`+scopeKeys+`,"steps":{`, num(scope), `}}}}`)},
		{name: "(c) steps spread", doc: flood(size, head+`,"scenarios":{`, steps("steps", 300), `}}`)},
		{name: "(d) duplicate scenarios", doc: flood(size, head+`,"scenarios":{`, func(int) string { return `"a":` + scope }, `}}`)},
		{name: "(d) duplicate steps", doc: flood(size, head+`,"scenarios":{"s":{`+scopeKeys+`,"steps":{`, func(int) string { return `"a":` + scope }, `}}}}`)},
		{name: "(e) Scenarios", doc: flood(size, head+`,"Scenarios":{`, num(scope), `}}`)},
		{name: "(e) SCENARIOS", doc: flood(size, head+`,"SCENARIOS":{`, num(scope), `}}`)},
		{name: "(e) ſcenarios", doc: flood(size, head+`,"ſcenarios":{`, num(scope), `}}`)},
		{name: "(e) ſcenarios, empty scopes", doc: flood(size, head+`,"ſcenarios":{`, num(`{}`), `}}`)},
		{name: "(e) STEPS", doc: flood(size, head+`,"scenarios":{"s":{`+scopeKeys+`,"STEPS":{`, num(scope), `}}}}`)},
		{name: "(e) STEPS spread", doc: flood(size, head+`,"scenarios":{`, steps("STEPS", 300), `}}`)},
		{name: "(f) unknown keys", doc: []byte(head[:len(head)-1] + `,` + filler(third) + `},` + filler(third) +
			`,"scenarios":{"a":{` + scopeKeys + `,` + filler(third) + `},"a":` + scope + `}}`)},
		{name: "(g) histograms", doc: flood(size, head[:len(head)-1]+`,`+filler(size-300_000)+`},"scenarios":{`, num(threeHists), `}}`)},
		// Inputs that make encoding/json allocate per key.
		{name: "escaped unknown keys", doc: flood(size, head+`,"scenarios":{"a":{`+scopeKeys+`,`, func(int) string { return `"` + esc("0078") + `":0` }, `},"a":`+scope+`}}`)},
		{name: "long unknown keys", doc: flood(size, head+`,"scenarios":{"a":{`+scopeKeys+`,`, func(int) string { return `"` + strings.Repeat("y", 40) + `":0` }, `},"a":`+scope+`}}`)},
		{name: "null toggles", doc: flood(size, head+`,"scenarios":{"a":{`+scopeKeys+`,`, func(int) string { return `"duration":null,"duration":{` + distKeys + `}` }, `},"a":`+scope+`}}`)},
		// After a null, encoding/json allocates the object again.
		{name: "test toggles", doc: flood(size, `{`+wireStart+`,`, func(int) string { return `"test":null,"test":{}` }, `}`)},
		{name: "distribution toggles in test", doc: flood(size, `{`+wireStart+`,"test":{`+scopeKeys+`,`, func(int) string { return `"ttfb":null,"TTFB":{}` }, `}}`)},
		{name: "distribution toggles in a scenario", doc: flood(size, `{`+wireStart+`,"test":`+scope+`,"scenarios":{"s":{`+scopeKeys+`,`, func(int) string { return `"ttfb":null,"ttfb":{}` }, `}}}`)},
		{name: "distribution toggles in a step", doc: flood(size, `{`+wireStart+`,"test":`+scope+`,"scenarios":{"s":{`+scopeKeys+`,"steps":{"b":{`+scopeKeys+`,`, func(int) string { return `"ttfb":null,"ttfb":{}` }, `}}}}}`)},
		{name: "full distribution toggles in a step", doc: flood(size, `{`+wireStart+`,"test":`+scope+`,"scenarios":{"s":{`+scopeKeys+`,"steps":{"b":{`+scopeKeys+`,`, func(int) string { return `"duration":null,"duration":{` + distKeys + `}` }, `}}}}}`)},
		// Every occurrence of a key is decoded: a time whose offset is not
		// whole hours allocates a time zone, and an hdr is copied.
		{name: "repeated start, offset", doc: flood(size, `{`, func(int) string { return `"start":"0001-01-01T00:00:00+00:01"` },
			`,"start":"2026-10-16T11:00:00Z","end":"2026-10-16T12:00:00Z","responses":0,"test":`+scope+`}`)},
		{name: "repeated end, offset", doc: flood(size, `{`, func(int) string { return `"end":"2026-10-16T12:00:00+00:01"` },
			`,"start":"2026-10-16T11:00:00Z","responses":0,"test":`+scope+`}`)},
		{name: "repeated hdr in test", doc: flood(size, `{`+wireStart+`,"test":{`+scopeKeys+`,"duration":{`+distKeys+`,`, func(int) string { return `"hdr":"ABCDEFGH"` }, `}}}`)},
		{name: "repeated hdr in a step", doc: flood(size, `{`+wireStart+`,"test":`+scope+`,"scenarios":{"s":{`+scopeKeys+`,"steps":{"b":{`+scopeKeys+`,"duration":{`+distKeys+`,`,
			func(int) string { return `"hdr":"` + strings.Repeat("A", 1537) + `"` }, `}}}}}}`)},
		{name: "repeated steps", doc: flood(size, `{`+wireStart+`,"test":`+scope+`,"scenarios":{"s":{`+scopeKeys+`,`, func(int) string { return `"steps":{"b":` + scope + `}` }, `}}}`)},
		{name: "repeated scenarios", doc: flood(size, `{`+wireStart+`,"test":`+scope+`,`, func(int) string { return `"scenarios":{"s":` + scope + `}` }, `}`)},
		{name: "repeated counter in a step", doc: flood(size, `{`+wireStart+`,"test":`+scope+`,"scenarios":{"s":{`+scopeKeys+`,"steps":{"b":{`+scopeKeys+`,`, func(int) string { return `"checks":null,"checks":7` }, `}}}}}`)},
		// One huge value.
		{name: "one long name", doc: []byte(head + `,"scenarios":{"` + strings.Repeat("n", size-len(head)-100) + `":` + scope + `}}`)},
		{name: "one long escaped name", doc: []byte(head + `,"scenarios":{"` + strings.Repeat(esc("00e9"), (size-len(head)-100)/6) + `":` + scope + `}}`)},
		{name: "one long number", doc: []byte(head[:len(head)-1] + `,"checks":` + strings.Repeat("7", size-len(head)-100) + `}}`)},
		{name: "one long time", doc: []byte(`{"start":"2026-10-16T12:00:00.` + strings.Repeat("1", size-200) + `xZ","end":"2026-10-16T12:01:00Z","responses":3,"test":` + scope + `}`)},
		{name: "long number for a hdr", doc: []byte(`{` + wireStart + `,"test":{` + scopeKeys + `,"duration":{` + distKeys + `,"hdr":` + strings.Repeat("7", size-300) + `}}}`)},
		{name: "long array for a counter", doc: []byte(`{` + wireStart + `,"test":{` + scopeKeys + `,"checks":[` + strings.Repeat("7,", (size-300)/2) + `7]}}`)},
		{name: "one long hdr", doc: []byte(`{` + wireStart + `,"test":{` + scopeKeys + `,"duration":{` + distKeys + `,"hdr":"` + strings.Repeat("A", size-300) + `"}}}`)},
		// encoding/json unquotes an escaped string into a new buffer, then
		// copies it; at step depth the input is already held twice.
		{name: "escaped step hdr", doc: []byte(`{` + wireStart + `,"test":` + scope + `,"scenarios":{"s":{` + scopeKeys + `,"steps":{"b":{` + scopeKeys + `,"duration":{` + distKeys + `,"hdr":"` + strings.Repeat(esc("d800"), (size-400)/6) + `"}}}}}}`)},
		{name: "escaped step hdr, euro", doc: []byte(`{` + wireStart + `,"test":` + scope + `,"scenarios":{"s":{` + scopeKeys + `,"steps":{"b":{` + scopeKeys + `,"duration":{` + distKeys + `,"hdr":"` + strings.Repeat(esc("20ac"), (size-400)/6) + `"}}}}}}`)},
		// A key matched against struct fields that is long or not ASCII:
		// encoding/json folds it into a growing buffer, then ignores it.
		{name: "long non-ASCII key at the top", doc: []byte(`{` + wireStart + `,"test":` + scope + `,"` + longKey("\U0001F600", size-300) + `":0}`)},
		{name: "long non-ASCII key in test", doc: []byte(`{` + wireStart + `,"test":{` + scopeKeys + `,"` + longKey("\U0001F600", size-300) + `":0}}`)},
		{name: "long non-ASCII key in TEST", doc: []byte(`{` + wireStart + `,"TEST":{` + scopeKeys + `,"` + longKey("\U0001F600", size-300) + `":0}}`)},
		{name: "long non-ASCII key in a scenario", doc: []byte(`{` + wireStart + `,"test":` + scope + `,"scenarios":{"s":{` + scopeKeys + `,"` + longKey("\U0001F600", size-300) + `":0}}}`)},
		{name: "long non-ASCII key in a step", doc: []byte(`{` + wireStart + `,"test":` + scope + `,"scenarios":{"s":{` + scopeKeys + `,"steps":{"b":{` + scopeKeys + `,"` + longKey("\U0001F600", size-400) + `":0}}}}}`)},
		{name: "long non-ASCII key in a step's Duration", doc: []byte(`{` + wireStart + `,"test":` + scope + `,"scenarios":{"s":{` + scopeKeys + `,"steps":{"b":{` + scopeKeys + `,"Duration":{` + distKeys + `,"` + longKey("\U0001F600", size-400) + `":0}}}}}}`)},
		{name: "long é key in test", doc: []byte(`{` + wireStart + `,"test":{` + scopeKeys + `,"` + longKey("é", size-300) + `":0}}`)},
		{name: "long ſ key in test", doc: []byte(`{` + wireStart + `,"test":{` + scopeKeys + `,"` + longKey("ſ", size-300) + `":0}}`)},
		{name: "long ASCII key in a step", doc: []byte(`{` + wireStart + `,"test":` + scope + `,"scenarios":{"s":{` + scopeKeys + `,"steps":{"b":{` + scopeKeys + `,"` + longKey("y", size-400) + `":0}}}}}`)},
		{name: "33-byte keys in a step", doc: flood(size, `{`+wireStart+`,"test":`+scope+`,"scenarios":{"s":{`+scopeKeys+`,"steps":{"b":{`+scopeKeys+`,`, func(int) string { return `"` + strings.Repeat("y", 33) + `":0` }, `}}}}}`)},
		{name: "escaped keys in a step", doc: flood(size, `{`+wireStart+`,"test":`+scope+`,"scenarios":{"s":{`+scopeKeys+`,"steps":{"b":{`+scopeKeys+`,`, func(int) string { return `"` + esc("0078") + `":0` }, `}}}}}`)},
		// Invalid UTF-8: encoding/json would replace each byte with the
		// three-byte U+FFFD in a growing buffer before any limit applies.
		{name: "invalid UTF-8 step hdr", doc: []byte(`{` + wireStart + `,"test":{` + scopeKeys + `},"scenarios":{"s":{` + scopeKeys + `,"steps":{"b":{` + scopeKeys + `,"duration":{` + distKeys + `,"hdr":"` + strings.Repeat("\xff", size-400) + `"}}}}}}`)},
		{name: "invalid UTF-8 test", doc: []byte(`{` + wireStart + `,"test":"` + strings.Repeat("\xff", size-200) + `"}`)},
		{name: "invalid UTF-8 duration", doc: []byte(`{` + wireStart + `,"test":{` + scopeKeys + `,"duration":"` + strings.Repeat("\xff", size-300) + `"}}`)},
		{name: "invalid UTF-8 top-level key", doc: []byte(`{` + wireStart + `,"test":` + scope + `,"` + strings.Repeat("\xff", size-300) + `":0}`)},
		{name: "invalid UTF-8 key in test", doc: []byte(`{` + wireStart + `,"test":{` + scopeKeys + `,"` + strings.Repeat("\xff", size-300) + `":0}}`)},
		{name: "invalid UTF-8 last byte", doc: []byte(`{` + wireStart + `,"test":{` + scopeKeys + `,"duration":{` + distKeys + `,"hdr":"` + strings.Repeat("A", size-300) + "\xff" + `"}}}`)},
		{name: "deep nesting", doc: []byte(head[:len(head)-1] + `,"x":` + strings.Repeat("[", 9000) + strings.Repeat("]", 9000) + `},"scenarios":{"a":{` + scopeKeys + `,"x":` + strings.Repeat(`{"y":`, 9000) + `0` + strings.Repeat("}", 9000) + `},"a":` + scope + `}}`)},
	}
	for _, p := range payloads {
		t.Run(p.name, func(t *testing.T) {
			if len(p.doc) > size {
				t.Fatalf("payload is %d bytes, over %d", len(p.doc), size)
			}
			if !json.Valid(p.doc) {
				t.Fatal("payload is not valid JSON")
			}
			var err error
			elapsed, alloc, mallocs := measure(func() { _, err = thresholds.DecodeSnapshot(p.doc, lim) })
			t.Logf("%d bytes: %v, %d KiB (%.2fx) in %d allocations: %v", len(p.doc), elapsed, alloc>>10, float64(alloc)/float64(len(p.doc)), mallocs, err)
			switch {
			case p.want != nil && !errors.Is(err, p.want):
				t.Errorf("error %v, want %v", err, p.want)
			case p.want == nil && (!errors.Is(err, thresholds.ErrMalformedSnapshot) || errors.Is(err, thresholds.ErrMalformedHistogram)):
				t.Errorf("error %v, want ErrMalformedSnapshot", err)
			}
			copies := p.copies
			if copies == 0 {
				copies = 3
			}
			if limit := copies*uint64(len(p.doc)) + 2<<20; alloc > limit {
				t.Errorf("%d bytes allocated, over %d", alloc, limit)
			}
			// A small constant per scope, at most 1024 scopes.
			limit := uint64(8 * 1025)
			if p.linear {
				limit = uint64(len(p.doc) / 8)
			}
			if mallocs > limit {
				t.Errorf("%d allocations, over %d", mallocs, limit)
			}
		})
	}

	// A valid snapshot at the scope and histogram caps, for scale.
	doc := []byte(docWithScopes(t, 1024, 256))
	var err error
	elapsed, alloc, mallocs := measure(func() { _, err = thresholds.DecodeSnapshot(doc, thresholds.SnapshotLimits{}) })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("valid, 1024 scopes and 256 histograms (%d bytes): %v, %d MiB in %d allocations", len(doc), elapsed, alloc>>20, mallocs)
}

// sampleScope is a scope with n requests, f of them failed, and the
// given request durations and times to first byte.
func sampleScope(t testing.TB, n, f int64, us []int64, ttfb []int64) thresholds.Scope {
	return thresholds.Scope{Requests: n, Failed: f, BytesSent: n * 100, BytesReceived: n * 2000, Duration: dist(t, us...), TTFB: dist(t, ttfb...)}
}

func cat(xs ...[]int64) []int64 {
	var out []int64
	for _, x := range xs {
		out = append(out, x...)
	}
	return out
}

func wireMerge(t testing.TB, nodes ...*thresholds.Snapshot) *thresholds.Snapshot {
	t.Helper()
	var decoded []*thresholds.Snapshot
	for _, n := range nodes {
		decoded = append(decoded, mustDecode(t, mustEncode(t, n)))
	}
	m, err := thresholds.Merge(decoded...)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Node snapshots sent over the wire and merged evaluate exactly like the
// run's combined measurements, for every metric at every scope.
func TestWireMergeEqualsCombined(t *testing.T) {
	va, vb, ta, tb := values(31, 300), values(32, 700), values(33, 300), values(34, 700)
	ia, ib := values(35, 50), values(36, 70)
	ha, hb, sa := values(37, 120), values(38, 200), values(39, 30)
	withIter := func(s thresholds.Scope, iters *int64, us []int64) thresholds.Scope {
		s.Iterations, s.DroppedIterations, s.IterationDuration = iters, ptr(1), dist(t, us...)
		return s
	}
	withChecks := func(s thresholds.Scope, n, p int64) thresholds.Scope {
		s.Checks, s.ChecksPassed = ptr(n), ptr(p)
		return s
	}
	a := &thresholds.Snapshot{
		Start: t0, End: t0.Add(30 * time.Second), Responses: 299,
		Test: withChecks(withIter(sampleScope(t, 300, 3, va, ta), ptr(50), ia), 600, 590),
		Scenarios: map[string]*thresholds.ScenarioScope{
			"browse": {Scope: withIter(sampleScope(t, 150, 2, va[:150], ta[:150]), ptr(50), ia), Steps: map[string]*thresholds.Scope{
				"home":   ptrScope(withChecks(sampleScope(t, 120, 1, ha, nil), 240, 239)),
				"search": ptrScope(sampleScope(t, 30, 1, sa, sa)),
			}},
		},
	}
	b := &thresholds.Snapshot{
		Start: t0.Add(2 * time.Second), End: t0.Add(31 * time.Second), Responses: 700,
		// Iterations not measured on this node.
		Test: withChecks(withIter(sampleScope(t, 700, 0, vb, tb), nil, ib), 1400, 1400),
		Scenarios: map[string]*thresholds.ScenarioScope{
			"browse": {Scope: withIter(sampleScope(t, 200, 0, vb[:200], tb[:200]), ptr(70), ib), Steps: map[string]*thresholds.Scope{
				"home": ptrScope(withChecks(sampleScope(t, 200, 0, hb, nil), 400, 400)),
			}},
			"buy": {Scope: withIter(sampleScope(t, 500, 0, vb[200:], tb[200:]), ptr(9), nil)},
		},
	}
	combined := &thresholds.Snapshot{
		Start: t0, End: t0.Add(31 * time.Second), Responses: 999,
		Test: withChecks(withIter(sampleScope(t, 1000, 3, cat(va, vb), cat(ta, tb)), nil, cat(ia, ib)), 2000, 1990),
		Scenarios: map[string]*thresholds.ScenarioScope{
			"browse": {Scope: withIter(sampleScope(t, 350, 2, cat(va[:150], vb[:200]), cat(ta[:150], tb[:200])), ptr(120), cat(ia, ib)), Steps: map[string]*thresholds.Scope{
				"home":   ptrScope(withChecks(sampleScope(t, 320, 1, cat(ha, hb), nil), 640, 639)),
				"search": ptrScope(sampleScope(t, 30, 1, sa, sa)),
			}},
			"buy": {Scope: withIter(sampleScope(t, 500, 0, vb[200:], tb[200:]), ptr(9), nil)},
		},
	}
	combined.Test.DroppedIterations, combined.Scenarios["browse"].DroppedIterations = ptr(2), ptr(2)
	m := wireMerge(t, a, b)
	if m.Responses != 999 {
		t.Errorf("responses = %d", m.Responses)
	}
	bounds := everyBound(t, refsOf(combined))
	got, want := thresholds.Evaluate(m, bounds), thresholds.Evaluate(combined, bounds)
	requireSameResults(t, got, want)
	noData := 0
	for i, r := range got {
		if r.Status == thresholds.StatusNoData {
			noData++
		}
		if bounds[i].Ref == (thresholds.Ref{}) && r.Expr == "iterations < 100" && r.Status != thresholds.StatusNoData {
			t.Errorf("test iterations with a node not measuring them: %s", r.Status)
		}
	}
	if noData == 0 || noData == len(got) {
		t.Errorf("%d of %d results no_data", noData, len(got))
	}
}

// esc is the JSON escape of a UTF-16 code unit, such as esc("0061") for
// "a".
func esc(hex string) string { return "\\u" + hex }

func ptrScope(s thresholds.Scope) *thresholds.Scope { return &s }

// Each node passes a p95 bound that the merged run fails: with 11 values
// per node, a node's p95 is its 10th value; with 22, it is the 21st.
func TestWireMergeP95Rounding(t *testing.T) {
	vals := append(repeat(100_000, 10), 900_000)
	node := func(start time.Duration) *thresholds.Snapshot {
		return &thresholds.Snapshot{Start: t0.Add(start), End: t0.Add(start + 10*time.Second), Responses: 11,
			Test: thresholds.Scope{Requests: 11, Duration: dist(t, vals...)}}
	}
	a, b := node(0), node(time.Second)
	for _, n := range []*thresholds.Snapshot{a, b} {
		if r := evaluate(t, mustDecode(t, mustEncode(t, n)), thresholds.Ref{}, "p95 < 500ms"); r.Status != thresholds.StatusPassed {
			t.Fatalf("node p95 %v %s", deref(r.Actual), r.Status)
		}
	}
	m := wireMerge(t, a, b)
	r := evaluate(t, m, thresholds.Ref{}, "p95 < 500ms")
	if r.Status != thresholds.StatusFailed || actual(t, r) < 900 {
		t.Errorf("merged p95 %v %s, want about 900ms and failed", deref(r.Actual), r.Status)
	}
	combined := &thresholds.Snapshot{Start: t0, End: t0.Add(11 * time.Second), Test: thresholds.Scope{Requests: 22, Duration: dist(t, append(vals, vals...)...)}}
	if w := evaluate(t, combined, thresholds.Ref{}, "p95 < 500ms"); !reflect.DeepEqual(r, w) {
		t.Errorf("merged %+v, combined %+v", r, w)
	}
}

func repeat(v int64, n int) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// rps over merged nodes is requests over the union of their windows: the
// sum of the nodes' rates when the windows coincide.
func TestWireMergeRPS(t *testing.T) {
	node := func(start, end time.Duration, n int64) *thresholds.Snapshot {
		return &thresholds.Snapshot{Start: t0.Add(start), End: t0.Add(end), Responses: n, Test: thresholds.Scope{Requests: n}}
	}
	for _, c := range []struct {
		name string
		a, b *thresholds.Snapshot
		want float64
	}{
		{"same window", node(0, 10*time.Second, 100), node(0, 10*time.Second, 300), 40},
		{"overlapping", node(0, 10*time.Second, 100), node(5*time.Second, 15*time.Second, 200), 20},
	} {
		if got := actual(t, evaluate(t, wireMerge(t, c.a, c.b), thresholds.Ref{}, "rps > 1")); got != c.want {
			t.Errorf("%s: rps %v, want %v", c.name, got, c.want)
		}
	}
	a := node(0, 10*time.Second, 100)
	if got := actual(t, evaluate(t, wireMerge(t, a), thresholds.Ref{}, "rps > 1")) + actual(t, evaluate(t, wireMerge(t, node(0, 10*time.Second, 300)), thresholds.Ref{}, "rps > 1")); got != 40 {
		t.Errorf("node rates sum to %v", got)
	}
}

// DecodeSnapshot never panics; whatever it accepts Merge accepts, and
// re-encoding gives back the same snapshot. Values are compared, not
// bytes.
func FuzzDecodeSnapshot(f *testing.F) {
	lim := thresholds.SnapshotLimits{MaxBytes: 1 << 20, MaxScopes: 8, MaxHistograms: 3, MaxNameBytes: 16}
	h, err := thresholds.EncodeHistogram(dist(f, 5, 900, 70_000).Hist)
	if err != nil {
		f.Fatal(err)
	}
	head := `{` + wireStart + `,"test":{` + scopeKeys + `,"duration":{"count":3,"sum_us":70905,"min_us":5,"max_us":70000,"hdr":"` + string(h) + `"}}`
	scope := `{` + scopeKeys + `}`
	for _, s := range []string{
		head + `}`,
		head + `,"scenarios":{"a":{` + scopeKeys + `,"checks":0,"checks_passed":0,"steps":{"b":` + scope + `}}}}`,
		head + `,"Scenarios":{"a":` + scope + `},"ſcenarios":{"b":` + scope + `}}`,
		head + `,"scenarios":{"a":{` + scopeKeys + `,"STEPS":{"0":{},"1":{}}}}}`,
		head + `,"scenarios":{"0":{},"1":{},"2":{},"3":{},"4":{},"5":{},"6":{},"7":{},"8":{}}}`,
		head + `,"scenarios":{"a":` + scope + `,"a":` + scope + `}}`,
		head + `,"scenarios":{"a":` + scope + `,"` + esc("0061") + `":` + scope + `,"` + esc("d800") + `":` + scope + `}}`,
		head + `,"scenarios":{"a":null}}`,
		head + `,"scenarios":{"` + "\xff" + `":` + scope + `}}`,
		head + `,"x":[1.5,1.5,{"y":[]}],"scenarios":{"a":{` + scopeKeys + `,"x":[1.5]}}}`,
		head + `,"scenarios":{"a":{` + scopeKeys + `,"ttfb":{"count":0,"sum_us":0,"min_us":0,"max_us":0,"hdr":"!!!"}}}}`,
		`{"start":"2026-10-16T11:00:00Z","end":"2026-10-16T12:00:00+00:01","END":"2026-10-16T12:00:00+00:01","responses":0,"test":` + scope + `}`,
		head[:len(head)-2] + `,"hdr":"` + string(h) + `"}}}`,
		`{` + wireStart + `,"test":null}`,
		`{"start":"2026-10-16T12:00:00+05:30","end":"2026-10-16T07:00:00-00:00","responses":0,"test":` + scope + `}`,
		`[]`, `null`, ``, `{`,
		string(mustEncode(f, wireSnapshot(f))),
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := thresholds.DecodeSnapshot(data, lim)
		if err != nil {
			known := false
			for _, e := range []error{thresholds.ErrMalformedSnapshot, thresholds.ErrInvalidScope, thresholds.ErrMalformedHistogram,
				thresholds.ErrHistogramConfig, thresholds.ErrOverflow} {
				known = known || errors.Is(err, e)
			}
			if !known {
				t.Fatalf("error %v wraps no sentinel", err)
			}
			if len(err.Error()) > 1024 {
				t.Fatalf("message of %d bytes", len(err.Error()))
			}
			return
		}
		if _, err := thresholds.Merge(s); err != nil {
			t.Fatalf("Merge refused a decoded snapshot: %v", err)
		}
		b, err := thresholds.EncodeSnapshot(s)
		if err != nil {
			t.Fatalf("re-encode: %v", err)
		}
		s2, err := thresholds.DecodeSnapshot(b, lim)
		if err != nil {
			t.Fatalf("re-decode: %v\n%s", err, b)
		}
		if !equalSnapshots(s, s2) {
			t.Fatalf("round trip changed the snapshot:\n%s\n%s", data, b)
		}
	})
}
