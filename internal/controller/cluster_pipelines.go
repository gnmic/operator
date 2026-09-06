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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
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

// updatePipelineStatus updates the status of a pipeline based on its resolved
// resources. A non-empty unresolved list marks the pipeline as not reconciled and
// names the refs that did not resolve; the counts are still reported so the partial
// resolution stays visible while diagnosing.
func (r *ClusterReconciler) updatePipelineStatus(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline, pipelineData *gnmic.PipelineData, unresolved []string) error {
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

	resolvedCondition := metav1.Condition{
		Type:               PipelineConditionTypeResourcesResolved,
		ObservedGeneration: pipeline.Generation,
		LastTransitionTime: now,
	}

	if len(unresolved) > 0 {
		// This condition existed before but was hardcoded to True, so a pipeline whose
		// refs had silently evaporated still reported that everything resolved.
		message := "unresolved references: " + strings.Join(unresolved, ", ")

		newStatus.Status = "Error"

		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = ReasonUnresolvedReferences
		readyCondition.Message = message

		resolvedCondition.Status = metav1.ConditionFalse
		resolvedCondition.Reason = ReasonUnresolvedReferences
		resolvedCondition.Message = message
	} else {
		resolvedCondition.Status = metav1.ConditionTrue
		resolvedCondition.Reason = "ResourcesResolved"
		resolvedCondition.Message = "All referenced resources were successfully resolved"

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
	}

	// order matters: pipelineStatusEqual compares conditions positionally
	newStatus.Conditions = append(newStatus.Conditions, readyCondition, resolvedCondition)

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
				if apierrors.IsConflict(err) {
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
			if apierrors.IsConflict(err) {
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

func pipelineSetEqual(a, b []gnmicv1alpha1.Pipeline) bool {
	if len(a) != len(b) {
		return false
	}
	names := make(map[string]struct{}, len(a))
	for i := range a {
		names[a[i].Name] = struct{}{}
	}
	for i := range b {
		if _, ok := names[b[i].Name]; !ok {
			return false
		}
	}
	return true
}

// unresolvedRef formats a dangling reference for a Pipeline's status message.
//
// Only explicit *Refs entries produce one. A *Selectors block that matches nothing is
// a legitimate empty result -- it is a query, and an empty answer is an answer. A ref
// names a specific resource the user asked for, so failing to find it means the
// configuration that would be applied is not the configuration that was requested.
func unresolvedRef(kind, name string) string {
	return kind + "/" + name
}

// resolveTargets resolves targets for a pipeline using refs and selectors (union of all selectors)
func (r *ClusterReconciler) resolveTargets(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.Target, []string, error) {
	var result []gnmicv1alpha1.Target
	var unresolved []string
	seen := make(map[string]struct{})

	// get targets by direct refs
	for _, ref := range pipeline.Spec.TargetRefs {
		var item gnmicv1alpha1.Target
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &item); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, nil, err
			}
			// A ref naming something that does not exist is a failed intent, not an
			// empty result. Report it instead of quietly resolving to a shorter list.
			unresolved = append(unresolved, unresolvedRef("target", ref))
			continue
		}
		if _, ok := seen[item.Name]; !ok {
			result = append(result, item)
			seen[item.Name] = struct{}{}
		}
	}

	// get targets by selectors (union of all selectors)
	for _, labelSelector := range pipeline.Spec.TargetSelectors {
		if len(labelSelector.MatchLabels) == 0 && len(labelSelector.MatchExpressions) == 0 {
			continue
		}
		var list gnmicv1alpha1.TargetList
		selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
		if err != nil {
			return nil, nil, err
		}
		if err := r.List(ctx, &list, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, nil, err
		}
		for _, item := range list.Items {
			if _, ok := seen[item.Name]; !ok {
				result = append(result, item)
				seen[item.Name] = struct{}{}
			}
		}
	}

	return result, unresolved, nil
}

// resolveSubscriptions resolves subscriptions for a pipeline using refs and selectors (union of all selectors)
func (r *ClusterReconciler) resolveSubscriptions(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.Subscription, []string, error) {
	var result []gnmicv1alpha1.Subscription
	var unresolved []string
	seen := make(map[string]struct{})

	// get subscriptions by direct refs
	for _, ref := range pipeline.Spec.SubscriptionRefs {
		var item gnmicv1alpha1.Subscription
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &item); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, nil, err
			}
			// A ref naming something that does not exist is a failed intent, not an
			// empty result. Report it instead of quietly resolving to a shorter list.
			unresolved = append(unresolved, unresolvedRef("subscription", ref))
			continue
		}
		if _, ok := seen[item.Name]; !ok {
			result = append(result, item)
			seen[item.Name] = struct{}{}
		}
	}

	// get subscriptions by selectors (union of all selectors)
	for _, labelSelector := range pipeline.Spec.SubscriptionSelectors {
		if len(labelSelector.MatchLabels) == 0 && len(labelSelector.MatchExpressions) == 0 {
			continue
		}
		var list gnmicv1alpha1.SubscriptionList
		selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
		if err != nil {
			return nil, nil, err
		}
		if err := r.List(ctx, &list, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, nil, err
		}
		for _, item := range list.Items {
			if _, ok := seen[item.Name]; !ok {
				result = append(result, item)
				seen[item.Name] = struct{}{}
			}
		}
	}

	return result, unresolved, nil
}

// resolveOutputs resolves outputs for a pipeline using refs and selectors (union of all selectors)
func (r *ClusterReconciler) resolveOutputs(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.Output, []string, error) {
	var result []gnmicv1alpha1.Output
	var unresolved []string
	seen := make(map[string]struct{})

	// get outputs by direct refs
	for _, ref := range pipeline.Spec.Outputs.OutputRefs {
		var item gnmicv1alpha1.Output
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &item); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, nil, err
			}
			// A ref naming something that does not exist is a failed intent, not an
			// empty result. Report it instead of quietly resolving to a shorter list.
			unresolved = append(unresolved, unresolvedRef("output", ref))
			continue
		}
		if _, ok := seen[item.Name]; !ok {
			result = append(result, item)
			seen[item.Name] = struct{}{}
		}
	}

	// get outputs by selectors (union of all selectors)
	for _, labelSelector := range pipeline.Spec.Outputs.OutputSelectors {
		if len(labelSelector.MatchLabels) == 0 && len(labelSelector.MatchExpressions) == 0 {
			continue
		}
		var list gnmicv1alpha1.OutputList
		selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
		if err != nil {
			return nil, nil, err
		}
		if err := r.List(ctx, &list, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, nil, err
		}
		for _, item := range list.Items {
			if _, ok := seen[item.Name]; !ok {
				result = append(result, item)
				seen[item.Name] = struct{}{}
			}
		}
	}

	return result, unresolved, nil
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
		host := fmt.Sprintf("%s.%s.svc.%s", svc.Name, svc.Namespace, gnmic.ClusterDomain())
		addr := gnmic.FormatServiceAddress(spec, host, port)
		if spec.ServiceRef.URL != "" {
			url := strings.TrimPrefix(spec.ServiceRef.URL, "/")
			addr = strings.TrimSuffix(addr, "/")
			addr = fmt.Sprintf("%s/%s", addr, url)
		}
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
			host := fmt.Sprintf("%s.%s.svc.%s", svc.Name, svc.Namespace, gnmic.ClusterDomain())
			addr := gnmic.FormatServiceAddress(spec, host, port)
			if spec.ServiceSelector.URL != "" {
				url := strings.TrimPrefix(spec.ServiceSelector.URL, "/")
				addr = strings.TrimSuffix(addr, "/")
				addr = fmt.Sprintf("%s/%s", addr, url)
			}
			resolved = append(resolved, addr)
		}
	}

	return resolved, nil
}

// resolveInputs resolves inputs for a pipeline using refs and selectors (union of all selectors)
func (r *ClusterReconciler) resolveInputs(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.Input, []string, error) {
	var result []gnmicv1alpha1.Input
	var unresolved []string
	seen := make(map[string]struct{})

	// get inputs by direct refs
	for _, ref := range pipeline.Spec.Inputs.InputRefs {
		var item gnmicv1alpha1.Input
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &item); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, nil, err
			}
			// A ref naming something that does not exist is a failed intent, not an
			// empty result. Report it instead of quietly resolving to a shorter list.
			unresolved = append(unresolved, unresolvedRef("input", ref))
			continue
		}
		if _, ok := seen[item.Name]; !ok {
			result = append(result, item)
			seen[item.Name] = struct{}{}
		}
	}

	// get inputs by selectors (union of all selectors)
	for _, labelSelector := range pipeline.Spec.Inputs.InputSelectors {
		if len(labelSelector.MatchLabels) == 0 && len(labelSelector.MatchExpressions) == 0 {
			continue
		}
		var list gnmicv1alpha1.InputList
		selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
		if err != nil {
			return nil, nil, err
		}
		if err := r.List(ctx, &list, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, nil, err
		}
		for _, item := range list.Items {
			if _, ok := seen[item.Name]; !ok {
				result = append(result, item)
				seen[item.Name] = struct{}{}
			}
		}
	}

	return result, unresolved, nil
}

// resolveOutputProcessors resolves processors for outputs in a pipeline using refs and selectors.
// order is preserved: refs first (in order, may contain duplicates), then selected processors (sorted by name, deduplicated against refs).
func (r *ClusterReconciler) resolveOutputProcessors(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.Processor, []string, error) {
	var refProcessors []gnmicv1alpha1.Processor
	var selectorProcessors []gnmicv1alpha1.Processor
	var unresolved []string
	// track what's already in refs to avoid duplicating from selectors
	inRefs := make(map[string]struct{})

	// get processors by direct refs (order preserved, duplicates allowed)
	for _, ref := range pipeline.Spec.Outputs.ProcessorRefs {
		var processor gnmicv1alpha1.Processor
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &processor); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, nil, err
			}
			// Falling through here appended the zero-valued Processor, which reached
			// the plan as a processor with an empty name and an empty type and was
			// then attached to every output in the pipeline.
			unresolved = append(unresolved, unresolvedRef("processor", ref))
			continue
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
			return nil, nil, err
		}
		if err := r.List(ctx, &processorList, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, nil, err
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
	return append(refProcessors, selectorProcessors...), unresolved, nil
}

// resolveInputProcessors resolves processors for inputs in a pipeline using refs and selectors (union of all selectors)
// resolveInputProcessors resolves processors for inputs in a pipeline using refs and selectors.
// order is preserved: refs first (in order, may contain duplicates), then selected processors (sorted by name, deduplicated against refs).
func (r *ClusterReconciler) resolveInputProcessors(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.Processor, []string, error) {
	var refProcessors []gnmicv1alpha1.Processor
	var selectorProcessors []gnmicv1alpha1.Processor
	var unresolved []string
	// track what's already in refs to avoid duplicating from selectors
	inRefs := make(map[string]struct{})

	// get processors by direct refs (order preserved, duplicates allowed)
	for _, ref := range pipeline.Spec.Inputs.ProcessorRefs {
		var processor gnmicv1alpha1.Processor
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &processor); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, nil, err
			}
			// Falling through here appended the zero-valued Processor, which reached
			// the plan as a processor with an empty name and an empty type and was
			// then attached to every output in the pipeline.
			unresolved = append(unresolved, unresolvedRef("processor", ref))
			continue
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
			return nil, nil, err
		}
		if err := r.List(ctx, &processorList, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, nil, err
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
	return append(refProcessors, selectorProcessors...), unresolved, nil
}

// resolveTunnelTargetPolicies resolves tunnel target policies for a pipeline using refs and selectors (union of all selectors)
func (r *ClusterReconciler) resolveTunnelTargetPolicies(ctx context.Context, pipeline *gnmicv1alpha1.Pipeline) ([]gnmicv1alpha1.TunnelTargetPolicy, []string, error) {
	var result []gnmicv1alpha1.TunnelTargetPolicy
	var unresolved []string
	seen := make(map[string]struct{})

	// get policies by direct refs
	for _, ref := range pipeline.Spec.TunnelTargetPolicyRefs {
		var item gnmicv1alpha1.TunnelTargetPolicy
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, &item); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, nil, err
			}
			// A ref naming something that does not exist is a failed intent, not an
			// empty result. Report it instead of quietly resolving to a shorter list.
			unresolved = append(unresolved, unresolvedRef("tunneltargetpolicy", ref))
			continue
		}
		if _, ok := seen[item.Name]; !ok {
			result = append(result, item)
			seen[item.Name] = struct{}{}
		}
	}

	// get policies by selectors (union of all selectors)
	for _, labelSelector := range pipeline.Spec.TunnelTargetPolicySelectors {
		if len(labelSelector.MatchLabels) == 0 && len(labelSelector.MatchExpressions) == 0 {
			continue
		}
		var list gnmicv1alpha1.TunnelTargetPolicyList
		selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
		if err != nil {
			return nil, nil, err
		}
		if err := r.List(ctx, &list, client.InNamespace(pipeline.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return nil, nil, err
		}
		for _, item := range list.Items {
			if _, ok := seen[item.Name]; !ok {
				result = append(result, item)
				seen[item.Name] = struct{}{}
			}
		}
	}

	return result, unresolved, nil
}

// resolvedPipelines is what the pipeline resolution pass hands to the apply and
// status phases.
type resolvedPipelines struct {
	// plan is the cluster-wide apply plan built from every pipeline that resolved.
	plan *gnmic.ApplyPlan
	// data holds the resolved PipelineData, keyed by namespace/name, for the
	// pipelines that made it into the plan.
	data map[string]*gnmic.PipelineData
	// skipped counts pipelines left out of the plan because a ref did not resolve.
	// It tells "the plan is empty because nothing is configured" apart from "the
	// plan is empty because everything was skipped", which need opposite handling.
	skipped int
}

// targetsWithoutSubscriptions reports the one plan shape that must not be applied.
//
// A pipeline's targets and subscriptions are resolved independently
// (resolveTargets vs resolveSubscriptions), each with its own List/Get calls
// against the informer cache. A Subscription being deleted and recreated (or a
// selector momentarily not matching due to cache lag) can leave a brief window
// where a pipeline's targets resolve but its subscriptions come back empty.
// gNMIc's config/apply rejects any request with targets but zero subscriptions
// outright (400), and applying that would tear down the streams already running
// on the pods for no reason. The caller skips this apply and requeues fast
// instead of sending it: the existing config on the pods is left untouched, and
// the next pass a moment later almost always sees a consistent read.
func (p *resolvedPipelines) targetsWithoutSubscriptions() bool {
	return len(p.plan.Targets) > 0 && len(p.plan.Subscriptions) == 0
}

// suppressApply reports whether every pipeline on the cluster was skipped for
// unresolved references, so the plan is empty for a reason that has nothing to do
// with what the user asked for. An empty plan is applied deliberately elsewhere,
// it is how collectors stop streaming once the last Pipeline is deleted, so pushing
// this one would drain the whole cluster over a name that usually resolves a moment
// later.
//
// Only the apply is suppressed. Returning outright also stopped Prometheus Service
// garbage collection and froze the Cluster's status counters, so deleting an Output
// before the Pipeline naming it left an orphaned Service behind and a status that no
// longer described anything.
func (p *resolvedPipelines) suppressApply() bool {
	return p.skipped > 0 && len(p.data) == 0
}

// resolvePipelines resolves every enabled pipeline bound to the cluster and builds
// the apply plan from the ones whose references all exist. Pipeline status is
// written as a side effect: a resolved pipeline is marked ready, one with dangling
// refs is reported on the Pipeline itself and left out of the plan so the rest keep
// reconciling.
func (r *ClusterReconciler) resolvePipelines(ctx context.Context, cluster *gnmicv1alpha1.Cluster, pipelines []gnmicv1alpha1.Pipeline) (*resolvedPipelines, error) {
	logger := log.FromContext(ctx)

	planBuilder := gnmic.NewPlanBuilder(cluster.Name, r)
	planBuilder = planBuilder.WithClientTLS(
		gnmic.ClientTLSConfigForCluster(cluster),
	)
	if cluster.Spec.TargetDistribution != nil && cluster.Spec.TargetDistribution.PodCapacity > 0 {
		planBuilder.WithTargetDistributionCapacity(cluster.Spec.TargetDistribution.PodCapacity)
	}
	resolved := &resolvedPipelines{data: make(map[string]*gnmic.PipelineData)}

	for i := range pipelines {
		pipeline := &pipelines[i]
		if !pipeline.Spec.Enabled {
			continue
		}
		logger.Info("cluster pipeline", "pipeline", pipeline.Name, "enabled", pipeline.Spec.Enabled)
		pipelineNN := pipeline.Namespace + gnmic.Delimiter + pipeline.Name

		pipelineData, unresolvedRefs, err := r.resolvePipeline(ctx, cluster, pipeline)
		if err != nil {
			return nil, err
		}
		if pipelineData == nil {
			continue // rejected and reported by resolvePipeline
		}

		// A ref that does not resolve means the configuration that would reach the
		// collectors is not the one that was asked for: an output the data never
		// arrives at, a processor stage that silently stops filtering. Leave the
		// pipeline out of the plan rather than apply a partial version of it, and say
		// which refs on the Pipeline itself. Other pipelines are unaffected.
		if len(unresolvedRefs) > 0 {
			sort.Strings(unresolvedRefs)
			resolved.skipped++
			logger.Info("skipping pipeline with unresolved references",
				"pipeline", pipeline.Name, "unresolved", unresolvedRefs)
			if err := r.updatePipelineStatus(ctx, pipeline, pipelineData, unresolvedRefs); err != nil {
				logger.Error(err, "failed to update pipeline status", "pipeline", pipeline.Name)
			}
			continue
		}

		planBuilder.AddPipeline(pipelineNN, pipelineData)
		resolved.data[pipelineNN] = pipelineData

		if err := r.updatePipelineStatus(ctx, pipeline, pipelineData, nil); err != nil {
			logger.Error(err, "failed to update pipeline status", "pipeline", pipeline.Name)
			// don't return, continue with other pipelines
		}
	}

	plan, err := planBuilder.Build()
	if err != nil {
		return nil, err
	}
	resolved.plan = plan
	return resolved, nil
}

// resolvePipeline gathers everything one pipeline references into a PipelineData.
// Refs naming a resource that does not exist are collected and returned rather than
// acted on one at a time, so the status lists all of them instead of whichever
// happened to be checked first. A nil PipelineData means the pipeline was rejected
// outright and its status already written; the caller moves on.
func (r *ClusterReconciler) resolvePipeline(ctx context.Context, cluster *gnmicv1alpha1.Cluster, pipeline *gnmicv1alpha1.Pipeline) (*gnmic.PipelineData, []string, error) {
	logger := log.FromContext(ctx)
	pipelineNN := pipeline.Namespace + gnmic.Delimiter + pipeline.Name
	pipelineData := gnmic.NewPipelineData()
	var unresolvedRefs []string

	// targets, and the profiles they name
	targets, unresolved, err := r.resolveTargets(ctx, pipeline)
	if err != nil {
		return nil, nil, err
	}
	unresolvedRefs = append(unresolvedRefs, unresolved...)
	targetProfileNames := make(map[string]struct{})
	for _, target := range targets {
		pipelineData.Targets[target.Namespace+gnmic.Delimiter+target.Name] = target
		targetProfileNames[target.Spec.Profile] = struct{}{}
	}
	// A missing profile used to fail the entire reconcile, which stalled every
	// other pipeline on the cluster over one bad name. It is now treated as the
	// dangling ref it is: this pipeline is skipped, the rest keep reconciling.
	unresolved, err = r.resolveTargetProfiles(ctx, pipeline.Namespace, targetProfileNames, pipelineData)
	if err != nil {
		return nil, nil, err
	}
	unresolvedRefs = append(unresolvedRefs, unresolved...)
	logger.Info("cluster pipeline resolved targets", "count", len(targets), "targetProfiles", len(targetProfileNames))

	// subscriptions
	subscriptions, unresolved, err := r.resolveSubscriptions(ctx, pipeline)
	if err != nil {
		return nil, nil, err
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

	// outputs, with Service addresses resolved for the types that support it (nats, kafka, jetstream)
	outputs, unresolved, err := r.resolveOutputs(ctx, pipeline)
	if err != nil {
		return nil, nil, err
	}
	unresolvedRefs = append(unresolvedRefs, unresolved...)
	for _, output := range outputs {
		outputNN := pipelineNN + gnmic.Delimiter + output.Name
		pipelineData.Outputs[outputNN] = output.Spec
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

	// inputs
	inputs, unresolved, err := r.resolveInputs(ctx, pipeline)
	if err != nil {
		return nil, nil, err
	}
	unresolvedRefs = append(unresolvedRefs, unresolved...)
	for _, input := range inputs {
		pipelineData.Inputs[pipelineNN+gnmic.Delimiter+input.Name] = input.Spec
	}
	logger.Info("cluster pipeline resolved inputs", "count", len(inputs))

	// output processors (order: refs first, then sorted selectors)
	outputProcessors, unresolved, err := r.resolveOutputProcessors(ctx, pipeline)
	if err != nil {
		return nil, nil, err
	}
	unresolvedRefs = append(unresolvedRefs, unresolved...)
	for _, processor := range outputProcessors {
		processorNN := pipelineNN + gnmic.Delimiter + processor.Name
		pipelineData.OutputProcessors[processorNN] = processor.Spec
		pipelineData.OutputProcessorOrder = append(pipelineData.OutputProcessorOrder, processorNN)
	}
	logger.Info("cluster pipeline resolved output processors", "count", len(outputProcessors))

	// input processors (order: refs first, then sorted selectors)
	inputProcessors, unresolved, err := r.resolveInputProcessors(ctx, pipeline)
	if err != nil {
		return nil, nil, err
	}
	unresolvedRefs = append(unresolvedRefs, unresolved...)
	for _, processor := range inputProcessors {
		processorNN := pipelineNN + gnmic.Delimiter + processor.Name
		pipelineData.InputProcessors[processorNN] = processor.Spec
		pipelineData.InputProcessorOrder = append(pipelineData.InputProcessorOrder, processorNN)
	}
	logger.Info("cluster pipeline resolved input processors", "count", len(inputProcessors))

	// tunnel target policies, and the profiles they share with targets
	tunnelTargetPolicies, unresolved, err := r.resolveTunnelTargetPolicies(ctx, pipeline)
	if err != nil {
		return nil, nil, err
	}
	unresolvedRefs = append(unresolvedRefs, unresolved...)
	if len(tunnelTargetPolicies) > 0 && cluster.Spec.GRPCTunnel == nil {
		logger.Error(nil, "pipeline has tunnel target policies but cluster has no gRPC tunnel configured",
			"pipeline", pipeline.Name, "cluster", cluster.Name)
		if err := r.updatePipelineStatusWithError(ctx, pipeline,
			"ClusterMissingTunnel",
			fmt.Sprintf("Cluster %s does not have gRPC tunnel configured, but pipeline references tunnel target policies", cluster.Name),
		); err != nil {
			logger.Error(err, "failed to update pipeline status with error")
		}
		return nil, nil, nil
	}
	tunnelProfileNames := make(map[string]struct{})
	for _, policy := range tunnelTargetPolicies {
		pipelineData.TunnelTargetPolicies[policy.Namespace+gnmic.Delimiter+policy.Name] = policy.Spec
		if policy.Spec.Profile != "" {
			tunnelProfileNames[policy.Spec.Profile] = struct{}{}
		}
	}
	unresolved, err = r.resolveTargetProfiles(ctx, pipeline.Namespace, tunnelProfileNames, pipelineData)
	if err != nil {
		return nil, nil, err
	}
	unresolvedRefs = append(unresolvedRefs, unresolved...)
	logger.Info("cluster pipeline tunnel target policies", "policies", len(tunnelTargetPolicies))

	return pipelineData, unresolvedRefs, nil
}

// resolveTargetProfiles fetches the named TargetProfiles from namespace into
// data.TargetProfiles, skipping any already present, and returns the names that do
// not exist as unresolved refs.
func (r *ClusterReconciler) resolveTargetProfiles(ctx context.Context, namespace string, names map[string]struct{}, data *gnmic.PipelineData) ([]string, error) {
	var unresolved []string
	for name := range names {
		if _, exists := data.TargetProfiles[namespace+gnmic.Delimiter+name]; exists {
			continue
		}
		var profile gnmicv1alpha1.TargetProfile
		if err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &profile); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, err
			}
			unresolved = append(unresolved, unresolvedRef("targetprofile", name))
			continue
		}
		data.TargetProfiles[profile.Namespace+gnmic.Delimiter+profile.Name] = profile.Spec
	}
	return unresolved, nil
}
