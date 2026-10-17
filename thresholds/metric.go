package thresholds

import (
	"sort"
	"strconv"
)

// Kind is the kind of value a metric measures. It decides which units a
// threshold value may use and the unit of Threshold.Value.
type Kind int

const (
	// KindDuration values are times. Threshold.Value is in microseconds.
	KindDuration Kind = iota + 1
	// KindRate values are shares of a total. Threshold.Value is a fraction
	// (5% is 0.05).
	KindRate
	// KindCount values are numbers of events. Threshold.Value is the count.
	KindCount
	// KindSize values are amounts of data. Threshold.Value is in bytes.
	KindSize
	// KindThroughput values are requests per second.
	KindThroughput
)

func (k Kind) String() string {
	switch k {
	case KindDuration:
		return "duration"
	case KindRate:
		return "rate"
	case KindCount:
		return "count"
	case KindSize:
		return "size"
	case KindThroughput:
		return "throughput"
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// family is the measurement a metric reads.
type family int

const (
	famRequestDuration family = iota + 1
	famTTFB
	famIterationDuration
	famErrorRate
	famCheckRate
	famRPS
	famRequests
	famFailed
	famBytesSent
	famBytesReceived
	famIterations
	famDroppedIterations
)

// agg is how a duration metric summarises its distribution.
type agg int

const (
	aggNone agg = iota // not a distribution
	aggAvg
	aggMin
	aggMax
	aggPercentile
)

// Metric is one entry of the closed metric catalog. Get one from Parse;
// the zero Metric is not in the catalog.
type Metric struct {
	// Name is the metric as written in a threshold, e.g. "p95", "ttfb" or
	// "ttfb_p99.9".
	Name string
	// Kind decides the units the threshold value may use.
	Kind Kind

	family family
	agg    agg
	// percentile is the percentile of a percentile metric, e.g. 99.9 for
	// p99.9 (a percent, as HDR histograms take it); 0 otherwise.
	percentile float64
}

// stepScoped reports whether the metric is available on a step. Iteration
// metrics describe whole scenario runs, so they exist only for the test and
// its scenarios.
func (m Metric) stepScoped() bool {
	switch m.family {
	case famIterationDuration, famIterations, famDroppedIterations:
		return false
	}
	return true
}

// aggregates are the summaries every duration family offers, in the order
// the documentation lists them.
var aggregates = []struct {
	name       string
	agg        agg
	percentile float64
}{
	{"avg", aggAvg, 0},
	{"min", aggMin, 0},
	{"max", aggMax, 0},
	{"p50", aggPercentile, 50},
	{"p75", aggPercentile, 75},
	{"p90", aggPercentile, 90},
	{"p95", aggPercentile, 95},
	{"p99", aggPercentile, 99},
	{"p99.9", aggPercentile, 99.9},
}

// catalog maps every metric name to its Metric.
var catalog = buildCatalog()

func buildCatalog() map[string]Metric {
	c := map[string]Metric{}
	add := func(m Metric) {
		if _, dup := c[m.Name]; dup {
			panic("thresholds: duplicate metric " + m.Name)
		}
		c[m.Name] = m
	}
	for _, a := range aggregates {
		add(Metric{Name: a.name, Kind: KindDuration, family: famRequestDuration, agg: a.agg, percentile: a.percentile})
	}
	for _, fam := range []struct {
		prefix string
		family family
	}{{"ttfb", famTTFB}, {"iteration_duration", famIterationDuration}} {
		// The plain name is the average.
		add(Metric{Name: fam.prefix, Kind: KindDuration, family: fam.family, agg: aggAvg})
		for _, a := range aggregates {
			add(Metric{Name: fam.prefix + "_" + a.name, Kind: KindDuration, family: fam.family, agg: a.agg, percentile: a.percentile})
		}
	}
	for _, m := range []Metric{
		{Name: "error_rate", Kind: KindRate, family: famErrorRate},
		{Name: "check_rate", Kind: KindRate, family: famCheckRate},
		{Name: "rps", Kind: KindThroughput, family: famRPS},
		{Name: "requests", Kind: KindCount, family: famRequests},
		{Name: "failed", Kind: KindCount, family: famFailed},
		{Name: "iterations", Kind: KindCount, family: famIterations},
		{Name: "dropped_iterations", Kind: KindCount, family: famDroppedIterations},
		{Name: "bytes_sent", Kind: KindSize, family: famBytesSent},
		{Name: "bytes_received", Kind: KindSize, family: famBytesReceived},
	} {
		add(m)
	}
	return c
}

// Metrics returns the name of every metric in the catalog, sorted, with
// each ttfb_ and iteration_duration_ variant spelt out.
func Metrics() []string {
	names := make([]string, 0, len(catalog))
	for n := range catalog {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func lookupMetric(name string) (Metric, bool) {
	m, ok := catalog[name]
	return m, ok
}
