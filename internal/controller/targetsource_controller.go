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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/discovery"
	"github.com/gnmic/operator/internal/discovery/provider"
)

const (
	defaultTargetSourceInterval = 5 * time.Minute
	defaultTargetSourceTimeout  = time.Minute
	// DefaultTargetSourceConcurrency is how many TargetSources reconcile at
	// once. A fetch holds a worker for up to spec.timeout, so one slow source
	// must not be able to stall the rest; four keeps a burst of first runs
	// inside the client rate limit shared with the Cluster controller.
	DefaultTargetSourceConcurrency = 4
)

// TargetSourceReconciler turns the devices an external system knows about into
// Target resources.
//
// One reconcile is one complete pass: fetch the source, build the desired set,
// apply the differences, prune what is gone under the guards, write status
// once, and schedule the next pass. Nothing lives between passes except what
// is in status, so a restart loses nothing and a failure cannot leave a dead
// runtime behind that still looks healthy.
type TargetSourceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// Concurrency is MaxConcurrentReconciles. Zero means the default.
	Concurrency int

	// Now and Random are overridable for tests. Nil means the real ones.
	Now    func() time.Time
	Random func() float64
}

// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile runs one discovery pass for a TargetSource.
func (r *TargetSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var ts gnmicv1alpha1.TargetSource
	if err := r.Get(ctx, req.NamespacedName, &ts); err != nil {
		// Gone: garbage collection removes the Targets through their owner
		// references. There is no finalizer and nothing in memory to release.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !ts.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	logger = logger.WithValues("targetsource", ts.Name, "namespace", ts.Namespace, "type", ts.Spec.Source.Type)
	ctx = log.IntoContext(ctx, logger)

	run := r.newRun(&ts)

	if ts.Spec.Suspend {
		run.suspended()
		return run.finish(ctx, ctrl.Result{})
	}

	prov, ok := provider.Lookup(ts.Spec.Source.Type)
	if !ok {
		run.stalled("InvalidSpec", fmt.Errorf("unknown source type %q", ts.Spec.Source.Type))
		return run.finish(ctx, run.stalledRequeue())
	}

	fetchCtx, cancel := context.WithTimeout(ctx, run.timeout())
	result, err := prov.Fetch(fetchCtx, provider.Request{
		Source:    ts.Spec.Source,
		Namespace: ts.Namespace,
		Objects:   objectResolver{Reader: r.Client},
	})
	cancel()
	if err != nil {
		if provider.IsSpecError(err) {
			run.stalled(specFailureReason(err), err)
			return run.finish(ctx, run.stalledRequeue())
		}
		run.fetchFailed(err)
		return run.finish(ctx, run.retryRequeue())
	}
	run.discovered(result)

	built := discovery.Build(&ts, result.Devices)
	run.built(built)

	managed, err := r.listManaged(ctx, &ts)
	if err != nil {
		return ctrl.Result{}, err
	}
	run.managedBefore(managed)

	if limit := ptr.Deref(ts.Spec.MaxTargets, 0); limit > 0 && int32(len(built.Desired)) > limit {
		run.capacityExceeded(len(built.Desired))
		return run.finish(ctx, run.normalRequeue())
	}

	existing := make([]discovery.Existing, 0, len(managed))
	byName := make(map[string]*gnmicv1alpha1.Target, len(managed))
	for i := range managed {
		t := &managed[i]
		byName[t.Name] = t
		existing = append(existing, discovery.Existing{Name: t.Name, Hash: t.Annotations[discovery.AnnotationHash]})
	}
	toApply, toRemove := discovery.Diff(built.Desired, existing)

	for _, d := range toApply {
		if _, ours := byName[d.Name]; !ours {
			conflicted, err := r.isUnmanaged(ctx, ts.Namespace, d.Name)
			if err != nil {
				return ctrl.Result{}, err
			}
			if conflicted {
				run.conflict(d.Name)
				continue
			}
		}
		if err := r.applyTarget(ctx, &ts, d); err != nil {
			logger.Error(err, "failed to apply target", "target", d.Name)
			run.applyFailed(d.Name, err)
			continue
		}
		run.applied(d.Name, byName[d.Name] == nil)
	}

	decision := discovery.PruneGuard(ts.Spec.Prune, result.Truncated, len(result.Devices), len(managed), len(toRemove))
	run.pruneDecision(decision)
	if decision.Allowed {
		for _, name := range toRemove {
			if err := r.deleteTarget(ctx, byName[name]); err != nil {
				logger.Error(err, "failed to delete target", "target", name)
				run.applyFailed(name, err)
				continue
			}
			run.pruned()
		}
	}

	return run.finish(ctx, run.completeRequeue())
}

// listManaged returns the Targets this TargetSource owns. The label narrows
// the cached list; the controller owner reference is what makes it ours. A
// Target carrying the label with a different or absent owner is not managed,
// is never deleted, and is a conflict if its name is wanted.
func (r *TargetSourceReconciler) listManaged(ctx context.Context, ts *gnmicv1alpha1.TargetSource) ([]gnmicv1alpha1.Target, error) {
	var list gnmicv1alpha1.TargetList
	if err := r.List(ctx, &list, client.InNamespace(ts.Namespace), client.MatchingLabels{discovery.LabelTargetSource: ts.Name}); err != nil {
		return nil, err
	}
	managed := make([]gnmicv1alpha1.Target, 0, len(list.Items))
	for i := range list.Items {
		owner := metav1.GetControllerOf(&list.Items[i])
		if owner != nil && owner.UID == ts.UID {
			managed = append(managed, list.Items[i])
		}
	}
	return managed, nil
}

// isUnmanaged reports whether a Target of that name exists that this source
// does not own. The caller already knows the name is not in the managed set,
// so existence alone means conflict.
func (r *TargetSourceReconciler) isUnmanaged(ctx context.Context, namespace, name string) (bool, error) {
	var t gnmicv1alpha1.Target
	err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &t)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// applyTarget writes one Target with server-side apply. The object is built by
// hand so only the fields this source owns are sent: a typed Target would
// carry every zero value in spec and status and claim ownership of them all.
// Force ownership is deliberate: keys the source sets belong to the source,
// and a user edit to one of them is overwritten rather than stalling the
// Target until a human resolves the conflict. Everything else on the Target
// is the user's and survives every run.
func (r *TargetSourceReconciler) applyTarget(ctx context.Context, ts *gnmicv1alpha1.TargetSource, d discovery.Desired) error {
	annotations := make(map[string]string, len(d.Annotations)+1)
	for k, v := range d.Annotations {
		annotations[k] = v
	}
	annotations[discovery.AnnotationHash] = d.Hash

	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": gnmicv1alpha1.GroupVersion.String(),
		"kind":       "Target",
		"metadata": map[string]any{
			"name":      d.Name,
			"namespace": ts.Namespace,
			"ownerReferences": []any{map[string]any{
				"apiVersion":         gnmicv1alpha1.GroupVersion.String(),
				"kind":               "TargetSource",
				"name":               ts.Name,
				"uid":                string(ts.UID),
				"controller":         true,
				"blockOwnerDeletion": true,
			}},
		},
		"spec": map[string]any{
			"address": d.Address,
			"profile": d.Profile,
		},
	}}
	if err := unstructured.SetNestedStringMap(u.Object, d.Labels, "metadata", "labels"); err != nil {
		return err
	}
	if err := unstructured.SetNestedStringMap(u.Object, annotations, "metadata", "annotations"); err != nil {
		return err
	}
	return r.Apply(ctx, client.ApplyConfigurationFromUnstructured(u),
		client.FieldOwner(discovery.FieldManagerPrefix+ts.Name), client.ForceOwnership)
}

// deleteTarget removes a managed Target, with a UID precondition so a Target
// recreated under the same name by someone else between the list and the
// delete is left alone.
func (r *TargetSourceReconciler) deleteTarget(ctx context.Context, t *gnmicv1alpha1.Target) error {
	if t == nil {
		return nil
	}
	uid := t.UID
	err := r.Delete(ctx, t, client.Preconditions{UID: &uid})
	if apierrors.IsNotFound(err) || apierrors.IsConflict(err) {
		return nil
	}
	return err
}

// specFailureReason picks the Stalled reason for a spec error from what the
// resolver could not find.
func specFailureReason(err error) string {
	var nf *provider.NotFoundError
	if asNotFound(err, &nf) {
		switch nf.Kind {
		case "Secret":
			return "SecretNotFound"
		case "ConfigMap":
			return "ConfigMapNotFound"
		}
	}
	return "InvalidSpec"
}

// SetupWithManager sets up the controller with the Manager.
func (r *TargetSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.registerIndexes(mgr); err != nil {
		return err
	}
	concurrency := r.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultTargetSourceConcurrency
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("targetsource").
		WithOptions(controller.Options{MaxConcurrentReconciles: concurrency}).
		For(&gnmicv1alpha1.TargetSource{}, builder.WithPredicates(targetSourceChangedPredicate{})).
		// Status writes from the TargetState controller do not bump the
		// generation, so this fires for spec edits and deletions only.
		Owns(&gnmicv1alpha1.Target{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.targetSourcesForSecret),
			builder.WithPredicates(secretDataChangedPredicate{})).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.targetSourcesForConfigMap),
			builder.WithPredicates(configMapDataChangedPredicate{})).
		Complete(r)
}
