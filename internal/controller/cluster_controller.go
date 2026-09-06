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
	"sort"
	"sync"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var cluster gnmicv1alpha1.Cluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		// if the Cluster CR was deleted before we reconciled, cleanup related resources
		if apierrors.IsNotFound(err) {
			prefixedNN := types.NamespacedName{
				Name:      resourcePrefix + req.Name,
				Namespace: req.Namespace,
			}
			// cleanup statefulset
			if err := r.ensureStatefulSetAbsent(ctx, prefixedNN); err != nil {
				return ctrl.Result{}, err
			}
			// cleanup headless service
			if err := r.ensureServiceAbsent(ctx, prefixedNN); err != nil {
				return ctrl.Result{}, err
			}
			// clean up Prometheus output services
			if err := r.cleanupPrometheusServices(ctx, req.Namespace, req.Name); err != nil {
				return ctrl.Result{}, err
			}
			// cleanup plan
			r.cleanupPlan(req.Namespace, req.Name)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger = logger.WithValues("cluster", cluster.Name, "namespace", cluster.Namespace)

	// handle deletion with a finalizer to guarantee statefulset, headless service and Prometheus output services cleanup
	if !cluster.DeletionTimestamp.IsZero() {
		// cleanup plan
		r.cleanupPlan(req.Namespace, req.Name)
		// if the Cluster CR is being deleted, cleanup related resources
		if controllerutil.ContainsFinalizer(&cluster, clusterFinalizer) {
			nn := types.NamespacedName{Name: resourcePrefix + cluster.Name, Namespace: cluster.Namespace}
			// cleanup statefulset
			if cleanupErr := r.ensureStatefulSetAbsent(ctx, nn); cleanupErr != nil {
				return ctrl.Result{}, cleanupErr
			}
			// cleanup headless service
			if cleanupErr := r.ensureServiceAbsent(ctx, nn); cleanupErr != nil {
				return ctrl.Result{}, cleanupErr
			}
			// clean up Prometheus output services
			if cleanupErr := r.cleanupPrometheusServices(ctx, req.Namespace, req.Name); cleanupErr != nil {
				return ctrl.Result{}, cleanupErr
			}
			// cleanup TLS certificates (only if not using CSI driver)
			if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil &&
				cluster.Spec.API.TLS.IssuerRef != "" && !cluster.Spec.API.TLS.UseCSIDriver {
				if cleanupErr := r.cleanupCertificates(ctx, &cluster); cleanupErr != nil {
					return ctrl.Result{}, cleanupErr
				}
			}
			// cleanup controller CA secret
			if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil && cluster.Spec.API.TLS.IssuerRef != "" {
				if cleanupErr := r.cleanupControllerCA(ctx, &cluster); cleanupErr != nil {
					return ctrl.Result{}, cleanupErr
				}
			}
			// cleanup tunnel TLS certificates (only if not using CSI driver)
			if cluster.Spec.GRPCTunnel != nil && cluster.Spec.GRPCTunnel.TLS != nil &&
				cluster.Spec.GRPCTunnel.TLS.IssuerRef != "" && !cluster.Spec.GRPCTunnel.TLS.UseCSIDriver {
				if cleanupErr := r.cleanupTunnelCertificates(ctx, &cluster); cleanupErr != nil {
					return ctrl.Result{}, cleanupErr
				}
			}
			// cleanup client TLS certificates (only if not using CSI driver)
			if cluster.Spec.ClientTLS != nil &&
				cluster.Spec.ClientTLS.IssuerRef != "" && !cluster.Spec.ClientTLS.UseCSIDriver {
				if cleanupErr := r.cleanupClientTLSCertificates(ctx, &cluster); cleanupErr != nil {
					return ctrl.Result{}, cleanupErr
				}
			}
			// cleanup tunnel service
			if cluster.Spec.GRPCTunnel != nil {
				if cleanupErr := r.cleanupTunnelService(ctx, &cluster); cleanupErr != nil {
					return ctrl.Result{}, cleanupErr
				}
			}
			controllerutil.RemoveFinalizer(&cluster, clusterFinalizer)
			if updateErr := r.Update(ctx, &cluster); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
		}
		return ctrl.Result{}, nil
	}

	// ensure we get a finalizer so we can clean up if the CR is deleted
	if !controllerutil.ContainsFinalizer(&cluster, clusterFinalizer) {
		controllerutil.AddFinalizer(&cluster, clusterFinalizer)
		if err := r.Update(ctx, &cluster); err != nil {
			return ctrl.Result{}, err
		}
		// requeue after adding a finalizer to continue reconciliation with the updated object.
		// this is necessary we reconcile on generation change (Spec changes)
		return ctrl.Result{Requeue: true}, nil
	}

	// reconcile headless service first
	err := r.reconcileHeadlessService(ctx, &cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	// if TLS is enabled with cert-manager (non-CSI mode), reconcile certificates first
	// when using CSI driver, the driver handles certificate creation automatically
	if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil &&
		cluster.Spec.API.TLS.IssuerRef != "" && !cluster.Spec.API.TLS.UseCSIDriver {
		certsReady, err := r.reconcileCertificates(ctx, &cluster)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !certsReady {
			logger.Info("waiting for TLS certificates to be ready")
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}
	}

	// sync controller's CA to cluster namespace for mTLS client verification
	if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil && cluster.Spec.API.TLS.IssuerRef != "" {
		if err := r.reconcileControllerCA(ctx, &cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	// if gRPC tunnel TLS is enabled with cert-manager (non-CSI mode), reconcile tunnel certificates
	if cluster.Spec.GRPCTunnel != nil && cluster.Spec.GRPCTunnel.TLS != nil &&
		cluster.Spec.GRPCTunnel.TLS.IssuerRef != "" && !cluster.Spec.GRPCTunnel.TLS.UseCSIDriver {
		tunnelCertsReady, err := r.reconcileTunnelCertificates(ctx, &cluster)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !tunnelCertsReady {
			logger.Info("waiting for tunnel TLS certificates to be ready")
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}
	}

	// if client TLS is enabled with cert-manager (non-CSI mode), reconcile client certificates
	// these are used by gNMIc to connect to targets with mTLS
	if cluster.Spec.ClientTLS != nil &&
		cluster.Spec.ClientTLS.IssuerRef != "" && !cluster.Spec.ClientTLS.UseCSIDriver {
		clientCertsReady, err := r.reconcileClientTLSCertificates(ctx, &cluster)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !clientCertsReady {
			logger.Info("waiting for client TLS certificates to be ready")
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}
	}

	// reconcile tunnel service if gRPC tunnel is configured
	if cluster.Spec.GRPCTunnel != nil {
		if err := r.reconcileTunnelService(ctx, &cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	// reconcile statefulset
	statefulSet, err := r.reconcileStatefulSet(ctx, &cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("reconciled cluster statefulset", "replicas", ptr.Deref(statefulSet.Spec.Replicas, 0), "image", statefulSet.Spec.Template.Spec.Containers[0].Image)

	// retrieve enabled pipelines referencing this cluster
	pipelines, err := r.listPipelinesForCluster(ctx, &cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	// build pipeline data for the gNMIc plan builder
	planBuilder := gnmic.NewPlanBuilder(cluster.Name, r)
	planBuilder = planBuilder.WithClientTLS(
		gnmic.ClientTLSConfigForCluster(&cluster),
	)
	if cluster.Spec.TargetDistribution != nil && cluster.Spec.TargetDistribution.PodCapacity > 0 {
		planBuilder.WithTargetDistributionCapacity(cluster.Spec.TargetDistribution.PodCapacity)
	}
	pipelineDataMap := make(map[string]*gnmic.PipelineData)
	// Pipelines left out of the plan because a ref did not resolve. Used below to tell
	// "the plan is empty because nothing is configured" apart from "the plan is empty
	// because everything was skipped", which need opposite handling.
	skippedPipelines := 0

	for _, pipeline := range pipelines {
		if !pipeline.Spec.Enabled {
			continue
		}
		logger.Info("cluster pipeline", "pipeline", pipeline.Name, "enabled", pipeline.Spec.Enabled)
		pipelineNN := pipeline.Namespace + gnmic.Delimiter + pipeline.Name
		pipelineData := gnmic.NewPipelineData()

		// Refs naming a resource that does not exist. Every resolver below appends to
		// this and it is acted on once, after resolution finishes, so the status lists
		// all of them rather than whichever happened to be checked first.
		var unresolvedRefs []string

		// retrieve targets for this pipeline
		targets, unresolved, err := r.resolveTargets(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
		unresolvedRefs = append(unresolvedRefs, unresolved...)
		targetProfilesNames := make(map[string]struct{})
		for _, target := range targets {
			pipelineData.Targets[target.Namespace+gnmic.Delimiter+target.Name] = target
			targetProfilesNames[target.Spec.Profile] = struct{}{}
		}

		// retrieve target profiles for targets in this pipeline
		//
		// A missing profile used to fail the entire reconcile, which stalled every
		// other pipeline on the cluster over one bad name. It is now treated as the
		// dangling ref it is: this pipeline is skipped, the rest keep reconciling.
		for targetProfileName := range targetProfilesNames {
			var targetProfile gnmicv1alpha1.TargetProfile
			if err := r.Get(ctx, types.NamespacedName{Name: targetProfileName, Namespace: pipeline.Namespace}, &targetProfile); err != nil {
				if !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
				unresolvedRefs = append(unresolvedRefs, unresolvedRef("targetprofile", targetProfileName))
				continue
			}
			pipelineData.TargetProfiles[targetProfile.Namespace+gnmic.Delimiter+targetProfile.Name] = targetProfile.Spec
		}
		logger.Info("cluster pipeline resolved targets", "count", len(targets), "targetProfiles", len(targetProfilesNames))

		// retrieve subscriptions for this pipeline
		subscriptions, unresolved, err := r.resolveSubscriptions(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
		unresolvedRefs = append(unresolvedRefs, unresolved...)
		for _, subscription := range subscriptions {
			// Key by pipeline like outputs so two pipelines sharing one
			// Subscription CR each get their own output binding. A flat
			// namespace/name key merges both pipelines' outputs onto every
			// target that uses the subscription.
			pipelineData.Subscriptions[pipelineNN+gnmic.Delimiter+subscription.Name] = subscription.Spec
		}
		logger.Info("cluster pipeline resolved subscriptions", "count", len(subscriptions))

		// retrieve outputs for this pipeline
		outputs, unresolved, err := r.resolveOutputs(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
		unresolvedRefs = append(unresolvedRefs, unresolved...)
		for _, output := range outputs {
			outputNN := pipelineNN + gnmic.Delimiter + output.Name
			pipelineData.Outputs[outputNN] = output.Spec

			// resolve service addresses for outputs that support it (nats, kafka, jetstream)
			if gnmic.OutputTypesWithServiceRef[output.Spec.Type] {
				resolvedAddrs, err := r.resolveOutputServiceAddresses(ctx, &output)
				if err != nil {
					logger.Error(err, "failed to resolve service addresses for output", "output", output.Name)
					// continue without resolved addresses - the output config may have static address
				} else if len(resolvedAddrs) > 0 {
					pipelineData.ResolvedOutputAddresses[outputNN] = resolvedAddrs
				}
			}
		}
		logger.Info("cluster pipeline resolved outputs", "count", len(outputs))

		// retrieve inputs for this pipeline
		inputs, unresolved, err := r.resolveInputs(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
		unresolvedRefs = append(unresolvedRefs, unresolved...)
		for _, input := range inputs {
			pipelineData.Inputs[pipelineNN+gnmic.Delimiter+input.Name] = input.Spec
		}
		logger.Info("cluster pipeline resolved inputs", "count", len(inputs))

		// retrieve output processors for this pipeline (order: refs first, then sorted selectors)
		outputProcessors, unresolved, err := r.resolveOutputProcessors(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
		unresolvedRefs = append(unresolvedRefs, unresolved...)
		for _, processor := range outputProcessors {
			processorNN := pipelineNN + gnmic.Delimiter + processor.Name
			pipelineData.OutputProcessors[processorNN] = processor.Spec
			pipelineData.OutputProcessorOrder = append(pipelineData.OutputProcessorOrder, processorNN)
		}
		logger.Info("cluster pipeline resolved output processors", "count", len(outputProcessors))

		// retrieve input processors for this pipeline (order: refs first, then sorted selectors)
		inputProcessors, unresolved, err := r.resolveInputProcessors(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
		unresolvedRefs = append(unresolvedRefs, unresolved...)
		for _, processor := range inputProcessors {
			processorNN := pipelineNN + gnmic.Delimiter + processor.Name
			pipelineData.InputProcessors[processorNN] = processor.Spec
			pipelineData.InputProcessorOrder = append(pipelineData.InputProcessorOrder, processorNN)
		}
		logger.Info("cluster pipeline resolved input processors", "count", len(inputProcessors))

		// retrieve tunnel target policies for this pipeline
		tunnelTargetPolicies, unresolved, err := r.resolveTunnelTargetPolicies(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
		unresolvedRefs = append(unresolvedRefs, unresolved...)
		// validate: if pipeline has tunnel target policies, cluster must have GRPCTunnel configured
		if len(tunnelTargetPolicies) > 0 && cluster.Spec.GRPCTunnel == nil {
			logger.Error(nil, "pipeline has tunnel target policies but cluster has no gRPC tunnel configured",
				"pipeline", pipeline.Name, "cluster", cluster.Name)
			// update pipeline status with error
			if err := r.updatePipelineStatusWithError(ctx, &pipeline,
				"ClusterMissingTunnel",
				fmt.Sprintf("Cluster %s does not have gRPC tunnel configured, but pipeline references tunnel target policies", cluster.Name),
			); err != nil {
				logger.Error(err, "failed to update pipeline status with error")
			}
			continue // skip this pipeline
		}
		tunnelProfileNames := make(map[string]struct{})
		for _, policy := range tunnelTargetPolicies {
			pipelineData.TunnelTargetPolicies[policy.Namespace+gnmic.Delimiter+policy.Name] = policy.Spec
			if policy.Spec.Profile != "" {
				tunnelProfileNames[policy.Spec.Profile] = struct{}{}
			}
		}
		// retrieve target profiles for tunnel target policies (they share TargetProfiles)
		for profileName := range tunnelProfileNames {
			if _, exists := pipelineData.TargetProfiles[pipeline.Namespace+gnmic.Delimiter+profileName]; exists {
				continue // already fetched for targets
			}
			var targetProfile gnmicv1alpha1.TargetProfile
			if err := r.Get(ctx, types.NamespacedName{Name: profileName, Namespace: pipeline.Namespace}, &targetProfile); err != nil {
				if !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
				unresolvedRefs = append(unresolvedRefs, unresolvedRef("targetprofile", profileName))
				continue
			}
			pipelineData.TargetProfiles[targetProfile.Namespace+gnmic.Delimiter+targetProfile.Name] = targetProfile.Spec
		}
		logger.Info("cluster pipeline tunnel target policies", "policies", len(tunnelTargetPolicies))

		// A ref that does not resolve means the configuration that would reach the
		// collectors is not the one that was asked for: an output the data never
		// arrives at, a processor stage that silently stops filtering. Leave the
		// pipeline out of the plan rather than apply a partial version of it, and say
		// which refs on the Pipeline itself. Other pipelines are unaffected.
		if len(unresolvedRefs) > 0 {
			sort.Strings(unresolvedRefs)
			skippedPipelines++
			logger.Info("skipping pipeline with unresolved references",
				"pipeline", pipeline.Name, "unresolved", unresolvedRefs)
			if err := r.updatePipelineStatus(ctx, &pipeline, pipelineData, unresolvedRefs); err != nil {
				logger.Error(err, "failed to update pipeline status", "pipeline", pipeline.Name)
			}
			continue
		}

		planBuilder.AddPipeline(pipelineNN, pipelineData)
		pipelineDataMap[pipelineNN] = pipelineData

		// update pipeline status
		if err := r.updatePipelineStatus(ctx, &pipeline, pipelineData, nil); err != nil {
			logger.Error(err, "failed to update pipeline status", "pipeline", pipeline.Name)
			// don't return, continue with other pipelines
		}
	}

	// build the apply plan
	applyPlan, err := planBuilder.Build()
	if err != nil {
		return ctrl.Result{}, err
	}
	r.m.Lock()
	r.plans[cluster.Namespace+"/"+cluster.Name] = applyPlan
	r.m.Unlock()

	// A pipeline's targets and subscriptions are resolved independently
	// (resolveTargets vs resolveSubscriptions), each with its own List/Get
	// calls against the informer cache. A Subscription being deleted and
	// recreated (or a selector momentarily not matching due to cache lag)
	// can leave a brief window where a pipeline's targets resolve but its
	// subscriptions come back empty. gNMIc's config/apply rejects any
	// request with targets but zero subscriptions outright (400), and
	// applying that would tear down the streams already running on the
	// pods for no reason. Skip this apply and requeue fast instead of
	// sending it: the existing config on the pods is left untouched, and
	// the next pass a moment later almost always sees a consistent read.
	if len(applyPlan.Targets) > 0 && len(applyPlan.Subscriptions) == 0 {
		logger.Info("apply plan has targets but no subscriptions, skipping apply (likely a transient cache read) and requeueing")
		return ctrl.Result{RequeueAfter: 250 * time.Millisecond}, nil
	}

	// Every pipeline on this cluster was skipped for unresolved references, so the
	// plan is empty for a reason that has nothing to do with what the user asked for.
	// An empty plan is applied deliberately elsewhere -- it is how collectors stop
	// streaming once the last Pipeline is deleted -- so pushing this one would drain
	// the whole cluster over a name that usually resolves a moment later.
	//
	// Only the apply is suppressed. Returning outright also stopped Prometheus
	// Service garbage collection and froze the Cluster's status counters, so
	// deleting an Output before the Pipeline naming it left an orphaned Service
	// behind and a status that no longer described anything.
	suppressApply := skippedPipelines > 0 && len(pipelineDataMap) == 0
	if suppressApply {
		logger.Info("all pipelines skipped for unresolved references, leaving the collectors' current config in place",
			"pipelines", len(pipelines), "skipped", skippedPipelines)
	}

	// reconcile Prometheus output services
	if err := r.reconcilePrometheusServices(ctx, &cluster, pipelineDataMap, applyPlan.PrometheusPorts); err != nil {
		logger.Error(err, "failed to reconcile Prometheus output services")
		return ctrl.Result{}, err
	}

	desiredReplicas := ptr.Deref(statefulSet.Spec.Replicas, 0)
	// only apply new config when all desired replicas are ready
	if statefulSet.Status.ReadyReplicas < desiredReplicas {
		logger.Info("waiting for gNMIc pods to be ready before applying config",
			"readyReplicas", statefulSet.Status.ReadyReplicas, "desiredReplicas", desiredReplicas)
		// The StatefulSet is watched via Owns() with no predicate, so ReadyReplicas
		// transitions already wake this controller. This requeue is only a backstop for a
		// missed event; at 1s it re-ran the entire plan build once per second for the whole
		// duration of a rollout.
		return ctrl.Result{RequeueAfter: readinessBackstopInterval}, nil
	}
	// A reconcile queued while Pipelines were empty can run after a newer
	// reconcile already applied a non-empty plan. Re-list immediately before
	// apply and bail out if membership moved under us.
	if fresh, err := r.listPipelinesForCluster(ctx, &cluster); err != nil {
		return ctrl.Result{}, err
	} else if !pipelineSetEqual(pipelines, fresh) {
		logger.Info("pipelines changed during reconcile, requeueing before apply")
		return ctrl.Result{Requeue: true}, nil
	}
	// send the plan to all gNMIc pods with distributed targets
	// distrubute to desired replicas only, this makes redistribution fast in case of scaling down.
	numPods := int(desiredReplicas)
	configApplied := false
	var configError error
	var unassignedTargets int32
	if suppressApply {
		// Nothing is pushed: the pods keep the configuration they already hold.
	} else if unassigned, err := r.applyConfigToPods(ctx, &cluster, applyPlan, numPods); err != nil {
		logger.Error(err, "failed to apply config to gNMIc pods")
		configError = err
	} else {
		configApplied = true
		unassignedTargets = unassigned
		logger.Info("successfully applied config to gNMIc cluster", "pods", numPods)
	}

	// calculate resource counts from pipelineDataMap
	var totalTargets, totalSubscriptions, totalInputs, totalOutputs int32
	uniqueTargets := make(map[string]struct{})
	uniqueSubscriptions := make(map[string]struct{})
	uniqueInputs := make(map[string]struct{})
	uniqueOutputs := make(map[string]struct{})

	for _, pipelineData := range pipelineDataMap {
		for k := range pipelineData.Targets {
			uniqueTargets[k] = struct{}{}
		}
		for k := range pipelineData.Subscriptions {
			uniqueSubscriptions[k] = struct{}{}
		}
		for k := range pipelineData.Inputs {
			uniqueInputs[k] = struct{}{}
		}
		for k := range pipelineData.Outputs {
			uniqueOutputs[k] = struct{}{}
		}
	}
	totalTargets = int32(len(uniqueTargets))
	totalSubscriptions = int32(len(uniqueSubscriptions))
	totalInputs = int32(len(uniqueInputs))
	totalOutputs = int32(len(uniqueOutputs))

	// update status
	newStatus := gnmicv1alpha1.ClusterStatus{
		ReadyReplicas:      statefulSet.Status.ReadyReplicas,
		Selector:           metav1.FormatLabelSelector(statefulSet.Spec.Selector),
		PipelinesCount:     int32(len(pipelines)),
		TargetsCount:       totalTargets,
		UnassignedTargets:  unassignedTargets,
		SubscriptionsCount: totalSubscriptions,
		InputsCount:        totalInputs,
		OutputsCount:       totalOutputs,
	}

	// set conditions
	now := metav1.Now()

	// ready condition
	readyCondition := metav1.Condition{
		Type:               ConditionTypeReady,
		ObservedGeneration: cluster.Generation,
		LastTransitionTime: now,
	}
	desired := ptr.Deref(cluster.Spec.Replicas, 0)
	if statefulSet.Status.ReadyReplicas >= desired && configApplied {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "ClusterReady"
		readyCondition.Message = fmt.Sprintf("All %d replicas are ready and configured", statefulSet.Status.ReadyReplicas)
	} else if statefulSet.Status.ReadyReplicas > 0 && configApplied {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "ClusterPartiallyReady"
		readyCondition.Message = fmt.Sprintf("%d of %d replicas are ready and configured", statefulSet.Status.ReadyReplicas, cluster.Spec.Replicas)
	} else {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "ClusterNotReady"
		if statefulSet.Status.ReadyReplicas == 0 {
			readyCondition.Message = "Waiting for pods to be ready"
		} else {
			readyCondition.Message = "Configuration not yet applied"
		}
	}
	newStatus.Conditions = append(newStatus.Conditions, readyCondition)

	// certificatesReady condition (only if TLS is configured)
	if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil && cluster.Spec.API.TLS.IssuerRef != "" {
		certCondition := metav1.Condition{
			Type:               ConditionTypeCertificatesReady,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: cluster.Generation,
			LastTransitionTime: now,
			Reason:             "CertificatesIssued",
			Message:            "TLS certificates are ready",
		}
		newStatus.Conditions = append(newStatus.Conditions, certCondition)
	}

	// configApplied condition
	configCondition := metav1.Condition{
		Type:               ConditionTypeConfigApplied,
		ObservedGeneration: cluster.Generation,
		LastTransitionTime: now,
	}
	switch {
	case configApplied:
		configCondition.Status = metav1.ConditionTrue
		configCondition.Reason = "ConfigurationApplied"
		configCondition.Message = fmt.Sprintf("Configuration applied to %d pods", numPods)
	case suppressApply:
		// Distinct from a failed apply: nothing was sent, and what the pods are
		// running is the last configuration that did resolve.
		configCondition.Status = metav1.ConditionFalse
		configCondition.Reason = ReasonUnresolvedReferences
		configCondition.Message = fmt.Sprintf(
			"%d pipeline(s) have unresolved references; the collectors keep their current configuration", skippedPipelines)
	default:
		configCondition.Status = metav1.ConditionFalse
		configCondition.Reason = "ConfigurationFailed"
		if configError != nil {
			configCondition.Message = fmt.Sprintf("Failed to apply configuration: %v", configError)
		} else {
			configCondition.Message = "Waiting for pods to be ready"
		}
	}
	newStatus.Conditions = append(newStatus.Conditions, configCondition)

	// capacityExhausted condition
	if unassignedTargets > 0 {
		newStatus.Conditions = append(newStatus.Conditions, metav1.Condition{
			Type:               ConditionTypeCapacityExhausted,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: cluster.Generation,
			LastTransitionTime: now,
			Reason:             "InsufficientCapacity",
			Message:            fmt.Sprintf("%d target(s) could not be assigned, all pods at capacity", unassignedTargets),
		})
	} else if configApplied {
		newStatus.Conditions = append(newStatus.Conditions, metav1.Condition{
			Type:               ConditionTypeCapacityExhausted,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: cluster.Generation,
			LastTransitionTime: now,
			Reason:             "SufficientCapacity",
			Message:            "All targets assigned",
		})
	}

	// preserve LastTransitionTime for unchanged conditions
	for i := range newStatus.Conditions {
		for _, oldCond := range cluster.Status.Conditions {
			if oldCond.Type == newStatus.Conditions[i].Type &&
				oldCond.Status == newStatus.Conditions[i].Status {
				newStatus.Conditions[i].LastTransitionTime = oldCond.LastTransitionTime
				break
			}
		}
	}
	// Re-fetch before comparing: a concurrent reconcile may have already
	// written a newer status, and comparing against the start-of-reconcile
	// copy can skip a needed update (e.g. stale empty reconcile sees
	// pipelinesCount=0 in-memory while the live status is still 1).
	clusterNN := types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}
	if err := r.Get(ctx, clusterNN, &cluster); err != nil {
		return ctrl.Result{}, err
	}
	if !clusterStatusEqual(cluster.Status, newStatus) {
		var statusErr error
		for attempt := 0; attempt < 5; attempt++ {
			if attempt > 0 {
				if err := r.Get(ctx, clusterNN, &cluster); err != nil {
					statusErr = err
					break
				}
			}
			cluster.Status = newStatus
			if err := r.Status().Update(ctx, &cluster); err != nil {
				if apierrors.IsConflict(err) {
					statusErr = err
					continue
				}
				statusErr = err
				break
			}
			statusErr = nil
			break
		}
		if statusErr != nil {
			logger.Error(statusErr, "failed to update cluster status")
			return ctrl.Result{}, statusErr
		}
	}

	if configError != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	// An empty apply from a briefly stale cache can race a Pipeline create.
	// Requeue once so the next pass sees the live membership and restores
	// config if needed; non-empty applies are left alone.
	if configApplied && len(pipelines) == 0 {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	return ctrl.Result{RequeueAfter: reconcileBackstopInterval}, nil
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
