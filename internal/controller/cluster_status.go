/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// clusterStatusEqual compares two ClusterStatus structs for equality
func clusterStatusEqual(a, b gnmicv1alpha1.ClusterStatus) bool {
	if a.ReadyReplicas != b.ReadyReplicas ||
		a.PipelinesCount != b.PipelinesCount ||
		a.TargetsCount != b.TargetsCount ||
		a.UnassignedTargets != b.UnassignedTargets ||
		a.SubscriptionsCount != b.SubscriptionsCount ||
		a.InputsCount != b.InputsCount ||
		a.OutputsCount != b.OutputsCount {
		return false
	}
	if len(a.Conditions) != len(b.Conditions) {
		return false
	}
	for i := range a.Conditions {
		if a.Conditions[i].Type != b.Conditions[i].Type ||
			a.Conditions[i].Status != b.Conditions[i].Status ||
			a.Conditions[i].Reason != b.Conditions[i].Reason ||
			a.Conditions[i].Message != b.Conditions[i].Message {
			return false
		}
	}
	return true
}

const (
	// statusUpdateAttempts bounds the retries on a conflicting status write.
	statusUpdateAttempts = 5
	// applyFailureRequeue is how soon to retry after the collectors rejected a plan.
	applyFailureRequeue = 10 * time.Second
	// transientReadRequeue is how soon to re-run after a plan shape that points at a
	// stale informer read rather than at user intent.
	transientReadRequeue = 250 * time.Millisecond
)

// buildClusterStatus derives the Cluster status for this pass: resource counters from
// the pipelines that made it into the plan, and the Ready, CertificatesReady,
// ConfigApplied and CapacityExhausted conditions from the StatefulSet and the apply
// outcome. Every condition is stamped with now; writeClusterStatus carries the prior
// LastTransitionTime over for conditions whose status did not change, against the live
// object. It reads nothing from the API and is safe to unit test directly.
func buildClusterStatus(cluster *gnmicv1alpha1.Cluster, statefulSet *appsv1.StatefulSet, pipelineCount int, data map[string]*gnmic.PipelineData, outcome applyOutcome) gnmicv1alpha1.ClusterStatus {
	targets, subscriptions, inputs, outputs := uniqueResourceCounts(data)
	status := gnmicv1alpha1.ClusterStatus{
		ReadyReplicas:      statefulSet.Status.ReadyReplicas,
		Selector:           metav1.FormatLabelSelector(statefulSet.Spec.Selector),
		PipelinesCount:     int32(pipelineCount),
		TargetsCount:       targets,
		UnassignedTargets:  outcome.unassignedTargets,
		SubscriptionsCount: subscriptions,
		InputsCount:        inputs,
		OutputsCount:       outputs,
	}

	now := metav1.Now()
	status.Conditions = append(status.Conditions, readyCondition(cluster, statefulSet, outcome, now))
	if tls := apiTLS(cluster); tls != nil && tls.IssuerRef != "" {
		status.Conditions = append(status.Conditions, metav1.Condition{
			Type:               ConditionTypeCertificatesReady,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: cluster.Generation,
			LastTransitionTime: now,
			Reason:             "CertificatesIssued",
			Message:            "TLS certificates are ready",
		})
	}
	status.Conditions = append(status.Conditions, configAppliedCondition(cluster, outcome, now))
	if cond, ok := capacityCondition(cluster, outcome, now); ok {
		status.Conditions = append(status.Conditions, cond)
	}

	return status
}

// uniqueResourceCounts counts the distinct targets, subscriptions, inputs and outputs
// across the pipelines in the plan. A resource bound to several pipelines counts once.
func uniqueResourceCounts(data map[string]*gnmic.PipelineData) (targets, subscriptions, inputs, outputs int32) {
	uniqueTargets := make(map[string]struct{})
	uniqueSubscriptions := make(map[string]struct{})
	uniqueInputs := make(map[string]struct{})
	uniqueOutputs := make(map[string]struct{})
	for _, pipelineData := range data {
		for k := range pipelineData.Targets {
			uniqueTargets[k] = struct{}{}
		}
		for k := range pipelineData.Subscriptions {
			uniqueSubscriptions[k] = struct{}{}
		}
		for k := range pipelineData.Inputs {
			uniqueInputs[k] = struct{}{}
		}
		for k := range pipelineData.Outputs {
			uniqueOutputs[k] = struct{}{}
		}
	}
	return int32(len(uniqueTargets)), int32(len(uniqueSubscriptions)), int32(len(uniqueInputs)), int32(len(uniqueOutputs))
}

// readyCondition is true once the pods are up and hold the current configuration.
func readyCondition(cluster *gnmicv1alpha1.Cluster, statefulSet *appsv1.StatefulSet, outcome applyOutcome, now metav1.Time) metav1.Condition {
	cond := metav1.Condition{
		Type:               ConditionTypeReady,
		ObservedGeneration: cluster.Generation,
		LastTransitionTime: now,
	}
	desired := ptr.Deref(cluster.Spec.Replicas, 0)
	switch {
	case statefulSet.Status.ReadyReplicas >= desired && outcome.applied:
		cond.Status = metav1.ConditionTrue
		cond.Reason = "ClusterReady"
		cond.Message = fmt.Sprintf("All %d replicas are ready and configured", statefulSet.Status.ReadyReplicas)
	case statefulSet.Status.ReadyReplicas > 0 && outcome.applied:
		cond.Status = metav1.ConditionTrue
		cond.Reason = "ClusterPartiallyReady"
		cond.Message = fmt.Sprintf("%d of %d replicas are ready and configured", statefulSet.Status.ReadyReplicas, desired)
	default:
		cond.Status = metav1.ConditionFalse
		cond.Reason = "ClusterNotReady"
		if statefulSet.Status.ReadyReplicas == 0 {
			cond.Message = "Waiting for pods to be ready"
		} else {
			cond.Message = "Configuration not yet applied"
		}
	}
	return cond
}

// configAppliedCondition distinguishes a plan that reached the pods, one that was
// deliberately withheld over unresolved references, and one that failed.
func configAppliedCondition(cluster *gnmicv1alpha1.Cluster, outcome applyOutcome, now metav1.Time) metav1.Condition {
	cond := metav1.Condition{
		Type:               ConditionTypeConfigApplied,
		ObservedGeneration: cluster.Generation,
		LastTransitionTime: now,
	}
	switch {
	case outcome.applied:
		cond.Status = metav1.ConditionTrue
		cond.Reason = "ConfigurationApplied"
		cond.Message = fmt.Sprintf("Configuration applied to %d pods", outcome.numPods)
	case outcome.suppressed:
		// Distinct from a failed apply: nothing was sent, and what the pods are
		// running is the last configuration that did resolve.
		cond.Status = metav1.ConditionFalse
		cond.Reason = ReasonUnresolvedReferences
		cond.Message = fmt.Sprintf(
			"%d pipeline(s) have unresolved references; the collectors keep their current configuration", outcome.skippedPipelines)
	default:
		cond.Status = metav1.ConditionFalse
		cond.Reason = "ConfigurationFailed"
		if outcome.err != nil {
			cond.Message = fmt.Sprintf("Failed to apply configuration: %v", outcome.err)
		} else {
			cond.Message = "Waiting for pods to be ready"
		}
	}
	return cond
}

// capacityCondition reports targets that found no pod with room. It is only set once
// a plan has been applied, or when targets were actually left unassigned.
func capacityCondition(cluster *gnmicv1alpha1.Cluster, outcome applyOutcome, now metav1.Time) (metav1.Condition, bool) {
	switch {
	case outcome.unassignedTargets > 0:
		return metav1.Condition{
			Type:               ConditionTypeCapacityExhausted,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: cluster.Generation,
			LastTransitionTime: now,
			Reason:             "InsufficientCapacity",
			Message:            fmt.Sprintf("%d target(s) could not be assigned, all pods at capacity", outcome.unassignedTargets),
		}, true
	case outcome.applied:
		return metav1.Condition{
			Type:               ConditionTypeCapacityExhausted,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: cluster.Generation,
			LastTransitionTime: now,
			Reason:             "SufficientCapacity",
			Message:            "All targets assigned",
		}, true
	}
	return metav1.Condition{}, false
}

// preserveTransitionTimes keeps LastTransitionTime from prior for every condition in
// conds whose status has not changed, so the timestamp marks the last real transition
// rather than the last reconcile. prior must be the live conditions: merging against a
// copy read at the start of the reconcile can miss a transition a concurrent pass
// already recorded, and stamp a fresh time over it.
func preserveTransitionTimes(prior, conds []metav1.Condition) {
	for i := range conds {
		for _, old := range prior {
			if old.Type == conds[i].Type && old.Status == conds[i].Status {
				conds[i].LastTransitionTime = old.LastTransitionTime
				break
			}
		}
	}
}

// writeClusterStatus persists status when it differs from what is live. The object is
// re-fetched into cluster first: a concurrent reconcile may have already written a
// newer status, and comparing against the start-of-reconcile copy can skip a needed
// update (e.g. a stale empty reconcile sees pipelinesCount=0 in-memory while the live
// status is still 1). Conflicts on the write itself are retried a bounded number of
// times.
func (r *ClusterReconciler) writeClusterStatus(ctx context.Context, cluster *gnmicv1alpha1.Cluster, status gnmicv1alpha1.ClusterStatus) error {
	logger := log.FromContext(ctx)
	clusterNN := types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}
	if err := r.Get(ctx, clusterNN, cluster); err != nil {
		return err
	}
	preserveTransitionTimes(cluster.Status.Conditions, status.Conditions)
	if clusterStatusEqual(cluster.Status, status) {
		return nil
	}
	var statusErr error
	for attempt := 0; attempt < statusUpdateAttempts; attempt++ {
		if attempt > 0 {
			if err := r.Get(ctx, clusterNN, cluster); err != nil {
				statusErr = err
				break
			}
			preserveTransitionTimes(cluster.Status.Conditions, status.Conditions)
		}
		cluster.Status = status
		if err := r.Status().Update(ctx, cluster); err != nil {
			if apierrors.IsConflict(err) {
				statusErr = err
				continue
			}
			statusErr = err
			break
		}
		statusErr = nil
		break
	}
	if statusErr != nil {
		logger.Error(statusErr, "failed to update cluster status")
		return statusErr
	}
	return nil
}

// requeueFor picks the interval for the next pass from how this one ended.
func requeueFor(outcome applyOutcome, pipelineCount int) ctrl.Result {
	if outcome.err != nil {
		return ctrl.Result{RequeueAfter: applyFailureRequeue}
	}
	// An empty apply from a briefly stale cache can race a Pipeline create.
	// Requeue once so the next pass sees the live membership and restores
	// config if needed; non-empty applies are left alone.
	if outcome.applied && pipelineCount == 0 {
		return ctrl.Result{RequeueAfter: time.Second}
	}
	return ctrl.Result{RequeueAfter: reconcileBackstopInterval}
}
