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

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	m *sync.RWMutex
	// key is namespace/name of the cluster
	// value is the apply plan for the cluster
	plans map[string]*gnmic.ApplyPlan

	// Applied records what each pod was last known to hold, so an unchanged
	// plan is not re-POSTed to every pod on every reconcile. Shared with the
	// TargetState controller, which invalidates a pod's entry when its SSE
	// stream drops. Nil disables the short-circuit.
	Applied *ApplyCache
}

const (
	resourcePrefix    = "gnmic-"
	clusterFinalizer  = "operator.gnmic.dev/cluster-finalizer"
	defaultRestPort   = 7890
	controllerCACMSfx = "-controller-ca"

	// readinessBackstopInterval is how often to re-check StatefulSet readiness when it is
	// not yet satisfied. Readiness changes already arrive via the Owns(&appsv1.StatefulSet{})
	// watch, so this only needs to cover a missed event, not to drive the wait.
	readinessBackstopInterval = 20 * time.Second

	// reconcileBackstopInterval bounds how long a Cluster can go un-reconciled
	// when every watch fires as expected. Referenced-resource changes (a
	// Subscription/Output/Target/etc. edit) reach this controller only
	// through the mapping funcs registered via Watches(...); a watch event
	// dropped by the API server or missed by an informer during a relist has
	// nothing else to re-trigger it, and the manager's cache resync period is
	// far too coarse to be a useful safety net on its own. Re-checking on this
	// cadence caps the damage from a dropped event to one interval instead of
	// leaving the cluster's applied config stale indefinitely.
	reconcileBackstopInterval = 20 * time.Second
)

// Condition types for Cluster status
const (
	// ConditionTypeReady indicates the cluster is fully operational
	ConditionTypeReady = "Ready"
	// ConditionTypeCertificatesReady indicates TLS certificates are ready
	ConditionTypeCertificatesReady = "CertificatesReady"
	// ConditionTypeConfigApplied indicates configuration was applied to pods
	ConditionTypeConfigApplied = "ConfigApplied"
	// ConditionTypeCapacityExhausted indicates some targets could not be assigned
	ConditionTypeCapacityExhausted = "CapacityExhausted"
)

// Condition types for Pipeline status
const (
	// PipelineConditionTypeReady indicates the pipeline is active and has resources
	PipelineConditionTypeReady = "Ready"
	// PipelineConditionTypeResourcesResolved indicates all resources were resolved
	PipelineConditionTypeResourcesResolved = "ResourcesResolved"

	// ReasonUnresolvedReferences is set on both Pipeline conditions when one or more
	// *Refs entries name a resource that does not exist. The pipeline is left out of
	// the apply plan entirely while this holds.
	ReasonUnresolvedReferences = "UnresolvedReferences"
)

//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=clusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=clusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=clusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=pipelines,verbs=get;list;watch
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=pipelines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=targets,verbs=get;list;watch
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=targetprofiles,verbs=get;list;watch
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=subscriptions,verbs=get;list;watch
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=outputs,verbs=get;list;watch
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=inputs,verbs=get;list;watch
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=processors,verbs=get;list;watch
//+kubebuilder:rbac:groups=operator.gnmic.dev,resources=tunneltargetpolicies,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// Secrets are read-only everywhere in this operator (credential and issuer-CA lookups);
// list and watch are required because they are served from the informer cache.
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// A pass runs in phases, each owned by a method in its own file: lifecycle
// (fetch, finalizer), infrastructure (Services, certificates), the StatefulSet,
// pipeline resolution into an apply plan, the apply itself, and status.
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	cluster, found, err := r.fetchCluster(ctx, req)
	if err != nil || !found {
		return ctrl.Result{}, err
	}
	logger = logger.WithValues("cluster", cluster.Name, "namespace", cluster.Namespace)
	ctx = log.IntoContext(ctx, logger)

	// handle deletion with a finalizer to guarantee statefulset, headless service and Prometheus output services cleanup
	if !cluster.DeletionTimestamp.IsZero() {
		return r.finalizeCluster(ctx, cluster)
	}
	if res, done, err := r.ensureFinalizer(ctx, cluster); done {
		return res, err
	}

	if res, wait, err := r.reconcileInfrastructure(ctx, cluster); err != nil || wait {
		return res, err
	}
	statefulSet, err := r.reconcileStatefulSet(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	logger.Info("reconciled cluster statefulset", "replicas", ptr.Deref(statefulSet.Spec.Replicas, 0), "image", statefulSet.Spec.Template.Spec.Containers[0].Image)

	// resolve the enabled pipelines referencing this cluster into an apply plan
	pipelines, err := r.listPipelinesForCluster(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	resolved, err := r.resolvePipelines(ctx, cluster, pipelines)
	if err != nil {
		return ctrl.Result{}, err
	}
	r.CachePlan(cluster.Namespace, cluster.Name, resolved.plan)

	if resolved.targetsWithoutSubscriptions() {
		logger.Info("apply plan has targets but no subscriptions, skipping apply (likely a transient cache read) and requeueing")
		return ctrl.Result{RequeueAfter: transientReadRequeue}, nil
	}
	if resolved.suppressApply() {
		logger.Info("all pipelines skipped for unresolved references, leaving the collectors' current config in place",
			"pipelines", len(pipelines), "skipped", resolved.skipped)
	}

	// Prometheus output Services are reconciled even when the apply is withheld,
	// so a deleted Output is garbage collected either way.
	if err := r.reconcilePrometheusServices(ctx, cluster, resolved.data, resolved.plan.PrometheusPorts); err != nil {
		logger.Error(err, "failed to reconcile Prometheus output services")
		return ctrl.Result{}, err
	}

	outcome, res, wait, err := r.applyPlan(ctx, cluster, statefulSet, pipelines, resolved)
	if err != nil || wait {
		return res, err
	}

	status := buildClusterStatus(cluster, statefulSet, len(pipelines), resolved.data, outcome)
	if err := r.writeClusterStatus(ctx, cluster, status); err != nil {
		return ctrl.Result{}, err
	}
	return requeueFor(outcome, len(pipelines)), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.m = &sync.RWMutex{}
	r.plans = make(map[string]*gnmic.ApplyPlan)

	specOrLabelsPredicate := generationOrLabelsChangedPredicate{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&gnmicv1alpha1.Cluster{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Owns(&certmanagerv1.Certificate{}). // Watch owned certificates (status updates trigger reconcile for readiness)
		Watches(
			&gnmicv1alpha1.Pipeline{},
			handler.EnqueueRequestsFromMapFunc(r.findClusterForPipeline),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Watches(
			&gnmicv1alpha1.Target{},
			handler.EnqueueRequestsFromMapFunc(r.findClustersForTarget),
			builder.WithPredicates(specOrLabelsPredicate),
		).
		Watches(
			&gnmicv1alpha1.Subscription{},
			handler.EnqueueRequestsFromMapFunc(r.findClustersForSubscription),
			builder.WithPredicates(specOrLabelsPredicate),
		).
		Watches(
			&gnmicv1alpha1.Output{},
			handler.EnqueueRequestsFromMapFunc(r.findClustersForOutput),
			builder.WithPredicates(specOrLabelsPredicate),
		).
		Watches(
			&gnmicv1alpha1.Input{},
			handler.EnqueueRequestsFromMapFunc(r.findClustersForInput),
			builder.WithPredicates(specOrLabelsPredicate),
		).
		Watches(
			&gnmicv1alpha1.Processor{},
			handler.EnqueueRequestsFromMapFunc(r.findClustersForProcessor),
			builder.WithPredicates(specOrLabelsPredicate),
		).
		Watches(
			&gnmicv1alpha1.TargetProfile{},
			handler.EnqueueRequestsFromMapFunc(r.findClustersForTargetProfile),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}), // TargetProfile is referenced by name, not labels
		).
		Watches(
			&gnmicv1alpha1.TunnelTargetPolicy{},
			handler.EnqueueRequestsFromMapFunc(r.findClustersForTunnelTargetPolicy),
			builder.WithPredicates(specOrLabelsPredicate),
		).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findClustersForSecret),
			builder.WithPredicates(secretDataChangedPredicate{}),
		).
		Complete(r)
}
