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
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery"
	"github.com/gnmic/operator/internal/controller/discovery/core"
	_ "github.com/gnmic/operator/internal/controller/discovery/loaders/all"
	"github.com/gnmic/operator/internal/controller/discovery/registry"
	"github.com/go-logr/logr"
)

const (
	targetSourceFinalizer = "operator.gnmic.dev/targetsource-finalizer"

	pipelineMaxRestarts = 5
	pipelineBackoff     = 3 * time.Second
)

type runningSource struct {
	cancel context.CancelFunc
}

// TargetSourceReconciler reconciles a TargetSource object
type TargetSourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	mu      sync.Mutex
	running map[types.NamespacedName]runningSource

	BufferSize int
	ChunkSize  int

	DiscoveryRegistry *registry.Registry[types.NamespacedName, []core.DiscoveryMessage]
}

// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources/finalizers,verbs=update
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *TargetSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("targetsource controller").WithValues(
		"targetsource", req.NamespacedName,
	)

	targetSource, err := r.getTargetSource(ctx, req.NamespacedName)
	if err != nil {
		// If the TargetSource no longer exists, ensure runtime cleanup
		if client.IgnoreNotFound(err) == nil {
			logger.Info("TargetSource not found, ensuring cleanup")
			r.stopDiscovery(req.NamespacedName)
			return ctrl.Result{}, nil
		}
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
	if err := r.startDiscoveryPipeline(req.NamespacedName, targetSource, logger); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("TargetSource pipeline started")
	return ctrl.Result{}, nil
}

// getTargetSource retrieves a TargetSource by name, handling cleanup if not found
func (r *TargetSourceReconciler) getTargetSource(ctx context.Context, key types.NamespacedName) (*gnmicv1alpha1.TargetSource, error) {
	var targetSource gnmicv1alpha1.TargetSource
	if err := r.Get(ctx, key, &targetSource); err != nil {
		return nil, err
	}
	return &targetSource, nil
}

// handleTargetSourceDeletion stops the discovery pipeline and removes the finalizer
func (r *TargetSourceReconciler) handleTargetSourceDeletion(ctx context.Context, key types.NamespacedName, targetSource *gnmicv1alpha1.TargetSource) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("TargetSource is being deleted, stopping pipeline", "name", key)

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
func (r *TargetSourceReconciler) isPipelineRunning(key types.NamespacedName) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, exists := r.running[key]
	return exists
}

// startDiscoveryPipeline creates and starts the loader and target manager
func (r *TargetSourceReconciler) startDiscoveryPipeline(key types.NamespacedName, targetSource *gnmicv1alpha1.TargetSource, logger logr.Logger) error {
	supervisor := discovery.NewSupervisor(context.Background())

	targetChannel := make(chan []core.DiscoveryMessage, r.BufferSize)
	if err := r.DiscoveryRegistry.Register(key, targetChannel); err != nil {
		return err
	}

	// Create target applier instance
	applier := discovery.NewTargetApplier(
		r.Client,
		r.Scheme,
		targetSource,
		targetChannel,
	)
	// Start target applier
	applierReady := make(chan struct{})
	supervisor.RunComponent(discovery.Component{
		Name: "target-applier",
		Policy: discovery.RestartPolicy{
			MaxRestarts: pipelineMaxRestarts,
			Backoff:     pipelineBackoff,
		},
		DegradeOnFailure: true,
		Run: func(ctx context.Context) error {
			close(applierReady)
			return applier.Run(ctx)
		},
	})
	// Wait for applier to be ready before starting loader
	select {
	case <-applierReady:
	case <-supervisor.Done():
		return nil
	}

	// Create loader instance
	loaderConfigured := targetSource.Spec.Provider != nil
	webhookConfigured := targetSource.Spec.Webhook.Enabled != nil
	if loaderConfigured {
		loader, err := discovery.NewLoader(
			key,
			targetSource.Spec,
			core.LoaderConfig{ChunkSize: r.ChunkSize},
		)
		if err != nil {
			supervisor.Stop()
			return err
		}

		supervisor.RunComponent(discovery.Component{
			Name: "loader",
			Policy: discovery.RestartPolicy{
				MaxRestarts: pipelineMaxRestarts,
				Backoff:     pipelineBackoff,
			},
			DegradeOnFailure: !webhookConfigured,
			Run: func(ctx context.Context) error {
				return loader.Start(ctx, key, targetSource.Spec, targetChannel)
			},
		})
	}

	go func() {
		<-supervisor.Done()
		supervisor.Wait() // Wait for components to exit

		logger.Info("Pipeline stopped; performing final cleanup")
		close(targetChannel)
		r.DiscoveryRegistry.Unregister(key)
		r.stopDiscovery(key)
	}()

	r.mu.Lock()
	r.running[key] = runningSource{
		cancel: func() {
			supervisor.Stop()
		},
	}
	r.mu.Unlock()

	return nil
}

// stopDiscovery stops and removes a running discovery pipeline
func (r *TargetSourceReconciler) stopDiscovery(key types.NamespacedName) {
	r.mu.Lock()
	running, ok := r.running[key]
	if ok {
		delete(r.running, key)
	}
	r.mu.Unlock()

	if ok {
		running.cancel()
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TargetSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.running = make(map[types.NamespacedName]runningSource)

	return ctrl.NewControllerManagedBy(mgr).
		For(&gnmicv1alpha1.TargetSource{}).
		Named("targetsource").
		Complete(r)
}
