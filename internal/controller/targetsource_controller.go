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
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/targetsource"
	_ "github.com/gnmic/operator/internal/controller/targetsource/loaders/all"
)

type runningSource struct {
	cancel context.CancelFunc
}

// TargetSourceReconciler reconciles a TargetSource object
type TargetSourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	mu      sync.Mutex
	running map[client.ObjectKey]runningSource
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
	// 1. Check if a pipeline is already running for this TargetSource
	// 2. If not, create and start a new pipeline:
	//    a. Create a Loader based on TargetSource spec
	//    b. Start the Loader in a new goroutine, passing a channel for discovered targets
	//    c. Start a TargetManager in another goroutine to consume discovered targets and manage Target CRs
	// 3. If yes, check if the spec has changed and restart the pipeline if needed

	r.mu.Lock()
	_, exists := r.running[req.NamespacedName]
	r.mu.Unlock()

	// If a targetsource loader exists, return immediately without starting
	// any new loader or target manager
	if exists {
		return ctrl.Result{}, nil
	}

	loader, err := targetsource.NewLoader(targetSource.ObjectMeta.Name, targetSource.ObjectMeta.Namespace, targetSource.Spec) // TODO: pass configuration to loader based on spec
	if err != nil {
		return ctrl.Result{}, err
	}

	runtimeCtx, cancel := context.WithCancel(context.Background())
	target_channel := make(chan []targetsource.DiscoveredTarget)

	// start loader
	go loader.Start(runtimeCtx, targetSource.Name, target_channel)

	// start target manager
	manager := targetsource.NewTargetManager(
		r.Client,
		targetSource.Name,
		target_channel,
	)
	go manager.Run(runtimeCtx)

	r.mu.Lock()
	r.running[req.NamespacedName] = runningSource{cancel: cancel}
	r.mu.Unlock()

	logger.Info("TargetSource pipeline started", "name", targetSource.Name)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TargetSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.running = make(map[client.ObjectKey]runningSource)

	return ctrl.NewControllerManagedBy(mgr).
		For(&gnmicv1alpha1.TargetSource{}).
		Named("targetsource").
		Complete(r)
}
