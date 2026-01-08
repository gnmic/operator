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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	operatorv1alpha1 "github.com/karimra/gnmic-operator/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var processorlog = logf.Log.WithName("processor-resource")

// SetupProcessorWebhookWithManager registers the webhook for Processor in the manager.
func SetupProcessorWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&operatorv1alpha1.Processor{}).
		WithValidator(&ProcessorCustomValidator{}).
		WithDefaulter(&ProcessorCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-operator-gnmic-dev-v1alpha1-processor,mutating=true,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=processors,verbs=create;update,versions=v1alpha1,name=mprocessor-v1alpha1.kb.io,admissionReviewVersions=v1

// ProcessorCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Processor when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type ProcessorCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &ProcessorCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Processor.
func (d *ProcessorCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	processor, ok := obj.(*operatorv1alpha1.Processor)

	if !ok {
		return fmt.Errorf("expected an Processor object but got %T", obj)
	}
	processorlog.Info("Defaulting for Processor", "name", processor.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-operator-gnmic-dev-v1alpha1-processor,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=processors,verbs=create;update,versions=v1alpha1,name=vprocessor-v1alpha1.kb.io,admissionReviewVersions=v1

// ProcessorCustomValidator struct is responsible for validating the Processor resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ProcessorCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &ProcessorCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Processor.
func (v *ProcessorCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	processor, ok := obj.(*operatorv1alpha1.Processor)
	if !ok {
		return nil, fmt.Errorf("expected a Processor object but got %T", obj)
	}
	processorlog.Info("Validation for Processor upon creation", "name", processor.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Processor.
func (v *ProcessorCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	processor, ok := newObj.(*operatorv1alpha1.Processor)
	if !ok {
		return nil, fmt.Errorf("expected a Processor object for the newObj but got %T", newObj)
	}
	processorlog.Info("Validation for Processor upon update", "name", processor.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Processor.
func (v *ProcessorCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	processor, ok := obj.(*operatorv1alpha1.Processor)
	if !ok {
		return nil, fmt.Errorf("expected a Processor object but got %T", obj)
	}
	processorlog.Info("Validation for Processor upon deletion", "name", processor.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
