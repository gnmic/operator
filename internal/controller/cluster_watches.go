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
	"maps"
	"slices"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

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

// secretDataChangedPredicate triggers reconciliation only when a Secret's
// contents change.
//
// Secrets carry no generation, so the predicates used for the CRDs do not apply
// and the default would wake the controller on every write to every Secret in
// scope — annotations, ownership churn, cert-manager renewals of unrelated
// material. Only Data decides what gets baked into a TargetConfig, so only Data
// is worth a reconcile.
type secretDataChangedPredicate struct {
	predicate.Funcs
}

func (secretDataChangedPredicate) Update(e event.UpdateEvent) bool {
	oldSecret, okOld := e.ObjectOld.(*corev1.Secret)
	newSecret, okNew := e.ObjectNew.(*corev1.Secret)
	if !okOld || !okNew {
		return false
	}
	return !maps.EqualFunc(oldSecret.Data, newSecret.Data, bytes.Equal)
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
	return r.findClustersUsingProfiles(ctx, profile.Namespace, map[string]struct{}{profile.Name: {}})
}

// findClustersForSecret finds all Clusters collecting with the credentials this
// Secret holds.
//
// Without this the credentials baked into each TargetConfig are only rebuilt
// when something else happens to wake the Cluster reconciler, so a rotated
// password reaches the pods whenever the next unrelated event does — or at the
// resync, which is the framework default of about ten hours.
func (r *ClusterReconciler) findClustersForSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}
	// A TargetProfile is the only thing that turns a Secret into target
	// credentials. Most Secrets in a namespace belong to something else
	// entirely, and they stop here at the cost of one cached list.
	var profileList gnmicv1alpha1.TargetProfileList
	if err := r.List(ctx, &profileList, client.InNamespace(secret.Namespace)); err != nil {
		return nil
	}
	profiles := make(map[string]struct{})
	for i := range profileList.Items {
		if profileList.Items[i].Spec.CredentialsRef == secret.Name {
			profiles[profileList.Items[i].Name] = struct{}{}
		}
	}

	var requests []reconcile.Request
	if len(profiles) > 0 {
		requests = r.findClustersUsingProfiles(ctx, secret.Namespace, profiles)
	}

	// A Secret can also be the backing store of a cert-manager Issuer a Cluster
	// names. Nothing mapped that before, so rotating an issuing CA reached the
	// operator only when some unrelated event happened to wake this controller --
	// in practice the backstop, up to a reconcile interval later.
	seen := make(map[types.NamespacedName]struct{}, len(requests))
	for _, req := range requests {
		seen[req.NamespacedName] = struct{}{}
	}
	for _, req := range r.findClustersTrustingSecret(ctx, secret.Namespace, secret.Name) {
		if _, ok := seen[req.NamespacedName]; ok {
			continue
		}
		seen[req.NamespacedName] = struct{}{}
		requests = append(requests, req)
	}
	return requests
}

// findClustersTrustingSecret returns the Clusters whose TLS configuration names an
// Issuer backed by this Secret.
func (r *ClusterReconciler) findClustersTrustingSecret(ctx context.Context, namespace, secretName string) []reconcile.Request {
	var issuerList certmanagerv1.IssuerList
	if err := r.List(ctx, &issuerList, client.InNamespace(namespace)); err != nil {
		return nil
	}
	issuers := make(map[string]struct{})
	for i := range issuerList.Items {
		ca := issuerList.Items[i].Spec.CA
		if ca != nil && ca.SecretName == secretName {
			issuers[issuerList.Items[i].Name] = struct{}{}
		}
	}
	if len(issuers) == 0 {
		return nil
	}

	var clusterList gnmicv1alpha1.ClusterList
	if err := r.List(ctx, &clusterList, client.InNamespace(namespace)); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for i := range clusterList.Items {
		cluster := &clusterList.Items[i]
		refs := []string{}
		if cluster.Spec.API != nil && cluster.Spec.API.TLS != nil {
			refs = append(refs, cluster.Spec.API.TLS.IssuerRef)
		}
		if cluster.Spec.GRPCTunnel != nil && cluster.Spec.GRPCTunnel.TLS != nil {
			refs = append(refs, cluster.Spec.GRPCTunnel.TLS.IssuerRef)
		}
		if cluster.Spec.ClientTLS != nil {
			refs = append(refs, cluster.Spec.ClientTLS.IssuerRef)
		}
		for _, ref := range refs {
			if _, ok := issuers[ref]; ok {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: namespace},
				})
				break
			}
		}
	}
	return requests
}

// profileUser is something that names a TargetProfile and can itself be
// selected by a Pipeline, which is what makes it a path from a profile to a
// cluster.
type profileUser struct {
	name   string
	labels map[string]string
	kind   string // as understood by pipelineReferencesResource
}

// findClustersUsingProfiles resolves a set of TargetProfile names to the
// Clusters that collect with them.
//
// Three cached lists regardless of the size of the set. The obvious
// implementation calls findClustersReferencingResource once per matching
// target, which re-lists every Pipeline each time and turns a single event into
// O(targets x pipelines) work.
func (r *ClusterReconciler) findClustersUsingProfiles(ctx context.Context, namespace string, profiles map[string]struct{}) []reconcile.Request {
	var targetList gnmicv1alpha1.TargetList
	if err := r.List(ctx, &targetList, client.InNamespace(namespace)); err != nil {
		return nil
	}
	var users []profileUser
	for i := range targetList.Items {
		t := &targetList.Items[i]
		if _, ok := profiles[t.Spec.Profile]; ok {
			users = append(users, profileUser{name: t.Name, labels: t.Labels, kind: "target"})
		}
	}

	// Tunnel targets are discovered at runtime rather than declared, so their
	// credentials come from the policy's profile and no Target object exists to
	// find them by.
	var policyList gnmicv1alpha1.TunnelTargetPolicyList
	if err := r.List(ctx, &policyList, client.InNamespace(namespace)); err != nil {
		return nil
	}
	for i := range policyList.Items {
		p := &policyList.Items[i]
		if _, ok := profiles[p.Spec.Profile]; ok {
			users = append(users, profileUser{name: p.Name, labels: p.Labels, kind: "tunnel-target-policy"})
		}
	}
	if len(users) == 0 {
		return nil
	}

	var pipelineList gnmicv1alpha1.PipelineList
	if err := r.List(ctx, &pipelineList, client.InNamespace(namespace)); err != nil {
		return nil
	}
	clusterSet := make(map[string]struct{})
	for i := range pipelineList.Items {
		pipeline := &pipelineList.Items[i]
		if !pipeline.Spec.Enabled {
			continue
		}
		if _, done := clusterSet[pipeline.Spec.ClusterRef]; done {
			continue
		}
		for _, u := range users {
			if pipelineReferencesResource(pipeline, u.name, u.labels, u.kind) {
				clusterSet[pipeline.Spec.ClusterRef] = struct{}{}
				break
			}
		}
	}

	requests := make([]reconcile.Request, 0, len(clusterSet))
	for clusterName := range clusterSet {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: clusterName, Namespace: namespace},
		})
	}
	return requests
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
