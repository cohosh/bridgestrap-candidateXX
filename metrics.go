package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	PrometheusNamespace = "bridgestrap"
)

type Metrics struct {
	OrconnLaunched          prometheus.Counter
	CacheHits               prometheus.Counter
	CacheMisses             prometheus.Counter
	CacheSize               prometheus.Gauge
	PendingReqs             prometheus.Gauge
	FracFunctional          prometheus.Gauge
	ApiNumRequests          prometheus.Counter
	ApiNumValidRequests     prometheus.Counter
	WebNumRequests          prometheus.Counter
	WebNumValidRequests     prometheus.Counter
	NumFunctionalBridges    prometheus.Counter
	NumDysfunctionalBridges prometheus.Counter
	TorTestTime             prometheus.Histogram
}

var metrics *Metrics

// InitMetrics initialises our Prometheus metrics.
func InitMetrics() {

	metrics = &Metrics{}

	metrics.OrconnLaunched = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: PrometheusNamespace,
		Name:      "tor_events_orconn_launched",
		Help:      "The number of ORCONN launch events",
	})

	metrics.PendingReqs = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: PrometheusNamespace,
		Name:      "pending_requests",
		Help:      "The number of pending requests",
	})

	metrics.FracFunctional = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: PrometheusNamespace,
		Name:      "fraction_functional",
		Help:      "The fraction of functional bridges currently in the cache",
	})

	metrics.CacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: PrometheusNamespace,
		Name:      "cache_hits",
		Help:      "The number of requests that hit the cache",
	})

	metrics.CacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: PrometheusNamespace,
		Name:      "cache_misses",
		Help:      "The number of requests that missed the cache",
	})

	metrics.CacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: PrometheusNamespace,
		Name:      "cache_size",
		Help:      "The number of cached elements",
	})

	metrics.ApiNumRequests = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: PrometheusNamespace,
		Name:      "api_num_requests",
		Help:      "The number of API requests",
	})

	metrics.ApiNumValidRequests = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: PrometheusNamespace,
		Name:      "api_num_validrequests",
		Help:      "The number of valid API requests",
	})

	metrics.WebNumRequests = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: PrometheusNamespace,
		Name:      "web_num_requests",
		Help:      "The number of Web requests",
	})

	metrics.WebNumValidRequests = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: PrometheusNamespace,
		Name:      "web_num_valid_requests",
		Help:      "The number of valid Web requests",
	})

	metrics.NumFunctionalBridges = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: PrometheusNamespace,
		Name:      "num_functional_bridges",
		Help:      "The number of functional bridges",
	})

	metrics.NumDysfunctionalBridges = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: PrometheusNamespace,
		Name:      "num_dysfunctional_bridges",
		Help:      "The number of dysfunctional bridges",
	})

	buckets := []float64{}
	TorTestTimeout.Seconds()
	for i := 0.5; i < TorTestTimeout.Seconds(); i *= 2 {
		buckets = append(buckets, i)
	}
	buckets = append(buckets, TorTestTimeout.Seconds()+1)

	metrics.TorTestTime = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: PrometheusNamespace,
		Name:      "tor_test_time",
		Help:      "The time it took to finish bridge tests",
		Buckets:   buckets,
	})
}
