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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
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

	// TODO: Implement target discovery logic based on spec:
	// - HTTP: fetch targets from HTTP endpoint
	// - Consul: discover from Consul
	// - ConfigMap: read from ConfigMap
	// - PodSelector: select Kubernetes pods
	// - ServiceSelector: select Kubernetes services

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TargetSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gnmicv1alpha1.TargetSource{}).
		Named("targetsource").
		Complete(r)
}
