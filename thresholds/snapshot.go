package thresholds

import (
	"errors"
	"fmt"
	"time"

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

var errHistogramConfig = fmt.Errorf("histogram configuration must be %d..%dus with %d significant figures",
	HistogramLowestUs, HistogramHighestUs, HistogramSigFigs)

// EncodeHistogram returns h in the base64 V2 compressed HDR encoding, the
// form in which node results carry histograms.
func EncodeHistogram(h *hdrhistogram.Histogram) ([]byte, error) {
	if h == nil {
		return nil, errors.New("thresholds: nil histogram")
	}
	b, err := h.Encode(hdrhistogram.V2CompressedEncodingCookieBase)
	if err != nil {
		return nil, fmt.Errorf("thresholds: encode histogram: %w", err)
	}
	return b, nil
}

// DecodeHistogram reads a histogram in the base64 V2 compressed HDR
// encoding. Unlike hdrhistogram.Decode it never panics on malformed input,
// and it rejects a histogram whose configuration is not the snapshot
// configuration, so it is safe on bytes received from another process.
func DecodeHistogram(data []byte) (h *hdrhistogram.Histogram, err error) {
	defer func() {
		if r := recover(); r != nil {
			h, err = nil, errors.New("thresholds: decode histogram: malformed data")
		}
	}()
	h, err = hdrhistogram.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("thresholds: decode histogram: %w", err)
	}
	if !standardHistogram(h) {
		return nil, errors.New("thresholds: decode histogram: " + errHistogramConfig.Error())
	}
	if h.TotalCount() < 0 {
		return nil, errors.New("thresholds: decode histogram: count overflow")
	}
	return h, nil
}

// Distribution summarises a set of durations, in microseconds. Count,
// SumUs, MinUs and MaxUs are exact and cover every value; Hist holds the
// values for percentiles. The two can differ: a producer may leave values
// it cannot record (over an hour) out of Hist, so averages use Count and
// SumUs, never Hist.TotalCount. MinUs and MaxUs mean nothing when Count
// is 0. A nil Hist with Count > 0 means the percentiles are unknown.
type Distribution struct {
	Count, SumUs, MinUs, MaxUs int64
	Hist                       *hdrhistogram.Histogram
}

// Scope is the end-of-run measurements of the test, a scenario or a step.
// The plain counters are always measured. The optional counters are nil
// when the producer did not measure them, which is not the same as 0:
// thresholds on them give no_data.
type Scope struct {
	Requests, Failed, BytesSent, BytesReceived int64

	Checks, ChecksPassed, Iterations, DroppedIterations *int64

	Duration, TTFB, IterationDuration Distribution
}

// ScenarioScope is a scenario's own measurements and its steps', by step
// name.
type ScenarioScope struct {
	Scope
	Steps map[string]*Scope
}

// Snapshot is the measurements of one run, or of one node's share of a
// run, by scope. Scenarios are keyed by name. Start and End bound the
// measurement window; rps is Requests over End-Start at every scope.
type Snapshot struct {
	Start, End time.Time
	Test       Scope
	Scenarios  map[string]*ScenarioScope
}

// Add adds o's measurements to s: counters add, an optional counter stays
// set only if it is set in both (a nil on either side gives nil), and the
// distributions combine (see the package documentation for the rules). It
// never modifies o, and on error it leaves s unchanged.
//
// A zero Scope has nil optional counters, so adding to one keeps them nil.
// To sum scopes, start from a Clone of the first and Add the others.
// Add merges o's histograms into s's in place, allocating a histogram when
// s has none.
func (s *Scope) Add(o *Scope) error {
	if o == nil {
		return nil
	}
	if err := s.check(); err != nil {
		return err
	}
	if err := o.check(); err != nil {
		return err
	}
	r := *s
	var err error
	add := func(a, b int64) int64 {
		v, ok := addInt(a, b)
		if !ok && err == nil {
			err = errors.New("thresholds: counter overflow")
		}
		return v
	}
	r.Requests = add(s.Requests, o.Requests)
	r.Failed = add(s.Failed, o.Failed)
	r.BytesSent = add(s.BytesSent, o.BytesSent)
	r.BytesReceived = add(s.BytesReceived, o.BytesReceived)
	opt := func(a, b *int64) *int64 {
		if a == nil || b == nil {
			return nil
		}
		v := add(*a, *b)
		return &v
	}
	r.Checks = opt(s.Checks, o.Checks)
	r.ChecksPassed = opt(s.ChecksPassed, o.ChecksPassed)
	r.Iterations = opt(s.Iterations, o.Iterations)
	r.DroppedIterations = opt(s.DroppedIterations, o.DroppedIterations)
	r.Duration = addDistribution(s.Duration, o.Duration, add)
	r.TTFB = addDistribution(s.TTFB, o.TTFB, add)
	r.IterationDuration = addDistribution(s.IterationDuration, o.IterationDuration, add)
	if err != nil {
		return err
	}
	// Histograms last: nothing can fail after this point.
	r.Duration.Hist = mergeHist(s.Duration, o.Duration)
	r.TTFB.Hist = mergeHist(s.TTFB, o.TTFB)
	r.IterationDuration.Hist = mergeHist(s.IterationDuration, o.IterationDuration)
	*s = r
	return nil
}

func addInt(a, b int64) (int64, bool) {
	c := a + b
	return c, (c >= a) == (b >= 0)
}

// addDistribution combines the exact fields of two distributions; the
// histogram is left to mergeHist.
func addDistribution(a, b Distribution, add func(a, b int64) int64) Distribution {
	r := Distribution{Count: add(a.Count, b.Count), SumUs: add(a.SumUs, b.SumUs)}
	switch {
	case a.Count > 0 && b.Count > 0:
		r.MinUs, r.MaxUs = min(a.MinUs, b.MinUs), max(a.MaxUs, b.MaxUs)
	case a.Count > 0:
		r.MinUs, r.MaxUs = a.MinUs, a.MaxUs
	case b.Count > 0:
		r.MinUs, r.MaxUs = b.MinUs, b.MaxUs
	}
	return r
}

// mergeHist returns the histogram of a and b combined. A side with values
// but no histogram makes the result nil (the percentiles are unknown).
// Otherwise the result is nil only when neither side has a histogram, and
// a known histogram, possibly empty, when either has one: a.Hist with b's
// values merged in, or a new histogram when a has none. The rule is
// symmetric, so the order of the nodes does not matter.
func mergeHist(a, b Distribution) *hdrhistogram.Histogram {
	if (a.Count > 0 && a.Hist == nil) || (b.Count > 0 && b.Hist == nil) {
		return nil
	}
	if a.Hist == nil && b.Hist == nil {
		return nil
	}
	h := a.Hist
	if h == nil {
		h = NewHistogram()
	}
	if b.Hist != nil && b.Hist.TotalCount() > 0 {
		from := b.Hist
		if from == h {
			from = cloneHist(from) // Merge cannot read and write the same histogram
		}
		h.Merge(from) // same configuration, so nothing is dropped
	}
	return h
}

func cloneHist(h *hdrhistogram.Histogram) *hdrhistogram.Histogram {
	if h == nil {
		return nil
	}
	c := NewHistogram()
	c.Merge(h)
	return c
}

// check rejects negative counters and histograms with another
// configuration.
func (s *Scope) check() error {
	for _, v := range []int64{s.Requests, s.Failed, s.BytesSent, s.BytesReceived} {
		if v < 0 {
			return errors.New("thresholds: negative counter")
		}
	}
	for _, p := range []*int64{s.Checks, s.ChecksPassed, s.Iterations, s.DroppedIterations} {
		if p != nil && *p < 0 {
			return errors.New("thresholds: negative counter")
		}
	}
	for _, d := range []*Distribution{&s.Duration, &s.TTFB, &s.IterationDuration} {
		if d.Count < 0 || d.SumUs < 0 || (d.Count > 0 && (d.MinUs < 0 || d.MaxUs < d.MinUs)) {
			return errors.New("thresholds: invalid distribution")
		}
		if d.Hist != nil && !standardHistogram(d.Hist) {
			return errors.New("thresholds: " + errHistogramConfig.Error())
		}
	}
	return nil
}

// Clone returns a deep copy of s: optional counters and histograms are
// copied, not shared.
func (s *Scope) Clone() *Scope {
	c := *s
	cp := func(p *int64) *int64 {
		if p == nil {
			return nil
		}
		v := *p
		return &v
	}
	c.Checks, c.ChecksPassed = cp(s.Checks), cp(s.ChecksPassed)
	c.Iterations, c.DroppedIterations = cp(s.Iterations), cp(s.DroppedIterations)
	c.Duration.Hist = cloneHist(s.Duration.Hist)
	c.TTFB.Hist = cloneHist(s.TTFB.Hist)
	c.IterationDuration.Hist = cloneHist(s.IterationDuration.Hist)
	return &c
}

// Merge combines node snapshots of one run into a new snapshot, leaving
// the inputs untouched. Each scope is the sum of the nodes' scopes with the
// same name (Scope.Add rules); a scenario or step missing on a node counts
// as nothing measured there. Start is the earliest start and End the
// latest end, so rps over the merged snapshot is the run's wall-clock
// throughput, not the sum of the nodes' rates.
//
// Merge returns an error for no snapshots, a nil snapshot, a zero Start or
// End, a negative counter, or a histogram that does not have the snapshot
// configuration. Merge allocates a histogram (about 188 KB) for every
// distribution that has values.
func Merge(nodes ...*Snapshot) (*Snapshot, error) {
	if len(nodes) == 0 {
		return nil, errors.New("thresholds: merge: no snapshots")
	}
	out := &Snapshot{}
	for i, n := range nodes {
		if n == nil {
			return nil, fmt.Errorf("thresholds: merge: snapshot %d is nil", i)
		}
		if n.Start.IsZero() || n.End.IsZero() {
			return nil, fmt.Errorf("thresholds: merge: snapshot %d has no start or end time", i)
		}
		if i == 0 || n.Start.Before(out.Start) {
			out.Start = n.Start
		}
		if i == 0 || n.End.After(out.End) {
			out.End = n.End
		}
		if err := addInto(&out.Test, &n.Test, i == 0); err != nil {
			return nil, fmt.Errorf("thresholds: merge: snapshot %d: %w", i, err)
		}
		for name, sc := range n.Scenarios {
			if sc == nil {
				continue
			}
			if out.Scenarios == nil {
				out.Scenarios = map[string]*ScenarioScope{}
			}
			dst, seen := out.Scenarios[name]
			if !seen {
				dst = &ScenarioScope{}
				out.Scenarios[name] = dst
			}
			if err := addInto(&dst.Scope, &sc.Scope, !seen); err != nil {
				return nil, fmt.Errorf("thresholds: merge: snapshot %d, scenario %q: %w", i, name, err)
			}
			for step, st := range sc.Steps {
				if st == nil {
					continue
				}
				if dst.Steps == nil {
					dst.Steps = map[string]*Scope{}
				}
				d, seen := dst.Steps[step]
				if !seen {
					d = &Scope{}
					dst.Steps[step] = d
				}
				if err := addInto(d, st, !seen); err != nil {
					return nil, fmt.Errorf("thresholds: merge: snapshot %d, scenario %q, step %q: %w", i, name, step, err)
				}
			}
		}
	}
	return out, nil
}

// addInto adds src to dst, or makes dst a copy of src when first is set.
func addInto(dst, src *Scope, first bool) error {
	if !first {
		return dst.Add(src)
	}
	if err := src.check(); err != nil {
		return err
	}
	*dst = *src.Clone()
	return nil
}

// span returns the snapshot window in seconds, or false when it is empty.
func (s *Snapshot) span() (float64, bool) {
	d := s.End.Sub(s.Start)
	if s.Start.IsZero() || s.End.IsZero() || d <= 0 {
		return 0, false
	}
	return d.Seconds(), true
}

// percentile reads a percentile from d's histogram; ok is false when there
// is none to read.
func (d *Distribution) percentile(p float64) (float64, bool) {
	if d.Count <= 0 || d.Hist == nil || d.Hist.TotalCount() <= 0 || !standardHistogram(d.Hist) {
		return 0, false
	}
	return float64(d.Hist.ValueAtQuantile(p)), true
}
