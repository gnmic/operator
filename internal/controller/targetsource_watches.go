package controller

import (
	"bytes"
	"context"
	"maps"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/discovery"
)

// Field indexes from TargetSource to the Secrets and ConfigMaps its spec
// names, so a change to one of them is a cache lookup, not a full list.
const (
	TargetSourceSecretsIndex    = ".index.targetsource.secrets"
	TargetSourceConfigMapsIndex = ".index.targetsource.configmaps"
)

func (r *TargetSourceReconciler) registerIndexes(mgr ctrl.Manager) error {
	ctx := context.Background()
	if err := mgr.GetFieldIndexer().IndexField(ctx, &gnmicv1alpha1.TargetSource{}, TargetSourceSecretsIndex, func(o client.Object) []string {
		ts, ok := o.(*gnmicv1alpha1.TargetSource)
		if !ok {
			return nil
		}
		return discovery.ReferencedSecrets(&ts.Spec)
	}); err != nil {
		return err
	}
	return mgr.GetFieldIndexer().IndexField(ctx, &gnmicv1alpha1.TargetSource{}, TargetSourceConfigMapsIndex, func(o client.Object) []string {
		ts, ok := o.(*gnmicv1alpha1.TargetSource)
		if !ok {
			return nil
		}
		return discovery.ReferencedConfigMaps(&ts.Spec)
	})
}

// targetSourceChangedPredicate fires for spec changes and for the webhook
// handler's requested-at annotation. Status writes, which happen once per
// run, do not come back around as another run.
type targetSourceChangedPredicate struct {
	predicate.Funcs
}

func (targetSourceChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}
	if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
		return true
	}
	return e.ObjectOld.GetAnnotations()[discovery.AnnotationRequestedAt] != e.ObjectNew.GetAnnotations()[discovery.AnnotationRequestedAt]
}

// configMapDataChangedPredicate mirrors secretDataChangedPredicate: only the
// contents matter to a run.
type configMapDataChangedPredicate struct {
	predicate.Funcs
}

func (configMapDataChangedPredicate) Update(e event.UpdateEvent) bool {
	oldCM, okOld := e.ObjectOld.(*corev1.ConfigMap)
	newCM, okNew := e.ObjectNew.(*corev1.ConfigMap)
	if !okOld || !okNew {
		return false
	}
	return !maps.Equal(oldCM.Data, newCM.Data) || !maps.EqualFunc(oldCM.BinaryData, newCM.BinaryData, bytes.Equal)
}

func (r *TargetSourceReconciler) targetSourcesForSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.targetSourcesReferencing(ctx, obj, TargetSourceSecretsIndex)
}

func (r *TargetSourceReconciler) targetSourcesForConfigMap(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.targetSourcesReferencing(ctx, obj, TargetSourceConfigMapsIndex)
}

func (r *TargetSourceReconciler) targetSourcesReferencing(ctx context.Context, obj client.Object, index string) []reconcile.Request {
	var list gnmicv1alpha1.TargetSourceList
	if err := r.List(ctx, &list, client.InNamespace(obj.GetNamespace()), client.MatchingFields{index: obj.GetName()}); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}
	return reqs
}
