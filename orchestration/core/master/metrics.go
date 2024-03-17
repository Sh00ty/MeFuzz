package master

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	nodeIDlabel   = "node_id"
	onNodeIDLabel = "on_node_id"
)

var (
	workerLabels = []string{nodeIDlabel, onNodeIDLabel}

	evaludatedTestcases = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "testcase_evaluated_total",
			Help: "Number of testcase evaluated by evaler",
		},
		workerLabels,
	)

	evaluationTime = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "testcase_evaludation_duration_seconds",
			Help:    "Latency of testcase evaluation",
			Buckets: prometheus.DefBuckets,
		},
		workerLabels,
	)

	testcaseInChan = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "testcase_count_in_queue_current",
		Help: "Count of testcase in testcase channel now",
	})

	generatedTestcaseCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "testcase_generated_total",
			Help: "Number of testcase evaluated by evaler",
		},
		workerLabels,
	)

	masterEventCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "master_event_total",
			Help: "Number of master events with its id",
		},
		[]string{"event", "element_id"},
	)

	totalFuzzerCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "fuzzer_count_current",
		Help: "Number of active fuzzer right now",
	})

	totalEvalerCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "evaler_count_current",
		Help: "Number of active evaler right now",
	})
)
