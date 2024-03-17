package infodb

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	nodeIDlabel   = "node_id"
	onNodeIDLabel = "on_node_id"
)

var (
	fuzzerLabels = []string{nodeIDlabel, onNodeIDLabel}

	savedTestcaseCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "saved_testcases_total",
			Help: "Total count of saved testcase",
		},
		fuzzerLabels,
	)

	crashCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "crash_count_total",
			Help: "Total count of crashes found by all fuzzers",
		},
		fuzzerLabels,
	)

	evaluationTime = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "testcase_bulk_saving_duration_seconds",
			Help:    "Latency of testcase saving",
			Buckets: prometheus.DefBuckets,
		},
	)

	analyzeCreationTime = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "analyze_creation_duration_seconds",
			Help:    "Latency of creation analyze struct",
			Buckets: prometheus.DefBuckets,
		},
	)
)
