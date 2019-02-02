// Package metrics provides universal base metrics: counter, gauge, and histogram.
package metrics

import (
	"math"
	"math/rand"
	"sort"
	"sync"
)

var defaultSampleSize = 2000

// Config represents Gauge and Histogram configuration. Currently, only percentiles
// are configured. This struct is placeholder for future configurations, if needed.
type Config struct {
	// Percentiles to calculate for each Snapshot. Values must be divided by 100,
	// so the 99th percentile is 0.99. If the list is nil or empty, no percentiles
	// are calculated.
	Percentiles []float64
}

// A Metric generates a Snapshot of its current values.
type Metric interface {
	Snapshot(reset bool) Snapshot
}

// Snapshot represents a Metric at one point in time.
type Snapshot struct {
	N          int64               // c, g, h
	Sum        float64             // c, g, h
	Min        float64             // _, g, h
	Max        float64             // _, g, h
	Percentile map[float64]float64 // _, g, h
	Last       float64             // _, g, _
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
	c.Lock()
	c.n += 1
	c.sum += delta
	c.Unlock()
}

func (c *Counter) Count() int64 {
	c.Lock()
	cnt := c.sum
	c.Unlock()
	return cnt
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
		//pos := p * (float64(n) + 1) // R6
		//pos := p*(float64(n)-1) + 1 // R7
		pos := p*(n+(1/3.0)) + (1 / 3.0) // R8
		if pos < 1.0 {
			scores[p] = values[0]
		} else if pos >= n {
			scores[p] = values[int(n)-1]
		} else {
			i, d := math.Modf(pos) // 8.53 -> i=8, d=53
			lower := values[int(i)-1]
			upper := values[int(i)]
			scores[p] = lower + d*(upper-lower)
		}
	}
	return scores
}
