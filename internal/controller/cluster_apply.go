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
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
)

// applyConfigToPods sends the apply plan to all gNMIc pods with distributed targets.
// Returns the number of targets that could not be assigned due to capacity limits.
func (r *ClusterReconciler) applyConfigToPods(ctx context.Context, cluster *gnmicv1alpha1.Cluster, plan *gnmic.ApplyPlan, numPods int) (int32, error) {
	logger := log.FromContext(ctx)

	stsName := fmt.Sprintf("%s%s", resourcePrefix, cluster.Name)

	restPort := int32(defaultRestPort)
	if cluster.Spec.API != nil && cluster.Spec.API.RestPort != 0 {
		restPort = cluster.Spec.API.RestPort
	}
	// create an HTTP client to send the apply plan to the gNMIc pods
	httpClient, err := r.createHTTPClientForCluster(ctx, cluster)
	if err != nil {
		return 0, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	distResult := gnmic.DistributeTargets(plan, numPods, cluster.Spec.TargetDistribution)

	scheme := "http"
	if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil && cluster.Spec.API.TLS.IssuerRef != "" {
		scheme = "https"
	}
	podURL := func(podIndex int) string {
		// statefulSet pods have predictable DNS names:
		//  <statefulset-name>-<ordinal>.<service-name>.<namespace>.svc.<cluster-domain>
		podDNS := fmt.Sprintf("%s-%d.%s.%s.svc.%s", stsName, podIndex, stsName, cluster.Namespace, gnmic.ClusterDomain())
		return fmt.Sprintf("%s://%s:%d/api/v1/config/apply", scheme, podDNS, restPort)
	}

	// Work out which pods actually need the plan before sending anything. With
	// the two-phase apply every reconcile otherwise costs 2 x numPods full
	// config POSTs, each carrying the shared subscriptions/outputs/processors
	// maps, and all of it is waste when nothing moved.
	//
	// Skipping the shrink pass for an unchanged pod is safe: a target moving
	// from one pod to another changes the desired set of both, so a pod whose
	// desired set is unchanged is holding no mover for anyone else to wait on.
	bodies := make(map[int][]byte, numPods)
	hashes := make(map[int]string, numPods)
	changed := make(map[int]bool, numPods)
	anyChanged := false
	for podIndex := 0; podIndex < numPods; podIndex++ {
		podPlan, ok := distResult.PerPodPlans[podIndex]
		if !ok {
			continue
		}
		body, err := json.Marshal(podPlan)
		if err != nil {
			return 0, fmt.Errorf("failed to marshal apply plan for pod %d: %w", podIndex, err)
		}
		hash := fingerprint(body)
		bodies[podIndex] = body
		hashes[podIndex] = hash
		if !r.Applied.Unchanged(streamKey(cluster.Namespace, cluster.Name, podIndex), hash) {
			changed[podIndex] = true
			anyChanged = true
		}
	}
	if !anyChanged {
		return int32(len(distResult.UnassignedTargets)), nil
	}

	// Two-phase apply avoids double-collection when a target moves between pods.
	// A single ordered pass can start the new owner before the old owner has
	// dropped the target (e.g. moving from pod 2 to pod 0). Phase 1 shrinks
	// each pod to the intersection of its previous and next assignment so
	// movers are stopped everywhere; phase 2 installs the full new sets.
	// Pods that are about to disappear on scale-down are drained explicitly.
	if len(plan.CurrentTargetAssignment) > 0 {
		template, ok := distResult.PerPodPlans[0]
		if !ok {
			for _, p := range distResult.PerPodPlans {
				template = p
				break
			}
		}
		for podIndex := range plan.CurrentTargetAssignment {
			if podIndex < numPods {
				continue
			}
			if template == nil {
				break
			}
			drain := shrinkPodPlan(template, nil)
			url := podURL(podIndex)
			logger.Info("draining gNMIc pod before scale-down", "url", url)
			if err := r.sendApplyRequest(ctx, url, drain, httpClient); err != nil {
				// The pod may already be gone; do not fail the reconcile for that.
				logger.Info("drain of scaled-down pod failed (continuing)", "pod", podIndex, "error", err)
			}
		}
		for podIndex := 0; podIndex < numPods; podIndex++ {
			podPlan, ok := distResult.PerPodPlans[podIndex]
			if !ok || !changed[podIndex] {
				continue
			}
			shrink := shrinkPodPlan(podPlan, plan.CurrentTargetAssignment[podIndex])
			url := podURL(podIndex)
			logger.Info("shrinking gNMIc pod targets before reassignment", "url", url, "targets", len(shrink.Targets))
			if err := r.sendApplyRequest(ctx, url, shrink, httpClient); err != nil {
				return 0, fmt.Errorf("failed to shrink config on pod %d: %w", podIndex, err)
			}
		}
		// gNMIc's config/apply returns before Subscribe streams are fully torn
		// down. Give movers a beat to drop on the old owner before phase 2
		// starts them on the new one; otherwise a scale transition briefly
		// double-collects.
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Second):
		}
	}

	for podIndex := 0; podIndex < numPods; podIndex++ {
		podPlan, ok := distResult.PerPodPlans[podIndex]
		if !ok || !changed[podIndex] {
			continue
		}
		url := podURL(podIndex)
		logger.Info("sending config to gNMIc pod", "url", url)
		if err := r.sendApplyBody(ctx, url, bodies[podIndex], httpClient); err != nil {
			return 0, fmt.Errorf("failed to apply config to pod %d: %w", podIndex, err)
		}
		// Recorded only after the POST succeeds. Recording the attempt would
		// make a failed apply look applied until the plan changes again.
		r.Applied.Record(streamKey(cluster.Namespace, cluster.Name, podIndex), hashes[podIndex])
		logger.Info("config applied to pod", "pod", podIndex, "targets", len(podPlan.Targets))
	}

	unassigned := int32(len(distResult.UnassignedTargets))
	if unassigned > 0 {
		logger.Info("targets unassigned due to capacity limits", "count", unassigned)
	}

	return unassigned, nil
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

// shrinkPodPlan returns a copy of podPlan whose Targets are the intersection of
// the desired set and the pod's previously assigned targets. A nil previous set
// yields an empty target map — used to drain a pod.
func shrinkPodPlan(podPlan *gnmic.ApplyPlan, previous map[string]struct{}) *gnmic.ApplyPlan {
	out := &gnmic.ApplyPlan{
		Targets:             maps.Clone(podPlan.Targets),
		Subscriptions:       podPlan.Subscriptions,
		Outputs:             podPlan.Outputs,
		Inputs:              podPlan.Inputs,
		Processors:          podPlan.Processors,
		TunnelTargetMatches: podPlan.TunnelTargetMatches,
	}
	if out.Targets == nil {
		out.Targets = make(map[string]*gapi.TargetConfig)
	}
	clear(out.Targets)
	if previous == nil {
		return out
	}
	for name, cfg := range podPlan.Targets {
		if _, ok := previous[name]; ok {
			out.Targets[name] = cfg
		}
	}
	return out
}

// sendApplyRequest sends an apply plan to a single gNMIc pod.
func (r *ClusterReconciler) sendApplyRequest(ctx context.Context, url string, plan *gnmic.ApplyPlan, httpClient *http.Client) error {
	jsonData, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("failed to marshal apply plan: %w", err)
	}
	return r.sendApplyBody(ctx, url, jsonData, httpClient)
}

// sendApplyBody POSTs an already-marshalled plan. The install pass marshals up
// front to fingerprint the payload, so re-marshalling here would double the
// cost of the thing this change exists to reduce.
func (r *ClusterReconciler) sendApplyBody(ctx context.Context, url string, jsonData []byte, httpClient *http.Client) error {
	logger := log.FromContext(ctx)

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
		rspErr := fmt.Errorf("gNMIc pod returned non-success status: %d", resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}
		logger.Error(rspErr, "", "body", string(body))
		return rspErr
	}

	return nil
}

// applyOutcome records what the apply phase did, for the status and requeue decisions.
type applyOutcome struct {
	// applied is true when the plan reached every pod.
	applied bool
	// err is the apply failure, when there was one.
	err error
	// unassignedTargets is how many targets found no pod with capacity.
	unassignedTargets int32
	// suppressed is true when nothing was sent because every pipeline was skipped
	// for unresolved references; the pods keep the configuration they already hold.
	suppressed bool
	// skippedPipelines is the number of pipelines left out for unresolved references.
	skippedPipelines int
	// numPods is how many pods the plan was distributed over.
	numPods int
}

// applyPlan pushes the plan to the collector pods once they are all ready. wait is
// true when the apply has to be postponed, with the result to return: pods still
// rolling out, or Pipeline membership changed while the plan was being built.
func (r *ClusterReconciler) applyPlan(ctx context.Context, cluster *gnmicv1alpha1.Cluster, statefulSet *appsv1.StatefulSet, pipelines []gnmicv1alpha1.Pipeline, resolved *resolvedPipelines) (applyOutcome, ctrl.Result, bool, error) {
	logger := log.FromContext(ctx)
	outcome := applyOutcome{
		suppressed:       resolved.suppressApply(),
		skippedPipelines: resolved.skipped,
	}

	// only apply new config when all desired replicas are ready
	desiredReplicas := ptr.Deref(statefulSet.Spec.Replicas, 0)
	if statefulSet.Status.ReadyReplicas < desiredReplicas {
		logger.Info("waiting for gNMIc pods to be ready before applying config",
			"readyReplicas", statefulSet.Status.ReadyReplicas, "desiredReplicas", desiredReplicas)
		// The StatefulSet is watched via Owns() with no predicate, so ReadyReplicas
		// transitions already wake this controller. This requeue is only a backstop for a
		// missed event; at 1s it re-ran the entire plan build once per second for the whole
		// duration of a rollout.
		return outcome, ctrl.Result{RequeueAfter: readinessBackstopInterval}, true, nil
	}
	// A reconcile queued while Pipelines were empty can run after a newer
	// reconcile already applied a non-empty plan. Re-list immediately before
	// apply and bail out if membership moved under us.
	if fresh, err := r.listPipelinesForCluster(ctx, cluster); err != nil {
		return outcome, ctrl.Result{}, true, err
	} else if !pipelineSetEqual(pipelines, fresh) {
		logger.Info("pipelines changed during reconcile, requeueing before apply")
		return outcome, ctrl.Result{Requeue: true}, true, nil
	}

	// distribute to desired replicas only, this makes redistribution fast in case of scaling down.
	outcome.numPods = int(desiredReplicas)
	if outcome.suppressed {
		return outcome, ctrl.Result{}, false, nil
	}
	unassigned, err := r.applyConfigToPods(ctx, cluster, resolved.plan, outcome.numPods)
	if err != nil {
		logger.Error(err, "failed to apply config to gNMIc pods")
		outcome.err = err
		return outcome, ctrl.Result{}, false, nil
	}
	outcome.applied = true
	outcome.unassignedTargets = unassigned
	logger.Info("successfully applied config to gNMIc cluster", "pods", outcome.numPods)
	return outcome, ctrl.Result{}, false, nil
}
