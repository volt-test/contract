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
// to the microsecond, rates in percent. rps uses the snapshot's window at
// every scope, so on a merged run it is wall-clock throughput, not the
// sum of the nodes' rates.
//
// # No data
//
// A threshold has status no_data, with a null actual, when there is
// nothing to measure: a distribution with Count 0; a percentile with no
// histogram, an empty one, or one with another configuration; a rate with
// a zero denominator; an optional counter (Checks, ChecksPassed,
// Iterations, DroppedIterations) that is nil because it was not measured;
// rps over an empty window (End not after Start). A scope the snapshot
// does not have, such as a step that never ran, is evaluated as an empty
// scope: its plain counters are 0 and everything else is no_data.
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
// HistogramSigFigs, as created by NewHistogram. Node results carry them in
// the base64 V2 compressed HDR encoding; DecodeHistogram reads that
// safely and rejects any other configuration.
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
//     percentiles unknown (no_data) for the whole merge.
//   - Start is the earliest start and End the latest end.
//
// Merge fails on no snapshots, a nil snapshot, a zero Start or End, a
// negative counter or an overflow, and a histogram with another
// configuration. A histogram takes about 188 KB, so Merge allocates one
// per merged distribution with values. A caller that evaluates repeatedly
// during a run should keep cumulative scopes and update them with
// Scope.Add rather than rebuild and merge snapshots.
package thresholds
