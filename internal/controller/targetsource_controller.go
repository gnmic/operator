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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/targetsource"
)

// TargetSourceReconciler reconciles a TargetSource object
type TargetSourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources/finalizers,verbs=update
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *TargetSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var targetSource gnmicv1alpha1.TargetSource
	if err := r.Get(ctx, req.NamespacedName, &targetSource); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("reconciling TargetSource", "name", targetSource.Name)

	// TODO:
	// 1. Start go routines for loader of target source
	// 2. Retrieve list of targets from go channel
	// 3. Fetch existing Targets from Kubernetes API
	// 4. Compare and determine which Targets to create/update/delete
	// 5. Create/update/delete Target CRs accordingly
	// 6. Update TargetSource status with sync results

	// Step 1: Get desired state from discovery source
	discoveredTargets, err := targetsource.FetchDiscoveryTargets(ctx, targetSource)
	if err != nil {
		logger.Error(err, "error getting discovered targets")
		return ctrl.Result{}, err
	}

	// Step 2: Get current state from Kubernetes cluster (lookup by label of TargetSource)
	existingTargets, err := targetsource.FetchExistingTargets(ctx, r.Client, targetSource)
	if err != nil {
		logger.Error(err, "error fetching existing targets")
		return ctrl.Result{}, err
	}

	// Step 3: Compute diff
	diff := targetsource.BuildDiff(existingTargets, discoveredTargets)

	// Step 4: Iterate over each list and do create, update, delete respectively
	for _, t := range diff.ToCreate {
		err = controllerutil.SetControllerReference(&targetSource, &t, r.Scheme)
		if err != nil {
			logger.Error(err, "error setting the owner reference")
			return ctrl.Result{}, err
		}

		err = r.Client.Create(ctx, &t)
		if err != nil {
			logger.Error(err, "error creating target object")
			return ctrl.Result{}, err
		}
		logger.Info(fmt.Sprintf("created new target object %s/%s", t.ObjectMeta.Namespace, t.ObjectMeta.Name))
	}

	for _, t := range diff.ToUpdate {
		existing := &gnmicv1alpha1.Target{}

		err := r.Get(ctx, types.NamespacedName{
			Name:      t.ObjectMeta.Name,
			Namespace: t.ObjectMeta.Namespace,
		}, existing)

		if err != nil {
			logger.Error(err, "error fetching existing target object")
			return ctrl.Result{}, err
		}

		existing.Spec = t.Spec

		err = r.Update(ctx, existing)
		if err != nil {
			logger.Error(err, "error updating object")
			return ctrl.Result{}, err
		}
		logger.Info(fmt.Sprintf("updated existing target object %s/%s", t.ObjectMeta.Namespace, t.ObjectMeta.Name))
	}

	for _, t := range diff.ToDelete {
		err = r.Client.Delete(ctx, &t)
		logger.Info(fmt.Sprintf("resource name to be deleted: %s/%s", t.ObjectMeta.Namespace, t.ObjectMeta.Name))
		if err != nil {
			logger.Error(err, "error deleting the object")
			return ctrl.Result{}, err
		}
		logger.Info(fmt.Sprintf("deleted target object %s/%s", t.ObjectMeta.Namespace, t.ObjectMeta.Name))
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TargetSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gnmicv1alpha1.TargetSource{}).
		Named("targetsource").
		Complete(r)
}
