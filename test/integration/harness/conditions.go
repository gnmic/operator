//go:build integration

package harness

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Condition types set by the operator. Mirrors the constants in
// internal/controller/cluster_controller.go.
const (
	CondReady             = "Ready"
	CondCertificatesReady = "CertificatesReady"
	CondConfigApplied     = "ConfigApplied"
	CondCapacityExhausted = "CapacityExhausted"
	CondResourcesResolved = "ResourcesResolved"
)

// WaitClusterCondition waits for a condition on a Cluster.
func WaitClusterCondition(t *testing.T, k *K8s, name, condType string, want metav1.ConditionStatus, timeout time.Duration) {
	t.Helper()
	Wait(t, timeout, fmt.Sprintf("Cluster %s condition %s=%s", name, condType, want), func() (bool, string) {
		c, err := k.clusterQuiet(name)
		if err != nil {
			return false, err.Error()
		}
		cond := meta.FindStatusCondition(c.Status.Conditions, condType)
		if cond == nil {
			return false, "condition not present"
		}
		return cond.Status == want, fmt.Sprintf("status=%s reason=%s message=%s", cond.Status, cond.Reason, cond.Message)
	})
}

// WaitClusterReady waits for a Cluster to report Ready.
func WaitClusterReady(t *testing.T, k *K8s, name string) {
	t.Helper()
	WaitClusterCondition(t, k, name, CondReady, metav1.ConditionTrue, Long)
}

// WaitConfigApplied waits for a Cluster to report that it pushed its rendered
// config to the collector pods.
func WaitConfigApplied(t *testing.T, k *K8s, name string) {
	t.Helper()
	WaitClusterCondition(t, k, name, CondConfigApplied, metav1.ConditionTrue, Medium)
}

// ClusterCounts declares the status counters a test cares about. Nil fields are
// not checked, so a test stays silent about counters it is not asserting.
type ClusterCounts struct {
	ReadyReplicas      *int32
	PipelinesCount     *int32
	TargetsCount       *int32
	SubscriptionsCount *int32
	InputsCount        *int32
	OutputsCount       *int32
	UnassignedTargets  *int32
}

// I32 is a convenience for building ClusterCounts.
func I32(v int32) *int32 { return &v }

// WaitClusterCounts waits until every declared counter matches.
func WaitClusterCounts(t *testing.T, k *K8s, name string, want ClusterCounts) {
	t.Helper()
	Wait(t, Medium, fmt.Sprintf("Cluster %s status counters", name), func() (bool, string) {
		c, err := k.clusterQuiet(name)
		if err != nil {
			return false, err.Error()
		}
		got := ClusterCounts{
			ReadyReplicas:      &c.Status.ReadyReplicas,
			PipelinesCount:     &c.Status.PipelinesCount,
			TargetsCount:       &c.Status.TargetsCount,
			SubscriptionsCount: &c.Status.SubscriptionsCount,
			InputsCount:        &c.Status.InputsCount,
			OutputsCount:       &c.Status.OutputsCount,
			UnassignedTargets:  &c.Status.UnassignedTargets,
		}
		type pair struct {
			name       string
			want, have *int32
		}
		for _, p := range []pair{
			{"readyReplicas", want.ReadyReplicas, got.ReadyReplicas},
			{"pipelinesCount", want.PipelinesCount, got.PipelinesCount},
			{"targetsCount", want.TargetsCount, got.TargetsCount},
			{"subscriptionsCount", want.SubscriptionsCount, got.SubscriptionsCount},
			{"inputsCount", want.InputsCount, got.InputsCount},
			{"outputsCount", want.OutputsCount, got.OutputsCount},
			{"unassignedTargets", want.UnassignedTargets, got.UnassignedTargets},
		} {
			if p.want != nil && *p.want != *p.have {
				return false, fmt.Sprintf("%s: want %d, got %d", p.name, *p.want, *p.have)
			}
		}
		return true, ""
	})
}

// WaitTargetState waits for a Target's aggregate connection state.
func WaitTargetState(t *testing.T, k *K8s, name, want string) {
	t.Helper()
	Wait(t, Medium, fmt.Sprintf("Target %s connectionState=%s", name, want), func() (bool, string) {
		tgt, err := k.targetQuiet(name)
		if err != nil {
			return false, err.Error()
		}
		return tgt.Status.State == want, fmt.Sprintf("state=%q clusters=%d", tgt.Status.State, tgt.Status.Clusters)
	})
}

// TargetPod reports which collector pod currently owns a target for a cluster.
func TargetPod(t *testing.T, k *K8s, target, cluster string) string {
	t.Helper()
	tgt, err := k.targetQuiet(target)
	if err != nil {
		return ""
	}
	return tgt.Status.ClusterStates[cluster].Pod
}

// PipelineCounts declares the Pipeline status counters a test cares about.
type PipelineCounts struct {
	TargetsCount       *int32
	SubscriptionsCount *int32
	OutputsCount       *int32
	InputsCount        *int32
}

// WaitPipelineCounts waits until every declared Pipeline counter matches.
func WaitPipelineCounts(t *testing.T, k *K8s, name string, want PipelineCounts) {
	t.Helper()
	Wait(t, Medium, fmt.Sprintf("Pipeline %s status counters", name), func() (bool, string) {
		p, err := k.pipelineQuiet(name)
		if err != nil {
			return false, err.Error()
		}
		type pair struct {
			name       string
			want, have *int32
		}
		for _, x := range []pair{
			{"targetsCount", want.TargetsCount, &p.Status.TargetsCount},
			{"subscriptionsCount", want.SubscriptionsCount, &p.Status.SubscriptionsCount},
			{"outputsCount", want.OutputsCount, &p.Status.OutputsCount},
			{"inputsCount", want.InputsCount, &p.Status.InputsCount},
		} {
			if x.want != nil && *x.want != *x.have {
				return false, fmt.Sprintf("%s: want %d, got %d", x.name, *x.want, *x.have)
			}
		}
		return true, ""
	})
}

// WaitPipelineCondition waits for a condition on a Pipeline.
func WaitPipelineCondition(t *testing.T, k *K8s, name, condType string, want metav1.ConditionStatus) {
	t.Helper()
	Wait(t, Medium, fmt.Sprintf("Pipeline %s condition %s=%s", name, condType, want), func() (bool, string) {
		p, err := k.pipelineQuiet(name)
		if err != nil {
			return false, err.Error()
		}
		cond := meta.FindStatusCondition(p.Status.Conditions, condType)
		if cond == nil {
			return false, "condition not present"
		}
		return cond.Status == want, fmt.Sprintf("status=%s reason=%s", cond.Status, cond.Reason)
	})
}

// WaitPipelineStatusString waits for Pipeline.status.status to equal want.
func WaitPipelineStatusString(t *testing.T, k *K8s, name, want string) {
	t.Helper()
	Wait(t, Medium, fmt.Sprintf("Pipeline %s status=%s", name, want), func() (bool, string) {
		p, err := k.pipelineQuiet(name)
		if err != nil {
			return false, err.Error()
		}
		return p.Status.Status == want, "status=" + p.Status.Status
	})
}
