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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
)

const (
	sseTargetsPath          = "/api/v1/sse/targets?store=state"
	pollTargetsPath         = "/api/v1/targets"
	reconnectMinDelay       = 2 * time.Second
	reconnectMaxDelay       = 10 * time.Second
	pollInterval            = 15 * time.Second // TODO: make it an ENV var
	sseStreamBufferCapacity = 1024             // TODO: make it an ENV var
)

// TargetStateReconciler watches Cluster resources and manages SSE stream
// goroutines to collect target state from gNMIc pods. State updates are
// reflected into the Target CR's .status.clusterStates field.
type TargetStateReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// mu protects the streams map.
	mu sync.Mutex
	// streams tracks active SSE goroutines.
	// Key: "namespace/clusterName/podIndex"
	streams map[string]context.CancelFunc
}

// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targets,verbs=get;list
// +kubebuilder:rbac:groups=operator.gnmic.dev,resources=targets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get
// +kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

func (r *TargetStateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("controller", "TargetState")

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.streams == nil {
		r.streams = make(map[string]context.CancelFunc)
	}

	// fetch the cluster
	var cluster gnmicv1alpha1.Cluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			// cluster deleted — stop all streams and clean up target statuses
			r.stopStreamsForCluster(req.Namespace, req.Name)
			r.removeClusterFromTargets(ctx, req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// if replicas is nil or 0, stop all streams and clean up target statuses
	if cluster.Spec.Replicas == nil || *cluster.Spec.Replicas == 0 {
		r.stopStreamsForCluster(cluster.Namespace, cluster.Name)
		r.removeClusterFromTargets(ctx, cluster.Name)
		return ctrl.Result{}, nil
	}

	// check if statefulset is ready
	stsName := fmt.Sprintf("%s%s", resourcePrefix, cluster.Name)
	var sts appsv1.StatefulSet
	if err := r.Get(ctx, types.NamespacedName{Name: stsName, Namespace: cluster.Namespace}, &sts); err != nil {
		if apierrors.IsNotFound(err) {
			// statefulset not created yet, stop existing streams and requeue
			r.stopStreamsForCluster(cluster.Namespace, cluster.Name)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	desiredPods := int(*cluster.Spec.Replicas)

	// build the set of desired stream keys
	desiredKeys := make(map[string]struct{}, desiredPods)
	for i := 0; i < desiredPods; i++ {
		key := streamKey(cluster.Namespace, cluster.Name, i)
		desiredKeys[key] = struct{}{}
	}

	// stop streams that are no longer needed (e.g., scale down)
	prefix := cluster.Namespace + "/" + cluster.Name + "/"
	for key, cancel := range r.streams {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if _, ok := desiredKeys[key]; !ok {
			logger.Info("stopping SSE stream (pod removed)", "key", key)
			cancel()
			delete(r.streams, key)
		}
	}

	// start streams for new pods
	for i := 0; i < desiredPods; i++ {
		key := streamKey(cluster.Namespace, cluster.Name, i)
		if _, ok := r.streams[key]; ok {
			continue // already running
		}

		podURL, err := r.buildPodSSEURL(&cluster, stsName, i)
		if err != nil {
			logger.Error(err, "failed to build pod SSE URL", "podIndex", i)
			continue
		}

		streamCtx, cancel := context.WithCancel(context.Background())
		r.streams[key] = cancel

		logger.Info("starting SSE stream", "key", key, "url", podURL)
		go r.runStream(streamCtx, &cluster, stsName, i, podURL)
	}

	return ctrl.Result{}, nil
}

// runStream manages the SSE connection lifecycle for a single pod.
// It reconnects with exponential backoff on failure.
func (r *TargetStateReconciler) runStream(ctx context.Context, cluster *gnmicv1alpha1.Cluster, stsName string, podIndex int, sseURL string) {
	logger := log.FromContext(ctx).WithValues(
		"cluster", cluster.Name,
		"namespace", cluster.Namespace,
		"pod", fmt.Sprintf("%s-%d", stsName, podIndex),
	)

	podName := fmt.Sprintf("%s-%d", stsName, podIndex)
	pollURL := r.buildPodPollURL(cluster, stsName, podIndex)
	delay := reconnectMinDelay

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		httpClient, err := r.createHTTPClient(ctx, cluster)
		if err != nil {
			logger.Error(err, "failed to create HTTP client, retrying")
			sleepOrDone(ctx, delay)
			delay = backoff(delay)
			continue
		}

		events := make(chan gnmic.SSEEvent, sseStreamBufferCapacity)

		// start the SSE stream reader in a separate goroutine
		streamDone := make(chan error, 1)
		go func() {
			streamDone <- gnmic.StreamTargetState(ctx, httpClient, sseURL, events)
		}()

		// reset backoff on successful connection
		delay = reconnectMinDelay

		// process events and poll periodically until the stream ends
		r.processEvents(ctx, httpClient, events, streamDone, cluster.Name, cluster.Namespace, podName, pollURL, logger)

		// check if context was cancelled (intentional stop)
		select {
		case <-ctx.Done():
			return
		default:
			logger.Info("SSE stream disconnected, reconnecting", "delay", delay)
			sleepOrDone(ctx, delay)
			delay = backoff(delay)
		}
	}
}

// processEvents reads from the events channel, updates Target CR statuses,
// and periodically polls the pod for a full state snapshot to catch missed events.
func (r *TargetStateReconciler) processEvents(
	ctx context.Context,
	httpClient *http.Client,
	events <-chan gnmic.SSEEvent,
	streamDone <-chan error,
	clusterName, namespace, podName, pollURL string,
	logger interface {
		Info(string, ...any)
		Error(error, string, ...any)
	},
) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-streamDone:
			if err != nil {
				logger.Error(err, "SSE stream ended with error")
			}
			return
		case event := <-events:
			r.handleEvent(ctx, event, clusterName, namespace, podName)
		case <-ticker.C:
			r.pollAndSync(ctx, httpClient, pollURL, clusterName, podName, logger)
		}
	}
}

// pollAndSync fetches the full target state from a pod and reconciles it
// with the Target CR statuses to catch any events missed by the SSE stream.
func (r *TargetStateReconciler) pollAndSync(
	ctx context.Context,
	httpClient *http.Client,
	pollURL, clusterName, podName string,
	logger interface {
		Info(string, ...any)
		Error(error, string, ...any)
	},
) {
	entries, err := gnmic.PollTargetState(ctx, httpClient, pollURL)
	if err != nil {
		logger.Error(err, "periodic poll failed")
		return
	}

	for _, entry := range entries {
		if entry.State == nil {
			continue
		}

		targetNamespace, targetName, ok := parseTargetName(entry.Name)
		if !ok {
			continue
		}

		targetNN := types.NamespacedName{Name: targetName, Namespace: targetNamespace}

		for attempt := 0; attempt < maxConflictRetries; attempt++ {
			var target gnmicv1alpha1.Target
			if err := r.Get(ctx, targetNN, &target); err != nil {
				if !apierrors.IsNotFound(err) {
					logger.Error(err, "poll: failed to get target", "target", entry.Name)
				}
				break
			}

			if target.Status.ClusterStates == nil {
				target.Status.ClusterStates = make(map[string]gnmicv1alpha1.ClusterTargetState)
			}

			target.Status.ClusterStates[clusterName] = gnmicv1alpha1.ClusterTargetState{
				Pod:             podName,
				State:           entry.State.State,
				FailedReason:    entry.State.FailedReason,
				ConnectionState: entry.State.ConnectionState,
				Subscriptions:   entry.State.Subscriptions,
				LastUpdated:     metav1.NewTime(entry.State.LastUpdated),
			}

			computeStatusSummary(&target.Status)

			if err := r.Status().Update(ctx, &target); err != nil {
				if apierrors.IsConflict(err) {
					continue
				}
				logger.Error(err, "poll: failed to update target status", "target", entry.Name)
			}
			break
		}
	}
}

const maxConflictRetries = 5

// handleEvent processes a single SSE event and updates the corresponding Target CR status.
func (r *TargetStateReconciler) handleEvent(ctx context.Context, event gnmic.SSEEvent, clusterName, namespace, podName string) {
	logger := log.FromContext(ctx)

	targetNamespace, targetName, ok := parseTargetName(event.Data.Name)
	if !ok {
		logger.Info("skipping event with invalid target name", "name", event.Data.Name)
		return
	}

	stateObj, err := gnmic.ParseTargetStateObject(event.Data.Object)
	if err != nil {
		logger.Error(err, "failed to parse target state object", "target", event.Data.Name)
		return
	}

	targetNN := types.NamespacedName{Name: targetName, Namespace: targetNamespace}

	for attempt := 0; attempt < maxConflictRetries; attempt++ {
		// always fetch a fresh copy before updating
		var target gnmicv1alpha1.Target
		if err := r.Get(ctx, targetNN, &target); err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to get target", "target", event.Data.Name)
			}
			return
		}

		if target.Status.ClusterStates == nil {
			target.Status.ClusterStates = make(map[string]gnmicv1alpha1.ClusterTargetState)
		}

		if event.EventType == gnmic.SSEEventDelete {
			delete(target.Status.ClusterStates, clusterName)
		} else {
			target.Status.ClusterStates[clusterName] = gnmicv1alpha1.ClusterTargetState{
				Pod:             podName,
				State:           stateObj.State,
				FailedReason:    stateObj.FailedReason,
				ConnectionState: stateObj.ConnectionState,
				Subscriptions:   stateObj.Subscriptions,
				LastUpdated:     metav1.NewTime(stateObj.LastUpdated),
			}
		}

		computeStatusSummary(&target.Status)

		if err := r.Status().Update(ctx, &target); err != nil {
			if apierrors.IsConflict(err) {
				continue // retry with a fresh read
			}
			logger.Error(err, "failed to update target status", "target", event.Data.Name)
			return
		}
		return // success
	}

	logger.Info("giving up after max conflict retries", "target", event.Data.Name, "retries", maxConflictRetries)
}

// computeStatusSummary updates the top-level summary fields (Clusters, ConnectionState)
// based on the current ClusterStates map.
//
// A cluster is considered healthy only when both its State is "running" and
// its ConnectionState is "READY". Any failed/stopped target or non-READY
// connection results in a DEGRADED summary.
func computeStatusSummary(status *gnmicv1alpha1.TargetStatus) {
	status.Clusters = int32(len(status.ClusterStates))
	if status.Clusters == 0 {
		status.State = ""
		return
	}
	allHealthy := true
	for _, s := range status.ClusterStates {
		if s.State != "running" || s.ConnectionState != "READY" {
			allHealthy = false
			break
		}
	}
	if allHealthy {
		status.State = "READY"
	} else {
		status.State = "DEGRADED"
	}
}

// stopStreamsForCluster cancels all SSE goroutines for the given cluster.
func (r *TargetStateReconciler) stopStreamsForCluster(namespace, clusterName string) {
	prefix := namespace + "/" + clusterName + "/"
	for key, cancel := range r.streams {
		if strings.HasPrefix(key, prefix) {
			cancel()
			delete(r.streams, key)
		}
	}
}

// removeClusterFromTargets removes the cluster's entry from all Target CR statuses.
func (r *TargetStateReconciler) removeClusterFromTargets(ctx context.Context, clusterName string) {
	logger := log.FromContext(ctx).WithValues("controller", "TargetState")

	var targets gnmicv1alpha1.TargetList
	if err := r.List(ctx, &targets); err != nil {
		logger.Error(err, "failed to list targets for cluster cleanup")
		return
	}

	for i := range targets.Items {
		target := &targets.Items[i]
		if target.Status.ClusterStates == nil {
			continue
		}
		if _, ok := target.Status.ClusterStates[clusterName]; !ok {
			continue
		}

		for attempt := 0; attempt < maxConflictRetries; attempt++ {
			if attempt > 0 {
				if err := r.Get(ctx, types.NamespacedName{Name: target.Name, Namespace: target.Namespace}, target); err != nil {
					if !apierrors.IsNotFound(err) {
						logger.Error(err, "failed to re-fetch target during cluster cleanup", "target", target.Name)
					}
					break
				}
			}

			delete(target.Status.ClusterStates, clusterName)
			computeStatusSummary(&target.Status)

			if err := r.Status().Update(ctx, target); err != nil {
				if apierrors.IsConflict(err) {
					continue
				}
				logger.Error(err, "failed to remove cluster state from target", "target", target.Name, "cluster", clusterName)
				break
			}
			logger.Info("removed cluster state from target", "target", target.Name, "cluster", clusterName)
			break
		}
	}
}

func (r *TargetStateReconciler) podBaseURL(cluster *gnmicv1alpha1.Cluster, stsName string, podIndex int) string {
	restPort := int32(defaultRestPort)
	if cluster.Spec.API != nil && cluster.Spec.API.RestPort != 0 {
		restPort = cluster.Spec.API.RestPort
	}

	podDNS := fmt.Sprintf("%s-%d.%s.%s.svc.cluster.local", stsName, podIndex, stsName, cluster.Namespace)
	scheme := "http"
	if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil && cluster.Spec.API.TLS.IssuerRef != "" {
		scheme = "https"
	}

	return fmt.Sprintf("%s://%s:%d", scheme, podDNS, restPort)
}

func (r *TargetStateReconciler) buildPodSSEURL(cluster *gnmicv1alpha1.Cluster, stsName string, podIndex int) (string, error) {
	return r.podBaseURL(cluster, stsName, podIndex) + sseTargetsPath, nil
}

func (r *TargetStateReconciler) buildPodPollURL(cluster *gnmicv1alpha1.Cluster, stsName string, podIndex int) string {
	return r.podBaseURL(cluster, stsName, podIndex) + pollTargetsPath
}

// createHTTPClient creates an HTTP client with appropriate TLS configuration for the cluster.
func (r *TargetStateReconciler) createHTTPClient(ctx context.Context, cluster *gnmicv1alpha1.Cluster) (*http.Client, error) {
	if cluster.Spec.API == nil || cluster.Spec.API.TLS == nil {
		return &http.Client{}, nil
	}
	tlsConfig := &tls.Config{}
	if cluster.Spec.API.TLS.IssuerRef != "" {
		cert, err := os.ReadFile(gnmic.GetControllerCertPath())
		if err != nil {
			return nil, fmt.Errorf("failed to read controller cert: %w", err)
		}
		key, err := os.ReadFile(gnmic.GetControllerKeyPath())
		if err != nil {
			return nil, fmt.Errorf("failed to read controller key: %w", err)
		}
		certificate, err := tls.X509KeyPair(cert, key)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}

		ca, err := r.getIssuerCA(ctx, cluster.Namespace, cluster.Spec.API.TLS.IssuerRef)
		if err != nil {
			return nil, fmt.Errorf("failed to get issuer CA: %w", err)
		}
		tlsConfig.RootCAs = x509.NewCertPool()
		tlsConfig.RootCAs.AppendCertsFromPEM(ca)
	}
	if cluster.Spec.API.TLS.BundleRef != "" {
		ca, err := os.ReadFile(gnmic.GetControllerCAPath())
		if err != nil {
			return nil, fmt.Errorf("failed to read controller CA: %w", err)
		}
		if tlsConfig.RootCAs == nil {
			tlsConfig.RootCAs = x509.NewCertPool()
		}
		tlsConfig.RootCAs.AppendCertsFromPEM(ca)
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}, nil
}

// getIssuerCA fetches the CA certificate from a cert-manager Issuer's backing secret.
func (r *TargetStateReconciler) getIssuerCA(ctx context.Context, namespace, issuerName string) ([]byte, error) {
	issuer := &certmanagerv1.Issuer{}
	if err := r.Get(ctx, types.NamespacedName{Name: issuerName, Namespace: namespace}, issuer); err != nil {
		return nil, fmt.Errorf("failed to get issuer %s: %w", issuerName, err)
	}
	if issuer.Spec.CA == nil || issuer.Spec.CA.SecretName == "" {
		return nil, fmt.Errorf("issuer %s is not a CA issuer or has no secret configured", issuerName)
	}
	caSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: issuer.Spec.CA.SecretName, Namespace: namespace}, caSecret); err != nil {
		return nil, fmt.Errorf("failed to get CA secret %s: %w", issuer.Spec.CA.SecretName, err)
	}
	caCert, ok := caSecret.Data["tls.crt"]
	if !ok {
		return nil, fmt.Errorf("CA secret %s does not contain tls.crt", issuer.Spec.CA.SecretName)
	}
	return caCert, nil
}

func (r *TargetStateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gnmicv1alpha1.Cluster{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("targetstate").
		Complete(r)
}

// streamKey builds a unique key for tracking a stream goroutine.
func streamKey(namespace, clusterName string, podIndex int) string {
	return fmt.Sprintf("%s/%s/%d", namespace, clusterName, podIndex)
}

// parseTargetName splits "namespace/name" into its parts.
func parseTargetName(name string) (namespace, targetName string, ok bool) {
	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func backoff(current time.Duration) time.Duration {
	next := current * 2
	if next > reconnectMaxDelay {
		return reconnectMaxDelay
	}
	return next
}

func sleepOrDone(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
