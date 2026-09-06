package controller

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

var (
	targetSourceSyncDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gnmic_operator_targetsource_sync_duration_seconds",
		Help:    "Wall time of one TargetSource discovery run.",
		Buckets: prometheus.ExponentialBuckets(0.05, 2, 12),
	}, []string{"namespace", "name", "type"})
	targetSourceSyncTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "gnmic_operator_targetsource_sync_total",
		Help: "TargetSource discovery runs by result: success, failure or held (prune guard).",
	}, []string{"namespace", "name", "type", "result"})
	targetSourceLastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gnmic_operator_targetsource_last_success_timestamp_seconds",
		Help: "Unix time of the last successful TargetSource run. Alert when it stops moving.",
	}, []string{"namespace", "name"})
	targetSourceTargets = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gnmic_operator_targetsource_targets",
		Help: "Targets by state after the last run: managed, invalid or conflicted.",
	}, []string{"namespace", "name", "state"})
)

func init() {
	metrics.Registry.MustRegister(targetSourceSyncDuration, targetSourceSyncTotal, targetSourceLastSuccess, targetSourceTargets)
}

func recordSync(ts *gnmicv1alpha1.TargetSource, result string, started time.Time) {
	typ := string(ts.Spec.Source.Type)
	targetSourceSyncTotal.WithLabelValues(ts.Namespace, ts.Name, typ, result).Inc()
	targetSourceSyncDuration.WithLabelValues(ts.Namespace, ts.Name, typ).Observe(time.Since(started).Seconds())
	if result == "success" {
		targetSourceLastSuccess.WithLabelValues(ts.Namespace, ts.Name).Set(float64(time.Now().Unix()))
	}
}

func recordTargets(ts *gnmicv1alpha1.TargetSource, managed, invalid, conflicted int) {
	targetSourceTargets.WithLabelValues(ts.Namespace, ts.Name, "managed").Set(float64(managed))
	targetSourceTargets.WithLabelValues(ts.Namespace, ts.Name, "invalid").Set(float64(invalid))
	targetSourceTargets.WithLabelValues(ts.Namespace, ts.Name, "conflicted").Set(float64(conflicted))
}
