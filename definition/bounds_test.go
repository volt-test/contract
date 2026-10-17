package definition_test

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/volt-test/contract/definition"
)

// sized returns n, or a quarter of it under the race detector.
func sized(n int) int {
	if raceEnabled {
		return n / 4
	}
	return n
}

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

func TestV1TypeProblemsBounded(t *testing.T) {
	const regions = 700
	n := sized(1_000_000)
	doc := `{"name": "x", "weights": [` + strings.TrimSuffix(strings.Repeat(`"x",`, n), ",") + `],
		"region_config": [` + strings.TrimSuffix(strings.Repeat(`{"region": 1, "weight": "2"},`, regions), ",") + `]}`
	var d *definition.Definition
	var err error
	elapsed, alloc, mallocs := measure(func() { d, err = definition.Parse([]byte(doc)) })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d bad weights (%d bytes): %v, %d MB in %d allocations", n, len(doc), elapsed, alloc>>20, mallocs)
	// No allocation per bad value: paths are built only for kept problems.
	if mallocs > 100_000 {
		t.Errorf("%d allocations for %d bad values", mallocs, n)
	}
	want := []definition.Problem{
		{Path: "weights[0]", Message: "must be an integer", Count: n},
		{Path: "region_config[0].region", Message: "must be a string", Count: regions},
		{Path: "region_config[0].weight", Message: "must be an integer", Count: regions},
	}
	if len(d.V1TypeProblems) != len(want) {
		t.Fatalf("V1TypeProblems has %d entries: %v", len(d.V1TypeProblems), d.V1TypeProblems[:min(len(d.V1TypeProblems), 5)])
	}
	for i := range want {
		if d.V1TypeProblems[i] != want[i] {
			t.Errorf("V1TypeProblems[%d] = %+v, want %+v", i, d.V1TypeProblems[i], want[i])
		}
	}
	if len(d.Weights) != n {
		t.Errorf("len(Weights) = %d", len(d.Weights))
	}
	if got := d.V1TypeProblems[0].String(); got != fmt.Sprintf("weights[0]: must be an integer (%d values)", n) {
		t.Errorf("String() = %q", got)
	}
	// About 0.15 s and 90 MB without the race detector.
	if elapsed > 20*time.Second || alloc > 512<<20 {
		t.Errorf("too slow or too much memory: %v, %d MB", elapsed, alloc>>20)
	}
}

func TestValidationErrorBounded(t *testing.T) {
	n := sized(200_000)
	doc := `{"name": "x", "stages": [` + strings.TrimSuffix(strings.Repeat(`{"target": "1"},`, n), ",") + `]}`
	var err error
	elapsed, alloc, _ := measure(func() { _, err = definition.Parse([]byte(doc)) })
	t.Logf("%d core problems: %v, %d MB allocated", n, elapsed, alloc>>20)
	var ve *definition.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %v", err)
	}
	if len(ve.Problems) != definition.MaxKeptForTest || ve.Omitted != n-definition.MaxKeptForTest {
		t.Errorf("kept %d, omitted %d", len(ve.Problems), ve.Omitted)
	}
	msg := err.Error()
	shown := definition.MaxShownForTest
	if strings.Count(msg, "must be an integer") != shown || !strings.HasSuffix(msg, fmt.Sprintf(" (+%d more)", n-shown)) {
		t.Errorf("message lists %d problems, ends %q", strings.Count(msg, "must be an integer"), msg[len(msg)-40:])
	}
	if elapsed > 20*time.Second || alloc > 512<<20 {
		t.Errorf("too slow or too much memory: %v, %d MB", elapsed, alloc>>20)
	}
	if len(msg) > 10_000 {
		t.Errorf("message is %d bytes", len(msg))
	}
}

func TestUnknownFieldErrorBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"schema_version": "2.0", "name": "x", "virtual_users": 1`)
	const n = 5000
	for i := 0; i < n; i++ {
		b.WriteString(`, "k` + strings.Repeat("0", 6-len(itoa(i))) + itoa(i) + `": 1`)
	}
	b.WriteString(`, "` + strings.Repeat("é", 500) + `": 1}`)
	_, err := definition.Parse([]byte(b.String()))
	var ufe *definition.UnknownFieldError
	if !errors.As(err, &ufe) {
		t.Fatalf("error = %v", err)
	}
	// n "k" keys plus the long key, which sorts after them.
	if len(ufe.Paths) != definition.MaxKeptForTest || ufe.Omitted != n+1-definition.MaxKeptForTest {
		t.Errorf("kept %d, omitted %d", len(ufe.Paths), ufe.Omitted)
	}
	msg := err.Error()
	shown := definition.MaxShownForTest
	// The first path follows ": ", the others ", ".
	if strings.Count(msg, ", k0") != shown-1 || !strings.Contains(msg, fmt.Sprintf("(+%d more)", n+1-shown)) {
		t.Errorf("message not bounded: %d bytes", len(msg))
	}
	for _, p := range ufe.Paths {
		if len(p) > 140 {
			t.Errorf("path not truncated: %d bytes", len(p))
		}
	}
}

func TestLongKeyTruncatedInPath(t *testing.T) {
	long := strings.Repeat("é", 500)
	_, err := definition.Parse([]byte(`{"schema_version": "2.0", "` + long + `": 1}`))
	var ufe *definition.UnknownFieldError
	if !errors.As(err, &ufe) || len(ufe.Paths) != 1 {
		t.Fatalf("error = %v", err)
	}
	if p := ufe.Paths[0]; len(p) > 140 || !strings.Contains(p, "...") {
		t.Errorf("path = %d bytes: %.40q", len(p), p)
	}
	_, err = definition.Parse([]byte(`{"schema_version": "2.0", "name": "` + long + `", "virtual_users": 1,
		"scenarios": [{"name": "a", "steps": [{"name": "s", "request": {"method": "GET", "url": "https://a/"},
		"extract": [{"variable_name": "v", "selector": "s", "type": "` + long + `"}]}]}]}`))
	if err == nil || len(err.Error()) > 400 {
		t.Errorf("echoed value not truncated: %v", err)
	}
}

func itoa(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return digits[i : i+1]
	}
	return itoa(i/10) + digits[i%10:i%10+1]
}

// TestLargeV2DefinitionAllocations parses a valid 9 MB v2 definition, the
// path the backend runs for every submission.
func TestLargeV2DefinitionAllocations(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"schema_version":"2.0","name":"x","virtual_users":1,"scenarios":[{"name":"a","steps":[`)
	steps := 0
	for size := sized(9_000_000); b.Len() < size; {
		if steps > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"name":"s` + itoa(steps) + `","request":{"method":"GET","url":"https://a/` + itoa(steps) +
			`","headers":{"A":"b","C":"d"}},"extract":[{"variable_name":"v","selector":"$.a","type":"json"}],"think_time":"1ms"}`)
		steps++
	}
	b.WriteString(`]}]}`)
	doc := []byte(b.String())
	var err error
	elapsed, alloc, mallocs := measure(func() { _, err = definition.Parse(doc) })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d bytes, %d steps: %v, %d MB in %d allocations", len(doc), steps, elapsed, alloc>>20, mallocs)
	// About 110 MB, half of it encoding/json building the Definition.
	if alloc > 400<<20 {
		t.Errorf("%d MB allocated for a %d MB definition", alloc>>20, len(doc)>>20)
	}
}

// TestDeeplyNestedExpected checks that a v1 validator value nested 9000
// levels deep costs memory linear in its size.
func TestDeeplyNestedExpected(t *testing.T) {
	const depth = 9000
	wrap := func(inner string) string {
		return `{"scenarios":[{"steps":[{"validate":[{"expected":` + inner + `}]}]}]}`
	}
	cases := []struct {
		name, doc, wantPath string
	}{
		{"arrays", wrap(strings.Repeat("[", depth) + "1" + strings.Repeat("]", depth)), ""},
		{"objects, out of range at the bottom",
			wrap(strings.Repeat(`{"a":[0,`, depth/2) + "-1e999" + strings.Repeat("]}", depth/2)),
			"scenarios[0].steps[0].validate[0].expected.a[1].a[1].a[1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			elapsed, alloc, _ := measure(func() { _, err = definition.Parse([]byte(tc.doc)) })
			t.Logf("%d bytes: %v, %d KB allocated", len(tc.doc), elapsed, alloc>>10)
			if tc.wantPath == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var ve *definition.ValidationError
				if !errors.As(err, &ve) || len(ve.Problems) != 1 {
					t.Fatalf("error = %v", err)
				}
				p := ve.Problems[0]
				if !strings.HasPrefix(p.Path, tc.wantPath) || !strings.HasSuffix(p.Path, ".a[1]") ||
					!strings.Contains(p.Path, "...") || len(p.Path) > 600 || p.Message != "is out of range" {
					t.Errorf("problem = %.80q... (%d bytes): %s", p.Path, len(p.Path), p.Message)
				}
			}
			if alloc > 4<<20 {
				t.Errorf("%d MB allocated for %d bytes of input", alloc>>20, len(tc.doc))
			}
		})
	}
}
