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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
	"github.com/gnmic/operator/internal/utils"
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	m *sync.RWMutex
	// key is namespace/name of the cluster
	// value is the apply plan for the cluster
	plans map[string]*gnmic.ApplyPlan
}

const (
	resourcePrefix    = "gnmic-"
	clusterFinalizer  = "operator.gnmic.dev/cluster-finalizer"
	defaultRestPort   = 7890
	controllerCACMSfx = "-controller-ca"
)

// Condition types for Cluster status
const (
	// ConditionTypeReady indicates the cluster is fully operational
	ConditionTypeReady = "Ready"
	// ConditionTypeCertificatesReady indicates TLS certificates are ready
	ConditionTypeCertificatesReady = "CertificatesReady"
	// ConditionTypeConfigApplied indicates configuration was applied to pods
	ConditionTypeConfigApplied = "ConfigApplied"
)

// Condition types for Pipeline status
const (
	// PipelineConditionTypeReady indicates the pipeline is active and has resources
	PipelineConditionTypeReady = "Ready"
	// PipelineConditionTypeResourcesResolved indicates all resources were resolved
	PipelineConditionTypeResourcesResolved = "ResourcesResolved"
)

// fetchCredentials fetches credentials from a secret
func (r *ClusterReconciler) FetchCredentials(namespace, secretRef string) (*gnmic.Credentials, error) {
	var secret corev1.Secret
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Get(ctx, types.NamespacedName{Name: secretRef, Namespace: namespace}, &secret); err != nil {
		return nil, err
	}
	creds := &gnmic.Credentials{}
	if secret.Data["username"] != nil {
		creds.Username = string(secret.Data["username"])
	}
	if secret.Data["password"] != nil {
		creds.Password = string(secret.Data["password"])
	}
	if secret.Data["token"] != nil {
		creds.Token = string(secret.Data["token"])
	}
	return creds, nil
}

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
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=pods,verbs=list;watch
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var cluster gnmicv1alpha1.Cluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		// if the Cluster CR was deleted before we reconciled, cleanup related resources
		if errors.IsNotFound(err) {
			prefixedNN := types.NamespacedName{
				Name:      resourcePrefix + req.Name,
				Namespace: req.Namespace,
			}
			// cleanup statefulset
			if cleanupErr := r.ensureStatefulSetAbsent(ctx, prefixedNN); cleanupErr != nil {
				return ctrl.Result{}, cleanupErr
			}
			// cleanup headless service
			if cleanupErr := r.ensureServiceAbsent(ctx, prefixedNN); cleanupErr != nil {
				return ctrl.Result{}, cleanupErr
			}
			// clean up Prometheus output services
			if cleanupErr := r.cleanupPrometheusServices(ctx, &cluster); cleanupErr != nil {
				return ctrl.Result{}, err
			}
			// cleanup plan
			r.cleanupPlan(cluster.Namespace, cluster.Name)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger = logger.WithValues("cluster", cluster.Name, "namespace", cluster.Namespace)

	// handle deletion with a finalizer to guarantee statefulset, headless service and Prometheus output services cleanup
	if !cluster.DeletionTimestamp.IsZero() {
		// cleanup plan
		r.cleanupPlan(cluster.Namespace, cluster.Name)
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
			if cleanupErr := r.cleanupPrometheusServices(ctx, &cluster); cleanupErr != nil {
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
	planBuilder := gnmic.NewPlanBuilder(r).
		WithClientTLS(
			gnmic.ClientTLSConfigForCluster(&cluster),
		)
	pipelineDataMap := make(map[string]*gnmic.PipelineData)

	for _, pipeline := range pipelines {
		if !pipeline.Spec.Enabled {
			continue
		}
		logger.Info("cluster pipeline", "pipeline", pipeline.Name, "enabled", pipeline.Spec.Enabled)
		pipelineNN := pipeline.Namespace + gnmic.Delimiter + pipeline.Name
		pipelineData := gnmic.NewPipelineData()

		// retrieve targets for this pipeline
		targets, err := r.resolveTargets(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
		targetProfilesNames := make(map[string]struct{})
		for _, target := range targets {
			pipelineData.Targets[target.Namespace+gnmic.Delimiter+target.Name] = target.Spec
			targetProfilesNames[target.Spec.Profile] = struct{}{}
		}

		// retrieve target profiles for targets in this pipeline
		for targetProfileName := range targetProfilesNames {
			var targetProfile gnmicv1alpha1.TargetProfile
			if err := r.Get(ctx, types.NamespacedName{Name: targetProfileName, Namespace: pipeline.Namespace}, &targetProfile); err != nil {
				return ctrl.Result{}, err
			}
			pipelineData.TargetProfiles[targetProfile.Namespace+gnmic.Delimiter+targetProfile.Name] = targetProfile.Spec
		}
		logger.Info("cluster pipeline targets", "targets", targets)
		logger.Info("cluster pipeline target profiles", "targetProfiles", targetProfilesNames)

		// retrieve subscriptions for this pipeline
		subscriptions, err := r.resolveSubscriptions(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
		for _, subscription := range subscriptions {
			pipelineData.Subscriptions[subscription.Namespace+gnmic.Delimiter+subscription.Name] = subscription.Spec
		}
		logger.Info("cluster pipeline subscriptions", "subscriptions", subscriptions)

		// retrieve outputs for this pipeline
		outputs, err := r.resolveOutputs(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
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
		logger.Info("cluster pipeline outputs", "outputs", outputs)

		// retrieve inputs for this pipeline
		inputs, err := r.resolveInputs(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
		for _, input := range inputs {
			pipelineData.Inputs[pipelineNN+gnmic.Delimiter+input.Name] = input.Spec
		}
		logger.Info("cluster pipeline inputs", "inputs", inputs)

		// retrieve output processors for this pipeline (order: refs first, then sorted selectors)
		outputProcessors, err := r.resolveOutputProcessors(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
		for _, processor := range outputProcessors {
			processorNN := pipelineNN + gnmic.Delimiter + processor.Name
			pipelineData.OutputProcessors[processorNN] = processor.Spec
			pipelineData.OutputProcessorOrder = append(pipelineData.OutputProcessorOrder, processorNN)
		}
		logger.Info("cluster pipeline output processors", "outputProcessors", outputProcessors)

		// retrieve input processors for this pipeline (order: refs first, then sorted selectors)
		inputProcessors, err := r.resolveInputProcessors(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
		for _, processor := range inputProcessors {
			processorNN := pipelineNN + gnmic.Delimiter + processor.Name
			pipelineData.InputProcessors[processorNN] = processor.Spec
			pipelineData.InputProcessorOrder = append(pipelineData.InputProcessorOrder, processorNN)
		}
		logger.Info("cluster pipeline input processors", "inputProcessors", inputProcessors)

		// retrieve tunnel target policies for this pipeline
		tunnelTargetPolicies, err := r.resolveTunnelTargetPolicies(ctx, &pipeline)
		if err != nil {
			return ctrl.Result{}, err
		}
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
				if !errors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
				logger.Info("target profile not found for tunnel target policy, skipping", "profile", profileName)
				continue
			}
			pipelineData.TargetProfiles[targetProfile.Namespace+gnmic.Delimiter+targetProfile.Name] = targetProfile.Spec
		}
		logger.Info("cluster pipeline tunnel target policies", "policies", len(tunnelTargetPolicies))

		planBuilder.AddPipeline(pipelineNN, pipelineData)
		pipelineDataMap[pipelineNN] = pipelineData

		// update pipeline status
		if err := r.updatePipelineStatus(ctx, &pipeline, pipelineData); err != nil {
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

	// reconcile Prometheus output services
	if err := r.reconcilePrometheusServices(ctx, &cluster, pipelineDataMap, applyPlan.PrometheusPorts); err != nil {
		logger.Error(err, "failed to reconcile Prometheus output services")
		return ctrl.Result{}, err
	}

	// only apply config if there's at least one ready pod
	// TODO: wait for all pods ?
	if statefulSet.Status.ReadyReplicas == 0 {
		logger.Info("waiting for gNMIc pods to be ready before applying config")
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	// send the plan to all gNMIc pods with distributed targets
	numPods := int(statefulSet.Status.ReadyReplicas)
	configApplied := false
	var configError error
	if err := r.applyConfigToPods(ctx, &cluster, applyPlan, numPods); err != nil {
		logger.Error(err, "failed to apply config to gNMIc pods")
		configError = err
	} else {
		configApplied = true
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
		PipelinesCount:     int32(len(pipelines)),
		TargetsCount:       totalTargets,
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
	if statefulSet.Status.ReadyReplicas >= *cluster.Spec.Replicas && configApplied {
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
	if configApplied {
		configCondition.Status = metav1.ConditionTrue
		configCondition.Reason = "ConfigurationApplied"
		configCondition.Message = fmt.Sprintf("Configuration applied to %d pods", numPods)
	} else {
		configCondition.Status = metav1.ConditionFalse
		configCondition.Reason = "ConfigurationFailed"
		if configError != nil {
			configCondition.Message = fmt.Sprintf("Failed to apply configuration: %v", configError)
		} else {
			configCondition.Message = "Waiting for pods to be ready"
		}
	}
	newStatus.Conditions = append(newStatus.Conditions, configCondition)

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

	// update status if changed
	if !clusterStatusEqual(cluster.Status, newStatus) {
		cluster.Status = newStatus
		if err := r.Status().Update(ctx, &cluster); err != nil {
			logger.Error(err, "failed to update cluster status")
			return ctrl.Result{}, err
		}
	}

	if configError != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// clusterStatusEqual compares two ClusterStatus structs for equality
func clusterStatusEqual(a, b gnmicv1alpha1.ClusterStatus) bool {
	if a.ReadyReplicas != b.ReadyReplicas ||
		a.PipelinesCount != b.PipelinesCount ||
		a.TargetsCount != b.TargetsCount ||
		a.SubscriptionsCount != b.SubscriptionsCount ||
		a.InputsCount != b.InputsCount ||
		a.OutputsCount != b.OutputsCount {
		return false
	}
	if len(a.Conditions) != len(b.Conditions) {
		return false
	}
	for i := range a.Conditions {
		if a.Conditions[i].Type != b.Conditions[i].Type ||
			a.Conditions[i].Status != b.Conditions[i].Status ||
			a.Conditions[i].Reason != b.Conditions[i].Reason ||
			a.Conditions[i].Message != b.Conditions[i].Message {
			return false
		}
	}
	return true
}

// applyConfigToPods sends the apply plan to all gNMIc pods with distributed targets
func (r *ClusterReconciler) applyConfigToPods(ctx context.Context, cluster *gnmicv1alpha1.Cluster, plan *gnmic.ApplyPlan, numPods int) error {
	logger := log.FromContext(ctx)

	stsName := fmt.Sprintf("%s%s", resourcePrefix, cluster.Name)

	restPort := int32(defaultRestPort)
	if cluster.Spec.API != nil && cluster.Spec.API.RestPort != 0 {
		restPort = cluster.Spec.API.RestPort
	}
	// create an HTTP client to send the apply plan to the gNMIc pods
	httpClient, err := r.createHTTPClientForCluster(ctx, cluster)
	if err != nil {
		return fmt.Errorf("failed to create HTTP client: %w", err)
	}
	// apply config to each pod with distributed targets
	for podIndex := 0; podIndex < numPods; podIndex++ {
		// distribute targets for this pod
		distributedPlan := gnmic.DistributeTargets(plan, podIndex, numPods)

		// build the URL for this pod
		// statefulSet pods have predictable DNS names:
		//  <statefulset-name>-<ordinal>.<service-name>.<namespace>.svc.cluster.local
		podDNS := fmt.Sprintf("%s-%d.%s.%s.svc.cluster.local", stsName, podIndex, stsName, cluster.Namespace)
		scheme := "http"
		if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil && cluster.Spec.API.TLS.IssuerRef != "" {
			scheme = "https"
		}
		url := fmt.Sprintf("%s://%s:%d/api/v1/config/apply", scheme, podDNS, restPort)
		logger.Info("sending config to gNMIc pod", "url", url)
		if err := r.sendApplyRequest(ctx, url, distributedPlan, httpClient); err != nil {
			return fmt.Errorf("failed to apply config to pod %d: %w", podIndex, err)
		}

		logger.Info("config applied to pod", "pod", podIndex, "targets", len(distributedPlan.Targets))
	}

	return nil
}

func (r *ClusterReconciler) createHTTPClientForCluster(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (*http.Client, error) {
	if cluster.Spec.API == nil || cluster.Spec.API.TLS == nil {
		return &http.Client{
			Timeout: 30 * time.Second,
		}, nil
	}
	tlsConfig := &tls.Config{}
	if cluster.Spec.API.TLS.IssuerRef != "" {
		// load controller's client certificate for mTLS
		cert, err := os.ReadFile(gnmic.GetControllerCertPath())
		if err != nil {
			return nil, fmt.Errorf("failed to read controller cert file: %w", err)
		}
		key, err := os.ReadFile(gnmic.GetControllerKeyPath())
		if err != nil {
			return nil, fmt.Errorf("failed to read controller key file: %w", err)
		}
		certificate, err := tls.X509KeyPair(cert, key)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}

		// fetch the CA from the Issuer's secret to verify gNMIc pod certificates
		ca, err := r.getIssuerCA(ctx, cluster.Namespace, cluster.Spec.API.TLS.IssuerRef)
		if err != nil {
			return nil, fmt.Errorf("failed to get issuer CA: %w", err)
		}
		tlsConfig.RootCAs = x509.NewCertPool()
		tlsConfig.RootCAs.AppendCertsFromPEM(ca)
	}
	if cluster.Spec.API.TLS.BundleRef != "" {
		// load additional CA bundle to verify gNMIc pod server certificates
		ca, err := os.ReadFile(gnmic.GetControllerCAPath())
		if err != nil {
			return nil, fmt.Errorf("failed to read controller ca file: %w", err)
		}
		if tlsConfig.RootCAs == nil {
			tlsConfig.RootCAs = x509.NewCertPool()
		}
		tlsConfig.RootCAs.AppendCertsFromPEM(ca)
	}
	return &http.Client{
			Timeout: 30 * time.Second, // TODO: make configurable ?
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
		nil
}

// getIssuerCA fetches the CA certificate from a cert-manager Issuer's backing secret
func (r *ClusterReconciler) getIssuerCA(ctx context.Context, namespace, issuerName string) ([]byte, error) {
	// get the Issuer
	issuer := &certmanagerv1.Issuer{}
	if err := r.Get(ctx, types.NamespacedName{Name: issuerName, Namespace: namespace}, issuer); err != nil {
		return nil, fmt.Errorf("failed to get issuer %s: %w", issuerName, err)
	}

	// the Issuer should be a CA issuer with a secretName
	if issuer.Spec.CA == nil || issuer.Spec.CA.SecretName == "" {
		return nil, fmt.Errorf("issuer %s is not a CA issuer or has no secret configured", issuerName)
	}

	// get the CA secret
	caSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: issuer.Spec.CA.SecretName, Namespace: namespace}, caSecret); err != nil {
		return nil, fmt.Errorf("failed to get CA secret %s: %w", issuer.Spec.CA.SecretName, err)
	}

	// the CA certificate is stored in tls.crt
	caCert, ok := caSecret.Data["tls.crt"]
	if !ok {
		return nil, fmt.Errorf("CA secret %s does not contain tls.crt", issuer.Spec.CA.SecretName)
	}

	return caCert, nil
}

// sendApplyRequest sends an apply plan to a single gNMIc pod
func (r *ClusterReconciler) sendApplyRequest(ctx context.Context, url string, plan *gnmic.ApplyPlan, httpClient *http.Client) error {
	logger := log.FromContext(ctx)

	// marshal the plan to JSON
	jsonData, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("failed to marshal apply plan: %w", err)
	}

	logger.Info("sending config to gNMIc pod", "url", url, "payloadSize", len(jsonData))

	// create the request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// send the request
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// TODO: stream read the body to avoid loading it all into memory
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}
		logger.Error(err, "gNMIc pod returned non-success status", "status", resp.StatusCode, "body", string(body))
		return err
	}

	return nil
}

// generationOrLabelsChangedPredicate triggers reconciliation when either:
// - The resource's generation changes (spec changes)
// - The resource's labels change
type generationOrLabelsChangedPredicate struct {
	predicate.Funcs
}

func (p generationOrLabelsChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}
	// trigger on generation change (spec change)
	if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
		return true
	}
	// trigger on label change
	return !maps.Equal(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels())
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
		Complete(r)
}

// findClusterForPipeline returns a reconcile request for the Cluster referenced by the Pipeline
func (r *ClusterReconciler) findClusterForPipeline(ctx context.Context, obj client.Object) []reconcile.Request {
	pipeline, ok := obj.(*gnmicv1alpha1.Pipeline)
	if !ok {
		return nil
	}
	if pipeline.Spec.ClusterRef == "" {
		return nil
	}
	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      pipeline.Spec.ClusterRef,
				Namespace: pipeline.Namespace,
			},
		},
	}
}

// findClustersForTarget finds all Clusters that have Pipelines referencing this Target
func (r *ClusterReconciler) findClustersForTarget(ctx context.Context, obj client.Object) []reconcile.Request {
	target, ok := obj.(*gnmicv1alpha1.Target)
	if !ok {
		return nil
	}
	return r.findClustersReferencingResource(ctx, target.Namespace, target.Name, target.Labels, "target")
}

// findClustersForSubscription finds all Clusters that have Pipelines referencing this Subscription
func (r *ClusterReconciler) findClustersForSubscription(ctx context.Context, obj client.Object) []reconcile.Request {
	subscription, ok := obj.(*gnmicv1alpha1.Subscription)
	if !ok {
		return nil
	}
	return r.findClustersReferencingResource(ctx, subscription.Namespace, subscription.Name, subscription.Labels, "subscription")
}

// findClustersForOutput finds all Clusters that have Pipelines referencing this Output
func (r *ClusterReconciler) findClustersForOutput(ctx context.Context, obj client.Object) []reconcile.Request {
	output, ok := obj.(*gnmicv1alpha1.Output)
	if !ok {
		return nil
	}
	return r.findClustersReferencingResource(ctx, output.Namespace, output.Name, output.Labels, "output")
}

// findClustersForInput finds all Clusters that have Pipelines referencing this Input
func (r *ClusterReconciler) findClustersForInput(ctx context.Context, obj client.Object) []reconcile.Request {
	input, ok := obj.(*gnmicv1alpha1.Input)
	if !ok {
		return nil
	}
	return r.findClustersReferencingResource(ctx, input.Namespace, input.Name, input.Labels, "input")
}

// findClustersForProcessor finds all Clusters that have Pipelines referencing this Processor
func (r *ClusterReconciler) findClustersForProcessor(ctx context.Context, obj client.Object) []reconcile.Request {
	processor, ok := obj.(*gnmicv1alpha1.Processor)
	if !ok {
		return nil
	}
	// processors can be referenced via output processors or input processors
	outputResults := r.findClustersReferencingResource(ctx, processor.Namespace, processor.Name, processor.Labels, "output-processor")
	inputResults := r.findClustersReferencingResource(ctx, processor.Namespace, processor.Name, processor.Labels, "input-processor")

	// combine and deduplicate
	seen := make(map[types.NamespacedName]struct{})
	var results []reconcile.Request
	for _, req := range outputResults {
		if _, ok := seen[req.NamespacedName]; !ok {
			seen[req.NamespacedName] = struct{}{}
			results = append(results, req)
		}
	}
	for _, req := range inputResults {
		if _, ok := seen[req.NamespacedName]; !ok {
			seen[req.NamespacedName] = struct{}{}
			results = append(results, req)
		}
	}
	return results
}

// findClustersForTargetProfile finds all Clusters that have Pipelines with Targets using this TargetProfile
func (r *ClusterReconciler) findClustersForTargetProfile(ctx context.Context, obj client.Object) []reconcile.Request {
	profile, ok := obj.(*gnmicv1alpha1.TargetProfile)
	if !ok {
		return nil
	}

	// list all targets in the same namespace
	var targetList gnmicv1alpha1.TargetList
	if err := r.List(ctx, &targetList, client.InNamespace(profile.Namespace)); err != nil {
		return nil
	}

	// find clusters for each target that uses this profile
	seen := make(map[types.NamespacedName]struct{})
	var results []reconcile.Request

	for _, target := range targetList.Items {
		if target.Spec.Profile != profile.Name {
			continue
		}
		// find clusters referencing this target
		targetResults := r.findClustersReferencingResource(ctx, target.Namespace, target.Name, target.Labels, "target")
		for _, req := range targetResults {
			if _, ok := seen[req.NamespacedName]; !ok {
				seen[req.NamespacedName] = struct{}{}
				results = append(results, req)
			}
		}
	}

	return results
}

// findClustersForTunnelTargetPolicy finds all Clusters that have Pipelines referencing this TunnelTargetPolicy
func (r *ClusterReconciler) findClustersForTunnelTargetPolicy(ctx context.Context, obj client.Object) []reconcile.Request {
	policy, ok := obj.(*gnmicv1alpha1.TunnelTargetPolicy)
	if !ok {
		return nil
	}
	return r.findClustersReferencingResource(ctx, policy.Namespace, policy.Name, policy.Labels, "tunnel-target-policy")
}

// findClustersReferencingResource finds Clusters whose Pipelines reference the given resource
func (r *ClusterReconciler) findClustersReferencingResource(ctx context.Context, namespace, name string, resourceLabels map[string]string, resourceType string) []reconcile.Request {
	var pipelineList gnmicv1alpha1.PipelineList
	if err := r.List(ctx, &pipelineList, client.InNamespace(namespace)); err != nil {
		return nil
	}

	clusterSet := make(map[string]struct{})
	for _, pipeline := range pipelineList.Items {
		if !pipeline.Spec.Enabled {
			continue
		}
		// check if pipeline references this resource by name or selector
		if pipelineReferencesResource(&pipeline, name, resourceLabels, resourceType) {
			clusterSet[pipeline.Spec.ClusterRef] = struct{}{}
		}
	}

	var requests []reconcile.Request
	for clusterName := range clusterSet {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      clusterName,
				Namespace: namespace,
			},
		})
	}
	return requests
}

// pipelineReferencesResource checks if a pipeline references a resource by name or any of its label selectors
func pipelineReferencesResource(pipeline *gnmicv1alpha1.Pipeline, resourceName string, resourceLabels map[string]string, resourceType string) bool {
	var refs []string
	var selectors []metav1.LabelSelector

	switch resourceType {
	case "target":
		refs = pipeline.Spec.TargetRefs
		selectors = pipeline.Spec.TargetSelectors
	case "subscription":
		refs = pipeline.Spec.SubscriptionRefs
		selectors = pipeline.Spec.SubscriptionSelectors
	case "output":
		refs = pipeline.Spec.Outputs.OutputRefs
		selectors = pipeline.Spec.Outputs.OutputSelectors
	case "input":
		refs = pipeline.Spec.Inputs.InputRefs
		selectors = pipeline.Spec.Inputs.InputSelectors
	case "output-processor":
		refs = pipeline.Spec.Outputs.ProcessorRefs
		selectors = pipeline.Spec.Outputs.ProcessorSelectors
	case "input-processor":
		refs = pipeline.Spec.Inputs.ProcessorRefs
		selectors = pipeline.Spec.Inputs.ProcessorSelectors
	case "tunnel-target-policy":
		refs = pipeline.Spec.TunnelTargetPolicyRefs
		selectors = pipeline.Spec.TunnelTargetPolicySelectors
	default:
		return false
	}

	// check direct refs
	if slices.Contains(refs, resourceName) {
		return true
	}

	// check label selectors: any selector matching means the resource is referenced
	for _, selector := range selectors {
		if len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0 {
			continue
		}
		labelSelector, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			continue
		}
		if labelSelector.Matches(labels.Set(resourceLabels)) {
			return true
		}
	}

	return false
}

func (r *ClusterReconciler) reconcileStatefulSet(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (*appsv1.StatefulSet, error) {
	desired, desiredConfigMap, err := r.buildStatefulSet(cluster)
	if err != nil {
		return nil, err
	}

	// reconcile ConfigMap: only update if content changed
	if err := r.reconcileConfigMap(ctx, cluster, desiredConfigMap); err != nil {
		return nil, err
	}

	var current appsv1.StatefulSet
	err = r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, &current)
	if errors.IsNotFound(err) {
		if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
			return nil, err
		}
		return desired, r.Create(ctx, desired)
	}
	if err != nil {
		return nil, err
	}

	// update in-place only the fields we manage
	needsUpdate := false

	if current.Spec.Replicas == nil || *current.Spec.Replicas != *cluster.Spec.Replicas {
		current.Spec.Replicas = cluster.Spec.Replicas
		needsUpdate = true
	}

	if len(current.Spec.Template.Spec.Containers) == 0 {
		current.Spec.Template.Spec.Containers = desired.Spec.Template.Spec.Containers
		needsUpdate = true
	} else {
		container := &current.Spec.Template.Spec.Containers[0]
		if container.Image != cluster.Spec.Image {
			container.Image = cluster.Spec.Image
			needsUpdate = true
		}
		// update resources if changed
		if !resourcesEqual(container.Resources, cluster.Spec.Resources) {
			container.Resources = cluster.Spec.Resources
			needsUpdate = true
		}
	}

	// keep labels in sync for selectors.
	if current.Spec.Template.Labels == nil {
		current.Spec.Template.Labels = map[string]string{}
	}
	for k, v := range desired.Spec.Template.Labels {
		if current.Spec.Template.Labels[k] != v {
			current.Spec.Template.Labels[k] = v
			needsUpdate = true
		}
	}
	if current.Spec.Selector == nil {
		current.Spec.Selector = desired.Spec.Selector
		needsUpdate = true
	}

	// update volumes if they changed (needed when scaling with TLS enabled)
	// projected volumes include per-pod certificate secrets, so they change when replicas change
	if !volumesEqual(current.Spec.Template.Spec.Volumes, desired.Spec.Template.Spec.Volumes) {
		current.Spec.Template.Spec.Volumes = desired.Spec.Template.Spec.Volumes
		needsUpdate = true
	}

	if needsUpdate {
		if err := controllerutil.SetControllerReference(cluster, &current, r.Scheme); err != nil {
			return nil, err
		}
		if err := r.Update(ctx, &current); err != nil {
			return nil, err
		}
	}
	return &current, nil
}

// volumesEqual compares two volume slices for equality
// This is a simplified comparison that checks volume names and projected sources count
func volumesEqual(a, b []corev1.Volume) bool {
	if len(a) != len(b) {
		return false
	}
	aMap := make(map[string]corev1.Volume)
	for _, v := range a {
		aMap[v.Name] = v
	}
	for _, vb := range b {
		va, ok := aMap[vb.Name]
		if !ok {
			return false
		}
		// compare projected volume sources count (this is what changes on scale)
		if va.Projected != nil && vb.Projected != nil {
			if len(va.Projected.Sources) != len(vb.Projected.Sources) {
				return false
			}
		} else if (va.Projected == nil) != (vb.Projected == nil) {
			return false
		}
	}
	return true
}

// reconcileConfigMap creates or updates the ConfigMap only if content changed
func (r *ClusterReconciler) reconcileConfigMap(ctx context.Context, cluster *gnmicv1alpha1.Cluster, desired *corev1.ConfigMap) error {
	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	var current corev1.ConfigMap
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, &current)
	if errors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// only update if data changed
	if !maps.Equal(current.Data, desired.Data) {
		current.Data = desired.Data
		return r.Update(ctx, &current)
	}
	return nil
}

// reconcileCertificates creates/updates cert-manager Certificate resources for each pod
// returns true if all certificates are ready, false otherwise
func (r *ClusterReconciler) reconcileCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (bool, error) {
	logger := log.FromContext(ctx)

	if cluster.Spec.API == nil || cluster.Spec.API.TLS == nil || cluster.Spec.API.TLS.IssuerRef == "" {
		return true, nil // TLS not configured, skip
	}

	stsName := fmt.Sprintf("%s%s", resourcePrefix, cluster.Name)
	replicas := *cluster.Spec.Replicas

	allReady := true

	// create/update certificates for each replica
	for i := int32(0); i < replicas; i++ {
		podName := fmt.Sprintf("%s-%d", stsName, i)
		certName := fmt.Sprintf("%s-tls", podName)

		cert := r.buildCertificate(cluster, certName, podName, stsName)

		if err := controllerutil.SetControllerReference(cluster, cert, r.Scheme); err != nil {
			return false, err
		}

		var current certmanagerv1.Certificate
		err := r.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, &current)
		if errors.IsNotFound(err) {
			logger.Info("creating certificate", "certificate", certName)
			if err := r.Create(ctx, cert); err != nil {
				return false, err
			}
			allReady = false
			continue
		}
		if err != nil {
			return false, err
		}

		// check if certificate needs update
		if r.certificateNeedsUpdate(&current, cert) {
			current.Spec = cert.Spec
			if err := r.Update(ctx, &current); err != nil {
				return false, err
			}
		}

		// check if certificate is ready
		if !r.isCertificateReady(&current) {
			logger.Info("certificate not ready", "certificate", certName)
			allReady = false
		}
	}

	// clean up certificates for replicas that no longer exist (scale down)
	var certList certmanagerv1.CertificateList
	if err := r.List(ctx, &certList, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		LabelClusterName: cluster.Name,
	}); err != nil {
		return false, err
	}

	for _, cert := range certList.Items {
		// extract the ordinal from the certificate name (for example: "gnmic-cluster1-2-tls" -> 2)
		ordinal := r.extractOrdinalFromCertName(cert.Name, stsName)
		if ordinal >= int(replicas) {
			logger.Info("deleting certificate for scaled-down replica", "certificate", cert.Name)
			if err := r.Delete(ctx, &cert); err != nil && !errors.IsNotFound(err) {
				return false, err
			}
		}
	}

	return allReady, nil
}

// buildCertificate creates a cert-manager Certificate spec for a pod
func (r *ClusterReconciler) buildCertificate(cluster *gnmicv1alpha1.Cluster, certName, podName, stsName string) *certmanagerv1.Certificate {
	// build DNS names for the certificate
	// pod DNS: <pod-name>.<service-name>.<namespace>.svc.cluster.local
	dnsNames := []string{
		podName,
		fmt.Sprintf("%s.%s", podName, stsName),
		fmt.Sprintf("%s.%s.%s", podName, stsName, cluster.Namespace),
		fmt.Sprintf("%s.%s.%s.svc", podName, stsName, cluster.Namespace),
		fmt.Sprintf("%s.%s.%s.svc.cluster.local", podName, stsName, cluster.Namespace),
	}

	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "gnmic",
				"app.kubernetes.io/managed-by": "gnmic-operator",
				LabelClusterName:               cluster.Name,
			},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: certName,
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: map[string]string{
					LabelClusterName: cluster.Name,
					LabelPodName:     podName,
				},
			},
			IssuerRef: cmmeta.ObjectReference{
				Name: cluster.Spec.API.TLS.IssuerRef,
				Kind: "Issuer", // defaults to Issuer. TODO: configurable to ClusterIssuer ?
			},
			CommonName: podName,
			DNSNames:   dnsNames,
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageServerAuth,
				certmanagerv1.UsageClientAuth,
				certmanagerv1.UsageDigitalSignature,
				certmanagerv1.UsageKeyEncipherment,
			},
		},
	}
}

// certificateNeedsUpdate checks if the certificate spec has changed
func (r *ClusterReconciler) certificateNeedsUpdate(current, desired *certmanagerv1.Certificate) bool {
	if current.Spec.SecretName != desired.Spec.SecretName {
		return true
	}
	if current.Spec.IssuerRef.Name != desired.Spec.IssuerRef.Name {
		return true
	}
	if current.Spec.CommonName != desired.Spec.CommonName {
		return true
	}
	if !slices.Equal(current.Spec.DNSNames, desired.Spec.DNSNames) {
		return true
	}
	return false
}

// isCertificateReady checks if a certificate has the Ready condition set to True
func (r *ClusterReconciler) isCertificateReady(cert *certmanagerv1.Certificate) bool {
	for _, condition := range cert.Status.Conditions {
		if condition.Type == certmanagerv1.CertificateConditionReady {
			return condition.Status == cmmeta.ConditionTrue
		}
	}
	return false
}

// extractOrdinalFromCertName extracts the StatefulSet ordinal from a certificate name
// for example: "gnmic-cluster1-2-tls" with stsName "gnmic-cluster1" returns 2
func (r *ClusterReconciler) extractOrdinalFromCertName(certName, stsName string) int {
	// remove the "-tls" suffix and the stsName prefix
	suffix := strings.TrimPrefix(certName, stsName+"-")
	suffix = strings.TrimSuffix(suffix, "-tls")
	ordinal, err := strconv.Atoi(suffix)
	if err != nil {
		return -1 // invalid
	}
	return ordinal
}

// cleanupCertificates deletes all certificates for a cluster
func (r *ClusterReconciler) cleanupCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	var certList certmanagerv1.CertificateList
	if err := r.List(ctx, &certList, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		LabelClusterName: cluster.Name,
	}); err != nil {
		return client.IgnoreNotFound(err)
	}

	for _, cert := range certList.Items {
		logger.Info("deleting certificate", "certificate", cert.Name)
		if err := r.Delete(ctx, &cert); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// reconcileTunnelCertificates creates/updates cert-manager Certificate resources for tunnel TLS
// returns true if all certificates are ready, false otherwise
func (r *ClusterReconciler) reconcileTunnelCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (bool, error) {
	logger := log.FromContext(ctx)

	if cluster.Spec.GRPCTunnel == nil || cluster.Spec.GRPCTunnel.TLS == nil || cluster.Spec.GRPCTunnel.TLS.IssuerRef == "" {
		return true, nil // tunnel TLS not configured, skip
	}

	stsName := fmt.Sprintf("%s%s", resourcePrefix, cluster.Name)
	replicas := *cluster.Spec.Replicas

	allReady := true

	// create/update certificates for each replica
	for i := int32(0); i < replicas; i++ {
		podName := fmt.Sprintf("%s-%d", stsName, i)
		certName := fmt.Sprintf("%s-tunnel-tls", podName)

		cert := r.buildTunnelCertificate(cluster, certName, podName, stsName)

		if err := controllerutil.SetControllerReference(cluster, cert, r.Scheme); err != nil {
			return false, err
		}

		var current certmanagerv1.Certificate
		err := r.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, &current)
		if errors.IsNotFound(err) {
			logger.Info("creating tunnel certificate", "certificate", certName)
			if err := r.Create(ctx, cert); err != nil {
				return false, err
			}
			allReady = false
			continue
		}
		if err != nil {
			return false, err
		}

		// check if certificate needs update
		if r.certificateNeedsUpdate(&current, cert) {
			current.Spec = cert.Spec
			if err := r.Update(ctx, &current); err != nil {
				return false, err
			}
		}

		// check if certificate is ready
		if !r.isCertificateReady(&current) {
			logger.Info("tunnel certificate not ready", "certificate", certName)
			allReady = false
		}
	}

	// clean up certificates for replicas that no longer exist (scale down)
	var certList certmanagerv1.CertificateList
	if err := r.List(ctx, &certList, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		LabelClusterName: cluster.Name,
		LabelCertType:    LabelValueCertTypeTunnel,
	}); err != nil {
		return false, err
	}

	for _, cert := range certList.Items {
		// extract the ordinal from the certificate name (for example: "gnmic-cluster1-2-tunnel-tls" -> 2)
		ordinal := r.extractOrdinalFromTunnelCertName(cert.Name, stsName)
		if ordinal >= int(replicas) {
			logger.Info("deleting tunnel certificate for scaled-down replica", "certificate", cert.Name)
			if err := r.Delete(ctx, &cert); err != nil && !errors.IsNotFound(err) {
				return false, err
			}
		}
	}

	return allReady, nil
}

// buildTunnelCertificate creates a cert-manager Certificate spec for tunnel TLS
func (r *ClusterReconciler) buildTunnelCertificate(cluster *gnmicv1alpha1.Cluster, certName, podName, stsName string) *certmanagerv1.Certificate {
	// build DNS names for the certificate
	dnsNames := []string{
		podName,
		fmt.Sprintf("%s.%s", podName, stsName),
		fmt.Sprintf("%s.%s.%s", podName, stsName, cluster.Namespace),
		fmt.Sprintf("%s.%s.%s.svc", podName, stsName, cluster.Namespace),
		fmt.Sprintf("%s.%s.%s.svc.cluster.local", podName, stsName, cluster.Namespace),
	}

	// also add the tunnel service DNS names if service is configured
	if cluster.Spec.GRPCTunnel.Service != nil {
		tunnelServiceName := fmt.Sprintf("%s%s-grpc-tunnel", resourcePrefix, cluster.Name)
		dnsNames = append(dnsNames,
			tunnelServiceName,
			fmt.Sprintf("%s.%s", tunnelServiceName, cluster.Namespace),
			fmt.Sprintf("%s.%s.svc", tunnelServiceName, cluster.Namespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", tunnelServiceName, cluster.Namespace),
		)
	}

	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       LabelValueName,
				"app.kubernetes.io/managed-by": LabelValueManagedBy,
				LabelClusterName:               cluster.Name,
				LabelCertType:                  LabelValueCertTypeTunnel,
			},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: certName,
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: map[string]string{
					LabelClusterName: cluster.Name,
					LabelPodName:     podName,
					LabelCertType:    LabelValueCertTypeTunnel,
				},
			},
			IssuerRef: cmmeta.ObjectReference{
				Name: cluster.Spec.GRPCTunnel.TLS.IssuerRef,
				Kind: "Issuer",
			},
			CommonName: podName,
			DNSNames:   dnsNames,
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageServerAuth,
				certmanagerv1.UsageClientAuth,
				certmanagerv1.UsageDigitalSignature,
				certmanagerv1.UsageKeyEncipherment,
			},
		},
	}
}

// extractOrdinalFromTunnelCertName extracts the StatefulSet ordinal from a tunnel certificate name
// e.g., "gnmic-cluster1-2-tunnel-tls" with stsName "gnmic-cluster1" returns 2
func (r *ClusterReconciler) extractOrdinalFromTunnelCertName(certName, stsName string) int {
	// remove the "-tunnel-tls" suffix and the stsName prefix
	suffix := strings.TrimPrefix(certName, stsName+"-")
	suffix = strings.TrimSuffix(suffix, "-tunnel-tls")
	ordinal, err := strconv.Atoi(suffix)
	if err != nil {
		return -1 // Invalid
	}
	return ordinal
}

// cleanupTunnelCertificates deletes all tunnel certificates for a cluster
func (r *ClusterReconciler) cleanupTunnelCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	var certList certmanagerv1.CertificateList
	if err := r.List(ctx, &certList, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		LabelClusterName: cluster.Name,
		LabelCertType:    LabelValueCertTypeTunnel,
	}); err != nil {
		return client.IgnoreNotFound(err)
	}

	for _, cert := range certList.Items {
		logger.Info("deleting tunnel certificate", "certificate", cert.Name)
		if err := r.Delete(ctx, &cert); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// reconcileClientTLSCertificates creates/updates a single cert-manager Certificate for client TLS
// (used by gNMIc to connect to targets with mTLS)
// A single certificate is shared by all pods in the cluster for simplicity and to avoid
// volume changes when scaling.
// returns true if the certificate is ready, false otherwise
func (r *ClusterReconciler) reconcileClientTLSCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (bool, error) {
	logger := log.FromContext(ctx)

	if cluster.Spec.ClientTLS == nil || cluster.Spec.ClientTLS.IssuerRef == "" {
		return true, nil // client TLS not needed, skip
	}

	certName := fmt.Sprintf("%s%s-client-tls", resourcePrefix, cluster.Name)
	cert := r.buildClientTLSCertificate(cluster, certName)

	if err := controllerutil.SetControllerReference(cluster, cert, r.Scheme); err != nil {
		return false, err
	}

	var current certmanagerv1.Certificate
	err := r.Get(ctx, types.NamespacedName{Name: certName, Namespace: cluster.Namespace}, &current)
	if errors.IsNotFound(err) {
		logger.Info("creating client TLS certificate", "certificate", certName)
		if err := r.Create(ctx, cert); err != nil {
			return false, err
		}
		return false, nil // certificate just created, not ready yet
	}
	if err != nil {
		return false, err
	}

	// check if certificate needs update
	if r.certificateNeedsUpdate(&current, cert) {
		current.Spec = cert.Spec
		if err := r.Update(ctx, &current); err != nil {
			return false, err
		}
	}

	// check if certificate is ready
	if !r.isCertificateReady(&current) {
		logger.Info("client TLS certificate not ready", "certificate", certName)
		return false, nil
	}

	return true, nil
}

// buildClientTLSCertificate creates a cert-manager Certificate spec for client TLS
// (used by gNMIc to authenticate to targets)
// Uses cluster-name.namespace as CommonName - shared by all pods in the cluster
func (r *ClusterReconciler) buildClientTLSCertificate(cluster *gnmicv1alpha1.Cluster, certName string) *certmanagerv1.Certificate {
	// Use cluster-name.namespace as the common name for the client certificate
	// This certificate is shared by all pods in the cluster
	commonName := fmt.Sprintf("%s.%s", cluster.Name, cluster.Namespace)

	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       LabelValueName,
				"app.kubernetes.io/managed-by": LabelValueManagedBy,
				LabelClusterName:               cluster.Name,
				LabelCertType:                  LabelValueCertTypeClient,
			},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: certName,
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: map[string]string{
					LabelClusterName: cluster.Name,
					LabelCertType:    LabelValueCertTypeClient,
				},
			},
			IssuerRef: cmmeta.ObjectReference{
				Name: cluster.Spec.ClientTLS.IssuerRef,
				Kind: "Issuer",
			},
			CommonName: commonName,
			DNSNames:   []string{commonName},
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageClientAuth,
				certmanagerv1.UsageDigitalSignature,
				certmanagerv1.UsageKeyEncipherment,
			},
		},
	}
}

// cleanupClientTLSCertificates deletes the client TLS certificate for a cluster
func (r *ClusterReconciler) cleanupClientTLSCertificates(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	certName := fmt.Sprintf("%s%s-client-tls", resourcePrefix, cluster.Name)
	cert := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: cluster.Namespace,
		},
	}

	logger.Info("deleting client TLS certificate", "certificate", certName)
	if err := r.Delete(ctx, cert); err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

// cleanupClientTLSCertificatesLegacy deletes any legacy per-pod client TLS certificates
// This is for backwards compatibility when upgrading from per-pod to single certificate
func (r *ClusterReconciler) cleanupClientTLSCertificatesLegacy(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	var certList certmanagerv1.CertificateList
	if err := r.List(ctx, &certList, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		LabelClusterName: cluster.Name,
		LabelCertType:    LabelValueCertTypeClient,
	}); err != nil {
		return client.IgnoreNotFound(err)
	}

	for _, cert := range certList.Items {
		logger.Info("deleting client TLS certificate", "certificate", cert.Name)
		if err := r.Delete(ctx, &cert); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// reconcileTunnelService creates/updates the gRPC tunnel service for the cluster
func (r *ClusterReconciler) reconcileTunnelService(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	if cluster.Spec.GRPCTunnel == nil {
		return nil // No tunnel configured
	}

	serviceName := fmt.Sprintf("%s%s-grpc-tunnel", resourcePrefix, cluster.Name)

	labels := map[string]string{
		"app.kubernetes.io/name":       LabelValueName,
		"app.kubernetes.io/managed-by": LabelValueManagedBy,
		LabelClusterName:               cluster.Name,
		LabelServiceType:               LabelValueServiceTypeTunnel,
	}
	annotations := map[string]string{}

	// default to LoadBalancer if not specified
	serviceType := corev1.ServiceTypeLoadBalancer

	if cluster.Spec.GRPCTunnel.Service != nil {
		if cluster.Spec.GRPCTunnel.Service.Type != "" {
			serviceType = cluster.Spec.GRPCTunnel.Service.Type
		}
		if len(cluster.Spec.GRPCTunnel.Service.Labels) > 0 {
			maps.Copy(labels, cluster.Spec.GRPCTunnel.Service.Labels)
		}
		if len(cluster.Spec.GRPCTunnel.Service.Annotations) > 0 {
			maps.Copy(annotations, cluster.Spec.GRPCTunnel.Service.Annotations)
		}
	}
	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Selector: map[string]string{
				LabelClusterName: cluster.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "tunnel",
					Port:       cluster.Spec.GRPCTunnel.Port,
					TargetPort: intstr.FromInt32(cluster.Spec.GRPCTunnel.Port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	var current corev1.Service
	err := r.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: cluster.Namespace}, &current)
	if errors.IsNotFound(err) {
		logger.Info("creating tunnel service", "service", serviceName)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// check if service needs update
	if current.Spec.Type != desired.Spec.Type ||
		len(current.Spec.Ports) != len(desired.Spec.Ports) ||
		current.Spec.Ports[0].Port != desired.Spec.Ports[0].Port ||
		!maps.Equal(current.Spec.Selector, desired.Spec.Selector) {
		current.Spec = desired.Spec
		logger.Info("updating tunnel service", "service", serviceName)
		return r.Update(ctx, &current)
	}

	return nil
}

// cleanupTunnelService deletes the tunnel service for a cluster
func (r *ClusterReconciler) cleanupTunnelService(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	serviceName := fmt.Sprintf("%s%s-grpc-tunnel", resourcePrefix, cluster.Name)
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: cluster.Namespace,
		},
	}

	err := r.Delete(ctx, service)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	logger.Info("deleted tunnel service", "service", serviceName)
	return nil
}

// reconcileControllerCA syncs the controller's CA certificate to the cluster's namespace
// this allows gNMIc pods to verify the controller's client certificate during mTLS
func (r *ClusterReconciler) reconcileControllerCA(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	logger := log.FromContext(ctx)

	// read the controller's CA certificate
	caPath := gnmic.GetControllerCAPath()
	caData, err := os.ReadFile(caPath)
	if err != nil {
		// if the CA file doesn't exist, skip (controller TLS not configured)
		if os.IsNotExist(err) {
			logger.Info("controller CA not found, skipping CA sync", "path", caPath)
			return nil
		}
		return fmt.Errorf("failed to read controller CA: %w", err)
	}

	cmName := resourcePrefix + cluster.Name + controllerCACMSfx
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       LabelValueName,
				"app.kubernetes.io/managed-by": LabelValueManagedBy,
				LabelClusterName:               cluster.Name,
			},
		},
		Data: map[string]string{
			"ca.crt": string(caData),
		},
	}

	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	var current corev1.ConfigMap
	err = r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cluster.Namespace}, &current)
	if errors.IsNotFound(err) {
		logger.Info("creating controller CA configmap", "configmap", cmName)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// update only if CA data changed
	if current.Data["ca.crt"] != string(caData) {
		logger.Info("updating controller CA configmap", "configmap", cmName)
		current.Data = desired.Data
		return r.Update(ctx, &current)
	}

	return nil
}

// cleanupControllerCA removes the controller CA configmap when the cluster is deleted
func (r *ClusterReconciler) cleanupControllerCA(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	cmName := resourcePrefix + cluster.Name + controllerCACMSfx
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cluster.Namespace,
		},
	}
	return client.IgnoreNotFound(r.Delete(ctx, cm))
}

func (r *ClusterReconciler) buildConfigMap(cluster *gnmicv1alpha1.Cluster) (*corev1.ConfigMap, error) {
	configMapName := fmt.Sprintf("%s%s-config", resourcePrefix, cluster.Name)

	// build base config content
	content, err := r.buildConfigContent(cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to build config content: %w", err)
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: cluster.Namespace,
		},
		Data: map[string]string{
			gNMIcConfigFile: string(content),
		},
	}, nil
}

// updatePipelineStatus updates the status of a pipeline based on its resolved resources
func (r *ClusterReconciler) updatePipelineStatus(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline, pipelineData *gnmic.PipelineData) error {
	logger := log.FromContext(ctx)

	now := metav1.Now()

	newStatus := gnmicv1alpha1.PipelineStatus{
		Status:                    "Active",
		TargetsCount:              int32(len(pipelineData.Targets)),
		SubscriptionsCount:        int32(len(pipelineData.Subscriptions)),
		InputsCount:               int32(len(pipelineData.Inputs)),
		OutputsCount:              int32(len(pipelineData.Outputs)),
		TunnelTargetPoliciesCount: int32(len(pipelineData.TunnelTargetPolicies)),
	}

	// ready condition
	readyCondition := metav1.Condition{
		Type:               PipelineConditionTypeReady,
		ObservedGeneration: pipeline.Generation,
		LastTransitionTime: now,
	}

	hasTargets := len(pipelineData.Targets) > 0
	hasInputs := len(pipelineData.Inputs) > 0
	hasOutputs := len(pipelineData.Outputs) > 0
	hasSubscriptions := len(pipelineData.Subscriptions) > 0
	hasTunnelPolicies := len(pipelineData.TunnelTargetPolicies) > 0

	// pipeline is ready if it has (targets + subscriptions) OR (tunnel policies + subscriptions) OR inputs, AND has outputs
	if ((hasTargets && hasSubscriptions) || (hasTunnelPolicies && hasSubscriptions) || hasInputs) && hasOutputs {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "PipelineReady"
		readyCondition.Message = fmt.Sprintf("Pipeline has %d targets, %d tunnel policies, %d subscriptions, %d inputs, %d outputs",
			len(pipelineData.Targets), len(pipelineData.TunnelTargetPolicies), len(pipelineData.Subscriptions), len(pipelineData.Inputs), len(pipelineData.Outputs))
	} else {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "PipelineIncomplete"
		var missing []string
		if !hasOutputs {
			missing = append(missing, "outputs")
		}
		if !hasTargets && !hasInputs {
			missing = append(missing, "targets or inputs")
		}
		if hasTargets && !hasSubscriptions {
			missing = append(missing, "subscriptions")
		}
		readyCondition.Message = fmt.Sprintf("Pipeline missing: %s", strings.Join(missing, ", "))
		newStatus.Status = "Incomplete"
	}
	newStatus.Conditions = append(newStatus.Conditions, readyCondition)

	// resourcesResolved condition
	resolvedCondition := metav1.Condition{
		Type:               PipelineConditionTypeResourcesResolved,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: pipeline.Generation,
		LastTransitionTime: now,
		Reason:             "ResourcesResolved",
		Message:            "All referenced resources were successfully resolved",
	}
	newStatus.Conditions = append(newStatus.Conditions, resolvedCondition)

	// preserve LastTransitionTime for unchanged conditions
	for i := range newStatus.Conditions {
		for _, oldCond := range pipeline.Status.Conditions {
			if oldCond.Type == newStatus.Conditions[i].Type &&
				oldCond.Status == newStatus.Conditions[i].Status {
				newStatus.Conditions[i].LastTransitionTime = oldCond.LastTransitionTime
				break
			}
		}
	}

	// update status if changed, with retry on conflict
	if !pipelineStatusEqual(pipeline.Status, newStatus) {
		pipelineNN := types.NamespacedName{Name: pipeline.Name, Namespace: pipeline.Namespace}
		for attempt := 0; attempt < 5; attempt++ {
			// re-fetch to get the latest resourceVersion
			if err := r.Get(ctx, pipelineNN, pipeline); err != nil {
				return fmt.Errorf("failed to re-fetch pipeline: %w", err)
			}
			pipeline.Status = newStatus
			if err := r.Status().Update(ctx, pipeline); err != nil {
				if errors.IsConflict(err) {
					continue
				}
				return fmt.Errorf("failed to update pipeline status: %w", err)
			}
			logger.Info("updated pipeline status", "pipeline", pipeline.Name, "targets", newStatus.TargetsCount)
			break
		}
	}

	return nil
}

// pipelineStatusEqual compares two PipelineStatus structs for equality
func pipelineStatusEqual(a, b gnmicv1alpha1.PipelineStatus) bool {
	if a.Status != b.Status ||
		a.TargetsCount != b.TargetsCount ||
		a.SubscriptionsCount != b.SubscriptionsCount ||
		a.InputsCount != b.InputsCount ||
		a.OutputsCount != b.OutputsCount ||
		a.TunnelTargetPoliciesCount != b.TunnelTargetPoliciesCount {
		return false
	}
	if len(a.Conditions) != len(b.Conditions) {
		return false
	}
	for i := range a.Conditions {
		if a.Conditions[i].Type != b.Conditions[i].Type ||
			a.Conditions[i].Status != b.Conditions[i].Status ||
			a.Conditions[i].Reason != b.Conditions[i].Reason ||
			a.Conditions[i].Message != b.Conditions[i].Message {
			return false
		}
	}
	return true
}

// updatePipelineStatusWithError updates the pipeline status with an error condition
func (r *ClusterReconciler) updatePipelineStatusWithError(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline, reason, message string) error {
	now := metav1.Now()

	newStatus := gnmicv1alpha1.PipelineStatus{
		Status: "Error",
		Conditions: []metav1.Condition{
			{
				Type:               PipelineConditionTypeReady,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: pipeline.Generation,
				LastTransitionTime: now,
				Reason:             reason,
				Message:            message,
			},
		},
	}

	pipelineNN := types.NamespacedName{Name: pipeline.Name, Namespace: pipeline.Namespace}
	for attempt := 0; attempt < 5; attempt++ {
		if err := r.Get(ctx, pipelineNN, pipeline); err != nil {
			return fmt.Errorf("failed to re-fetch pipeline: %w", err)
		}
		pipeline.Status = newStatus
		if err := r.Status().Update(ctx, pipeline); err != nil {
			if errors.IsConflict(err) {
				continue
			}
			return fmt.Errorf("failed to update pipeline status: %w", err)
		}
		return nil
	}
	return fmt.Errorf("failed to update pipeline status after retries: conflict")
}

// listPipelinesForCluster returns all enabled Pipelines that reference this Cluster
func (r *ClusterReconciler) listPipelinesForCluster(ctx context.Context, cluster *gnmicv1alpha1.Cluster) ([]gnmicv1alpha1.Pipeline, error) {
	var pipelineList gnmicv1alpha1.PipelineList
	err := r.List(ctx, &pipelineList, client.InNamespace(cluster.Namespace))
	if err != nil {
		return nil, err
	}

	var result []gnmicv1alpha1.Pipeline
	for _, pipeline := range pipelineList.Items {
		if pipeline.Spec.ClusterRef == cluster.Name && pipeline.Spec.Enabled {
			result = append(result, pipeline)
		}
	}
	return result, nil
}

// buildConfigContent builds the gnmic configuration from the cluster and its pipelines
func (r *ClusterReconciler) buildConfigContent(cluster *gnmicv1alpha1.Cluster) ([]byte, error) {
	restPort := int32(defaultRestPort)
	if cluster.Spec.API != nil && cluster.Spec.API.RestPort != 0 {
		restPort = cluster.Spec.API.RestPort
	}

	tlsConfig := gnmic.TLSConfigForClusterPod(cluster)
	config := map[string]any{
		"api-server": map[string]any{
			"address":        fmt.Sprintf(":%d", restPort),
			"enable-metrics": true,
			"tls":            tlsConfig,
		},
		"log": true,
	}

	// add tunnel-server configuration if configured
	if cluster.Spec.GRPCTunnel != nil && cluster.Spec.GRPCTunnel.Port != 0 {
		tunnelConfig := map[string]any{
			"address":        fmt.Sprintf(":%d", cluster.Spec.GRPCTunnel.Port),
			"enable-metrics": true,
		}

		// add TLS configuration if specified
		tunnelTLSConfig := gnmic.TunnelServerTLSConfig(cluster)
		if tunnelTLSConfig != nil {
			tunnelConfig["tls"] = tunnelTLSConfig
		}

		config["tunnel-server"] = tunnelConfig
	}

	return yaml.Marshal(config)
}

// resolveTargets resolves targets for a pipeline using refs and selectors (union of all selectors)
func (r *ClusterReconciler) resolveTargets(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.Target, error) {
	var result []gnmicv1alpha1.Target
	seen := make(map[string]struct{})

	// get targets by direct refs
	for _, ref := range pipeline.Spec.TargetRefs {
		var target gnmicv1alpha1.Target
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &target); err != nil {
			if !errors.IsNotFound(err) {
				return nil, err
			}
			continue
		}
		if _, ok := seen[target.Name]; !ok {
			result = append(result, target)
			seen[target.Name] = struct{}{}
		}
	}

	// get targets by selectors (union of all selectors)
	for _, labelSelector := range pipeline.Spec.TargetSelectors {
		if len(labelSelector.MatchLabels) == 0 && len(labelSelector.MatchExpressions) == 0 {
			continue
		}
		var targetList gnmicv1alpha1.TargetList
		selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
		if err != nil {
			return nil, err
		}
		if err := r.List(ctx, &targetList, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, err
		}
		for _, target := range targetList.Items {
			if _, ok := seen[target.Name]; !ok {
				result = append(result, target)
				seen[target.Name] = struct{}{}
			}
		}
	}

	return result, nil
}

// resolveSubscriptions resolves subscriptions for a pipeline using refs and selectors (union of all selectors)
func (r *ClusterReconciler) resolveSubscriptions(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.Subscription, error) {
	var result []gnmicv1alpha1.Subscription
	seen := make(map[string]struct{})

	// get subscriptions by direct refs
	for _, ref := range pipeline.Spec.SubscriptionRefs {
		var sub gnmicv1alpha1.Subscription
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &sub); err != nil {
			if !errors.IsNotFound(err) {
				return nil, err
			}
			continue
		}
		if _, ok := seen[sub.Name]; !ok {
			result = append(result, sub)
			seen[sub.Name] = struct{}{}
		}
	}

	// get subscriptions by selectors (union of all selectors)
	for _, labelSelector := range pipeline.Spec.SubscriptionSelectors {
		if len(labelSelector.MatchLabels) == 0 && len(labelSelector.MatchExpressions) == 0 {
			continue
		}
		var subList gnmicv1alpha1.SubscriptionList
		selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
		if err != nil {
			return nil, err
		}
		if err := r.List(ctx, &subList, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, err
		}
		for _, sub := range subList.Items {
			if _, ok := seen[sub.Name]; !ok {
				result = append(result, sub)
				seen[sub.Name] = struct{}{}
			}
		}
	}

	return result, nil
}

// resolveOutputs resolves outputs for a pipeline using refs and selectors (union of all selectors)
func (r *ClusterReconciler) resolveOutputs(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.Output, error) {
	var result []gnmicv1alpha1.Output
	seen := make(map[string]struct{})

	// get outputs by direct refs
	for _, ref := range pipeline.Spec.Outputs.OutputRefs {
		var output gnmicv1alpha1.Output
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &output); err != nil {
			if !errors.IsNotFound(err) {
				return nil, err
			}
			continue
		}
		if _, ok := seen[output.Name]; !ok {
			result = append(result, output)
			seen[output.Name] = struct{}{}
		}
	}

	// get outputs by selectors (union of all selectors)
	for _, labelSelector := range pipeline.Spec.Outputs.OutputSelectors {
		if len(labelSelector.MatchLabels) == 0 && len(labelSelector.MatchExpressions) == 0 {
			continue
		}
		var outputList gnmicv1alpha1.OutputList
		selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
		if err != nil {
			return nil, err
		}
		if err := r.List(ctx, &outputList, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, err
		}
		for _, output := range outputList.Items {
			if _, ok := seen[output.Name]; !ok {
				result = append(result, output)
				seen[output.Name] = struct{}{}
			}
		}
	}

	return result, nil
}

// resolveOutputServiceAddresses resolves service addresses for outputs that support serviceRef or serviceSelector
func (r *ClusterReconciler) resolveOutputServiceAddresses(ctx context.Context, output *gnmicv1alpha1.Output) ([]string, error) {
	spec := &output.Spec

	// skip if output type doesn't support service references
	if !gnmic.OutputTypesWithServiceRef[spec.Type] {
		return nil, nil
	}

	// skip if neither serviceRef nor serviceSelector is configured
	if spec.ServiceRef == nil && spec.ServiceSelector == nil {
		return nil, nil
	}

	resolved := []string{}

	// resolve by direct service reference
	if spec.ServiceRef != nil {
		namespace := spec.ServiceRef.Namespace
		if namespace == "" {
			namespace = output.Namespace
		}

		var svc corev1.Service
		if err := r.Get(ctx, types.NamespacedName{Name: spec.ServiceRef.Name, Namespace: namespace}, &svc); err != nil {
			return nil, fmt.Errorf("failed to get service %s/%s: %w", namespace, spec.ServiceRef.Name, err)
		}

		// convert service ports to gnmic.ServicePort
		ports := make([]gnmic.ServicePort, len(svc.Spec.Ports))
		for i, p := range svc.Spec.Ports {
			ports[i] = gnmic.ServicePort{Name: p.Name, Port: p.Port}
		}

		port, err := gnmic.ParseServicePort(spec.ServiceRef.Port, ports)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve port for service %s/%s: %w", namespace, spec.ServiceRef.Name, err)
		}

		// use cluster DNS name for the service
		host := fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace)
		addr := gnmic.FormatServiceAddress(spec, host, port)
		resolved = append(resolved, addr)
	}

	// resolve by service selector
	if spec.ServiceSelector != nil {
		namespace := spec.ServiceSelector.Namespace
		if namespace == "" {
			// use output namespace if not specified
			namespace = output.Namespace
		}

		var svcList corev1.ServiceList
		if err := r.List(ctx, &svcList,
			client.InNamespace(namespace),
			client.MatchingLabels(spec.ServiceSelector.MatchLabels),
		); err != nil {
			return nil, fmt.Errorf("failed to list services with selector: %w", err)
		}

		for _, svc := range svcList.Items {
			// convert service ports to gnmic.ServicePort
			ports := make([]gnmic.ServicePort, len(svc.Spec.Ports))
			for i, p := range svc.Spec.Ports {
				ports[i] = gnmic.ServicePort{Name: p.Name, Port: p.Port}
			}

			port, err := gnmic.ParseServicePort(spec.ServiceSelector.Port, ports)
			if err != nil {
				// skip services that don't have the requested port
				continue
			}

			// use cluster DNS name for the service
			host := fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, svc.Namespace)
			addr := gnmic.FormatServiceAddress(spec, host, port)
			resolved = append(resolved, addr)
		}
	}

	return resolved, nil
}

// resolveInputs resolves inputs for a pipeline using refs and selectors (union of all selectors)
func (r *ClusterReconciler) resolveInputs(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.Input, error) {
	var result []gnmicv1alpha1.Input
	seen := make(map[string]struct{})

	// get inputs by direct refs
	for _, ref := range pipeline.Spec.Inputs.InputRefs {
		var input gnmicv1alpha1.Input
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &input); err != nil {
			if !errors.IsNotFound(err) {
				return nil, err
			}
			continue
		}
		if _, ok := seen[input.Name]; !ok {
			result = append(result, input)
			seen[input.Name] = struct{}{}
		}
	}

	// get inputs by selectors (union of all selectors)
	for _, labelSelector := range pipeline.Spec.Inputs.InputSelectors {
		if len(labelSelector.MatchLabels) == 0 && len(labelSelector.MatchExpressions) == 0 {
			continue
		}
		var inputList gnmicv1alpha1.InputList
		selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
		if err != nil {
			return nil, err
		}
		if err := r.List(ctx, &inputList, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, err
		}
		for _, input := range inputList.Items {
			if _, ok := seen[input.Name]; !ok {
				result = append(result, input)
				seen[input.Name] = struct{}{}
			}
		}
	}

	return result, nil
}

// resolveOutputProcessors resolves processors for outputs in a pipeline using refs and selectors.
// order is preserved: refs first (in order, may contain duplicates), then selected processors (sorted by name, deduplicated against refs).
func (r *ClusterReconciler) resolveOutputProcessors(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.Processor, error) {
	var refProcessors []gnmicv1alpha1.Processor
	var selectorProcessors []gnmicv1alpha1.Processor
	// track what's already in refs to avoid duplicating from selectors
	inRefs := make(map[string]struct{})

	logger := log.FromContext(ctx)
	// get processors by direct refs (order preserved, duplicates allowed)
	for _, ref := range pipeline.Spec.Outputs.ProcessorRefs {
		var processor gnmicv1alpha1.Processor
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &processor); err != nil {
			if !errors.IsNotFound(err) {
				return nil, err
			}
			logger.Info("output processor not found, skipping", "ref", ref)
		}
		refProcessors = append(refProcessors, processor)
		inRefs[processor.Name] = struct{}{}
	}

	// get processors by selectors (union of all selectors, sorted, skip those already in refs)
	selectorSeen := make(map[string]struct{})
	for _, labelSelector := range pipeline.Spec.Outputs.ProcessorSelectors {
		if len(labelSelector.MatchLabels) == 0 && len(labelSelector.MatchExpressions) == 0 {
			continue
		}
		var processorList gnmicv1alpha1.ProcessorList
		selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
		if err != nil {
			return nil, err
		}
		if err := r.List(ctx, &processorList, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, err
		}
		for _, processor := range processorList.Items {
			// skip if already in refs or already seen from another selector
			if _, ok := inRefs[processor.Name]; ok {
				continue
			}
			if _, ok := selectorSeen[processor.Name]; ok {
				continue
			}
			selectorProcessors = append(selectorProcessors, processor)
			selectorSeen[processor.Name] = struct{}{}
		}
	}

	// sort selector processors by name
	sort.Slice(selectorProcessors, func(i, j int) bool {
		return selectorProcessors[i].Name < selectorProcessors[j].Name
	})

	// combine: refs first, then sorted selectors
	return append(refProcessors, selectorProcessors...), nil
}

// resolveInputProcessors resolves processors for inputs in a pipeline using refs and selectors (union of all selectors)
// resolveInputProcessors resolves processors for inputs in a pipeline using refs and selectors.
// order is preserved: refs first (in order, may contain duplicates), then selected processors (sorted by name, deduplicated against refs).
func (r *ClusterReconciler) resolveInputProcessors(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.Processor, error) {
	var refProcessors []gnmicv1alpha1.Processor
	var selectorProcessors []gnmicv1alpha1.Processor
	// track what's already in refs to avoid duplicating from selectors
	inRefs := make(map[string]struct{})

	logger := log.FromContext(ctx)
	// get processors by direct refs (order preserved, duplicates allowed)
	for _, ref := range pipeline.Spec.Inputs.ProcessorRefs {
		var processor gnmicv1alpha1.Processor
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &processor); err != nil {
			if !errors.IsNotFound(err) {
				return nil, err
			}
			logger.Info("input processor not found, skipping", "ref", ref)
		}
		refProcessors = append(refProcessors, processor)
		inRefs[processor.Name] = struct{}{}
	}

	// get processors by selectors (union of all selectors, sorted, skip those already in refs)
	selectorSeen := make(map[string]struct{})
	for _, labelSelector := range pipeline.Spec.Inputs.ProcessorSelectors {
		if len(labelSelector.MatchLabels) == 0 && len(labelSelector.MatchExpressions) == 0 {
			continue
		}
		var processorList gnmicv1alpha1.ProcessorList
		selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
		if err != nil {
			return nil, err
		}
		if err := r.List(ctx, &processorList, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, err
		}
		for _, processor := range processorList.Items {
			// skip if already in refs or already seen from another selector
			if _, ok := inRefs[processor.Name]; ok {
				continue
			}
			if _, ok := selectorSeen[processor.Name]; ok {
				continue
			}
			selectorProcessors = append(selectorProcessors, processor)
			selectorSeen[processor.Name] = struct{}{}
		}
	}

	// sort selector processors by name
	sort.Slice(selectorProcessors, func(i, j int) bool {
		return selectorProcessors[i].Name < selectorProcessors[j].Name
	})

	// combine: refs first, then sorted selectors
	return append(refProcessors, selectorProcessors...), nil
}

// resolveTunnelTargetPolicies resolves tunnel target policies for a pipeline using refs and selectors (union of all selectors)
func (r *ClusterReconciler) resolveTunnelTargetPolicies(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.TunnelTargetPolicy, error) {
	var result []gnmicv1alpha1.TunnelTargetPolicy
	seen := make(map[string]struct{})

	// get policies by direct refs
	for _, ref := range pipeline.Spec.TunnelTargetPolicyRefs {
		var policy gnmicv1alpha1.TunnelTargetPolicy
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &policy); err != nil {
			if !errors.IsNotFound(err) {
				return nil, err
			}
			continue
		}
		if _, ok := seen[policy.Name]; !ok {
			result = append(result, policy)
			seen[policy.Name] = struct{}{}
		}
	}

	// get policies by selectors (union of all selectors)
	for _, labelSelector := range pipeline.Spec.TunnelTargetPolicySelectors {
		if len(labelSelector.MatchLabels) == 0 && len(labelSelector.MatchExpressions) == 0 {
			continue
		}
		var policyList gnmicv1alpha1.TunnelTargetPolicyList
		selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
		if err != nil {
			return nil, err
		}
		if err := r.List(ctx, &policyList, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, err
		}
		for _, policy := range policyList.Items {
			if _, ok := seen[policy.Name]; !ok {
				result = append(result, policy)
				seen[policy.Name] = struct{}{}
			}
		}
	}

	return result, nil
}

func (r *ClusterReconciler) buildStatefulSet(cluster *gnmicv1alpha1.Cluster) (*appsv1.StatefulSet, *corev1.ConfigMap, error) {
	// build gNMIc pod base configuration
	configMap, err := r.buildConfigMap(cluster)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build config map: %w", err)
	}

	labels := map[string]string{
		"app.kubernetes.io/name":       LabelValueName,
		"app.kubernetes.io/managed-by": LabelValueManagedBy,
		LabelClusterName:               cluster.Name,
	}

	stsName := fmt.Sprintf("%s%s", resourcePrefix, cluster.Name)

	// base volume mounts for gNMIc container
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: gNMIcConfigPath,
			SubPath:   gNMIcConfigFile,
		},
	}

	// base volumes
	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMap.Name,
					},
				},
			},
		},
	}

	var initContainers []corev1.Container

	// environment variables for the container
	// start with user-defined env vars
	envVars := append([]corev1.EnvVar{}, cluster.Spec.Env...)

	// add TLS volumes if TLS is configured using cert-manager
	if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil && cluster.Spec.API.TLS.IssuerRef != "" {
		if cluster.Spec.API.TLS.UseCSIDriver {
			// use cert-manager CSI driver for per-pod certificate
			volumes = append(volumes, corev1.Volume{
				Name: "tls-certs",
				VolumeSource: corev1.VolumeSource{
					CSI: &corev1.CSIVolumeSource{
						Driver:   "csi.cert-manager.io",
						ReadOnly: ptr.To(true),
						VolumeAttributes: map[string]string{
							"csi.cert-manager.io/issuer-name": cluster.Spec.API.TLS.IssuerRef,
							"csi.cert-manager.io/issuer-kind": "Issuer",
							"csi.cert-manager.io/dns-names":   "${POD_NAME}." + stsName + "." + cluster.Namespace + ".svc.cluster.local",
							// "csi.cert-manager.io/rebewBefore": "72h", // TODO: make configurable ?
						},
					},
				},
			})
		} else {
			// use projected volume with subPathExpr to mount the correct certificate per pod
			// each pod has a certificate secret named <stsName>-<ordinal>-tls
			// organize by pod name so subPathExpr can select the right one

			certSources := []corev1.VolumeProjection{}
			for i := int32(0); i < *cluster.Spec.Replicas; i++ {
				podName := fmt.Sprintf("%s-%d", stsName, i)
				secretName := fmt.Sprintf("%s-tls", podName)
				certSources = append(certSources, corev1.VolumeProjection{
					Secret: &corev1.SecretProjection{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: secretName,
						},
						Items: []corev1.KeyToPath{
							{Key: "tls.crt", Path: fmt.Sprintf("%s/tls.crt", podName)},
							{Key: "tls.key", Path: fmt.Sprintf("%s/tls.key", podName)},
							{Key: "ca.crt", Path: fmt.Sprintf("%s/ca.crt", podName)},
						},
						Optional: ptr.To(true),
					},
				})
			}

			volumes = append(volumes, corev1.Volume{
				Name: "tls-certs",
				VolumeSource: corev1.VolumeSource{
					Projected: &corev1.ProjectedVolumeSource{
						Sources: certSources,
					},
				},
			})
		}

		// add TLS volume mount to main container using subPathExpr to select the pod's certificate
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:        "tls-certs",
			MountPath:   gnmic.CertFilesBasePath,
			SubPathExpr: "$(POD_NAME)", // Selects the subdirectory matching the pod name
			ReadOnly:    true,
		})

		// add POD_NAME env var for subPathExpr (only needed for non-CSI approach)
		envVars = append(envVars, corev1.EnvVar{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		})
	}

	// add CA bundle volume if specified (for verifying target certificates)
	if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil && cluster.Spec.API.TLS.BundleRef != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "ca-bundle",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cluster.Spec.API.TLS.BundleRef,
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "ca-bundle",
			MountPath: "/etc/certs/ca",
			ReadOnly:  true,
		})
	}

	// add controller CA volume for mTLS client verification
	if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil && cluster.Spec.API.TLS.IssuerRef != "" {
		controllerCACMName := resourcePrefix + cluster.Name + controllerCACMSfx
		volumes = append(volumes, corev1.Volume{
			Name: "controller-ca",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: controllerCACMName,
					},
					Optional: ptr.To(true), // Optional in case controller CA isn't configured
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "controller-ca",
			MountPath: gnmic.ControllerCAMountPath,
			ReadOnly:  true,
		})
	}

	// add tunnel TLS volumes if gRPC tunnel with TLS is configured
	if cluster.Spec.GRPCTunnel != nil && cluster.Spec.GRPCTunnel.TLS != nil {
		// add tunnel TLS certificates if issuerRef is configured
		if cluster.Spec.GRPCTunnel.TLS.IssuerRef != "" {
			if cluster.Spec.GRPCTunnel.TLS.UseCSIDriver {
				// use cert-manager CSI driver for per-pod certificates
				volumes = append(volumes, corev1.Volume{
					Name: "tunnel-tls-certs",
					VolumeSource: corev1.VolumeSource{
						CSI: &corev1.CSIVolumeSource{
							Driver:   "csi.cert-manager.io",
							ReadOnly: ptr.To(true),
							VolumeAttributes: map[string]string{
								"csi.cert-manager.io/issuer-name": cluster.Spec.GRPCTunnel.TLS.IssuerRef,
								"csi.cert-manager.io/issuer-kind": "Issuer",
								"csi.cert-manager.io/dns-names":   "${POD_NAME}." + stsName + "." + cluster.Namespace + ".svc.cluster.local",
							},
						},
					},
				})
			} else {
				// use projected volume with subPathExpr to mount the correct certificate per pod
				tunnelCertSources := []corev1.VolumeProjection{}
				for i := int32(0); i < *cluster.Spec.Replicas; i++ {
					podName := fmt.Sprintf("%s-%d", stsName, i)
					secretName := fmt.Sprintf("%s-tunnel-tls", podName)
					tunnelCertSources = append(tunnelCertSources, corev1.VolumeProjection{
						Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretName,
							},
							Items: []corev1.KeyToPath{
								{Key: "tls.crt", Path: fmt.Sprintf("%s/tls.crt", podName)},
								{Key: "tls.key", Path: fmt.Sprintf("%s/tls.key", podName)},
								{Key: "ca.crt", Path: fmt.Sprintf("%s/ca.crt", podName)},
							},
							Optional: ptr.To(true),
						},
					})
				}

				volumes = append(volumes, corev1.Volume{
					Name: "tunnel-tls-certs",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							Sources: tunnelCertSources,
						},
					},
				})
			}

			// add tunnel TLS volume mount
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:        "tunnel-tls-certs",
				MountPath:   gnmic.TunnelCertFilesBasePath,
				SubPathExpr: "$(POD_NAME)",
				ReadOnly:    true,
			})

			// ensure POD_NAME env var is set (might already be set for API TLS)
			hasPodNameEnv := false
			for _, env := range envVars {
				if env.Name == "POD_NAME" {
					hasPodNameEnv = true
					break
				}
			}
			if !hasPodNameEnv {
				envVars = append(envVars, corev1.EnvVar{
					Name: "POD_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
					},
				})
			}
		}

		// add tunnel CA bundle volume if bundleRef is configured for client certificate verification
		if cluster.Spec.GRPCTunnel.TLS.BundleRef != "" {
			volumes = append(volumes, corev1.Volume{
				Name: "tunnel-ca-bundle",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: cluster.Spec.GRPCTunnel.TLS.BundleRef,
						},
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "tunnel-ca-bundle",
				MountPath: gnmic.TunnelCABundleMountPath,
				ReadOnly:  true,
			})
		}
	}

	// add client TLS volumes if ClientTLS is configured (for mTLS to targets)
	// A single certificate is shared by all pods in the cluster
	if cluster.Spec.ClientTLS != nil {
		// add client TLS certificates if issuerRef is configured
		if cluster.Spec.ClientTLS.IssuerRef != "" {
			// Single certificate shared by all pods - mounted from a secret
			clientCertSecretName := fmt.Sprintf("%s%s-client-tls", resourcePrefix, cluster.Name)

			if cluster.Spec.ClientTLS.UseCSIDriver {
				// use cert-manager CSI driver for client certificates
				// CN and DNS names use cluster-name.namespace
				commonName := fmt.Sprintf("%s.%s", cluster.Name, cluster.Namespace)
				volumes = append(volumes, corev1.Volume{
					Name: "client-tls-certs",
					VolumeSource: corev1.VolumeSource{
						CSI: &corev1.CSIVolumeSource{
							Driver:   "csi.cert-manager.io",
							ReadOnly: ptr.To(true),
							VolumeAttributes: map[string]string{
								"csi.cert-manager.io/issuer-name": cluster.Spec.ClientTLS.IssuerRef,
								"csi.cert-manager.io/issuer-kind": "Issuer",
								"csi.cert-manager.io/common-name": commonName,
								"csi.cert-manager.io/dns-names":   commonName,
							},
						},
					},
				})
			} else {
				// mount the single certificate secret directly
				volumes = append(volumes, corev1.Volume{
					Name: "client-tls-certs",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: clientCertSecretName,
							Optional:   ptr.To(true),
						},
					},
				})
			}

			// add client TLS volume mount (no subPathExpr needed - same cert for all pods)
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "client-tls-certs",
				MountPath: gnmic.ClientTLSCertFilesBasePath,
				ReadOnly:  true,
			})
		}

		// add client CA bundle volume if bundleRef is configured for target server certificate verification
		if cluster.Spec.ClientTLS.BundleRef != "" {
			volumes = append(volumes, corev1.Volume{
				Name: "client-ca-bundle",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: cluster.Spec.ClientTLS.BundleRef,
						},
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "client-ca-bundle",
				MountPath: gnmic.ClientCABundleMountPath,
				ReadOnly:  true,
			})
		}
	}

	return &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      stsName,
				Namespace: cluster.Namespace,
				Labels:    labels,
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas:    cluster.Spec.Replicas,
				ServiceName: stsName, // references the headless service
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						LabelClusterName: cluster.Name,
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
					Spec: corev1.PodSpec{
						InitContainers: initContainers,
						Containers: []corev1.Container{
							{
								Name:  "gnmic",
								Image: cluster.Spec.Image,
								Ports: buildContainerPorts(cluster),
								Command: []string{
									"/app/gnmic",
									"collector",
									"--config",
									"/etc/gnmic/config.yaml",
								},
								VolumeMounts: volumeMounts,
								Resources:    cluster.Spec.Resources,
								Env:          envVars,
							},
						},
						Volumes: volumes,
					},
				},
			},
		},
		configMap, nil
}

func buildContainerPorts(cluster *gnmicv1alpha1.Cluster) []corev1.ContainerPort {
	restPort := int32(defaultRestPort)
	if cluster.Spec.API != nil && cluster.Spec.API.RestPort != 0 {
		restPort = cluster.Spec.API.RestPort
	}
	ports := []corev1.ContainerPort{
		{
			Name:          "rest",
			ContainerPort: restPort,
		},
	}
	if cluster.Spec.API != nil && cluster.Spec.API.GNMIPort != 0 {
		ports = append(ports, corev1.ContainerPort{
			Name:          "gnmi",
			ContainerPort: cluster.Spec.API.GNMIPort,
		})
	}
	if cluster.Spec.GRPCTunnel != nil && cluster.Spec.GRPCTunnel.Port != 0 {
		ports = append(ports, corev1.ContainerPort{
			Name:          "tunnel",
			ContainerPort: cluster.Spec.GRPCTunnel.Port,
		})
	}
	return ports
}

func resourcesEqual(a, b corev1.ResourceRequirements) bool {
	// compare requests
	if !resourceListEqual(a.Requests, b.Requests) {
		return false
	}
	// compare limits
	if !resourceListEqual(a.Limits, b.Limits) {
		return false
	}
	return true
}

func resourceListEqual(a, b corev1.ResourceList) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || !v.Equal(bv) {
			return false
		}
	}
	return true
}

func (r *ClusterReconciler) reconcileHeadlessService(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	desired := r.buildHeadlessService(cluster)

	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	var current corev1.Service
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, &current)
	if errors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// update ports only if changed
	if !servicePortsEqual(current.Spec.Ports, desired.Spec.Ports) {
		current.Spec.Ports = desired.Spec.Ports
		return r.Update(ctx, &current)
	}
	return nil
}

func (r *ClusterReconciler) buildHeadlessService(cluster *gnmicv1alpha1.Cluster) *corev1.Service {
	labels := map[string]string{
		"app.kubernetes.io/name":       LabelValueName,
		"app.kubernetes.io/managed-by": LabelValueManagedBy,
		LabelClusterName:               cluster.Name,
		LabelServiceType:               LabelValueServiceTypeHeadless,
	}
	restPort := int32(defaultRestPort)
	if cluster.Spec.API != nil && cluster.Spec.API.RestPort != 0 {
		restPort = cluster.Spec.API.RestPort
	}

	ports := []corev1.ServicePort{
		{
			Name:     "rest",
			Port:     restPort,
			Protocol: corev1.ProtocolTCP,
		},
	}
	if cluster.Spec.API != nil && cluster.Spec.API.GNMIPort != 0 {
		ports = append(ports, corev1.ServicePort{
			Name:     "gnmi",
			Port:     cluster.Spec.API.GNMIPort,
			Protocol: corev1.ProtocolTCP,
		})
	}

	svcName := fmt.Sprintf("%s%s", resourcePrefix, cluster.Name)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector: map[string]string{
				LabelClusterName: cluster.Name,
			},
			Ports: ports,
		},
	}
}

func (r *ClusterReconciler) ensureStatefulSetAbsent(ctx context.Context, nn types.NamespacedName) error {
	var sts appsv1.StatefulSet
	if err := r.Get(ctx, nn, &sts); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return r.Delete(ctx, &sts)
}

func (r *ClusterReconciler) ensureServiceAbsent(ctx context.Context, nn types.NamespacedName) error {
	var service corev1.Service
	if err := r.Get(ctx, nn, &service); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return r.Delete(ctx, &service)
}

// reconcilePrometheusServices creates/updates/deletes services for Prometheus outputs
func (r *ClusterReconciler) reconcilePrometheusServices(ctx context.Context, cluster *gnmicv1alpha1.Cluster, plan map[string]*gnmic.PipelineData, prometheusPorts map[string]int32) error {
	logger := log.FromContext(ctx)

	// collect all Prometheus outputs across all pipelines
	prometheusOutputs := make(map[string]map[string]gnmicv1alpha1.OutputSpec) // outputNN -> pipelineName -> spec
	for pipelineName, pipelineData := range plan {
		for outputNN, outputSpec := range pipelineData.Outputs {
			if outputSpec.Type != gnmic.PrometheusOutputType {
				continue
			}
			if _, ok := prometheusOutputs[outputNN]; !ok {
				prometheusOutputs[outputNN] = make(map[string]gnmicv1alpha1.OutputSpec)
			}
			prometheusOutputs[outputNN][pipelineName] = outputSpec
		}
	}

	// get existing Prometheus services managed by this cluster
	existingServices, err := r.listPrometheusServicesForCluster(ctx, cluster)
	if err != nil {
		return err
	}
	existingServiceNames := make(map[string]struct{})
	for _, svc := range existingServices {
		existingServiceNames[svc.Name] = struct{}{}
	}

	// create/update services for each Prometheus output
	desiredServiceNames := make(map[string]struct{})

	for outputNN, pipelineOutputSpecs := range prometheusOutputs {
		// number of pipelines sharing the same Prometheus output
		pipelinePrometheusOutputCount := len(pipelineOutputSpecs)
		pipelines := make([]string, 0, len(pipelineOutputSpecs))
		for pipelineName := range pipelineOutputSpecs {
			pipelines = append(pipelines, pipelineName)
		}
		sort.Strings(pipelines)

		for _, pipelineName := range pipelines {
			outputSpec := pipelineOutputSpecs[pipelineName]
			port, urlPath, err := parseListenPortAndPath(outputSpec.Config.Raw)
			if err != nil {
				logger.Error(err, "failed to parse listen port and path for Prometheus output", "output", outputNN)
				continue
			}
			if port > 0 && pipelinePrometheusOutputCount > 1 {
				logger.Error(err, "prometheus output with port (listen field) specified is not supported when multiple pipelines share the same output", "output", outputNN)
				continue
			}
			if port == 0 { // port is not explicitly set, use the port from the plan
				port = prometheusPorts[outputNN]
			} else { // port is explicitly set, update the plan
				prometheusPorts[outputNN] = port
			}

			if urlPath == "" {
				urlPath = gnmic.PrometheusDefaultPath
			}
			// generate service name from output name
			// we use metadata.generateName to ensure the service name is unique
			// we will label the prometheus services with the pipeline and output names
			var pipelineName string
			var outputName string
			_, outputName = utils.SplitNN(outputNN)              // messy
			pipelineName, outputName = utils.SplitNN(outputName) // more messy
			serviceName := fmt.Sprintf("%s%s-prom-%s-%s", resourcePrefix, cluster.Name, pipelineName, outputName)
			desiredServiceNames[serviceName] = struct{}{}

			if err := r.reconcilePrometheusService(ctx, cluster, serviceName, outputName, pipelineName, port, urlPath, &outputSpec); err != nil {
				logger.Error(err, "failed to reconcile Prometheus service", "service", serviceName)
				return err
			}
			logger.Info("reconciled Prometheus service", "service", serviceName, "port", port)
		}
	}

	// delete services that are no longer needed
	for _, svc := range existingServices {
		if _, ok := desiredServiceNames[svc.Name]; !ok {
			logger.Info("deleting unused Prometheus service", "service", svc.Name)
			if err := r.Delete(ctx, &svc); err != nil && !errors.IsNotFound(err) {
				return err
			}
		}
	}

	return nil
}

// listPrometheusServicesForCluster lists all Prometheus output services for a cluster
func (r *ClusterReconciler) listPrometheusServicesForCluster(ctx context.Context, cluster *gnmicv1alpha1.Cluster) ([]corev1.Service, error) {
	var serviceList corev1.ServiceList
	err := r.List(ctx, &serviceList,
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels{
			LabelClusterName: cluster.Name,
			LabelServiceType: LabelValueServiceTypePrometheusOutput,
		},
	)
	if err != nil {
		return nil, err
	}
	return serviceList.Items, nil
}

// cleanupPrometheusServices deletes all Prometheus output services for a cluster
func (r *ClusterReconciler) cleanupPrometheusServices(ctx context.Context, cluster *gnmicv1alpha1.Cluster) error {
	services, err := r.listPrometheusServicesForCluster(ctx, cluster)
	if err != nil {
		return err
	}
	for _, svc := range services {
		if err := r.Delete(ctx, &svc); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// reconcilePrometheusService creates or updates a service for a Prometheus output
func (r *ClusterReconciler) reconcilePrometheusService(ctx context.Context, cluster *gnmicv1alpha1.Cluster, serviceName, outputName, pipelineName string, port int32, urlPath string, outputSpec *gnmicv1alpha1.OutputSpec) error {
	desired := r.buildPrometheusService(cluster, serviceName, outputName, pipelineName, port, urlPath, outputSpec)

	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	var current corev1.Service
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, &current)
	if errors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// check if update is needed
	needsUpdate := false
	if current.Spec.Type != desired.Spec.Type {
		current.Spec.Type = desired.Spec.Type
		needsUpdate = true
	}
	if !servicePortsEqual(current.Spec.Ports, desired.Spec.Ports) {
		current.Spec.Ports = desired.Spec.Ports
		needsUpdate = true
	}
	if !maps.Equal(current.Labels, desired.Labels) {
		current.Labels = desired.Labels
		needsUpdate = true
	}
	if !maps.Equal(current.Annotations, desired.Annotations) {
		current.Annotations = desired.Annotations
		needsUpdate = true
	}

	if needsUpdate {
		return r.Update(ctx, &current)
	}
	return nil
}

// buildPrometheusService builds a service for a Prometheus output
func (r *ClusterReconciler) buildPrometheusService(cluster *gnmicv1alpha1.Cluster, serviceName, outputName, pipelineName string, port int32, urlPath string, outputSpec *gnmicv1alpha1.OutputSpec) *corev1.Service {
	labels := map[string]string{
		"app.kubernetes.io/name":       LabelValueName,
		"app.kubernetes.io/managed-by": LabelValueManagedBy,
		LabelClusterName:               cluster.Name,
		LabelServiceType:               LabelValueOutputTypePrometheus,
		LabelOutputName:                outputName,
		LabelPipelineName:              pipelineName,
	}

	annotations := map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/port":   strconv.Itoa(int(port)),
		"prometheus.io/path":   urlPath,
	}
	// default to ClusterIP if no service config is provided
	serviceType := corev1.ServiceTypeClusterIP

	if outputSpec.Service != nil {
		if outputSpec.Service.Type != "" {
			serviceType = outputSpec.Service.Type
		}
		if len(outputSpec.Service.Annotations) > 0 {
			maps.Copy(annotations, outputSpec.Service.Annotations)
		}
		if len(outputSpec.Service.Labels) > 0 {
			maps.Copy(labels, outputSpec.Service.Labels)
		}
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			// TODO: conside using this for unique service names
			// It would change the reconcile logic (cleanup especially)
			// GenerateName: serviceName,
			Name:        serviceName,
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Selector: map[string]string{
				LabelClusterName: cluster.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "metrics",
					Port:       port,
					TargetPort: intstr.FromInt32(port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// servicePortsEqual compares two slices of ServicePort
func servicePortsEqual(a, b []corev1.ServicePort) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name ||
			a[i].Port != b[i].Port ||
			a[i].Protocol != b[i].Protocol ||
			a[i].TargetPort.String() != b[i].TargetPort.String() {
			return false
		}
	}
	return true
}

type listenPortAndPath struct {
	Listen string `yaml:"listen,omitempty" json:"listen,omitempty"`
	Path   string `yaml:"path,omitempty" json:"path,omitempty"`
}

// parseListenPortAndPath parses the "listen" and "path" fields from output config and returns the port and path.
// supports formats: "listen: ":9804", "0.0.0.0:9804", "localhost:9804", "[::1]:9804", "[2001:db8::8a2e:370:7334]:9804".
// returns the default port 9804 if not specified.
// returns the default path /metrics if not specified.
func parseListenPortAndPath(configRaw []byte) (int32, string, error) {
	if configRaw == nil {
		return 0, "", nil
	}

	var config listenPortAndPath
	if err := yaml.Unmarshal(configRaw, &config); err != nil {
		return 0, "", err
	}

	config.Listen = strings.TrimSpace(config.Listen)
	if config.Listen == "" {
		config.Listen = fmt.Sprintf(":%d", gnmic.PrometheusDefaultPort)
	}
	config.Path = strings.TrimSpace(config.Path)
	if config.Path == "" {
		config.Path = gnmic.PrometheusDefaultPath
	}

	// parse the port from the listen string value
	idx := strings.LastIndex(config.Listen, ":")
	if idx == -1 {
		return 0, "", fmt.Errorf("invalid listen format: %s", config.Listen)
	}

	portStr := config.Listen[idx+1:]
	port, err := strconv.ParseInt(portStr, 10, 32)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse port from listen: %s", config.Listen)
	}

	return int32(port), config.Path, nil
}
