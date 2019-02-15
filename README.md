# go-metrics

[![Go Report Card](https://goreportcard.com/badge/github.com/daniel-nichter/go-metrics)](https://goreportcard.com/report/github.com/daniel-nichter/go-metrics) [![Build Status](https://travis-ci.org/daniel-nichter/go-metrics.svg?branch=master)](https://travis-ci.org/daniel-nichter/go-metrics) [![Coverage Status](https://coveralls.io/repos/github/daniel-nichter/go-metrics/badge.svg?branch=master)](https://coveralls.io/github/daniel-nichter/go-metrics?branch=master) [![GoDoc](https://godoc.org/github.com/daniel-nichter/go-metrics?status.svg)](https://godoc.org/github.com/daniel-nichter/go-metrics)

Package metrics provides base metric types: counter, gauge, and histogram.

This package differs from other Go metric packages in three significant ways:

1. Metrics: Only base metric types are provide (counter, gauge, histogram). There are no sinks, registries, or derivative metric types. These should be implement by other packages which import this package.

2. Sampling: Only ["Algorithm R" by Jeffrey Vitter](https://www.cs.umd.edu/~samir/498/vitter.pdf) is used to sample values for Gauge and Histogram. The reservoir size is fixed at 2,000. Testing with real-world values shows that smaller and larger sizes yield no benefit. **And the true maximum value is kept and reported**, which is not a feature of the original Algorithm R but critical for application performance monitoring.

3. Percentiles: Both nearest rank and linear interpolation are used calculate percentile values. If the sample is full (>= 2,000 values), nearest rank is used; else, "Definition 8"--better known as "R8"--is used ([Hyndman and Fan (1996)](https://www.amherst.edu/media/view/129116/original/Sample+Quantiles.pdf)). Testing with real-world values shows that this combination produces more accurate P999 (99.9th percentile) values, which is the gold standard for high-performance, low-latency applications.

This is not a full-feature metrics package with various sampling algorithms, data sinks, etc. It is  _not_ right for:

* Streaming metrics (never resetting sample)
* Trending or smoothing (1/5/15 min. moving avg.)
* Derivative/hybrid metrics (timers, sets, etc.)

Those requirements are better handled by specialized algorithms, higher-level code abstractions, and metrics system like Datadog, SignalFx, Prometheus, etc. For example, trending/smoothing should be computed from time series data rather than storing and reporting 1/5/15 minutes of data.

This package does one thing very well: base app metrics: _counters, gauges, and histograms_. It is right for:

* Latency/response time in micro and milliseconds (with spikes >1s)
* 99.9th percentile&mdash;the gold standard for high-performance, low-latency apps
* Building block for an open-source program to provide its own metrics

Doing only one things makes it very easy to understand and use. [Read the docs](https://godoc.org/github.com/daniel-nichter/go-metrics) to see how.
