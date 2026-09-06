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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	operatorv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/discovery"
	"github.com/gnmic/operator/internal/discovery/provider"
)

// nolint:unused
// log is for logging in this package.
var targetsourcelog = logf.Log.WithName("targetsource-resource")

// SetupTargetSourceWebhookWithManager registers the webhook for TargetSource in the manager.
func SetupTargetSourceWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &operatorv1alpha1.TargetSource{}).
		WithValidator(&TargetSourceCustomValidator{Reader: mgr.GetAPIReader()}).
		WithDefaulter(&TargetSourceCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-operator-gnmic-dev-v1alpha1-targetsource,mutating=true,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=targetsources,verbs=create;update,versions=v1alpha1,name=mtargetsource-v1alpha1.kb.io,admissionReviewVersions=v1

// TargetSourceCustomDefaulter is registered but empty on purpose. Defaults live
// in the CRD schema so that kubectl explain and a client-side dry run agree
// with the server.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type TargetSourceCustomDefaulter struct{}

var _ admission.Defaulter[*operatorv1alpha1.TargetSource] = &TargetSourceCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind TargetSource.
func (d *TargetSourceCustomDefaulter) Default(_ context.Context, _ *operatorv1alpha1.TargetSource) error {
	return nil
}

// +kubebuilder:webhook:path=/validate-operator-gnmic-dev-v1alpha1-targetsource,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.gnmic.dev,resources=targetsources,verbs=create;update,versions=v1alpha1,name=vtargetsource-v1alpha1.kb.io,admissionReviewVersions=v1

// TargetSourceCustomValidator rejects what the CRD schema cannot express, above
// all a CEL expression that does not compile. Today a typo produces fewer
// targets and a green status; here it produces a rejected apply with the
// field path.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type TargetSourceCustomValidator struct {
	// Reader looks up referenced objects for warnings. Nil disables the
	// warnings, which is what unit tests want.
	Reader client.Reader
}

var _ admission.Validator[*operatorv1alpha1.TargetSource] = &TargetSourceCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type TargetSource.
func (v *TargetSourceCustomValidator) ValidateCreate(ctx context.Context, ts *operatorv1alpha1.TargetSource) (admission.Warnings, error) {
	targetsourcelog.Info("Validation for TargetSource upon creation", "name", ts.GetName())
	warnings := unwatchedNamespaceWarning("TargetSource", ts.GetNamespace())
	warnings = append(warnings, v.referenceWarnings(ctx, ts)...)
	return warnings, invalidTargetSource(ts, validateTargetSourceSpec(&ts.Spec))
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type TargetSource.
func (v *TargetSourceCustomValidator) ValidateUpdate(ctx context.Context, _ *operatorv1alpha1.TargetSource, ts *operatorv1alpha1.TargetSource) (admission.Warnings, error) {
	targetsourcelog.Info("Validation for TargetSource upon update", "name", ts.GetName())
	return v.referenceWarnings(ctx, ts), invalidTargetSource(ts, validateTargetSourceSpec(&ts.Spec))
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type TargetSource.
func (v *TargetSourceCustomValidator) ValidateDelete(_ context.Context, _ *operatorv1alpha1.TargetSource) (admission.Warnings, error) {
	return nil, nil
}

func invalidTargetSource(ts *operatorv1alpha1.TargetSource, errs field.ErrorList) error {
	if len(errs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: operatorv1alpha1.GroupVersion.Group, Kind: "TargetSource"}, ts.GetName(), errs)
}

const (
	minTargetSourceInterval = 10 * time.Second
	minTargetSourceTimeout  = time.Second
)

// validateTargetSourceSpec checks everything the schema cannot: expression
// syntax, cross-field rules, and the discriminator (repeated here so the
// message is readable).
func validateTargetSourceSpec(spec *operatorv1alpha1.TargetSourceSpec) field.ErrorList {
	var errs field.ErrorList
	root := field.NewPath("spec")

	errs = append(errs, validateSource(&spec.Source, root.Child("source"))...)

	if spec.Interval != nil && spec.Interval.Duration < minTargetSourceInterval {
		errs = append(errs, field.Invalid(root.Child("interval"), spec.Interval.Duration.String(), "must be at least 10s"))
	}
	if spec.Timeout != nil && spec.Timeout.Duration < minTargetSourceTimeout {
		errs = append(errs, field.Invalid(root.Child("timeout"), spec.Timeout.Duration.String(), "must be at least 1s"))
	}

	if p := spec.Target.Port; p != 0 && !isValidPort(p) {
		errs = append(errs, field.Invalid(root.Child("target", "port"), p, "must be between 1 and 65535"))
	}
	if r := spec.Prune.MaxDeleteRatio; r != nil && (*r < 0 || *r > 100) {
		errs = append(errs, field.Invalid(root.Child("prune", "maxDeleteRatio"), *r, "must be between 0 and 100"))
	}
	if m := spec.MaxTargets; m != nil && *m < 0 {
		errs = append(errs, field.Invalid(root.Child("maxTargets"), *m, "must not be negative"))
	}
	errs = append(errs, validateWebhook(spec.Webhook, root.Child("webhook"))...)
	return errs
}

func validateSource(src *operatorv1alpha1.SourceSpec, path *field.Path) field.ErrorList {
	var errs field.ErrorList
	set := map[operatorv1alpha1.SourceType]bool{
		operatorv1alpha1.SourceTypeHTTP:      src.HTTP != nil,
		operatorv1alpha1.SourceTypeStatic:    src.Static != nil,
		operatorv1alpha1.SourceTypeConfigMap: src.ConfigMap != nil,
		operatorv1alpha1.SourceTypeSecret:    src.Secret != nil,
	}
	if _, known := set[src.Type]; !known {
		types := make([]string, 0, len(set))
		for t := range set {
			types = append(types, string(t))
		}
		return append(errs, field.NotSupported(path.Child("type"), string(src.Type), types))
	}
	for t, present := range set {
		fieldName := sourceFieldName(t)
		switch {
		case t == src.Type && !present:
			errs = append(errs, field.Required(path.Child(fieldName), fmt.Sprintf("required when type is %s", t)))
		case t != src.Type && present:
			errs = append(errs, field.Forbidden(path.Child(fieldName), fmt.Sprintf("only allowed when type is %s", t)))
		}
	}

	switch src.Type {
	case operatorv1alpha1.SourceTypeHTTP:
		if src.HTTP != nil {
			errs = append(errs, validateHTTPSource(src.HTTP, path.Child("http"))...)
		}
	case operatorv1alpha1.SourceTypeStatic:
		if src.Static != nil {
			errs = append(errs, validateStaticSource(src.Static, path.Child("static"))...)
		}
	case operatorv1alpha1.SourceTypeConfigMap:
		if src.ConfigMap != nil {
			errs = append(errs, validateObjectSource(src.ConfigMap, path.Child("configMap"))...)
		}
	case operatorv1alpha1.SourceTypeSecret:
		if src.Secret != nil {
			errs = append(errs, validateObjectSource(src.Secret, path.Child("secret"))...)
		}
	}
	return errs
}

func sourceFieldName(t operatorv1alpha1.SourceType) string {
	switch t {
	case operatorv1alpha1.SourceTypeHTTP:
		return "http"
	case operatorv1alpha1.SourceTypeStatic:
		return "static"
	case operatorv1alpha1.SourceTypeConfigMap:
		return "configMap"
	case operatorv1alpha1.SourceTypeSecret:
		return "secret"
	}
	return strings.ToLower(string(t))
}

func validateHTTPSource(src *operatorv1alpha1.HTTPSource, path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if !strings.HasPrefix(src.URL, "http://") && !strings.HasPrefix(src.URL, "https://") {
		errs = append(errs, field.Invalid(path.Child("url"), src.URL, "must start with http:// or https://"))
	}
	if src.Method != "" && src.Method != "GET" && src.Method != "POST" {
		errs = append(errs, field.NotSupported(path.Child("method"), src.Method, []string{"GET", "POST"}))
	}
	if src.Body != "" && src.Method != "POST" {
		errs = append(errs, field.Forbidden(path.Child("body"), "only sent with method POST"))
	}
	if a := src.Auth; a != nil {
		n := 0
		for _, set := range []bool{a.Basic != nil, a.Token != nil, a.Header != nil} {
			if set {
				n++
			}
		}
		if n != 1 {
			errs = append(errs, field.Invalid(path.Child("auth"), "", "exactly one of basic, token or header must be set"))
		}
	}
	errs = append(errs, validateMapping(src.Mapping, path.Child("mapping"))...)
	if p := src.Pagination; p != nil {
		if p.NextField != "" {
			if _, err := provider.CompileExpression(p.NextField); err != nil {
				errs = append(errs, field.Invalid(path.Child("pagination", "nextField"), p.NextField, err.Error()))
			}
		}
		if p.RequestParam != "" && p.NextField == "" {
			errs = append(errs, field.Required(path.Child("pagination", "nextField"), "required when requestParam is set"))
		}
		if p.MaxPages < 0 {
			errs = append(errs, field.Invalid(path.Child("pagination", "maxPages"), p.MaxPages, "must be at least 1"))
		}
	}
	return errs
}

func validateStaticSource(src *operatorv1alpha1.StaticSource, path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if len(src.Devices) == 0 {
		errs = append(errs, field.Required(path.Child("devices"), "at least one device"))
	}
	for i, d := range src.Devices {
		p := path.Child("devices").Index(i)
		if d.Name == "" {
			errs = append(errs, field.Required(p.Child("name"), ""))
		}
		if d.Address == "" {
			errs = append(errs, field.Required(p.Child("address"), ""))
		}
		if d.Port != 0 && !isValidPort(d.Port) {
			errs = append(errs, field.Invalid(p.Child("port"), d.Port, "must be between 1 and 65535"))
		}
	}
	return errs
}

func validateObjectSource(src *operatorv1alpha1.ObjectSource, path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if src.Name == "" {
		errs = append(errs, field.Required(path.Child("name"), ""))
	}
	return append(errs, validateMapping(src.Mapping, path.Child("mapping"))...)
}

// validateMapping compiles every expression. This is the single highest-value
// rule in the webhook.
func validateMapping(m *operatorv1alpha1.MappingSpec, path *field.Path) field.ErrorList {
	if m == nil {
		return nil
	}
	var errs field.ErrorList
	for _, f := range []struct{ name, expr string }{
		{"items", m.Items}, {"name", m.Name}, {"address", m.Address}, {"port", m.Port}, {"profile", m.Profile}, {"labels", m.Labels},
	} {
		if f.expr == "" {
			continue
		}
		if _, err := provider.CompileExpression(f.expr); err != nil {
			errs = append(errs, field.Invalid(path.Child(f.name), f.expr, err.Error()))
		}
	}
	if m.OnError != "" && m.OnError != operatorv1alpha1.MappingErrorSkip && m.OnError != operatorv1alpha1.MappingErrorFail {
		errs = append(errs, field.NotSupported(path.Child("onError"), string(m.OnError), []string{"Skip", "Fail"}))
	}
	return errs
}

func validateWebhook(w *operatorv1alpha1.WebhookSpec, path *field.Path) field.ErrorList {
	if w == nil {
		return nil
	}
	var errs field.ErrorList
	if w.Enabled && (w.Auth == nil || (w.Auth.Bearer == nil && w.Auth.Signature == nil)) {
		errs = append(errs, field.Required(path.Child("auth"), "required when the webhook is enabled; an unauthenticated endpoint would let anyone drive fetches against the source"))
	}
	if w.Auth != nil && w.Auth.Signature != nil {
		if alg := w.Auth.Signature.Algorithm; alg != "" && alg != "sha256" && alg != "sha512" {
			errs = append(errs, field.NotSupported(path.Child("auth", "signature", "algorithm"), alg, []string{"sha256", "sha512"}))
		}
	}
	if w.Debounce != nil && w.Debounce.Duration < 0 {
		errs = append(errs, field.Invalid(path.Child("debounce"), w.Debounce.Duration.String(), "must not be negative"))
	}
	return errs
}

// referenceWarnings reports referenced Secrets, ConfigMaps and the
// TargetProfile that do not exist yet. Warnings, not rejections: a resource
// may legitimately be applied before the objects it names.
func (v *TargetSourceCustomValidator) referenceWarnings(ctx context.Context, ts *operatorv1alpha1.TargetSource) admission.Warnings {
	if v.Reader == nil {
		return nil
	}
	var warnings admission.Warnings
	for _, name := range discovery.ReferencedSecrets(&ts.Spec) {
		if apierrors.IsNotFound(v.Reader.Get(ctx, types.NamespacedName{Namespace: ts.Namespace, Name: name}, &corev1.Secret{})) {
			warnings = append(warnings, fmt.Sprintf("Secret %q does not exist in namespace %q; discovery will report Stalled until it does", name, ts.Namespace))
		}
	}
	for _, name := range discovery.ReferencedConfigMaps(&ts.Spec) {
		if apierrors.IsNotFound(v.Reader.Get(ctx, types.NamespacedName{Namespace: ts.Namespace, Name: name}, &corev1.ConfigMap{})) {
			warnings = append(warnings, fmt.Sprintf("ConfigMap %q does not exist in namespace %q; discovery will report Stalled until it does", name, ts.Namespace))
		}
	}
	if p := ts.Spec.Target.Profile; p != "" {
		if apierrors.IsNotFound(v.Reader.Get(ctx, types.NamespacedName{Namespace: ts.Namespace, Name: p}, &operatorv1alpha1.TargetProfile{})) {
			warnings = append(warnings, fmt.Sprintf("TargetProfile %q does not exist in namespace %q; Targets will not be collected until it does", p, ts.Namespace))
		}
	}
	return warnings
}
