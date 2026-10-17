package thresholds_test

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"

	"github.com/volt-test/contract/thresholds"
)

// hangSeed is a 52-byte encoding with lowest trackable value 2^53, highest
// 3.6e9 and 3 significant figures. hdrhistogram.New loops forever on that
// configuration, so DecodeHistogram must reject it from the header.
const hangSeed = "HISTFAAAACB42pJpmSzMgADMDAoIzrXJSxjsP0DYgAAAAP//U6kEpg=="

// zz encodes v as the V2 encoding writes counters.
func zz(v int64) []byte {
	u := uint64(v<<1) ^ uint64(v>>63)
	var out []byte
	for i := 0; i < 8; i++ {
		b := byte(u & 0x7f)
		u >>= 7
		if u == 0 {
			return append(out, b)
		}
		out = append(out, b|0x80)
	}
	return append(out, byte(u))
}

// inner builds the inflated form: the 40-byte header and the payload.
func inner(cookie int32, payloadLen int32, sigFigs int32, lowest, highest int64, payload []byte) []byte {
	return innerWith(cookie, payloadLen, 1, sigFigs, lowest, highest, 1, payload)
}

func innerWith(cookie, payloadLen, offset, sigFigs int32, lowest, highest int64, ratio float64, payload []byte) []byte {
	var b bytes.Buffer
	for _, v := range []any{cookie, payloadLen, offset, sigFigs, lowest, highest, ratio} {
		_ = binary.Write(&b, binary.BigEndian, v)
	}
	b.Write(payload)
	return b.Bytes()
}

func standardInner(payload []byte) []byte {
	return inner(0x1c849313, int32(len(payload)), 3, 1, 3_600_000_000, payload)
}

// outer compresses an inflated form and wraps it as the base64 V2
// compressed encoding.
func outer(inflated []byte) []byte { return outerWith(0x1c849314, inflated) }

func outerWith(cookie int32, inflated []byte) []byte {
	var z bytes.Buffer
	w, _ := zlib.NewWriterLevel(&z, zlib.BestCompression)
	_, _ = w.Write(inflated)
	_ = w.Close()
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, cookie)
	_ = binary.Write(&b, binary.BigEndian, int32(z.Len()))
	b.Write(z.Bytes())
	return []byte(base64.StdEncoding.EncodeToString(b.Bytes()))
}

// decodeQuickly runs DecodeHistogram with a deadline, so a hang fails the
// test instead of stalling it.
func decodeQuickly(t testing.TB, data []byte) (*hdrhistogram.Histogram, error) {
	t.Helper()
	type result struct {
		h   *hdrhistogram.Histogram
		err error
	}
	done := make(chan result, 1)
	go func() {
		h, err := thresholds.DecodeHistogram(data)
		done <- result{h, err}
	}()
	select {
	case r := <-done:
		return r.h, r.err
	case <-time.After(5 * time.Second):
		t.Fatalf("DecodeHistogram(%.40q...) did not return within 5s", data)
		return nil, nil
	}
}

func TestHistogramCodec(t *testing.T) {
	h := dist(t, values(1, 500)...).Hist
	enc, err := thresholds.EncodeHistogram(h)
	if err != nil {
		t.Fatal(err)
	}
	back, err := thresholds.DecodeHistogram(enc)
	if err != nil || !back.Equals(h) {
		t.Fatalf("round trip: %v", err)
	}
	// Surrounding whitespace, as in a text file, is fine.
	if back, err := thresholds.DecodeHistogram(append(append([]byte(" \n"), enc...), '\n')); err != nil || !back.Equals(h) {
		t.Errorf("with whitespace: %v", err)
	}
	// An empty histogram round-trips.
	empty, _ := thresholds.EncodeHistogram(thresholds.NewHistogram())
	if back, err := thresholds.DecodeHistogram(empty); err != nil || back.TotalCount() != 0 || !back.Equals(thresholds.NewHistogram()) {
		t.Errorf("empty: %v", err)
	}
	// Values near both ends of the range round-trip.
	edges := dist(t, 1, 2, 3_599_999_999, 3_600_000_000).Hist
	if b, _ := thresholds.EncodeHistogram(edges); b == nil {
		t.Fatal("encode edges")
	} else if back, err := thresholds.DecodeHistogram(b); err != nil || !back.Equals(edges) {
		t.Errorf("edges: %v", err)
	}

	// Header fields DecodeHistogram ignores, as hdrhistogram does: other
	// bits in the masked nibble of either cookie, the normalizing offset
	// and the conversion ratio.
	payload := append(zz(-10), append(zz(3), zz(1)...)...)
	want := thresholds.NewHistogram()
	_ = want.RecordValues(10, 3)
	_ = want.RecordValues(11, 1)
	for name, data := range map[string][]byte{
		"plain":             outer(standardInner(payload)),
		"outer cookie 0x24": outerWith(0x1c849324, standardInner(payload)),
		"outer cookie 0xf4": outerWith(0x1c8493f4, standardInner(payload)),
		"inner cookie 0x33": outer(innerWith(0x1c849333, int32(len(payload)), 1, 3, 1, 3_600_000_000, 1, payload)),
		"inner cookie 0x03": outer(innerWith(0x1c849303, int32(len(payload)), 1, 3, 1, 3_600_000_000, 1, payload)),
		"offset 0":          outer(innerWith(0x1c849313, int32(len(payload)), 0, 3, 1, 3_600_000_000, 1, payload)),
		"ratio 0.001":       outer(innerWith(0x1c849313, int32(len(payload)), 1, 3, 1, 3_600_000_000, 0.001, payload)),
	} {
		h, err := thresholds.DecodeHistogram(data)
		if err != nil || !h.Equals(want) {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if lib, err := hdrhistogram.Decode(data); err != nil || !lib.Equals(h) {
			t.Errorf("%s: hdrhistogram.Decode disagrees: %v", name, err)
		}
	}

	if _, err := thresholds.EncodeHistogram(nil); !errors.Is(err, thresholds.ErrMalformedHistogram) {
		t.Errorf("EncodeHistogram(nil) = %v", err)
	}
	if _, err := thresholds.EncodeHistogram(hdrhistogram.New(1, 1_000_000, 2)); !errors.Is(err, thresholds.ErrHistogramConfig) {
		t.Errorf("EncodeHistogram(non-standard) = %v", err)
	}
}

func TestDecodeHistogramRejects(t *testing.T) {
	other, err := hdrhistogram.New(1, 1_000_000, 2).Encode(hdrhistogram.V2CompressedEncodingCookieBase)
	if err != nil {
		t.Fatal(err)
	}
	valid, _ := thresholds.EncodeHistogram(dist(t, values(1, 500)...).Hist)
	raw, _ := base64.StdEncoding.DecodeString(string(valid))
	b64 := func(b []byte) []byte { return []byte(base64.StdEncoding.EncodeToString(b)) }
	header := func(cookie, size int32) []byte {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.BigEndian, cookie)
		_ = binary.Write(&b, binary.BigEndian, size)
		return b.Bytes()
	}
	// A zlib bomb: a valid header followed by 10 MiB of zeros, compressed
	// to about 10 KB.
	bomb := outer(append(standardInner(nil), make([]byte, 10<<20)...))
	if len(bomb) > 64<<10 {
		t.Fatalf("bomb is %d bytes", len(bomb))
	}
	many := bytes.Repeat(zz(1), 23_553)
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"hang seed: lowest 2^53", []byte(hangSeed), thresholds.ErrHistogramConfig},
		{"lowest 2^45, sig 5", outer(inner(0x1c849313, 0, 5, 1<<45, 3_600_000_000, nil)), thresholds.ErrHistogramConfig},
		{"negative lowest", outer(inner(0x1c849313, 0, 3, -1, 3_600_000_000, nil)), thresholds.ErrHistogramConfig},
		{"huge highest", outer(inner(0x1c849313, 0, 3, 1, math.MaxInt64, nil)), thresholds.ErrHistogramConfig},
		{"other sig figs", outer(inner(0x1c849313, 0, 2, 1, 3_600_000_000, nil)), thresholds.ErrHistogramConfig},
		{"other configuration", other, thresholds.ErrHistogramConfig},
		{"zlib bomb", bomb, thresholds.ErrMalformedHistogram},
		{"oversized input", bytes.Repeat([]byte("A"), 600<<10), thresholds.ErrMalformedHistogram},
		{"empty", nil, thresholds.ErrMalformedHistogram},
		{"not base64", []byte("%%%"), thresholds.ErrMalformedHistogram},
		{"seven bytes", b64([]byte{1, 2, 3, 4, 5, 6, 7}), thresholds.ErrMalformedHistogram},
		{"eight zero bytes", b64(make([]byte, 8)), thresholds.ErrMalformedHistogram},
		{"wrong outer cookie", b64(append(header(0x1c849303, int32(len(raw)-8)), raw[8:]...)), thresholds.ErrMalformedHistogram},
		{"declared length past the end", b64(append(header(0x1c849314, int32(len(raw)-7)), raw[8:]...)), thresholds.ErrMalformedHistogram},
		{"declared length max", b64(append(header(0x1c849314, math.MaxInt32), raw[8:]...)), thresholds.ErrMalformedHistogram},
		{"negative declared length", b64(append(header(0x1c849314, -1), raw[8:]...)), thresholds.ErrMalformedHistogram},
		{"truncated zlib", b64(append(header(0x1c849314, int32(len(raw)-8)/2), raw[8:8+(len(raw)-8)/2]...)), thresholds.ErrMalformedHistogram},
		{"not zlib", b64(append(header(0x1c849314, 16), make([]byte, 16)...)), thresholds.ErrMalformedHistogram},
		{"short inner header", outer(make([]byte, 39)), thresholds.ErrMalformedHistogram},
		{"wrong inner cookie", outer(inner(0x1c849314, 0, 3, 1, 3_600_000_000, nil)), thresholds.ErrMalformedHistogram},
		{"payload length mismatch", outer(inner(0x1c849313, 5, 3, 1, 3_600_000_000, zz(1))), thresholds.ErrMalformedHistogram},
		{"payload longer than declared", outer(inner(0x1c849313, 1, 3, 1, 3_600_000_000, append(zz(1), zz(1)...))), thresholds.ErrMalformedHistogram},
		{"negative payload length", outer(inner(0x1c849313, -1, 3, 1, 3_600_000_000, nil)), thresholds.ErrMalformedHistogram},
		{"payload length past the inflate limit", outer(inner(0x1c849313, 300<<10, 3, 1, 3_600_000_000, nil)), thresholds.ErrMalformedHistogram},
		{"truncated counter", outer(standardInner([]byte{0x81})), thresholds.ErrMalformedHistogram},
		{"zero run past the end", outer(standardInner(zz(-23_553))), thresholds.ErrMalformedHistogram},
		{"zero run of MinInt64", outer(standardInner(zz(math.MinInt64))), thresholds.ErrMalformedHistogram},
		{"too many counters", outer(standardInner(many)), thresholds.ErrMalformedHistogram},
		{"total overflow", outer(standardInner(append(zz(math.MaxInt64), zz(1)...))), thresholds.ErrOverflow},
	}
	for _, tc := range cases {
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		h, err := decodeQuickly(t, tc.data)
		elapsed := time.Since(start)
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		if h != nil || !errors.Is(err, tc.want) {
			t.Errorf("%s: DecodeHistogram = %v, %v; want %v", tc.name, h, err, tc.want)
		}
		if err != nil && len(err.Error()) > 200 {
			t.Errorf("%s: error is %d bytes", tc.name, len(err.Error()))
		}
		if elapsed > time.Second {
			t.Errorf("%s: took %v", tc.name, elapsed)
		}
		// The input itself can be large (the oversized case); beyond it,
		// a rejected decode allocates at most about one histogram.
		if alloc := after.TotalAlloc - before.TotalAlloc; alloc > uint64(len(tc.data))+2<<20 {
			t.Errorf("%s: allocated %d bytes", tc.name, alloc)
		}
	}

	// A large valid payload: every counter set, each in 8 bytes (about
	// 188 KB inflated), fits under the inflate limit.
	full := bytes.Repeat(zz(1<<48), 23_552)
	if len(full) != 8*23_552 {
		t.Fatalf("counter encoding is %d bytes", len(full)/23_552)
	}
	h, err := thresholds.DecodeHistogram(outer(standardInner(full)))
	if err != nil || h.TotalCount() != (1<<48)*23_552 {
		t.Errorf("full histogram: %v", err)
	}
	// A zero run up to the last counter, then one more counter, is too many.
	if _, err := thresholds.DecodeHistogram(outer(standardInner(append(zz(-23_552), zz(1)...)))); !errors.Is(err, thresholds.ErrMalformedHistogram) {
		t.Errorf("counter after the last: %v", err)
	}
	if _, err := thresholds.DecodeHistogram(outer(standardInner(append(zz(-23_551), zz(1)...)))); err != nil {
		t.Errorf("counter at the last index: %v", err)
	}
}

// FuzzDecodeHistogram: DecodeHistogram returns quickly with bounded
// allocation on any input, and agrees with hdrhistogram.Decode on every
// input it accepts.
func FuzzDecodeHistogram(f *testing.F) {
	for _, v := range [][]int64{nil, {1}, values(1, 300), {1, 3_600_000_000}} {
		enc, err := thresholds.EncodeHistogram(dist(f, v...).Hist)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(enc)
	}
	other, _ := hdrhistogram.New(1, 1_000_000, 2).Encode(hdrhistogram.V2CompressedEncodingCookieBase)
	f.Add(other)
	f.Add([]byte(hangSeed))
	f.Add(outer(append(standardInner(nil), make([]byte, 1<<20)...)))
	f.Add(outer(standardInner(append(zz(5), zz(-100)...))))
	f.Add(outer(standardInner(bytes.Repeat(zz(1), 50))))
	f.Fuzz(func(t *testing.T, data []byte) {
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		h, err := thresholds.DecodeHistogram(data)
		elapsed := time.Since(start)
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		if elapsed > 2*time.Second {
			t.Fatalf("took %v", elapsed)
		}
		if alloc := after.TotalAlloc - before.TotalAlloc; alloc > uint64(len(data))+4<<20 {
			t.Fatalf("allocated %d bytes", alloc)
		}
		if err != nil {
			if h != nil {
				t.Fatal("histogram with an error")
			}
			if !errors.Is(err, thresholds.ErrMalformedHistogram) && !errors.Is(err, thresholds.ErrHistogramConfig) && !errors.Is(err, thresholds.ErrOverflow) {
				t.Fatalf("untyped error %v", err)
			}
			if len(err.Error()) > 200 {
				t.Fatalf("error is %d bytes", len(err.Error()))
			}
			return
		}
		if h.LowestTrackableValue() != 1 || h.HighestTrackableValue() != 3_600_000_000 || h.SignificantFigures() != 3 || h.TotalCount() < 0 {
			t.Fatal("accepted a non-standard histogram")
		}
		// Accepted input is bounded and standard, so the library can read it.
		lib, err := hdrhistogram.Decode([]byte(strings.TrimSpace(string(data))))
		if err != nil || !lib.Equals(h) {
			t.Fatalf("hdrhistogram.Decode disagrees: %v", err)
		}
		// And it re-encodes to the same histogram.
		enc, err := thresholds.EncodeHistogram(h)
		if err != nil {
			t.Fatal(err)
		}
		again, err := thresholds.DecodeHistogram(enc)
		if err != nil || !again.Equals(h) {
			t.Fatalf("re-encode: %v", err)
		}
	})
}
