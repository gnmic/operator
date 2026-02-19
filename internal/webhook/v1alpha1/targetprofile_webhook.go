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
	"time"

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
var targetprofilelog = logf.Log.WithName("targetprofile-resource")

// SetupTargetProfileWebhookWithManager registers the webhook for TargetProfile in the manager.
func SetupTargetProfileWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&operatorv1alpha1.TargetProfile{}).
		WithValidator(&TargetProfileCustomValidator{}).
		WithDefaulter(&TargetProfileCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-operator-gnmic-dev-v1alpha1-targetprofile,mutating=true,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=targetprofiles,verbs=create;update,versions=v1alpha1,name=mtargetprofile-v1alpha1.kb.io,admissionReviewVersions=v1

// TargetProfileCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind TargetProfile when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type TargetProfileCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &TargetProfileCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind TargetProfile.
func (d *TargetProfileCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	targetprofile, ok := obj.(*operatorv1alpha1.TargetProfile)

	if !ok {
		return fmt.Errorf("expected an TargetProfile object but got %T", obj)
	}
	targetprofilelog.Info("Defaulting for TargetProfile", "name", targetprofile.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-operator-gnmic-dev-v1alpha1-targetprofile,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=targetprofiles,verbs=create;update,versions=v1alpha1,name=vtargetprofile-v1alpha1.kb.io,admissionReviewVersions=v1

// TargetProfileCustomValidator struct is responsible for validating the TargetProfile resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type TargetProfileCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &TargetProfileCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type TargetProfile.
func (v *TargetProfileCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	targetprofile, ok := obj.(*operatorv1alpha1.TargetProfile)
	if !ok {
		return nil, fmt.Errorf("expected a TargetProfile object but got %T", obj)
	}
	targetprofilelog.Info("Validation for TargetProfile upon creation", "name", targetprofile.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type TargetProfile.
func (v *TargetProfileCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	targetprofile, ok := newObj.(*operatorv1alpha1.TargetProfile)
	if !ok {
		return nil, fmt.Errorf("expected a TargetProfile object for the newObj but got %T", newObj)
	}
	targetprofilelog.Info("Validation for TargetProfile upon update", "name", targetprofile.GetName())

	errs := validateTargetProfileSpec(&targetprofile.Spec)
	if len(errs) > 0 {
		return nil, apierrors.NewInvalid(
			operatorv1alpha1.GroupVersion.WithKind("TargetProfile").GroupKind(),
			targetprofile.GetName(),
			errs,
		)
	}
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type TargetProfile.
func (v *TargetProfileCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	targetprofile, ok := obj.(*operatorv1alpha1.TargetProfile)
	if !ok {
		return nil, fmt.Errorf("expected a TargetProfile object but got %T", obj)
	}
	targetprofilelog.Info("Validation for TargetProfile upon deletion", "name", targetprofile.GetName())

	errs := validateTargetProfileSpec(&targetprofile.Spec)
	if len(errs) > 0 {
		return nil, apierrors.NewInvalid(
			operatorv1alpha1.GroupVersion.WithKind("TargetProfile").GroupKind(),
			targetprofile.GetName(),
			errs,
		)
	}
	return nil, nil
}

func validateTargetProfileSpec(spec *operatorv1alpha1.TargetProfileSpec) field.ErrorList {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	if spec.Timeout.Duration < 1*time.Second {
		allErrs = append(allErrs, field.Invalid(specPath.Child("timeout"), spec.Timeout.Duration, "timeout must be at least 1 second"))
	}
	if spec.RetryTimer.Duration < 1*time.Second {
		allErrs = append(allErrs, field.Invalid(specPath.Child("retryTimer"), spec.RetryTimer.Duration, "retry timer must be at least 1 second"))
	}
	if spec.Encoding != "JSON" && spec.Encoding != "BYTES" && spec.Encoding != "PROTO" && spec.Encoding != "ASCII" && spec.Encoding != "JSON_IETF" {
		allErrs = append(allErrs, field.Invalid(specPath.Child("encoding"), spec.Encoding, "encoding must be one of JSON, BYTES, PROTO, ASCII, or JSON_IETF"))
	}

	if spec.TLS != nil {
		tlsPath := specPath.Child("tls")
		if spec.TLS.MaxVersion != "" && spec.TLS.MaxVersion != "1.1" && spec.TLS.MaxVersion != "1.2" && spec.TLS.MaxVersion != "1.3" {
			allErrs = append(allErrs, field.Invalid(tlsPath.Child("maxVersion"), spec.TLS.MaxVersion, "TLS max version must be one of 1.1, 1.2, or 1.3"))
		}
		if spec.TLS.MinVersion != "" && spec.TLS.MinVersion != "1.1" && spec.TLS.MinVersion != "1.2" && spec.TLS.MinVersion != "1.3" {
			allErrs = append(allErrs, field.Invalid(tlsPath.Child("minVersion"), spec.TLS.MinVersion, "TLS min version must be one of 1.1, 1.2, or 1.3"))
		}
	}
	if spec.TCPKeepAlive != nil && spec.TCPKeepAlive.Duration < 1*time.Second {
		tcpKeepAlivePath := specPath.Child("tcpKeepAlive")
		allErrs = append(allErrs, field.Invalid(tcpKeepAlivePath.Child("duration"), spec.TCPKeepAlive.Duration, "TCP keep-alive interval must be at least 1 second"))
	}
	if spec.GRCPKeepAlive != nil {
		grpcKeepAlivePath := specPath.Child("grpcKeepAlive")
		if spec.GRCPKeepAlive.Time.Duration < 1*time.Second {
			allErrs = append(allErrs, field.Invalid(grpcKeepAlivePath.Child("time", "duration"), spec.GRCPKeepAlive.Time.Duration, "gRPC keep-alive time must be at least 1 second"))
		}
		if spec.GRCPKeepAlive.Timeout.Duration < 1*time.Second {
			allErrs = append(allErrs, field.Invalid(grpcKeepAlivePath.Child("timeout"), spec.GRCPKeepAlive.Timeout.Duration, "gRPC keep-alive timeout must be at least 1 second"))
		}
	}
	return allErrs
}
