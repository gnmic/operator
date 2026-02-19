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
	"net"
	"strconv"

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
var targetlog = logf.Log.WithName("target-resource")

// SetupTargetWebhookWithManager registers the webhook for Target in the manager.
func SetupTargetWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&operatorv1alpha1.Target{}).
		WithValidator(&TargetCustomValidator{}).
		WithDefaulter(&TargetCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-operator-gnmic-dev-v1alpha1-target,mutating=true,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=targets,verbs=create;update,versions=v1alpha1,name=mtarget-v1alpha1.kb.io,admissionReviewVersions=v1

// TargetCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Target when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type TargetCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &TargetCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Target.
func (d *TargetCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	target, ok := obj.(*operatorv1alpha1.Target)

	if !ok {
		return fmt.Errorf("expected an Target object but got %T", obj)
	}
	targetlog.Info("Defaulting for Target", "name", target.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-operator-gnmic-dev-v1alpha1-target,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=targets,verbs=create;update,versions=v1alpha1,name=vtarget-v1alpha1.kb.io,admissionReviewVersions=v1

// TargetCustomValidator struct is responsible for validating the Target resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type TargetCustomValidator struct{}

var _ webhook.CustomValidator = &TargetCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Target.
func (v *TargetCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	target, ok := obj.(*operatorv1alpha1.Target)
	if !ok {
		return nil, fmt.Errorf("expected a Target object but got %T", obj)
	}
	targetlog.Info("Validation for Target upon creation", "name", target.GetName())

	return nil, validateTargetSpec(target.GetName(), &target.Spec)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Target.
func (v *TargetCustomValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	target, ok := newObj.(*operatorv1alpha1.Target)
	if !ok {
		return nil, fmt.Errorf("expected a Target object but got %T", newObj)
	}
	targetlog.Info("Validation for Target upon update", "name", target.GetName())

	return nil, validateTargetSpec(target.GetName(), &target.Spec)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Target.
func (v *TargetCustomValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	target, ok := obj.(*operatorv1alpha1.Target)
	if !ok {
		return nil, fmt.Errorf("expected a Target object but got %T", obj)
	}
	targetlog.Info("Validation for Target upon deletion", "name", target.GetName())

	return nil, nil
}

// validateTargetSpec validates the TargetSpec fields.
func validateTargetSpec(name string, spec *operatorv1alpha1.TargetSpec) error {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	// address is required.
	if spec.Address == "" {
		allErrs = append(allErrs, field.Required(
			specPath.Child("address"),
			"address is required",
		))
	} else {
		// address must be a valid host:port.
		host, portStr, err := net.SplitHostPort(spec.Address)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(
				specPath.Child("address"),
				spec.Address,
				"address must be in host:port format",
			))
		} else {
			if host == "" {
				allErrs = append(allErrs, field.Invalid(
					specPath.Child("address"),
					spec.Address,
					"address must include a host",
				))
			}
			port, err := strconv.Atoi(portStr)
			if err != nil || port < 1 || port > 65535 {
				allErrs = append(allErrs, field.Invalid(
					specPath.Child("address"),
					spec.Address,
					"port must be between 1 and 65535",
				))
			}
		}
	}

	// profile is required.
	if spec.Profile == "" {
		allErrs = append(allErrs, field.Required(
			specPath.Child("profile"),
			"profile is required",
		))
	}

	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(
		operatorv1alpha1.GroupVersion.WithKind("Target").GroupKind(),
		name,
		allErrs,
	)
}
