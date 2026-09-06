package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/discovery"
	"github.com/gnmic/operator/internal/discovery/provider"
)

// Condition types and reasons for TargetSource status.
const (
	TargetSourceConditionReady       = "Ready"
	TargetSourceConditionReconciling = "Reconciling"
	TargetSourceConditionStalled     = "Stalled"
	TargetSourceConditionConflicted  = "Conflicted"

	ReasonSucceeded        = "Succeeded"
	ReasonFetchFailed      = "FetchFailed"
	ReasonApplyFailed      = "ApplyFailed"
	ReasonPartialFailure   = "PartialFailure"
	ReasonCapacityExceeded = "CapacityExceeded"
	ReasonSuspended        = "Suspended"
	ReasonRetryScheduled   = "RetryScheduled"
	ReasonConverged        = "Converged"
	ReasonNameConflict     = "NameConflict"
)

// run is the bookkeeping for one reconcile pass. It accumulates counters and
// the outcome, and writes status exactly once in finish.
type run struct {
	r    *TargetSourceReconciler
	ts   *gnmicv1alpha1.TargetSource
	base *gnmicv1alpha1.TargetSource
	now  time.Time

	failed    bool // the run did not complete: fetch or spec failure
	isStalled bool
	result    *provider.Result
	build     *discovery.BuildResult

	managed   int
	created   int
	updated   int
	prunedN   int
	conflicts []string
	applyErrs []string
	prune     discovery.PruneDecision
	capacity  int
}

func (r *TargetSourceReconciler) newRun(ts *gnmicv1alpha1.TargetSource) *run {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	return &run{r: r, ts: ts, base: ts.DeepCopy(), now: now()}
}

func (x *run) interval() time.Duration {
	if x.ts.Spec.Interval != nil && x.ts.Spec.Interval.Duration > 0 {
		return x.ts.Spec.Interval.Duration
	}
	return defaultTargetSourceInterval
}

// timeout bounds one fetch. It never exceeds the interval: a run that took
// longer than the gap to the next one would only queue behind itself.
func (x *run) timeout() time.Duration {
	t := defaultTargetSourceTimeout
	if x.ts.Spec.Timeout != nil && x.ts.Spec.Timeout.Duration > 0 {
		t = x.ts.Spec.Timeout.Duration
	}
	if interval := x.interval(); t > interval {
		return interval
	}
	return t
}

func (x *run) suspended() {
	x.setCondition(TargetSourceConditionReady, metav1.ConditionFalse, ReasonSuspended, "discovery is suspended; existing Targets are left alone")
	x.setCondition(TargetSourceConditionReconciling, metav1.ConditionFalse, ReasonSuspended, "suspended")
	apimeta.RemoveStatusCondition(&x.ts.Status.Conditions, TargetSourceConditionStalled)
}

// stalled records a failure a retry cannot fix.
func (x *run) stalled(reason string, err error) {
	x.failed, x.isStalled = true, true
	x.ts.Status.ConsecutiveFailures++
	x.ts.Status.LastError = err.Error()
	x.setCondition(TargetSourceConditionReady, metav1.ConditionFalse, reason, err.Error())
	x.setCondition(TargetSourceConditionReconciling, metav1.ConditionTrue, ReasonRetryScheduled, "waiting for the spec or a referenced object to change")
	x.setCondition(TargetSourceConditionStalled, metav1.ConditionTrue, reason, err.Error())
	x.r.event(x.ts, corev1.EventTypeWarning, reason, err.Error())
	recordSync(x.ts, "failure", x.now)
}

// fetchFailed records a run failure the next attempt may well recover from.
func (x *run) fetchFailed(err error) {
	x.failed = true
	x.ts.Status.ConsecutiveFailures++
	x.ts.Status.LastError = err.Error()
	x.setCondition(TargetSourceConditionReady, metav1.ConditionFalse, ReasonFetchFailed, err.Error())
	x.setCondition(TargetSourceConditionReconciling, metav1.ConditionTrue, ReasonRetryScheduled, "retry scheduled with backoff")
	apimeta.RemoveStatusCondition(&x.ts.Status.Conditions, TargetSourceConditionStalled)
	if x.ts.Status.ConsecutiveFailures == 1 {
		x.r.event(x.ts, corev1.EventTypeWarning, ReasonFetchFailed, err.Error())
	}
	recordSync(x.ts, "failure", x.now)
}

func (x *run) discovered(result provider.Result) { x.result = &result }
func (x *run) built(b discovery.BuildResult)     { x.build = &b }
func (x *run) managedBefore(managed []gnmicv1alpha1.Target) {
	x.managed = len(managed)
}
func (x *run) capacityExceeded(n int)                  { x.capacity = n }
func (x *run) conflict(name string)                    { x.conflicts = append(x.conflicts, name) }
func (x *run) pruneDecision(d discovery.PruneDecision) { x.prune = d }
func (x *run) pruned()                                 { x.prunedN++ }
func (x *run) applyFailed(name string, err error) {
	x.applyErrs = append(x.applyErrs, fmt.Sprintf("%s: %v", name, err))
}
func (x *run) applied(name string, created bool) {
	if created {
		x.created++
	} else {
		x.updated++
	}
}

func (x *run) normalRequeue() ctrl.Result {
	return ctrl.Result{RequeueAfter: discovery.Jitter(x.interval(), x.r.Random)}
}

func (x *run) retryRequeue() ctrl.Result {
	return ctrl.Result{RequeueAfter: discovery.Jitter(discovery.RetryAfter(x.interval(), x.ts.Status.ConsecutiveFailures), x.r.Random)}
}

// stalledRequeue waits at the backoff cap: the watches on referenced Secrets
// and ConfigMaps, and the spec itself, are the fast path back.
func (x *run) stalledRequeue() ctrl.Result {
	return ctrl.Result{RequeueAfter: discovery.Jitter(discovery.RetryAfter(x.interval(), 1<<20), x.r.Random)}
}

// completeRequeue picks the interval after a run that got as far as applying.
// Apply failures back off like fetch failures; a held prune keeps the normal
// interval, since it clears as soon as the source recovers.
func (x *run) completeRequeue() ctrl.Result {
	if len(x.applyErrs) > 0 {
		return x.retryRequeue()
	}
	return x.normalRequeue()
}

// finish derives the status for the pass and writes it in one patch.
func (x *run) finish(ctx context.Context, res ctrl.Result) (ctrl.Result, error) {
	st := &x.ts.Status
	st.ObservedGeneration = x.ts.Generation
	st.LastSyncTime = ptrTime(x.now)
	if res.RequeueAfter > 0 {
		st.NextSyncTime = ptrTime(x.now.Add(res.RequeueAfter))
	} else {
		st.NextSyncTime = nil
	}

	if x.result != nil {
		x.finishComplete()
	}

	if err := x.r.Status().Patch(ctx, x.ts, client.MergeFrom(x.base)); err != nil {
		log.FromContext(ctx).Error(err, "failed to write TargetSource status")
		return ctrl.Result{}, err
	}
	return res, nil
}

// finishComplete fills counters and conditions for a run that fetched
// successfully, whether or not everything after that went well.
func (x *run) finishComplete() {
	st := &x.ts.Status
	st.Discovered = int32(len(x.result.Devices))
	invalid := append([]provider.Failure{}, x.result.Invalid...)
	if x.build != nil {
		invalid = append(invalid, x.build.Invalid...)
		st.Sanitized = int32(x.build.Sanitized)
	}
	st.Invalid = int32(len(invalid))
	st.FailedDevices = nil
	for i, f := range invalid {
		if i >= discovery.MaxFailedDevices {
			break
		}
		st.FailedDevices = append(st.FailedDevices, gnmicv1alpha1.FailedDevice{Name: f.Name, Reason: f.Reason})
	}
	st.Conflicted = int32(len(x.conflicts))
	st.Pruned = int32(x.prunedN)
	st.Managed = int32(x.managed + x.created - x.prunedN)
	st.SourceDigest = discovery.Digest(x.result.Devices)

	if len(x.conflicts) > 0 {
		x.setCondition(TargetSourceConditionConflicted, metav1.ConditionTrue, ReasonNameConflict,
			fmt.Sprintf("%d wanted name(s) belong to Targets this source does not own: %s", len(x.conflicts), sample(x.conflicts)))
		x.r.event(x.ts, corev1.EventTypeWarning, ReasonNameConflict, sample(x.conflicts))
	} else {
		apimeta.RemoveStatusCondition(&st.Conditions, TargetSourceConditionConflicted)
	}
	apimeta.RemoveStatusCondition(&st.Conditions, TargetSourceConditionStalled)

	switch {
	case x.capacity > 0:
		msg := fmt.Sprintf("the source returned %d devices, over spec.maxTargets %d; nothing was changed", x.capacity, ptr.Deref(x.ts.Spec.MaxTargets, 0))
		st.LastError = msg
		x.setCondition(TargetSourceConditionReady, metav1.ConditionFalse, ReasonCapacityExceeded, msg)
		x.setCondition(TargetSourceConditionReconciling, metav1.ConditionFalse, ReasonCapacityExceeded, "waiting for the source or spec.maxTargets to change")
		x.r.event(x.ts, corev1.EventTypeWarning, ReasonCapacityExceeded, msg)
		recordSync(x.ts, "failure", x.now)
	case len(x.applyErrs) > 0:
		st.ConsecutiveFailures++
		reason := ReasonPartialFailure
		if x.created+x.updated+x.prunedN == 0 {
			reason = ReasonApplyFailed
		}
		msg := fmt.Sprintf("%d write(s) failed: %s", len(x.applyErrs), sample(x.applyErrs))
		if !x.prune.Allowed && x.prune.Reason != "" {
			msg += "; " + x.prune.Message
		}
		st.LastError = msg
		x.setCondition(TargetSourceConditionReady, metav1.ConditionFalse, reason, msg)
		x.setCondition(TargetSourceConditionReconciling, metav1.ConditionTrue, ReasonPartialFailure, "retry scheduled with backoff")
		recordSync(x.ts, "failure", x.now)
	case !x.prune.Allowed && x.prune.Reason != "":
		st.ConsecutiveFailures = 0
		st.LastError = ""
		x.setCondition(TargetSourceConditionReady, metav1.ConditionFalse, x.prune.Reason, x.prune.Message)
		x.setCondition(TargetSourceConditionReconciling, metav1.ConditionFalse, x.prune.Reason, "creates and updates applied; deletions held until the source recovers")
		x.r.event(x.ts, corev1.EventTypeWarning, x.prune.Reason, x.prune.Message)
		recordSync(x.ts, "held", x.now)
	default:
		first := st.LastSuccessfulSyncTime == nil
		st.ConsecutiveFailures = 0
		st.LastError = ""
		st.LastSuccessfulSyncTime = ptrTime(x.now)
		msg := fmt.Sprintf("%d device(s) discovered, %d Target(s) managed", len(x.result.Devices), st.Managed)
		x.setCondition(TargetSourceConditionReady, metav1.ConditionTrue, ReasonSucceeded, msg)
		x.setCondition(TargetSourceConditionReconciling, metav1.ConditionFalse, ReasonConverged, msg)
		if first {
			x.r.event(x.ts, corev1.EventTypeNormal, ReasonSucceeded, msg)
		}
		recordSync(x.ts, "success", x.now)
	}
	recordTargets(x.ts, int(st.Managed), int(st.Invalid), int(st.Conflicted))
}

func (x *run) setCondition(condType string, status metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&x.ts.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: x.ts.Generation,
		Reason:             reason,
		Message:            truncateMessage(message),
	})
}

// event records an event when a recorder is wired; tests run without one.
func (r *TargetSourceReconciler) event(obj client.Object, eventType, reason, message string) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Event(obj, eventType, reason, truncateMessage(message))
}

func asNotFound(err error, target **provider.NotFoundError) bool {
	return errors.As(err, target)
}

func ptrTime(t time.Time) *metav1.Time {
	mt := metav1.NewTime(t)
	return &mt
}

// sample joins up to five items so a condition message stays readable.
func sample(items []string) string {
	const limit = 5
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:limit], ", ") + fmt.Sprintf(" (and %d more)", len(items)-limit)
}

// truncateMessage keeps condition and event text under the API limit.
func truncateMessage(s string) string {
	const limit = 1024
	if len(s) <= limit {
		return s
	}
	return s[:limit-3] + "..."
}
