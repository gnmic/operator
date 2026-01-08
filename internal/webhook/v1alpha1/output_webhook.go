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
var outputlog = logf.Log.WithName("output-resource")

// SetupOutputWebhookWithManager registers the webhook for Output in the manager.
func SetupOutputWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&operatorv1alpha1.Output{}).
		WithValidator(&OutputCustomValidator{}).
		WithDefaulter(&OutputCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-operator-gnmic-dev-v1alpha1-output,mutating=true,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=outputs,verbs=create;update,versions=v1alpha1,name=moutput-v1alpha1.kb.io,admissionReviewVersions=v1

// OutputCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Output when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type OutputCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &OutputCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Output.
func (d *OutputCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	output, ok := obj.(*operatorv1alpha1.Output)

	if !ok {
		return fmt.Errorf("expected an Output object but got %T", obj)
	}
	outputlog.Info("Defaulting for Output", "name", output.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-operator-gnmic-dev-v1alpha1-output,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=outputs,verbs=create;update,versions=v1alpha1,name=voutput-v1alpha1.kb.io,admissionReviewVersions=v1

// OutputCustomValidator struct is responsible for validating the Output resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type OutputCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &OutputCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Output.
func (v *OutputCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	output, ok := obj.(*operatorv1alpha1.Output)
	if !ok {
		return nil, fmt.Errorf("expected a Output object but got %T", obj)
	}
	outputlog.Info("Validation for Output upon creation", "name", output.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Output.
func (v *OutputCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	output, ok := newObj.(*operatorv1alpha1.Output)
	if !ok {
		return nil, fmt.Errorf("expected a Output object for the newObj but got %T", newObj)
	}
	outputlog.Info("Validation for Output upon update", "name", output.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Output.
func (v *OutputCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	output, ok := obj.(*operatorv1alpha1.Output)
	if !ok {
		return nil, fmt.Errorf("expected a Output object but got %T", obj)
	}
	outputlog.Info("Validation for Output upon deletion", "name", output.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
