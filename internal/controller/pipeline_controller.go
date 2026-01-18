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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// PipelineReconciler reconciles a Pipeline object
type PipelineReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=pipelines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=pipelines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=pipelines/finalizers,verbs=update
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=clusters,verbs=get;list;watch

// Reconcile validates the Pipeline and updates its status.
// The actual configuration building happens in the ClusterReconciler which watches Pipelines.
func (r *PipelineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var pipeline gnmicv1alpha1.Pipeline
	if err := r.Get(ctx, req.NamespacedName, &pipeline); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger = logger.WithValues("pipeline", pipeline.Name, "namespace", pipeline.Namespace)

	// validate the referenced cluster exists
	var cluster gnmicv1alpha1.Cluster
	clusterNN := types.NamespacedName{
		Name:      pipeline.Spec.ClusterRef,
		Namespace: pipeline.Namespace,
	}
	if err := r.Get(ctx, clusterNN, &cluster); err != nil {
		if errors.IsNotFound(err) {
			logger.Error(err, "referenced cluster not found", "clusterRef", pipeline.Spec.ClusterRef)
			pipeline.Status.Status = "Error: Cluster not found"
			if statusErr := r.Status().Update(ctx, &pipeline); statusErr != nil {
				return ctrl.Result{}, statusErr
			}
			// requeue to check again later
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// update pipeline status
	newStatus := "Ready"
	if !pipeline.Spec.Enabled {
		newStatus = "Disabled"
	}
	if pipeline.Status.Status != newStatus {
		pipeline.Status.Status = newStatus
		if err := r.Status().Update(ctx, &pipeline); err != nil {
			return ctrl.Result{}, err
		}
	}

	logger.Info("reconciled pipeline", "clusterRef", pipeline.Spec.ClusterRef, "enabled", pipeline.Spec.Enabled)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PipelineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gnmicv1alpha1.Pipeline{}).
		Complete(r)
}
