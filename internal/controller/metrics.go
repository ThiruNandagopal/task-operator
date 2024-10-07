package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	taskPendingCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "task_operator_task_pending_count",
		Help: "Number of Task in Pending State",
	})

	taskRunningCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "task_operator_task_running_count",
		Help: "Number of Task in Running State",
	})

	taskCompletedCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "task_operator_task_completed_count",
		Help: "Number of Task in Completed State",
	})

	taskFailedCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "task_operator_task_failed_count",
		Help: "Number of Task in Failed State",
	})

	taskTotalCount = promauto.NewCounter(prometheus.CounterOpts{
		Name: "task_operator_task_total_count",
		Help: "Number of Task created",
	})

	taskFailureCount = promauto.NewCounter(prometheus.CounterOpts{
		Name: "task_operator_task_failure_count",
		Help: "Number of Task failed to create",
	})
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(
		taskPendingCount,
		taskRunningCount,
		taskCompletedCount,
		taskFailedCount,
		taskTotalCount,
		taskFailureCount,
	)
}
