// Package thresholds reads the threshold expressions of a VoltTest test
// definition: the pass/fail rules a run is judged by, such as
// "p95 < 500ms" or "error_rate < 1%".
//
// # Grammar
//
//	<metric> <operator> <value>
//
// Spaces around the three parts are optional ("p99<=1s" is valid); spaces,
// tabs and line breaks count as spaces. Inside the value there is no
// space: "500ms", not "500 ms".
//
// The operators are <, <=, >, >= and ==. There is no "=" or "!=".
//
// A value is digits with an optional fraction ("5", "0.5", "99.95") and
// an optional unit. It has no sign, no exponent and no leading dot, and a
// value with a non-zero digit must not round to zero.
// Units are ASCII and case-insensitive ("500MS" is "500ms"), except that
// the micro prefix may be written us, µs (micro sign) or μs (lowercase
// Greek mu). Greek capital Mu is rejected, because it reads as an ASCII
// M. Metric names are case-sensitive.
//
// # Metrics
//
// Request duration, measured per request:
//
//	avg  min  max  p50  p75  p90  p95  p99  p99.9
//
// Time to first byte, with the same summaries: ttfb_avg, ttfb_min,
// ttfb_max, ttfb_p50, ... ttfb_p99.9. Plain "ttfb" is ttfb_avg.
//
// Iteration duration (one run of a scenario), with the same summaries:
// iteration_duration_avg ... iteration_duration_p99.9. Plain
// "iteration_duration" is iteration_duration_avg.
//
// Only the listed percentiles exist: "p99.99" and "ttfb_p42" are errors.
//
//	metric                kind        value
//	error_rate            rate        failed requests / requests
//	check_rate            rate        passed checks / checks
//	rps                   throughput  requests per second
//	requests              count       requests sent
//	failed                count       failed requests
//	iterations            count       completed scenario runs
//	dropped_iterations    count       scenario runs that could not start
//	bytes_sent            size        request bytes
//	bytes_received        size        response bytes
//
// Metrics returns the full list.
//
// # Units
//
//	kind        units                                     canonical Value
//	duration    us (or µs), ms, s, m; a unit is required  microseconds
//	rate        %; required                               fraction (1% = 0.01)
//	count       none; a whole number                      the count
//	throughput  none                                      requests per second
//	size        b, kb, mb, gb (x1000), kib, mib, gib      bytes; a whole number
//	            (x1024), or none for bytes                of bytes
//
// A duration without a unit ("p95 < 500") is rejected, and so is "ns". A
// rate without "%" ("error_rate < 0.05") is rejected rather than guessed
// at, and a rate above 100% is rejected. Results report rates in percent,
// as they are written.
//
// # Scopes
//
// A threshold can be attached to the whole test, a scenario or a step.
// Every metric is available everywhere except the iteration metrics
// (iteration_duration and its variants, iterations, dropped_iterations),
// which describe whole scenario runs and so exist only for the test and
// its scenarios. Threshold.ValidFor checks this.
//
// # Equality
//
// For counts and sizes, which are whole numbers, == is exact:
// "dropped_iterations == 0" holds only when no iteration was dropped, and
// "requests == 1000000000" fails at 1000000001.
//
// Durations, rates and rps are computed in floating point, so == compares
// them with a small tolerance: a and b are equal when
//
//	|a-b| <= 1e-9 * max(1, |a|, |b|)
//
// For "p95 == 200ms" (200000us) any value within 0.0002us of 200000us is
// equal. With the tolerance, < and > need a value outside the band, and
// <= and >= accept one inside it.
//
// # Canonical form
//
// Threshold.String prints a threshold in canonical form, such as
// "p95 < 500ms" for "p95<0.5s"; parsing it gives back the same threshold.
//
// # Evaluation
//
// Evaluate checks bounds against a Snapshot: the end-of-run measurements
// of the test, each scenario and each step. It is a pure function with no
// dependency on an engine, so the engine and the backend (re-evaluating
// a run merged from several nodes) get the same verdict from the same
// data. Every scope is computed from its own measurements; nothing is
// averaged across scopes or nodes.
//
//	metric                  derived from                      Result unit
//	avg, min, max           Count, SumUs, MinUs, MaxUs        ms
//	p50 ... p99.9           Hist.ValueAtQuantile(50 ... 99.9) ms
//	ttfb*, iteration_dur*   the same, on TTFB / IterationDuration
//	error_rate              Failed / Requests                 %
//	check_rate              ChecksPassed / Checks             %
//	rps                     Requests / (End - Start)          req/s
//	requests, failed        Requests, Failed                  "" (a count)
//	iterations, dropped_... Iterations, DroppedIterations     ""
//	bytes_sent, _received   BytesSent, BytesReceived          bytes
//
// Averages use Count and SumUs, which cover every value, never the
// histogram's total: a producer may leave values over an hour out of the
// histogram. Comparisons run in canonical units (microseconds, fractions,
// bytes); Result.Actual converts for display only: milliseconds rounded
// to the microsecond, rates in percent.
//
// rps uses the snapshot's window (Start to End) at every scope, so on a
// merged run it is wall-clock throughput, not the sum of the nodes'
// rates. A scenario or step that runs for only part of the test is
// measured over the whole window too, so its rps is diluted: a scenario
// that ran for the last 10s of a 60s test shows a sixth of its rate.
//
// A percentile is HDR's highest equivalent value for its bucket, which
// can exceed the recorded values by up to 0.1% (3 significant figures):
// values of exactly 500000us read back as a p95 of 500.223ms. So
// "p95 == 500ms" and even "p95 <= 500ms" fail on them. Use < or <= with
// a little headroom for latency, not ==.
//
// # Comparisons
//
// ParseComparison reads a comparison without a metric, such as "< 500ms",
// for a single value measured elsewhere: a response time, a body size or
// a number of items. It takes the kind of the value, which must be a
// duration, a size or a count, and reads the text exactly as the operator
// and value of a threshold of that kind: the same operators, value forms,
// units and canonical Value ("< 500ms" is 500000us, "< 50kb" is 50000
// bytes). Comparison.Holds applies the same == rule as Evaluate: exact for
// sizes and counts, with the tolerance above for durations. Rates and
// throughput have no comparison form.
//
// # No data
//
// A threshold has status no_data, with a null actual, when there is
// nothing to measure: a distribution with Count 0; a percentile with no
// histogram, an empty one, or one with another configuration; a rate with
// a zero denominator; an optional counter (Checks, ChecksPassed,
// Iterations, DroppedIterations) that is nil because it was not measured;
// rps over an empty window (End not after Start).
//
// Evaluate fails closed on missing scopes. With a nil snapshot every
// result is no_data. A scope the snapshot does not have is no_data for
// every metric, counts, sizes and rps included, so a scenario that
// never ran cannot pass "failed == 0" or breach "requests > 10".
// Producers must therefore emit every scope of the definition, with zeros
// when nothing happened: a present scope with no requests is a measured
// zero, and its count, size and rps thresholds pass or fail normally.
//
// A threshold that is not Valid also gives no_data. Callers that build
// or receive thresholds other than through Parse should call Valid first
// and report the error (the engine at startup, the backend before it
// re-evaluates).
//
// AllPassed, the final verdict, fails on no_data as on failed. Callers that
// evaluate while the run is going (a rolling window) should act on
// Breached, which returns only failed results: no_data never aborts a run,
// because a scope may simply not have run yet.
//
// # Histograms
//
// Histograms record microseconds from 1 to 3,600,000,000 (one hour) with
// 3 significant figures: HistogramLowestUs, HistogramHighestUs and
// HistogramSigFigs, as created by NewHistogram.
//
// EncodeHistogram produces base64 text (the V2 compressed HDR encoding),
// and DecodeHistogram takes that same text. How a JSON payload carries it
// depends on the field: a JSON string field carries the text once, while
// a Go []byte field, such as the engine's existing hdr_data and
// summary_hdr_data fields, is base64-encoded again by encoding/json, so
// the JSON carries it twice, and a Go reader that unmarshals into []byte
// gets the text back. Writer and reader must agree on the field's
// convention.
//
// Read histograms with DecodeHistogram, never hdrhistogram.Decode, on
// bytes from another process: DecodeHistogram validates the headers and
// the configuration before it allocates, caps the input at 512 KiB and
// the inflated data at 256 KiB, and never panics or loops, while
// hdrhistogram.Decode can hang or allocate without bound on a crafted
// header.
//
// # Concurrency
//
// Snapshots, scopes and histograms are not safe for concurrent use.
// Evaluate only reads, but it must not run while anything Adds to or
// records into the same scopes; synchronise externally. Never pass a
// histogram that is still recording: give Evaluate and Merge a finished
// histogram or a copy.
//
// # Merging
//
// Merge combines node snapshots of one run into a new snapshot, by
// scenario and step name, without modifying them. Scope.Add applies the
// same rules to two scopes, so a producer can sum steps into a scenario:
//
//   - Counters add.
//   - An optional counter stays set only when it is set on every side; a
//     nil anywhere gives nil, so an unmeasured counter is never mistaken
//     for 0. A producer that measures a counter reports 0, not nil, for a
//     scope where nothing happened.
//   - Count and SumUs add; MinUs and MaxUs come only from sides with
//     Count > 0, so an empty side never contributes a 0 minimum.
//   - Histograms are merged into a new histogram after their
//     configuration is checked; hdrhistogram's own Merge would silently
//     drop or misplace values from a histogram with another configuration.
//     A side with values (Count > 0) but no histogram makes the merged
//     percentiles unknown (no_data) for the whole merge. With Count 0, an
//     empty histogram is the same as none; Clone and Merge return it as
//     none, so they allocate a histogram only when there is something to
//     copy.
//   - Start is the earliest start and End the latest end.
//
// Every scope must satisfy Failed <= Requests, ChecksPassed <= Checks when
// both are set, and Hist.TotalCount() <= Count for each distribution (a
// producer may leave values out of a histogram, never add any); counters
// and sums must not be negative. Merge and Add reject a scope that breaks
// these, and Evaluate gives every threshold on it no_data.
//
// Merge fails with ErrInvalidScope on no snapshots, a nil snapshot, a
// zero Start or End, an End before the Start, a negative counter or a
// broken invariant; with ErrOverflow on an overflow; and with
// ErrHistogramConfig on a histogram with another configuration.
//
// A histogram takes about 188 KB, so Merge allocates one per merged
// distribution with values. A caller that evaluates repeatedly during a
// run should keep cumulative scopes and update them with Scope.Add rather
// than rebuild and merge snapshots. Add keeps the receiver's histogram
// pointers, except that a side with values but no histogram makes the
// result's histogram nil (the percentiles become unknown). The caller can
// keep recording into a kept histogram between Adds, never during an Add
// or an Evaluate, but only if it updates Count, SumUs, MinUs and MaxUs
// with every recorded value: otherwise the histogram holds more values
// than Count, and the next Add, Merge or Evaluate rejects the scope (or,
// for Evaluate, gives its thresholds no_data).
package thresholds
