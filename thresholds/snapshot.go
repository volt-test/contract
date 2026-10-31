package thresholds

import (
	"fmt"
	"strings"
	"time"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"
)

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
// Responses is the number of responses received over the whole test. No
// threshold reads it; it is there for a consumer that needs a floor on
// the responses behind a verdict. It may differ from Test.Requests.
type Snapshot struct {
	Start, End time.Time
	Responses  int64
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
// Add merges o's histograms into s's in place and keeps s's histogram
// pointers, with one exception: when either side of a distribution has
// values but no histogram (Count > 0, Hist nil), the percentiles become
// unknown and the result's Hist is nil. Otherwise a caller can keep
// recording into s's histograms, provided it keeps Count, SumUs, MinUs
// and MaxUs in step (see the package documentation). Add allocates a
// histogram only when s has none and o has values or a histogram with
// Count > 0.
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
			err = ErrOverflow
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

// histOf returns d's histogram, or nil when it has none or it adds
// nothing: a distribution with Count 0 and an empty histogram is the same
// as one without a histogram.
func histOf(d Distribution) *hdrhistogram.Histogram {
	if d.Hist == nil || (d.Count == 0 && d.Hist.TotalCount() == 0) {
		return nil
	}
	return d.Hist
}

// mergeHist returns the histogram of a and b combined, where a is the
// receiver of Add. A side with values but no histogram makes the result
// nil (the percentiles are unknown). Otherwise a.Hist, when set, is kept
// and receives b's values, even when both are empty: a caller may hold it
// and keep recording into it. When a has no histogram, one is created
// only if b has one (see histOf). Merge starts from Clones, which drop
// empty histograms, so its output does not depend on the order of the
// nodes.
func mergeHist(a, b Distribution) *hdrhistogram.Histogram {
	if (a.Count > 0 && a.Hist == nil) || (b.Count > 0 && b.Hist == nil) {
		return nil
	}
	hb := histOf(b)
	h := a.Hist
	if h == nil {
		if hb == nil {
			return nil
		}
		h = NewHistogram()
	}
	if hb != nil && hb.TotalCount() > 0 {
		if hb == h {
			hb = cloneHist(hb) // Merge cannot read and write the same histogram
		}
		h.Merge(hb) // same configuration, so nothing is dropped
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

// check rejects negative counters, broken invariants and histograms with
// another configuration.
func (s *Scope) check() error {
	for _, v := range []int64{s.Requests, s.Failed, s.BytesSent, s.BytesReceived} {
		if v < 0 {
			return fmt.Errorf("%w: negative counter", ErrInvalidScope)
		}
	}
	for _, p := range []*int64{s.Checks, s.ChecksPassed, s.Iterations, s.DroppedIterations} {
		if p != nil && *p < 0 {
			return fmt.Errorf("%w: negative counter", ErrInvalidScope)
		}
	}
	if s.Failed > s.Requests {
		return fmt.Errorf("%w: more failed requests than requests", ErrInvalidScope)
	}
	if s.Checks != nil && s.ChecksPassed != nil && *s.ChecksPassed > *s.Checks {
		return fmt.Errorf("%w: more passed checks than checks", ErrInvalidScope)
	}
	for _, d := range []*Distribution{&s.Duration, &s.TTFB, &s.IterationDuration} {
		if d.Count < 0 || d.SumUs < 0 || (d.Count > 0 && (d.MinUs < 0 || d.MaxUs < d.MinUs)) {
			return fmt.Errorf("%w: invalid distribution", ErrInvalidScope)
		}
		if d.Hist != nil && !standardHistogram(d.Hist) {
			return ErrHistogramConfig
		}
		// A producer may leave values out of the histogram (over an hour),
		// never add any.
		if d.Hist != nil && d.Hist.TotalCount() > d.Count {
			return fmt.Errorf("%w: histogram holds more values than Count", ErrInvalidScope)
		}
	}
	return nil
}

// Validate reports whether s alone satisfies the invariants that Add and
// Merge require and that Evaluate needs to read it (see the package
// documentation). It returns an error wrapping ErrInvalidScope for a nil
// scope, a negative counter or sum, or a broken invariant, and
// ErrHistogramConfig for a histogram with another configuration. A valid
// scope can still fail Add or Merge with ErrOverflow, which only adding
// detects. Validate never modifies s.
//
// A producer can call Validate on each scope it builds before adding or
// evaluating it, to report the bad scope by name.
func (s *Scope) Validate() error {
	if s == nil {
		return fmt.Errorf("%w: nil scope", ErrInvalidScope)
	}
	return s.check()
}

// Clone returns a deep copy of s: optional counters and histograms are
// copied, not shared. A distribution with Count 0 and an empty histogram
// is cloned without one (see the package documentation), so Clone
// allocates a histogram only when there is something to copy.
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
	c.Duration.Hist = cloneHist(histOf(s.Duration))
	c.TTFB.Hist = cloneHist(histOf(s.TTFB))
	c.IterationDuration.Hist = cloneHist(histOf(s.IterationDuration))
	return &c
}

// Merge combines node snapshots of one run into a new snapshot, leaving
// the inputs untouched. Each scope is the sum of the nodes' scopes with the
// same name (Scope.Add rules); a scenario or step missing on a node counts
// as nothing measured there. Responses add. Start is the earliest start
// and End the latest end, so rps over the merged snapshot is the run's
// wall-clock throughput, not the sum of the nodes' rates.
//
// Merge returns an error wrapping ErrInvalidScope for no snapshots, a nil
// snapshot, a zero Start or End, an End before the Start, a negative
// Responses, a negative counter or a broken invariant; ErrOverflow for an
// overflow; and ErrHistogramConfig for a histogram without the snapshot
// configuration. Merge allocates a histogram (about 188 KB) for every
// distribution that has values.
func Merge(nodes ...*Snapshot) (*Snapshot, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: merge: no snapshots", ErrInvalidScope)
	}
	out := &Snapshot{}
	for i, n := range nodes {
		if n == nil {
			return nil, fmt.Errorf("%w: snapshot %d is nil", ErrInvalidScope, i)
		}
		if n.Start.IsZero() || n.End.IsZero() {
			return nil, fmt.Errorf("%w: snapshot %d has no start or end time", ErrInvalidScope, i)
		}
		if n.End.Before(n.Start) {
			return nil, fmt.Errorf("%w: snapshot %d ends before it starts", ErrInvalidScope, i)
		}
		if n.Responses < 0 {
			return nil, fmt.Errorf("%w: snapshot %d has negative responses", ErrInvalidScope, i)
		}
		r, ok := addInt(out.Responses, n.Responses)
		if !ok {
			return nil, inMerge(ErrOverflow, fmt.Sprintf("snapshot %d, responses", i))
		}
		out.Responses = r
		if i == 0 || n.Start.Before(out.Start) {
			out.Start = n.Start
		}
		if i == 0 || n.End.After(out.End) {
			out.End = n.End
		}
		if err := addInto(&out.Test, &n.Test, i == 0); err != nil {
			return nil, inMerge(err, fmt.Sprintf("snapshot %d, test", i))
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
				return nil, inMerge(err, fmt.Sprintf("snapshot %d, scenario %s", i, quoteShort(name)))
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
					return nil, inMerge(err, fmt.Sprintf("snapshot %d, scenario %s, step %s", i, quoteShort(name), quoteShort(step)))
				}
			}
		}
	}
	return out, nil
}

// mergeError adds where a merge or a decode failed to an error, keeping
// the error matchable with errors.Is.
type mergeError struct {
	msg string
	err error
}

func (e *mergeError) Error() string { return e.msg }
func (e *mergeError) Unwrap() error { return e.err }

func inMerge(err error, where string) error {
	return &mergeError{msg: "thresholds: merge: " + where + ": " + strings.TrimPrefix(err.Error(), "thresholds: "), err: err}
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
