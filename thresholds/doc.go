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
// the micro prefix may be written us, µs (micro sign) or μs (Greek mu, in
// either case). Metric names are case-sensitive.
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
// equal.
//
// # Canonical form
//
// Threshold.String prints a threshold in canonical form, such as
// "p95 < 500ms" for "p95<0.5s"; parsing it gives back the same threshold.
package thresholds
