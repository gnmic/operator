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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller/discovery"
	discoveryTypes "github.com/gnmic/operator/internal/controller/discovery/core"
	"github.com/go-logr/logr"
)

// TargetSourceReconciler reconciles a TargetSource object
//
// Responsibilities:
// - Ensure at most one discovery runtime per TargetSource
// - Start runtime on reconcile if not already running
// - Restart runtime on reconcile if spec changed
// - Stop runtime on deletion or NotFound
type TargetSourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	BufferSize int
	ChunkSize  int

	DiscoveryRegistry *discovery.Registry[
		types.NamespacedName,
		discoveryTypes.DiscoveryRegistryValue,
	]
}

// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetsources/finalizers,verbs=update
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *TargetSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).
		WithName("targetsource-controller").
		WithValues(
			"targetsource", req.NamespacedName.Name,
			"namespace", req.NamespacedName.Namespace,
		)

	targetSource, err := r.fetchTargetSource(ctx, req.NamespacedName)
	// If the TargetSource no longer exists, ensure runtime cleanup
	if apierrors.IsNotFound(err) {
		if runtime, ok := r.DiscoveryRegistry.Get(req.NamespacedName); ok {
			runtime.Stop()
			r.DiscoveryRegistry.Unregister(req.NamespacedName)
		}
		logger.Info("TargetSource not found; stopped discovery runtime")
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	if !targetSource.DeletionTimestamp.IsZero() {
		return r.reconcileDeletion(ctx, req.NamespacedName, targetSource)
	}

	if err := r.ensureFinalizer(ctx, targetSource); err != nil {
		return ctrl.Result{}, err
	}

	if r.DiscoveryRegistry.Exists(req.NamespacedName) {
		logger.Info("Discovery runtime already running; reconciliation completed")
		return ctrl.Result{}, nil
	}

	if err := r.startDiscovery(req.NamespacedName, targetSource, logger); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Started discovery runtime")
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

// reconcileDeletion stops the discovery runtime and removes the finalizer
func (r *TargetSourceReconciler) reconcileDeletion(ctx context.Context, key types.NamespacedName, targetSource *gnmicv1alpha1.TargetSource) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		"targetsource", key.Name,
		"namespace", key.Namespace,
	)
	logger.Info("TargetSource was marked for deletion; stopping discovery runtime")
	if runtime, ok := r.DiscoveryRegistry.Get(key); ok {
		runtime.Stop()
		r.DiscoveryRegistry.Unregister(key)
	}

	// Remove finalizer if exists
	if controllerutil.ContainsFinalizer(targetSource, LabelTargetSourceFinalizer) {
		controllerutil.RemoveFinalizer(targetSource, LabelTargetSourceFinalizer)
		if err := r.Update(ctx, targetSource); err != nil {
			return ctrl.Result{}, err
		}

		logger.Info("Removed TargetSource finalizer")
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

	log.FromContext(ctx).Info(
		"Added TargetSource finalizer",
		"targetsource", targetSource.Name,
		"namespace", targetSource.Namespace,
	)

	return nil
}

// startDiscovery creates and starts a discovery runtime for a TargetSource
//
// Invariant:
// - MessageProcessor and Loader must run for the lifetime of the TargetSource
// - Any unexpected exit is treated as a bug and triggers full shutdown
func (r *TargetSourceReconciler) startDiscovery(
	key types.NamespacedName,
	targetSource *gnmicv1alpha1.TargetSource,
	logger logr.Logger,
) error {
	targetChannel := make(chan []discoveryTypes.DiscoveryMessage, r.BufferSize)
	ctx, cancel := context.WithCancel(context.Background())

	// Register discovery runtime of targetsource
	if err := r.DiscoveryRegistry.Register(key, discoveryTypes.DiscoveryRegistryValue{
		Channel: targetChannel,
		Stop:    cancel,
	}); err != nil {
		return err
	}

	// Cleanup function to cleanup discovery runtime of targetsource
	cleanup := func() {
		cancel()
		r.DiscoveryRegistry.Unregister(key)
		close(targetChannel)
	}

	// Start message processor
	messageProcessor := discovery.NewMessageProcessor(
		r.Client,
		r.Scheme,
		targetSource,
		targetChannel,
	)
	go func() {
		logger.Info("Message processor started")

		if err := messageProcessor.Run(ctx); err != nil {
			logger.Error(err, "Message processor exited unecpectedly")
		} else {
			logger.Error(nil, "Message processor exited unexpectedly without error")
		}

		// Any exit is considered a bug that should stop the discovery runtime
		cleanup()
	}()

	// Start target loader
	// webhookActivated := targetSource.Spec.Webhook.Enabled != nil && *targetSource.Spec.Webhook.Enabled
	loaderConfig := discoveryTypes.LoaderConfig{
		TargetsourceNN: key,
		Spec:           &targetSource.Spec,
		ChunkSize:      r.ChunkSize,
	}
	loader, err := discovery.NewLoader(loaderConfig)
	if err != nil {
		logger.Error(err, "Target loader could not be created")
		cleanup()
		return err
	}
	go func() {
		if err := loader.Run(ctx, targetChannel); err != nil {
			logger.Error(err, "Target loader exited unexpectedly")
		} else {
			logger.Error(nil, "Target loader exited unexpectedly without error")
		}

		// Any exit is considered a bug that should stop the discovery runtime
		cleanup()
	}()

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TargetSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gnmicv1alpha1.TargetSource{}).
		Named("targetsource").
		Complete(r)
}
