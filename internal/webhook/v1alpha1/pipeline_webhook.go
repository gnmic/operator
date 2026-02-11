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

package v1alpha1

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	operatorv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var pipelinelog = logf.Log.WithName("pipeline-resource")

// SetupPipelineWebhookWithManager registers the webhook for Pipeline in the manager.
func SetupPipelineWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&operatorv1alpha1.Pipeline{}).
		WithValidator(&PipelineCustomValidator{}).
		WithDefaulter(&PipelineCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-operator-gnmic-dev-v1alpha1-pipeline,mutating=true,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=pipelines,verbs=create;update,versions=v1alpha1,name=mpipeline-v1alpha1.kb.io,admissionReviewVersions=v1

// PipelineCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Pipeline when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type PipelineCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &PipelineCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Pipeline.
func (d *PipelineCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	pipeline, ok := obj.(*operatorv1alpha1.Pipeline)

	if !ok {
		return fmt.Errorf("expected an Pipeline object but got %T", obj)
	}
	pipelinelog.Info("Defaulting for Pipeline", "name", pipeline.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-operator-gnmic-dev-v1alpha1-pipeline,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=pipelines,verbs=create;update,versions=v1alpha1,name=vpipeline-v1alpha1.kb.io,admissionReviewVersions=v1

// PipelineCustomValidator struct is responsible for validating the Pipeline resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type PipelineCustomValidator struct{}

var _ webhook.CustomValidator = &PipelineCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Pipeline.
func (v *PipelineCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	pipeline, ok := obj.(*operatorv1alpha1.Pipeline)
	if !ok {
		return nil, fmt.Errorf("expected a Pipeline object but got %T", obj)
	}
	pipelinelog.Info("Validation for Pipeline upon creation", "name", pipeline.GetName())

	return nil, validatePipelineSpec(&pipeline.Spec)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Pipeline.
func (v *PipelineCustomValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	pipeline, ok := newObj.(*operatorv1alpha1.Pipeline)
	if !ok {
		return nil, fmt.Errorf("expected a Pipeline object but got %T", newObj)
	}
	pipelinelog.Info("Validation for Pipeline upon update", "name", pipeline.GetName())

	return nil, validatePipelineSpec(&pipeline.Spec)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Pipeline.
func (v *PipelineCustomValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	pipeline, ok := obj.(*operatorv1alpha1.Pipeline)
	if !ok {
		return nil, fmt.Errorf("expected a Pipeline object but got %T", obj)
	}
	pipelinelog.Info("Validation for Pipeline upon deletion", "name", pipeline.GetName())

	return nil, nil
}

// validatePipelineSpec validates the PipelineSpec fields.
func validatePipelineSpec(spec *operatorv1alpha1.PipelineSpec) error {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	// clusterRef is required.
	if spec.ClusterRef == "" {
		allErrs = append(allErrs, field.Required(
			specPath.Child("clusterRef"),
			"clusterRef is required",
		))
	}

	// at least one data source must be configured:
	// targets (selectors or refs), tunnel target policies (selectors or refs), or inputs (selectors or refs).
	hasTargets := len(spec.TargetSelectors) > 0 || len(spec.TargetRefs) > 0
	hasTunnelTargets := len(spec.TunnelTargetPolicySelectors) > 0 || len(spec.TunnelTargetPolicyRefs) > 0
	hasInputs := len(spec.Inputs.InputSelectors) > 0 || len(spec.Inputs.InputRefs) > 0
	if !hasTargets && !hasTunnelTargets && !hasInputs {
		allErrs = append(allErrs, field.Required(
			specPath,
			"at least one data source is required: configure targetSelectors, targetRefs, tunnelTargetPolicySelectors, tunnelTargetPolicyRefs, or inputs",
		))
	}

	// when targets or tunnel target policies are configured, at least one subscription source is required.
	if hasTargets || hasTunnelTargets {
		hasSubscriptions := len(spec.SubscriptionSelectors) > 0 || len(spec.SubscriptionRefs) > 0
		if !hasSubscriptions {
			allErrs = append(allErrs, field.Required(
				specPath,
				"at least one subscription is required when targets or tunnel target policies are configured: set subscriptionSelectors or subscriptionRefs",
			))
		}
	}

	// at least one output must be configured.
	hasOutputs := len(spec.Outputs.OutputSelectors) > 0 || len(spec.Outputs.OutputRefs) > 0
	if !hasOutputs {
		allErrs = append(allErrs, field.Required(
			specPath.Child("outputs"),
			"at least one output is required: set outputSelectors or outputRefs",
		))
	}

	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(
		operatorv1alpha1.GroupVersion.WithKind("Pipeline").GroupKind(),
		"",
		allErrs,
	)
}
