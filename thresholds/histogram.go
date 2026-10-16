package thresholds

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"
)

// The histogram configuration every snapshot uses: values in microseconds
// from 1µs to one hour, with 3 significant figures. Producers create
// histograms with NewHistogram. Merge and Scope.Add reject a histogram
// with any other configuration, and Evaluate treats one as missing.
const (
	HistogramLowestUs  int64 = 1
	HistogramHighestUs int64 = 3_600_000_000
	HistogramSigFigs         = 3
)

// Errors returned by this package wrap one of these, so callers can test
// with errors.Is.
var (
	// ErrMalformedHistogram: histogram bytes that are not a valid, bounded
	// base64 V2 compressed HDR encoding.
	ErrMalformedHistogram = errors.New("thresholds: malformed histogram")
	// ErrHistogramConfig: a histogram whose configuration is not the
	// snapshot configuration.
	ErrHistogramConfig = fmt.Errorf("thresholds: histogram configuration must be %d..%dus with %d significant figures",
		HistogramLowestUs, HistogramHighestUs, HistogramSigFigs)
	// ErrInvalidScope: a scope with a negative counter, an invalid
	// distribution or a broken invariant (Failed > Requests,
	// ChecksPassed > Checks), or a snapshot with a bad window.
	ErrInvalidScope = errors.New("thresholds: invalid scope")
	// ErrOverflow: a counter or sum that overflows int64.
	ErrOverflow = errors.New("thresholds: counter overflow")
	// ErrInvalidThreshold: a threshold that Parse would not produce.
	ErrInvalidThreshold = errors.New("thresholds: invalid threshold")
)

// NewHistogram returns an empty histogram with the snapshot configuration.
// One takes about 188 KB.
func NewHistogram() *hdrhistogram.Histogram {
	return hdrhistogram.New(HistogramLowestUs, HistogramHighestUs, HistogramSigFigs)
}

// standardHistogram reports whether h has the snapshot configuration.
func standardHistogram(h *hdrhistogram.Histogram) bool {
	return h.LowestTrackableValue() == HistogramLowestUs &&
		h.HighestTrackableValue() == HistogramHighestUs &&
		h.SignificantFigures() == HistogramSigFigs
}

// countsLen is the number of counters in a standard histogram (23552).
var countsLen = sync.OnceValue(func() int { return len(NewHistogram().Export().Counts) })

// EncodeHistogram returns h in the base64 V2 compressed HDR encoding, the
// form in which node results carry histograms (as a JSON string). It
// refuses a histogram without the snapshot configuration.
func EncodeHistogram(h *hdrhistogram.Histogram) ([]byte, error) {
	if h == nil {
		return nil, fmt.Errorf("%w: nil histogram", ErrMalformedHistogram)
	}
	if !standardHistogram(h) {
		return nil, ErrHistogramConfig
	}
	b, err := h.Encode(hdrhistogram.V2CompressedEncodingCookieBase)
	if err != nil {
		return nil, fmt.Errorf("thresholds: encode histogram: %w", err)
	}
	return b, nil
}

// Limits on encoded histograms. A standard histogram has 23552 counters
// of at most 9 bytes each after a 40-byte header (about 207 KiB inflated);
// the compressed form is smaller still.
const (
	maxEncodedHistogram  = 512 << 10 // base64 text
	maxInflatedHistogram = 256 << 10
	histogramHeaderSize  = 40
)

// Cookies of the V2 encodings, as hdrhistogram writes them (the low
// nibble of the second byte is masked off when reading).
const (
	v2Cookie           int32 = 0x1c849303
	v2CompressedCookie int32 = 0x1c849304
)

// DecodeHistogram reads a histogram in the base64 V2 compressed HDR
// encoding, the output of EncodeHistogram. It is safe on bytes from
// another process: input over 512 KiB, data that inflates past 256 KiB, a
// configuration other than the snapshot configuration and malformed
// counters are rejected before anything large is allocated, and it never
// panics or loops. Allocation is bounded (about 0.5 MB). Errors wrap
// ErrMalformedHistogram or ErrHistogramConfig.
func DecodeHistogram(data []byte) (*hdrhistogram.Histogram, error) {
	bad := func(format string, args ...any) (*hdrhistogram.Histogram, error) {
		return nil, fmt.Errorf("%w: "+format, append([]any{ErrMalformedHistogram}, args...)...)
	}
	data = bytes.TrimSpace(data)
	if len(data) > maxEncodedHistogram {
		return bad("more than %d bytes of base64", maxEncodedHistogram)
	}
	raw := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
	n, err := base64.StdEncoding.Decode(raw, data)
	if err != nil {
		return bad("not base64")
	}
	raw = raw[:n]

	// Outer header: compressed cookie, then the length of the zlib data.
	if len(raw) < 8 {
		return bad("shorter than its header")
	}
	if cookie := int32(binary.BigEndian.Uint32(raw)) &^ 0xf0; cookie != v2CompressedCookie {
		return bad("not a V2 compressed encoding")
	}
	size := int32(binary.BigEndian.Uint32(raw[4:]))
	if size < 0 || int(size) > len(raw)-8 {
		return bad("declared length %d does not fit the data", size)
	}
	z, err := zlib.NewReader(bytes.NewReader(raw[8 : 8+int(size)]))
	if err != nil {
		return bad("not zlib data")
	}
	inflated, err := io.ReadAll(io.LimitReader(z, maxInflatedHistogram+1))
	if err != nil {
		return bad("corrupt zlib data")
	}
	if len(inflated) > maxInflatedHistogram {
		return bad("inflates past %d bytes", maxInflatedHistogram)
	}

	// Inner header: cookie, payload length, normalizing offset, significant
	// figures, lowest and highest trackable values, conversion ratio.
	if len(inflated) < histogramHeaderSize {
		return bad("inflated data shorter than its header")
	}
	if cookie := int32(binary.BigEndian.Uint32(inflated)) &^ 0xf0; cookie != v2Cookie {
		return bad("not a V2 encoding inside")
	}
	payload := int32(binary.BigEndian.Uint32(inflated[4:]))
	sigFigs := int32(binary.BigEndian.Uint32(inflated[12:]))
	lowest := int64(binary.BigEndian.Uint64(inflated[16:]))
	highest := int64(binary.BigEndian.Uint64(inflated[24:]))
	if lowest != HistogramLowestUs || highest != HistogramHighestUs || sigFigs != HistogramSigFigs {
		return nil, ErrHistogramConfig
	}
	if int(payload) != len(inflated)-histogramHeaderSize {
		return bad("payload length %d does not match the data", payload)
	}

	// Counters: ZigZag LEB128 values; a negative one is a run of zeros.
	counts := make([]int64, countsLen())
	var total int64
	idx := 0
	for p := inflated[histogramHeaderSize:]; len(p) > 0; {
		v, k, ok := zigzag(p)
		if !ok {
			return bad("truncated counter")
		}
		p = p[k:]
		if v < 0 {
			if v == math.MinInt64 || -v > int64(len(counts)-idx) {
				return bad("zero run past the last counter")
			}
			idx += int(-v)
			continue
		}
		if idx >= len(counts) {
			return bad("more counters than the configuration has")
		}
		counts[idx] = v
		idx++
		if total += v; total < 0 {
			return nil, fmt.Errorf("%w: histogram total", ErrOverflow)
		}
	}
	return hdrhistogram.Import(&hdrhistogram.Snapshot{
		LowestTrackableValue:  HistogramLowestUs,
		HighestTrackableValue: HistogramHighestUs,
		SignificantFigures:    HistogramSigFigs,
		Counts:                counts,
	}), nil
}

// zigzag reads one ZigZag LEB128 value as the V2 encoding writes it: seven
// bits per byte for up to eight bytes, then a ninth byte of eight bits.
func zigzag(p []byte) (v int64, n int, ok bool) {
	var u uint64
	for i := 0; i < 9; i++ {
		if i >= len(p) {
			return 0, 0, false
		}
		b := p[i]
		if i == 8 {
			u |= uint64(b) << 56
			n = 9
			break
		}
		u |= uint64(b&0x7f) << (7 * i)
		n = i + 1
		if b&0x80 == 0 {
			break
		}
	}
	return int64((u >> 1) ^ -(u & 1)), n, true
}
