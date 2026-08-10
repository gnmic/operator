package controller

import (
	"context"
	"maps"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// staleSweepInterval is how often a pod falls back to listing every Target to
// find status entries it owns but no longer reports.
//
// The per-poll path diffs against what the pod reported last time, which costs
// nothing and catches everything the operator itself observed. It cannot catch
// what happened while the operator was not running: an entry left pointing at
// this pod for a target that has since been removed from every pipeline is
// reported by nobody, so no diff will ever mention it. The sweep is the only
// thing that finds those, which is why it still exists — just not on every poll.
const staleSweepInterval = 5 * time.Minute

// clusterTargetStateEqual reports whether two per-cluster status entries carry
// the same information.
//
// LastUpdated is compared rather than ignored. gNMIc documents it as the
// timestamp of the last state *transition*, not the time the state was read, so
// it is stable while nothing changes. Were it a read timestamp, including it
// here would defeat the whole comparison — every poll would look like a change.
func clusterTargetStateEqual(a, b gnmicv1alpha1.ClusterTargetState) bool {
	return a.Pod == b.Pod &&
		a.State == b.State &&
		a.FailedReason == b.FailedReason &&
		a.ConnectionState == b.ConnectionState &&
		a.LastUpdated.Equal(&b.LastUpdated) &&
		maps.Equal(a.Subscriptions, b.Subscriptions)
}

// applyClusterState writes one cluster's entry into a Target's status, and does
// nothing at all when the stored entry already says the same thing.
//
// The unconditional write it replaces rewrote every target on every poll, which
// at a few thousand targets is a sustained write rate that throttles every other
// controller in the process behind it.
func (r *TargetStateReconciler) applyClusterState(
	ctx context.Context,
	targetNN types.NamespacedName,
	clusterName string,
	desired gnmicv1alpha1.ClusterTargetState,
	logger logr.Logger,
) {
	r.mutateStatus(ctx, targetNN, logger, func(target *gnmicv1alpha1.Target) bool {
		if current, ok := target.Status.ClusterStates[clusterName]; ok && clusterTargetStateEqual(current, desired) {
			return false
		}
		if target.Status.ClusterStates == nil {
			target.Status.ClusterStates = make(map[string]gnmicv1alpha1.ClusterTargetState)
		}
		target.Status.ClusterStates[clusterName] = desired
		return true
	})
}

// removeClusterState drops one cluster's entry from a Target's status.
func (r *TargetStateReconciler) removeClusterState(
	ctx context.Context,
	targetNN types.NamespacedName,
	clusterName string,
	logger logr.Logger,
) {
	r.mutateStatus(ctx, targetNN, logger, func(target *gnmicv1alpha1.Target) bool {
		if _, ok := target.Status.ClusterStates[clusterName]; !ok {
			return false
		}
		delete(target.Status.ClusterStates, clusterName)
		return true
	})
}

// mutateStatus reads a Target, lets mutate decide whether anything needs to
// change, and patches only if it does.
//
// The patch keeps its optimistic lock. Dropping the conflict retry was
// tempting — a merge patch does not need one — but the summary fields are
// derived from the *whole* ClusterStates map, so two clusters collecting the
// same target could each compute a summary from their own stale view and the
// later write would win with the wrong one. With writes now rare, a conflict is
// rare too, so the retry loop costs nothing in the common case and keeps the
// summary honest.
func (r *TargetStateReconciler) mutateStatus(
	ctx context.Context,
	targetNN types.NamespacedName,
	logger logr.Logger,
	mutate func(*gnmicv1alpha1.Target) bool,
) {
	for attempt := 0; attempt < maxConflictRetries; attempt++ {
		var target gnmicv1alpha1.Target
		if err := r.Get(ctx, targetNN, &target); err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to get target", "target", targetNN.String())
			}
			return
		}

		base := target.DeepCopy()
		if !mutate(&target) {
			return
		}
		computeStatusSummary(&target.Status)

		patch := client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})
		if err := r.Status().Patch(ctx, &target, patch); err != nil {
			if apierrors.IsConflict(err) {
				continue
			}
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to patch target status", "target", targetNN.String())
			}
			return
		}
		return
	}
	logger.Info("giving up after max conflict retries", "target", targetNN.String(), "retries", maxConflictRetries)
}

// podStateKey identifies one pod's view within a cluster.
func podStateKey(namespace, clusterName, podName string) string {
	return namespace + "/" + clusterName + "/" + podName
}

// swapReported replaces a pod's remembered target set and returns the names it
// reported last time but not now — the entries this pod owns and should release.
func (r *TargetStateReconciler) swapReported(key string, now map[string]struct{}) []string {
	r.reportedMu.Lock()
	defer r.reportedMu.Unlock()
	if r.reported == nil {
		r.reported = make(map[string]map[string]struct{})
	}
	previous := r.reported[key]
	r.reported[key] = now

	var dropped []string
	for name := range previous {
		if _, still := now[name]; !still {
			dropped = append(dropped, name)
		}
	}
	return dropped
}

// dueForSweep reports whether this pod should fall back to a full list, and
// records the attempt. The first poll of a stream always sweeps: the in-memory
// set is empty then, so it is exactly the moment the cheap path is blind.
func (r *TargetStateReconciler) dueForSweep(key string) bool {
	r.reportedMu.Lock()
	defer r.reportedMu.Unlock()
	if r.lastSweep == nil {
		r.lastSweep = make(map[string]time.Time)
	}
	last, seen := r.lastSweep[key]
	if seen && time.Since(last) < staleSweepInterval {
		return false
	}
	r.lastSweep[key] = time.Now()
	return true
}

// forgetPod drops a pod's remembered state, so a restarted stream sweeps again
// rather than trusting a set gathered before the gap.
func (r *TargetStateReconciler) forgetPod(key string) {
	r.reportedMu.Lock()
	defer r.reportedMu.Unlock()
	delete(r.reported, key)
	delete(r.lastSweep, key)
}
