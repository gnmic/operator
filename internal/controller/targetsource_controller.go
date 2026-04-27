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
	"github.com/gnmic/operator/internal/controller/discovery/loaders"
	_ "github.com/gnmic/operator/internal/controller/discovery/loaders/all"
	"github.com/gnmic/operator/internal/controller/discovery/registry"
	"github.com/go-logr/logr"
)

const (
	pipelineMaxRestarts = 5
	pipelineBackoff     = 3 * time.Second
)

// pipelineHandle represents a controller-owned handle to a running pipeline
// The controller never manipulates internals; it only invokes cancel()
type pipelineHandle struct {
	cancel context.CancelFunc
}

// TargetSourceReconciler reconciles a TargetSource object
//
// Responsibilities:
// - Ensure at most one pipeline per TargetSource
// - Start pipelines on reconcile
// - Stop pipelines on deletion or NotFound
// - Delegate runtime failure handling to the Supervisor
type TargetSourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	mu sync.Mutex
	// runningPipelines tracks currently active pipelines by NamespacedName
	runningPipelines map[types.NamespacedName]pipelineHandle

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
	logger := log.FromContext(ctx).
		WithName("targetsource controller").
		WithValues("targetsource", req.NamespacedName)

	targetSource, err := r.fetchTargetSource(ctx, req.NamespacedName)
	if err != nil {
		// If the TargetSource no longer exists, ensure runtime cleanup
		if client.IgnoreNotFound(err) == nil {
			logger.Info("TargetSource not found; stopping discovery pipeline")
			r.stopDiscoveryPipeline(req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !targetSource.DeletionTimestamp.IsZero() {
		return r.reconcileDeletion(ctx, req.NamespacedName, targetSource)
	}

	if err := r.ensureFinalizer(ctx, targetSource); err != nil {
		return ctrl.Result{}, err
	}

	if r.hasPipelineRunning(req.NamespacedName) {
		return ctrl.Result{}, nil
	}

	if err := r.startDiscoveryPipeline(req.NamespacedName, targetSource, logger); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Discover pipeline started")
	return ctrl.Result{}, nil
}

// fetchTargetSource retrieves a TargetSource by name, handling cleanup if not found
func (r *TargetSourceReconciler) fetchTargetSource(ctx context.Context, key types.NamespacedName) (*gnmicv1alpha1.TargetSource, error) {
	var targetSource gnmicv1alpha1.TargetSource
	if err := r.Get(ctx, key, &targetSource); err != nil {
		return nil, err
	}
	return &targetSource, nil
}

// hasPipelineRunning checks if a discovery pipeline is already running for the given key
func (r *TargetSourceReconciler) hasPipelineRunning(key types.NamespacedName) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, exists := r.runningPipelines[key]
	return exists
}

// reconcileDeletion stops the discovery pipeline and removes the finalizer
func (r *TargetSourceReconciler) reconcileDeletion(ctx context.Context, key types.NamespacedName, targetSource *gnmicv1alpha1.TargetSource) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("TargetSource is being deleted, stopping pipeline", "name", key)

	r.stopDiscoveryPipeline(key)

	// Remove finalizer if exists
	if controllerutil.ContainsFinalizer(targetSource, LabelTargetSourceFinalizer) {
		controllerutil.RemoveFinalizer(targetSource, LabelTargetSourceFinalizer)
		if err := r.Update(ctx, targetSource); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// ensureFinalizer adds the finalizer if not present and updates the TargetSource
func (r *TargetSourceReconciler) ensureFinalizer(ctx context.Context, targetSource *gnmicv1alpha1.TargetSource) error {
	if controllerutil.ContainsFinalizer(targetSource, LabelTargetSourceFinalizer) {
		return nil
	}

	controllerutil.AddFinalizer(targetSource, LabelTargetSourceFinalizer)
	if err := r.Update(ctx, targetSource); err != nil {
		return err
	}

	return nil
}

// startDiscoveryPipeline creates and starts a discover pipeline for a TargetSource
//
// Pipeline semantics:
// 1. target-applier is mandatory and must start first
// 2. loader is optional and conditional on spec
// 3. Permanent failure of required components shuts down the pipeline
// 4. Shutdown ordering: cancel ctx -> wait for goroutines to exit -> close channel -> unregister
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
	supervisor.StartSupervisedComponent(discovery.ComponentSpec{
		Name: "target-applier",
		Policy: discovery.RestartPolicy{
			MaxRestarts: pipelineMaxRestarts,
			Backoff:     pipelineBackoff,
		},
		EscalatesOnFailure: true,
		Run: func(ctx context.Context) error {
			close(applierReady) // Signals that applier started successfully
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
		loader, err := loaders.NewLoader(
			key,
			targetSource.Spec,
			core.LoaderConfig{ChunkSize: r.ChunkSize},
		)
		if err != nil {
			supervisor.Stop()
			return err
		}

		supervisor.StartSupervisedComponent(discovery.ComponentSpec{
			Name: "loader",
			Policy: discovery.RestartPolicy{
				MaxRestarts: pipelineMaxRestarts,
				Backoff:     pipelineBackoff,
			},
			EscalatesOnFailure: !webhookConfigured,
			Run: func(ctx context.Context) error {
				return loader.Start(ctx, key, targetSource.Spec, targetChannel)
			},
		})
	}

	// Monitor supervisor in a separate goroutine to handle shutdown and cleanup
	go func() {
		<-supervisor.Done()
		supervisor.Wait() // Wait for components to exit

		logger.Info("Pipeline stopped; cleaning up")
		close(targetChannel)
		r.DiscoveryRegistry.Unregister(key)
		r.stopDiscoveryPipeline(key)
	}()

	r.mu.Lock()
	r.runningPipelines[key] = pipelineHandle{
		cancel: func() {
			supervisor.Stop()
		},
	}
	r.mu.Unlock()

	return nil
}

// stopDiscoveryPipeline stops and removes a running discovery pipeline
func (r *TargetSourceReconciler) stopDiscoveryPipeline(key types.NamespacedName) {
	r.mu.Lock()
	running, ok := r.runningPipelines[key]
	if ok {
		delete(r.runningPipelines, key)
	}
	r.mu.Unlock()

	if ok {
		running.cancel()
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TargetSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.runningPipelines = make(map[types.NamespacedName]pipelineHandle)

	return ctrl.NewControllerManagedBy(mgr).
		For(&gnmicv1alpha1.TargetSource{}).
		Named("targetsource").
		Complete(r)
}
