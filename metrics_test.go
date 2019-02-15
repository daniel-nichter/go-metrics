package metrics_test

import (
	"bufio"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/daniel-nichter/go-metrics"
	"github.com/go-test/deep"
)

func init() {
	// Avoid false-positives like 95.19724333333333 != 95.1972
	deep.FloatPrecision = 4
}

var (
	// P90: https://www.itl.nist.gov/div898/handbook/prc/section2/prc262.htm
	control1    = []float64{95.1772, 95.1567, 95.1937, 95.1959, 95.1442, 95.0610, 95.1591, 95.1195, 95.1065, 95.0925, 95.1990, 95.1682}
	control1P90 = 95.1972
	control1Sum = 1141.7735
	control1Min = 95.0610
	control1Max = 95.1990

	p90Config  = metrics.Config{Percentiles: []float64{0.90}}
	p999Config = metrics.Config{Percentiles: []float64{0.999}}
)

func TestCounterZero(t *testing.T) {
	// Zero values in, zero values out
	c1 := metrics.NewCounter()
	gotSnap := c1.Snapshot(true) // reset
	expectSnap := metrics.Snapshot{}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}

	c2 := metrics.NewCounter()
	gotSnap = c2.Snapshot(false) // no reset
	expectSnap = metrics.Snapshot{}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestCounterAdd(t *testing.T) {
	// Typical case: +1 increments
	c1 := metrics.NewCounter()
	c1.Add(1)
	c1.Add(1)
	c1.Add(1)
	gotSnap := c1.Snapshot(true)
	expectSnap := metrics.Snapshot{
		N:   3,
		Sum: 3,
		// Other fields zero for counters
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}

	// >1 increments
	c2 := metrics.NewCounter()
	c2.Add(3)
	c2.Add(5)
	c2.Add(7)
	gotSnap = c2.Snapshot(true)
	expectSnap = metrics.Snapshot{
		N:   3,
		Sum: 15,
		// Other fields zero for counters
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestCounterIncDec(t *testing.T) {
	// Less typical case: increments and decrements
	c1 := metrics.NewCounter()
	c1.Add(1)
	c1.Add(1)
	c1.Add(-1)
	c1.Add(1)
	c1.Add(-1)
	gotSnap := c1.Snapshot(true)
	expectSnap := metrics.Snapshot{
		N:   5,
		Sum: 1,
		// Other fields zero for counters
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestCounterNegative(t *testing.T) {
	// A counter can be negative, but does it make sense?
	c1 := metrics.NewCounter()
	c1.Add(1)
	c1.Add(-1)
	c1.Add(-1)
	c1.Add(-1)
	gotSnap := c1.Snapshot(true)
	expectSnap := metrics.Snapshot{
		N:   4,
		Sum: -2,
		// Other fields zero for counters
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestCounterReset(t *testing.T) {
	// Prime the counter, reset, then verify it has zero values
	c1 := metrics.NewCounter()
	c1.Add(1)
	c1.Add(1)
	count := c1.Count()
	if count != 2 {
		t.Errorf("Count %d, expected 2", count)
	}
	gotSnap := c1.Snapshot(true) // reset
	expectSnap := metrics.Snapshot{
		N:   2,
		Sum: 2,
		// Other fields zero for counters
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
	count = c1.Count()
	if count != 0 {
		t.Errorf("Count %d, expected 0", count)
	}
	// Counter was reset, so should have zero values
	gotSnap = c1.Snapshot(true) // reset (again)
	expectSnap = metrics.Snapshot{}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}

	// Prime the counter, do not reset, and verify it has same values
	c2 := metrics.NewCounter()
	c2.Add(1)
	c2.Add(1)
	gotSnap = c2.Snapshot(false) // do not reset
	expectSnap = metrics.Snapshot{
		N:   2,
		Sum: 2,
		// Other fields zero for counters
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
	gotSnap = c2.Snapshot(false)
	// Expecting same snapshot
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

// --------------------------------------------------------------------------
// Gauge
// --------------------------------------------------------------------------

func TestGaugeZero(t *testing.T) {
	// Zero values in, zero values out
	g1 := metrics.NewGauge(p90Config)
	gotSnap := g1.Snapshot(true) // reset
	expectSnap := metrics.Snapshot{}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}

	g2 := metrics.NewGauge(p90Config)
	gotSnap = g2.Snapshot(false) // no reset
	expectSnap = metrics.Snapshot{}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestGaugeOneValue(t *testing.T) {
	// Can't interpolate with only 1 value, so algo should use the only val
	g1 := metrics.NewGauge(metrics.Config{Percentiles: []float64{0.999}})
	val := 1.201
	g1.Record(val)
	gotSnap := g1.Snapshot(true)
	expectSnap := metrics.Snapshot{
		N:   1,
		Sum: val,
		Min: val,
		Max: val,
		Percentile: map[float64]float64{
			0.999: val,
		},
		Last: val,
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestGaugeRecord(t *testing.T) {
	// Typical usage: record values, get snapshot and reset
	g1 := metrics.NewGauge(p90Config)
	for _, v := range control1 {
		g1.Record(v)
	}
	// Last value before reset should be last value recorded
	last := g1.Last()
	if last != control1[len(control1)-1] {
		t.Errorf("Last value %f, expected %f", last, control1[len(control1)-1])
	}
	gotSnap := g1.Snapshot(true) // reset
	expectSnap := metrics.Snapshot{
		N:   int64(len(control1)),
		Sum: control1Sum,
		Min: control1Min,
		Max: control1Max,
		Percentile: map[float64]float64{
			0.90: control1P90,
		},
		Last: control1[len(control1)-1],
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}

	// Gauge was reset, so should have zero values
	gotSnap = g1.Snapshot(true) // reset (again)
	expectSnap = metrics.Snapshot{}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
	last = g1.Last()
	if last != 0 {
		t.Errorf("Last value %f, expected 0", last)
	}
}

func TestGaugeReset(t *testing.T) {
	// Verify that after reset we have only new data

	// First, the control data
	g1 := metrics.NewGauge(p90Config)
	for _, v := range control1 {
		g1.Record(v)
	}
	gotSnap := g1.Snapshot(true) // reset
	expectSnap := metrics.Snapshot{
		N:   int64(len(control1)),
		Sum: control1Sum,
		Min: control1Min,
		Max: control1Max,
		Percentile: map[float64]float64{
			0.90: control1P90,
		},
		Last: control1[len(control1)-1],
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}

	// Now some fake, new data that's totally different
	newVals := []float64{1, 3, 5, 9, 8, 1, 7, 0, 0, 0, 1, 5, 6, 7, 8, 10, 9}
	for _, v := range newVals {
		g1.Record(v)
	}
	gotSnap2 := g1.Snapshot(true) // new snapshot
	expectSnap = metrics.Snapshot{
		N:   int64(len(newVals)),
		Sum: 80,
		Min: 0,
		Max: 10,
		Percentile: map[float64]float64{
			0.90: 9,
		},
		Last: 9,
	}
	if diff := deep.Equal(gotSnap2, expectSnap); diff != nil {
		t.Error(diff)
	}

}

func TestGaugeRecordNotReset(t *testing.T) {
	// Not reset, same values (until new ones recorded)
	g1 := metrics.NewGauge(p90Config)
	for _, v := range control1 {
		g1.Record(v)
	}
	gotSnap := g1.Snapshot(false) // do not reset
	expectSnap := metrics.Snapshot{
		N:   int64(len(control1)),
		Sum: control1Sum,
		Min: control1Min,
		Max: control1Max,
		Percentile: map[float64]float64{
			0.90: control1P90,
		},
		Last: control1[len(control1)-1],
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}

	// Same values because it wasn't reset and we didn't record new values
	gotSnap = g1.Snapshot(false) // reset (again)
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}

	// Record a new value which should change the snapshot
	val := control1Max + 1 // new max
	g1.Record(val)
	expectSnap.N += 1
	expectSnap.Sum += val
	expectSnap.Max = val
	expectSnap.Last = val
	expectSnap.Percentile[0.90] = 95.5323 // previous: 95.1972
	gotSnap = g1.Snapshot(false)          // reset (again)
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestGaugeNoPercentiles(t *testing.T) {
	// Percentiles aren't required, so if nil or empty list, the Percentile map is empty
	g1 := metrics.NewGauge(metrics.Config{})
	for _, v := range control1 {
		g1.Record(v)
	}
	gotSnap := g1.Snapshot(true)
	expectSnap := metrics.Snapshot{
		N:          int64(len(control1)),
		Sum:        control1Sum,
		Min:        control1Min,
		Max:        control1Max,
		Percentile: map[float64]float64{},
		Last:       control1[len(control1)-1],
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}

	g2 := metrics.NewGauge(metrics.Config{Percentiles: []float64{}}) // empty list
	for _, v := range control1 {
		g2.Record(v)
	}
	gotSnap = g2.Snapshot(true)
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestGaugeAdd(t *testing.T) {
	// See Gauge docs for why these values are correct
	g1 := metrics.NewGauge(metrics.Config{})
	g1.Add(3)  // 3 min
	g1.Add(2)  // 5 max
	g1.Add(-1) // 4
	g1.Add(1)  // 5 max again
	last := g1.Last()
	if last != 5 {
		t.Errorf("Last value %f, expected 5", last)
	}
	gotSnap := g1.Snapshot(true)
	expectSnap := metrics.Snapshot{
		N:          4,
		Sum:        17,
		Min:        3,
		Max:        5,
		Percentile: map[float64]float64{},
		Last:       5,
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

// --------------------------------------------------------------------------
// Histogram
// --------------------------------------------------------------------------

// Under the hood, histograms and gauges are almost identical. Main diff:
// gauges keep the last value. So these tests are less commented; see Gauge tests.

func TestHistogramZero(t *testing.T) {
	h1 := metrics.NewHistogram(p90Config)
	gotSnap := h1.Snapshot(true)
	expectSnap := metrics.Snapshot{}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}

	h2 := metrics.NewHistogram(p90Config)
	gotSnap = h2.Snapshot(false) // no reset
	expectSnap = metrics.Snapshot{}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestHistogramOneValue(t *testing.T) {
	// Can't interpolate with only 1 value, so algo should use the only val
	h1 := metrics.NewHistogram(metrics.Config{Percentiles: []float64{0.999}})
	val := 1.201
	h1.Record(val)
	gotSnap := h1.Snapshot(true)
	expectSnap := metrics.Snapshot{
		N:   1,
		Sum: val,
		Min: val,
		Max: val,
		Percentile: map[float64]float64{
			0.999: val,
		},
		Last: 0, // only Gauge
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestHistogramRecord(t *testing.T) {
	// Typical usage: record values, get snapshot and reset
	h1 := metrics.NewHistogram(p90Config)
	for _, v := range control1 {
		h1.Record(v)
	}
	gotSnap := h1.Snapshot(true) // reset
	expectSnap := metrics.Snapshot{
		N:   int64(len(control1)),
		Sum: control1Sum,
		Min: control1Min,
		Max: control1Max,
		Percentile: map[float64]float64{
			0.90: control1P90,
		},
		Last: 0, // only Gauge
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
	// Histogram was reset, so should have zero values
	gotSnap = h1.Snapshot(true) // reset (again)
	expectSnap = metrics.Snapshot{}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestHistogramLowPercentile(t *testing.T) {
	// These percentiles shouldn't be used in real apps, but code should
	// handle them anyway. It hits the case where pos < 1.0. They yield
	// the min value.
	h1 := metrics.NewHistogram(metrics.Config{Percentiles: []float64{0.01, 0.001, 0}})
	for _, v := range control1 {
		h1.Record(v)
	}
	gotSnap := h1.Snapshot(true) // reset
	expectSnap := metrics.Snapshot{
		N:   int64(len(control1)),
		Sum: control1Sum,
		Min: control1Min,
		Max: control1Max,
		Percentile: map[float64]float64{
			0.01:  control1Min, // 1%
			0.001: control1Min, // 0.1%
			0:     control1Min, // min
		},
		Last: 0, // only Gauge
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
	// Histogram was reset, so should have zero values
	gotSnap = h1.Snapshot(true) // reset (again)
	expectSnap = metrics.Snapshot{}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

// --------------------------------------------------------------------------
// Concurrency tests
// --------------------------------------------------------------------------

func TestConcurrentCount(t *testing.T) {
	// To confirm that this test causes a race, comment out the Lock/Unlock
	// in Counter.Add and go test -race will fail in this test
	c1 := metrics.NewCounter()
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewSource(time.Now().Unix()))
			for i := 0; i < 5; i++ {
				c1.Add(1)
				d := r.Intn(20)
				t.Logf("sleep %d\n", d)
				time.Sleep(time.Duration(d) * time.Microsecond)
			}
		}()
	}
	wg.Wait()
	gotSnap := c1.Snapshot(true)
	expectSnap := metrics.Snapshot{
		N:   10, // 2 * 5
		Sum: 10,
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestConcurrentGauge(t *testing.T) {
	// This produces 10 measurements (sorted): [0, 0, 1, 1, 2, 2, 3, 3, 4, 4]
	g1 := metrics.NewGauge(metrics.Config{Percentiles: []float64{0.8, 0.9}})
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewSource(time.Now().Unix()))
			for i := 0; i < 5; i++ {
				g1.Record(float64(i))
				d := r.Intn(20)
				t.Logf("sleep %d\n", d)
				time.Sleep(time.Duration(d) * time.Microsecond)
			}
		}()
	}
	wg.Wait()
	gotSnap := g1.Snapshot(true)
	expectSnap := metrics.Snapshot{
		N:   10, // 2 * 5
		Sum: 20,
		Min: 0,
		Max: 4,
		Percentile: map[float64]float64{
			0.80: 3.6,
			0.90: 4,
		},
		Last: 4,
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestConcurrentHistogram(t *testing.T) {
	// This produces 10 measurements (sorted): [0, 0, 1, 1, 2, 2, 3, 3, 4, 4]
	h1 := metrics.NewHistogram(p999Config)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewSource(time.Now().Unix()))
			for i := 0; i < 5; i++ {
				h1.Record(float64(i))
				d := r.Intn(20)
				t.Logf("sleep %d\n", d)
				time.Sleep(time.Duration(d) * time.Microsecond)
			}
		}()
	}
	wg.Wait()
	gotSnap := h1.Snapshot(true)
	expectSnap := metrics.Snapshot{
		N:   10, // 2 * 5
		Sum: 20,
		Min: 0,
		Max: 4,
		Percentile: map[float64]float64{
			0.999: 4,
		},
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

// --------------------------------------------------------------------------
// Data files with thousands of real-world values
// --------------------------------------------------------------------------

func valuesFromFile(file string, t *testing.T) []float64 {
	f, err := os.Open(file)
	if err != nil {
		t.Fatalf("%s: %s", file, err)
	}
	defer f.Close()
	vals := []float64{}
	r := bufio.NewReader(f)
	for {
		line, _, err := r.ReadLine()
		if err != nil {
			break
		}
		f, err := strconv.ParseFloat(string(line), 64)
		if err != nil {
			t.Fatalf("%s: %s", file, err)
		}
		vals = append(vals, f)
	}
	return vals
}

func TestDataFile_4ktrend1to7(t *testing.T) {
	// Greater than 2k values so nearest rank is used
	h1 := metrics.NewHistogram(p999Config)
	for _, v := range valuesFromFile("test/4k-trend-1-to-7", t) {
		h1.Record(v)
	}
	gotSnap := h1.Snapshot(true) // reset
	expectSnap := metrics.Snapshot{
		N:   4000,
		Sum: 8016.0053670,
		Min: 0.000566,
		Max: 6.989429,
		Percentile: map[float64]float64{
			0.999: 6.9546, // real: 6.967
		},
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestDataFile_1k(t *testing.T) {
	// Less than 2k values so R8 kicks in
	h1 := metrics.NewHistogram(p999Config)
	for _, v := range valuesFromFile("test/1k", t) {
		h1.Record(v)
	}
	gotSnap := h1.Snapshot(true) // reset
	expectSnap := metrics.Snapshot{
		N:   1000,
		Sum: 1.53073,
		Min: 0.000011,
		Max: 1.089862,
		Percentile: map[float64]float64{
			0.999: 0.78721666,
		},
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}

func TestDataFile_300(t *testing.T) {
	// At only 300 values, P999 = max
	h1 := metrics.NewHistogram(p999Config)
	for _, v := range valuesFromFile("test/300", t) {
		h1.Record(v)
	}
	gotSnap := h1.Snapshot(true) // reset
	expectSnap := metrics.Snapshot{
		N:   300,
		Sum: 0.260362,
		Min: 0.000011,
		Max: 0.182833,
		Percentile: map[float64]float64{
			0.999: 0.182833,
		},
	}
	if diff := deep.Equal(gotSnap, expectSnap); diff != nil {
		t.Error(diff)
	}
}
