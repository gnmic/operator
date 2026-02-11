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
	"strings"

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
var subscriptionlog = logf.Log.WithName("subscription-resource")

// SetupSubscriptionWebhookWithManager registers the webhook for Subscription in the manager.
func SetupSubscriptionWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&operatorv1alpha1.Subscription{}).
		WithValidator(&SubscriptionCustomValidator{}).
		WithDefaulter(&SubscriptionCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-operator-gnmic-dev-v1alpha1-subscription,mutating=true,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=subscriptions,verbs=create;update,versions=v1alpha1,name=msubscription-v1alpha1.kb.io,admissionReviewVersions=v1

// SubscriptionCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Subscription when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type SubscriptionCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &SubscriptionCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Subscription.
func (d *SubscriptionCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	subscription, ok := obj.(*operatorv1alpha1.Subscription)

	if !ok {
		return fmt.Errorf("expected an Subscription object but got %T", obj)
	}
	subscriptionlog.Info("Defaulting for Subscription", "name", subscription.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-operator-gnmic-dev-v1alpha1-subscription,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=subscriptions,verbs=create;update,versions=v1alpha1,name=vsubscription-v1alpha1.kb.io,admissionReviewVersions=v1

// SubscriptionCustomValidator struct is responsible for validating the Subscription resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type SubscriptionCustomValidator struct{}

var _ webhook.CustomValidator = &SubscriptionCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Subscription.
func (v *SubscriptionCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	subscription, ok := obj.(*operatorv1alpha1.Subscription)
	if !ok {
		return nil, fmt.Errorf("expected a Subscription object but got %T", obj)
	}
	subscriptionlog.Info("Validation for Subscription upon creation", "name", subscription.GetName())

	return nil, validateSubscriptionSpec(&subscription.Spec)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Subscription.
func (v *SubscriptionCustomValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	subscription, ok := newObj.(*operatorv1alpha1.Subscription)
	if !ok {
		return nil, fmt.Errorf("expected a Subscription object but got %T", newObj)
	}
	subscriptionlog.Info("Validation for Subscription upon update", "name", subscription.GetName())

	return nil, validateSubscriptionSpec(&subscription.Spec)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Subscription.
func (v *SubscriptionCustomValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	subscription, ok := obj.(*operatorv1alpha1.Subscription)
	if !ok {
		return nil, fmt.Errorf("expected a Subscription object but got %T", obj)
	}
	subscriptionlog.Info("Validation for Subscription upon deletion", "name", subscription.GetName())

	return nil, nil
}

// validateSubscriptionSpec validates the SubscriptionSpec fields.
func validateSubscriptionSpec(spec *operatorv1alpha1.SubscriptionSpec) error {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	// at least one path is required unless streamSubscriptions are provided.
	if len(spec.Paths) == 0 && len(spec.StreamSubscriptions) == 0 {
		allErrs = append(allErrs, field.Required(
			specPath.Child("paths"),
			"at least one path is required when streamSubscriptions is empty",
		))
	}

	// when streamSubscriptions are set, the mode must be a STREAM mode.
	if len(spec.StreamSubscriptions) > 0 && !strings.HasPrefix(spec.Mode, "STREAM") {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("mode"),
			spec.Mode,
			"mode must be STREAM when streamSubscriptions are set",
		))
	}

	// sampleInterval must be positive when set.
	if spec.SampleInterval.Duration < 0 {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("sampleInterval"),
			spec.SampleInterval.Duration.String(),
			"sampleInterval must be a positive duration",
		))
	}

	// heartbeatInterval must be positive when set.
	if spec.HeartbeatInterval.Duration < 0 {
		allErrs = append(allErrs, field.Invalid(
			specPath.Child("heartbeatInterval"),
			spec.HeartbeatInterval.Duration.String(),
			"heartbeatInterval must be a positive duration",
		))
	}

	// history: start must be before end when both are set.
	if spec.History != nil {
		if !spec.History.Start.IsZero() && !spec.History.End.IsZero() && spec.History.Start.Before(&spec.History.End) {
			allErrs = append(allErrs, field.Invalid(
				specPath.Child("history", "start"),
				spec.History.Start,
				"history start must be before end",
			))
		}
	}

	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(
		operatorv1alpha1.GroupVersion.WithKind("Subscription").GroupKind(),
		"",
		allErrs,
	)
}
