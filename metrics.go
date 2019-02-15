// Package metrics provides base metric types: counter, gauge, and histogram.
// This package is intended to implement low-level metrics in applications with
// short metric reporting intervals (1-60 seconds). The canonical use case is
// an API that reports metrics every 1-30s, resets the gauges and histograms,
// and emits the metrics to a 3rd-party metrics system like Datadog, SignalFx, Prometheus, etc.
// Use another package for longer intervals, streaming metrics, or trending.
//
// This package differs from other Go metric packages in three significant ways:
//
// 1. Metrics: Only base metric types are provide (counter, gauge, histogram).
// There are no sinks, registries, or derivative metric types. These should be
// implement by other packages which import this package.
//
// 2. Sampling: Only "Algorithm R" by Jeffrey Vitter (https://www.cs.umd.edu/~samir/498/vitter.pdf)
// is used to sample values for Gauge and Histogram. The reservoir size is fixed
// at 2,000. Testing with real-world values shows that smaller and larger sizes
// either yield no benefit or reduce accuracy. And the true maximum value is kept
// and reported, which is not a feature of the original Algorithm R but critical
// for application performance monitoring.
//
// 3. Percentiles: Both nearest rank and linear interpolation are used calculate
// percentile values. If the sample is full (>= 2,000 values), nearest rank is
// used; else, "Definition 8"--better known as "R8"--is used (https://www.amherst.edu/media/view/129116/original/Sample+Quantiles.pdf).
// Testing with real-world values shows that this combination produces more accurate
// P999 (99.9th percentile) values, which is the gold standard for high-performance,
// low-latency applications.
//
// Additionally, this package supports atomic snapshots: metric values can be
// reset to zero after snapshot with no loss of values between snapshot and reset.
//
// Counter, Gauge, and Histogram are safe for use by multiple goroutines.
package metrics

import (
	"math"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
)

var defaultSampleSize = 2000

// Config represents Gauge and Histogram configuration. Currently, only percentiles
// are configured. This struct is placeholder for future configurations, if needed.
type Config struct {
	// Percentiles to calculate for Gauge and Histogram snapshots. Values must
	// be divided by 100, so the 99th percentile is 0.99. If the list is nil or
	// empty, no percentiles are calculated.
	Percentiles []float64
}

// A Metric generates a Snapshot of its current values. If reset is true, all
// values are reset to zero.
type Metric interface {
	Snapshot(reset bool) Snapshot
}

// Snapshot represents Metric values at one point in time.
type Snapshot struct {
	// N is the number of values. For Counter, this is generally not used.
	// For Gauge and Histogram, this is used to calculate the true average:
	// Sum / N.
	N int64

	// Sum is the sum of all values. For Counter, this is the value returned by
	// Count(). For Gauge and Histogram, this is used to calculate the true
	// average: Sum / N.
	Sum float64

	// Min is the minimum sample value. It might not be the true minimum value.
	// For Counter, this is always zero. For Gauge and Histogram, it is the
	// minimum value in the sample.
	Min float64

	// Max is the true maximum value. For Counter, this is always zero.
	// For Gauge and Histogram, it is the true maximum value which might not
	// be present in the sample but was recorded.
	Max float64

	// Percentile is the percentile value for each Config.Percentiles.
	// For Counter, the map is always nil.
	Percentile map[float64]float64

	// Last is the last value recorded (or added) to a Gauge. This is the value
	// returned by Last(). For Counter and Histogram, it is always zero.
	Last float64
}

// --------------------------------------------------------------------------
// Counter
// --------------------------------------------------------------------------

// Counter counts events and things, like queries and connected clients.
type Counter struct {
	*sync.Mutex
	n   int64
	sum int64
}

func NewCounter() *Counter {
	return &Counter{
		Mutex: &sync.Mutex{},
	}
}

func (c *Counter) Add(delta int64) {
	atomic.AddInt64(&c.n, 1)
	atomic.AddInt64(&c.sum, delta)
}

func (c *Counter) Count() int64 {
	return atomic.LoadInt64(&c.sum)
}

func (c *Counter) Snapshot(reset bool) Snapshot {
	c.Lock()
	snapshot := Snapshot{
		N:   c.n,
		Sum: float64(c.sum),
	}
	if reset {
		c.n = 0
		c.sum = 0
	}
	c.Unlock()
	return snapshot
}

// --------------------------------------------------------------------------
// Gauge
// --------------------------------------------------------------------------

// Gauge represents a single value.
type Gauge struct {
	percentiles []float64
	*sync.Mutex
	resv *randomSample
	last float64
}

func NewGauge(cfg Config) *Gauge {
	return &Gauge{
		percentiles: cfg.Percentiles,
		Mutex:       &sync.Mutex{},
		resv:        newRandomSample(defaultSampleSize),
	}
}

func (g *Gauge) Record(v float64) {
	g.Lock()
	g.last = v
	g.resv.record(g.last)
	g.Unlock()
}

func (g *Gauge) Add(delta int64) {
	g.Lock()
	g.last += float64(delta)
	g.resv.record(g.last)
	g.Unlock()
}

func (g *Gauge) Last() float64 {
	g.Lock()
	last := g.last
	g.Unlock()
	return last
}

func (g *Gauge) Snapshot(reset bool) Snapshot {
	g.Lock()
	snapshot := Snapshot{
		Last: g.last,
	}
	finalizeSnapshot(&snapshot, g.resv, g.percentiles, reset)
	if reset {
		g.last = 0
	}
	g.Unlock()
	return snapshot
}

// --------------------------------------------------------------------------
// Histogram
// --------------------------------------------------------------------------

// Histogram summarizes a sample of many values.
type Histogram struct {
	percentiles []float64
	*sync.Mutex
	resv *randomSample
}

func NewHistogram(cfg Config) *Histogram {
	return &Histogram{
		percentiles: cfg.Percentiles,
		Mutex:       &sync.Mutex{},
		resv:        newRandomSample(defaultSampleSize),
	}
}

func (h *Histogram) Record(v float64) {
	h.Lock()
	h.resv.record(v)
	h.Unlock()
}

func (h *Histogram) Snapshot(reset bool) Snapshot {
	h.Lock()
	snapshot := Snapshot{}
	finalizeSnapshot(&snapshot, h.resv, h.percentiles, reset)
	h.Unlock()
	return snapshot
}

func finalizeSnapshot(snapshot *Snapshot, resv *randomSample, p []float64, reset bool) {
	if len(resv.values) == 0 {
		return // reset then called again without any new values
	}

	snapshot.N = resv.n
	snapshot.Sum = resv.sum
	snapshot.Max = resv.max

	// If reseting we can avoid the copy
	var values []float64
	if reset {
		values = resv.values
		sort.Float64s(values)
		snapshot.Min = values[0]
		resv.reset()
	} else {
		values = make([]float64, len(resv.values))
		copy(values, resv.values)
		sort.Float64s(values)
		snapshot.Min = values[0]
	}
	snapshot.Percentile = percentiles(p, values, resv.sampleSize)
}

// --------------------------------------------------------------------------
// Vitter's algorithm R: http://www.cs.umd.edu/~samir/498/vitter.pdf
// --------------------------------------------------------------------------

type randomSample struct {
	sampleSize int
	n          int64
	sum        float64
	max        float64
	values     []float64
}

func newRandomSample(size int) *randomSample {
	return &randomSample{
		sampleSize: size,
		values:     make([]float64, 0, size),
	}

}

func (s *randomSample) record(v float64) {
	s.n++
	s.sum += v
	if len(s.values) < s.sampleSize {
		s.values = append(s.values, v)
	} else {
		r := rand.Int63n(s.n)
		if r < int64(len(s.values)) {
			s.values[int(r)] = v
		}
	}
	if v > s.max {
		s.max = v
	}
}

func (s *randomSample) reset() {
	s.n = 0
	s.sum = 0
	s.max = 0
	s.values = make([]float64, 0, s.sampleSize)
}

// --------------------------------------------------------------------------
// Percentiles equations:
// https://www.amherst.edu/media/view/129116/original/Sample+Quantiles.pdf
// --------------------------------------------------------------------------

func percentiles(percentiles, values []float64, sampleSize int) map[float64]float64 {
	scores := map[float64]float64{}
	n := float64(len(values))
	if n == 0 || len(percentiles) == 0 {
		return scores
	}
	if int(n) >= sampleSize {
		for _, p := range percentiles {
			i := int(math.Ceil(p * n))
			scores[p] = values[i-1]
		}
		return scores
	}
	for _, p := range percentiles {
		//i := p * (float64(n) + 1) // R6
		//i := p*(float64(n)-1) + 1 // R7
		i := p*(n+(1/3.0)) + (1 / 3.0) // R8
		if i < 1.0 {
			scores[p] = values[0]
		} else if i >= n {
			scores[p] = values[int(n)-1]
		} else {
			k, f := math.Modf(i) // 8.53 -> i=8, d=53
			lower := values[int(k)-1]
			upper := values[int(k)]
			scores[p] = lower + f*(upper-lower)
		}
	}
	return scores
}
