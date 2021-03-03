package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	PrometheusNamespace = "bridgestrap"
)

type Metrics struct {
	CacheSize      prometheus.Gauge
	PendingReqs    prometheus.Gauge
	PendingEvents  prometheus.Gauge
	FracFunctional prometheus.Gauge
	TorTestTime    prometheus.Histogram
	Events         *prometheus.CounterVec
	Cache          *prometheus.CounterVec
	Requests       *prometheus.CounterVec
	BridgeStatus   *prometheus.CounterVec
}

var metrics *Metrics

// InitMetrics initialises our Prometheus metrics.
func InitMetrics() {

	metrics = &Metrics{}

	metrics.PendingReqs = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: PrometheusNamespace,
		Name:      "pending_requests",
		Help:      "The number of pending requests",
	})

	metrics.PendingEvents = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: PrometheusNamespace,
		Name:      "pending_events",
		Help:      "The number of pending Tor controller events",
	})

	metrics.FracFunctional = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: PrometheusNamespace,
		Name:      "fraction_functional",
		Help:      "The fraction of functional bridges currently in the cache",
	})

	metrics.CacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: PrometheusNamespace,
		Name:      "cache_size",
		Help:      "The number of cached elements",
	})

	metrics.Events = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: PrometheusNamespace,
			Name:      "tor_events_total",
			Help:      "The number of Tor events",
		},
		[]string{"type", "status"},
	)

	metrics.Cache = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: PrometheusNamespace,
			Name:      "cache_total",
			Help:      "The number of cache hits and misses",
		},
		[]string{"type"},
	)

	metrics.Requests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: PrometheusNamespace,
			Name:      "requests_total",
			Help:      "The type and status of requests",
		},
		[]string{"type", "status"},
	)

	metrics.BridgeStatus = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: PrometheusNamespace,
			Name:      "bridge_status_total",
			Help:      "The number of functional and dysfunctional bridges",
		},
		[]string{"status"},
	)

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
