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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	_ "github.com/gnmic/operator/internal/controller/discovery/loaders/all"
	"github.com/gnmic/operator/internal/controller/discovery/registry"
)

const targetSourceFinalizer = "operator.gnmic.dev/targetsource-finalizer"

type runningSource struct {
	cancel context.CancelFunc
}

// TargetSourceReconciler reconciles a TargetSource object
type TargetSourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	mu      sync.Mutex
	running map[client.ObjectKey]runningSource

	BufferSize int
	ChunkSize  int

	DiscoveryRegistry *registry.Registry[[]core.DiscoveryMessage]
}

// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources/finalizers,verbs=update
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *TargetSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		"Name", req.NamespacedName,
	)

	targetSource, err := r.getTargetSource(ctx, req.NamespacedName)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Handle deletion with finalizer
	if !targetSource.DeletionTimestamp.IsZero() {
		return r.handleTargetSourceDeletion(ctx, req.NamespacedName, targetSource)
	}

	// Ensure finalizer is set
	if err := r.ensureFinalizer(ctx, targetSource); err != nil {
		return ctrl.Result{}, err
	}

	// Check if pipeline is already running
	if r.isPipelineRunning(req.NamespacedName) {
		return ctrl.Result{}, nil
	}

	// Start discovery pipeline
	if err := r.startDiscoveryPipeline(req.NamespacedName, targetSource); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("TargetSource pipeline started")
	return ctrl.Result{}, nil
}

// getTargetSource retrieves a TargetSource by name, handling cleanup if not found
func (r *TargetSourceReconciler) getTargetSource(ctx context.Context, key client.ObjectKey) (*gnmicv1alpha1.TargetSource, error) {
	var targetSource gnmicv1alpha1.TargetSource
	if err := r.Get(ctx, key, &targetSource); err != nil {
		// If the TargetSource no longer exists, ensure runtime cleanup
		if client.IgnoreNotFound(err) == nil {
			r.stopDiscovery(key)
		}
		return nil, client.IgnoreNotFound(err)
	}
	return &targetSource, nil
}

// handleTargetSourceDeletion stops the discovery pipeline and removes the finalizer
func (r *TargetSourceReconciler) handleTargetSourceDeletion(ctx context.Context, key client.ObjectKey, targetSource *gnmicv1alpha1.TargetSource) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("TargetSource is being deleted, stopping pipeline", "name", targetSource.Name)

	r.stopDiscovery(key)

	// Remove finalizer if exists
	if controllerutil.ContainsFinalizer(targetSource, targetSourceFinalizer) {
		controllerutil.RemoveFinalizer(targetSource, targetSourceFinalizer)
		if err := r.Update(ctx, targetSource); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// ensureFinalizer adds the finalizer if not present and updates the TargetSource
func (r *TargetSourceReconciler) ensureFinalizer(ctx context.Context, targetSource *gnmicv1alpha1.TargetSource) error {
	if controllerutil.ContainsFinalizer(targetSource, targetSourceFinalizer) {
		return nil
	}

	controllerutil.AddFinalizer(targetSource, targetSourceFinalizer)
	if err := r.Update(ctx, targetSource); err != nil {
		return err
	}

	return nil
}

// isPipelineRunning checks if a discovery pipeline is already running for the given key
func (r *TargetSourceReconciler) isPipelineRunning(key client.ObjectKey) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, exists := r.running[key]
	return exists
}

// startDiscoveryPipeline creates and starts the loader and target manager
func (r *TargetSourceReconciler) startDiscoveryPipeline(key client.ObjectKey, targetSource *gnmicv1alpha1.TargetSource) error {
	cfg := core.LoaderConfig{
		ChunkSize: r.ChunkSize,
	}

	loader, err := discovery.NewLoader(
		targetSource.ObjectMeta.Name,
		targetSource.ObjectMeta.Namespace,
		targetSource.Spec,
		cfg,
	)
	if err != nil {
		return err
	}

	runtimeCtx, cancel := context.WithCancel(context.Background())
	targetChannel := make(chan []core.DiscoveryMessage, r.BufferSize)

	registryKey := key.Namespace + "/" + key.Name
	if err := r.DiscoveryRegistry.Register(registryKey, targetChannel); err != nil {
		cancel()
		return err
	}

	// Start loader
	go loader.Start(runtimeCtx, targetSource.Name, targetSource.Spec, targetChannel)

	// Start target manager
	manager := discovery.NewTargetManager(
		r.Client,
		r.Scheme,
		targetSource,
		targetChannel,
	)
	go manager.Run(runtimeCtx)

	r.mu.Lock()
	r.running[key] = runningSource{cancel: cancel}
	r.mu.Unlock()

	return nil
}

// stopDiscovery stops and removes a running discovery pipeline
// for the given TargetSource key
func (r *TargetSourceReconciler) stopDiscovery(key client.ObjectKey) {
	r.mu.Lock()
	running, ok := r.running[key]
	if ok {
		running.cancel()
		delete(r.running, key)
	}
	r.mu.Unlock()

	if ok {
		registryKey := key.Namespace + "/" + key.Name
		r.DiscoveryRegistry.Unregister(registryKey)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TargetSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.running = make(map[client.ObjectKey]runningSource)

	return ctrl.NewControllerManagedBy(mgr).
		For(&gnmicv1alpha1.TargetSource{}).
		Named("targetsource").
		Complete(r)
}
