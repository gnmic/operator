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
	"regexp"

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
var tunneltargetpolicylog = logf.Log.WithName("tunneltargetpolicy-resource")

// SetupTunnelTargetPolicyWebhookWithManager registers the webhook for TunnelTargetPolicy in the manager.
func SetupTunnelTargetPolicyWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&operatorv1alpha1.TunnelTargetPolicy{}).
		WithValidator(&TunnelTargetPolicyCustomValidator{}).
		WithDefaulter(&TunnelTargetPolicyCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-operator-gnmic-dev-v1alpha1-tunneltargetpolicy,mutating=true,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=tunneltargetpolicies,verbs=create;update,versions=v1alpha1,name=mtunneltargetpolicy-v1alpha1.kb.io,admissionReviewVersions=v1

// TunnelTargetPolicyCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind TunnelTargetPolicy when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type TunnelTargetPolicyCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &TunnelTargetPolicyCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind TunnelTargetPolicy.
func (d *TunnelTargetPolicyCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	tunneltargetpolicy, ok := obj.(*operatorv1alpha1.TunnelTargetPolicy)

	if !ok {
		return fmt.Errorf("expected an TunnelTargetPolicy object but got %T", obj)
	}
	tunneltargetpolicylog.Info("Defaulting for TunnelTargetPolicy", "name", tunneltargetpolicy.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-operator-gnmic-dev-v1alpha1-tunneltargetpolicy,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=tunneltargetpolicies,verbs=create;update,versions=v1alpha1,name=vtunneltargetpolicy-v1alpha1.kb.io,admissionReviewVersions=v1

// TunnelTargetPolicyCustomValidator struct is responsible for validating the TunnelTargetPolicy resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type TunnelTargetPolicyCustomValidator struct{}

var _ webhook.CustomValidator = &TunnelTargetPolicyCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type TunnelTargetPolicy.
func (v *TunnelTargetPolicyCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	tunneltargetpolicy, ok := obj.(*operatorv1alpha1.TunnelTargetPolicy)
	if !ok {
		return nil, fmt.Errorf("expected a TunnelTargetPolicy object but got %T", obj)
	}
	tunneltargetpolicylog.Info("Validation for TunnelTargetPolicy upon creation", "name", tunneltargetpolicy.GetName())

	return nil, validateTunnelTargetPolicySpec(&tunneltargetpolicy.Spec)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type TunnelTargetPolicy.
func (v *TunnelTargetPolicyCustomValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	tunneltargetpolicy, ok := newObj.(*operatorv1alpha1.TunnelTargetPolicy)
	if !ok {
		return nil, fmt.Errorf("expected a TunnelTargetPolicy object but got %T", newObj)
	}
	tunneltargetpolicylog.Info("Validation for TunnelTargetPolicy upon update", "name", tunneltargetpolicy.GetName())

	return nil, validateTunnelTargetPolicySpec(&tunneltargetpolicy.Spec)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type TunnelTargetPolicy.
func (v *TunnelTargetPolicyCustomValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	tunneltargetpolicy, ok := obj.(*operatorv1alpha1.TunnelTargetPolicy)
	if !ok {
		return nil, fmt.Errorf("expected a TunnelTargetPolicy object but got %T", obj)
	}
	tunneltargetpolicylog.Info("Validation for TunnelTargetPolicy upon deletion", "name", tunneltargetpolicy.GetName())

	return nil, nil
}

// validateTunnelTargetPolicySpec validates the TunnelTargetPolicySpec fields.
func validateTunnelTargetPolicySpec(spec *operatorv1alpha1.TunnelTargetPolicySpec) error {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	// profile is required.
	if spec.Profile == "" {
		allErrs = append(allErrs, field.Required(
			specPath.Child("profile"),
			"profile is required",
		))
	}

	// validate match regex patterns when set.
	if spec.Match != nil {
		matchPath := specPath.Child("match")

		if spec.Match.Type != "" {
			if _, err := regexp.Compile(spec.Match.Type); err != nil {
				allErrs = append(allErrs, field.Invalid(
					matchPath.Child("type"),
					spec.Match.Type,
					fmt.Sprintf("must be a valid regex: %v", err),
				))
			}
		}

		if spec.Match.ID != "" {
			if _, err := regexp.Compile(spec.Match.ID); err != nil {
				allErrs = append(allErrs, field.Invalid(
					matchPath.Child("id"),
					spec.Match.ID,
					fmt.Sprintf("must be a valid regex: %v", err),
				))
			}
		}
	}

	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(
		operatorv1alpha1.GroupVersion.WithKind("TunnelTargetPolicy").GroupKind(),
		"",
		allErrs,
	)
}
