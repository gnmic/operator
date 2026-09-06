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
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func validHTTPTargetSource() *operatorv1alpha1.TargetSource {
	return &operatorv1alpha1.TargetSource{
		ObjectMeta: metav1.ObjectMeta{Name: "inv", Namespace: "default"},
		Spec: operatorv1alpha1.TargetSourceSpec{
			Interval: &metav1.Duration{Duration: 5 * time.Minute},
			Timeout:  &metav1.Duration{Duration: time.Minute},
			Source: operatorv1alpha1.SourceSpec{Type: operatorv1alpha1.SourceTypeHTTP, HTTP: &operatorv1alpha1.HTTPSource{
				EndpointSpec: operatorv1alpha1.EndpointSpec{
					URL:  "https://inventory.example.com/devices",
					Auth: &operatorv1alpha1.AuthSpec{Token: &operatorv1alpha1.TokenAuth{SecretRef: operatorv1alpha1.SecretKeyReference{Name: "tok", Key: "token"}}},
				},
				Mapping: &operatorv1alpha1.MappingSpec{Items: "self.results", Name: "item.hostname", Address: "item.mgmt_ip"},
			}},
			Target: operatorv1alpha1.TargetTemplateSpec{Port: 57400, Profile: "default"},
		},
	}
}

func TestValidateTargetSourceSpec(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*operatorv1alpha1.TargetSource)
		wantErr string // substring of the field path or message; "" means valid
	}{
		{"valid http", nil, ""},
		{"valid static", func(ts *operatorv1alpha1.TargetSource) {
			ts.Spec.Source = operatorv1alpha1.SourceSpec{Type: operatorv1alpha1.SourceTypeStatic, Static: &operatorv1alpha1.StaticSource{
				Devices: []operatorv1alpha1.StaticDevice{{Name: "a", Address: "10.0.0.1"}}}}
		}, ""},
		{"valid configmap without mapping", func(ts *operatorv1alpha1.TargetSource) {
			ts.Spec.Source = operatorv1alpha1.SourceSpec{Type: operatorv1alpha1.SourceTypeConfigMap, ConfigMap: &operatorv1alpha1.ObjectSource{Name: "inv"}}
		}, ""},
		{"cel syntax error", func(ts *operatorv1alpha1.TargetSource) { ts.Spec.Source.HTTP.Mapping.Address = "item.mgmt_ip[" }, "spec.source.http.mapping.address"},
		{"cel unknown function", func(ts *operatorv1alpha1.TargetSource) { ts.Spec.Source.HTTP.Mapping.Labels = "frobnicate(item)" }, "spec.source.http.mapping.labels"},
		{"pagination expression", func(ts *operatorv1alpha1.TargetSource) {
			ts.Spec.Source.HTTP.Pagination = &operatorv1alpha1.PaginationSpec{NextField: "self.next)"}
		}, "spec.source.http.pagination.nextField"},
		{"discriminator mismatch", func(ts *operatorv1alpha1.TargetSource) { ts.Spec.Source.Type = operatorv1alpha1.SourceTypeStatic }, "spec.source.static"},
		{"two sources", func(ts *operatorv1alpha1.TargetSource) {
			ts.Spec.Source.Static = &operatorv1alpha1.StaticSource{Devices: []operatorv1alpha1.StaticDevice{{Name: "a", Address: "b"}}}
		}, "spec.source.static"},
		{"unknown type", func(ts *operatorv1alpha1.TargetSource) { ts.Spec.Source.Type = "NetBox" }, "spec.source.type"},
		{"bad url", func(ts *operatorv1alpha1.TargetSource) { ts.Spec.Source.HTTP.URL = "ftp://x" }, "spec.source.http.url"},
		{"body without POST", func(ts *operatorv1alpha1.TargetSource) { ts.Spec.Source.HTTP.Body = "{}" }, "spec.source.http.body"},
		{"two auth methods", func(ts *operatorv1alpha1.TargetSource) {
			ts.Spec.Source.HTTP.Auth.Basic = &operatorv1alpha1.BasicAuth{SecretRef: operatorv1alpha1.LocalSecretReference{Name: "b"}}
		}, "spec.source.http.auth"},
		{"interval too short", func(ts *operatorv1alpha1.TargetSource) { ts.Spec.Interval.Duration = 5 * time.Second }, "spec.interval"},
		{"timeout too short", func(ts *operatorv1alpha1.TargetSource) { ts.Spec.Timeout.Duration = 500 * time.Millisecond }, "spec.timeout"},
		{"timeout longer than interval is capped, not rejected", func(ts *operatorv1alpha1.TargetSource) {
			ts.Spec.Interval.Duration = 30 * time.Second
			ts.Spec.Timeout = nil
		}, ""},
		{"bad target port", func(ts *operatorv1alpha1.TargetSource) { ts.Spec.Target.Port = 70000 }, "spec.target.port"},
		{"bad ratio", func(ts *operatorv1alpha1.TargetSource) { ts.Spec.Prune.MaxDeleteRatio = ptr.To(int32(101)) }, "spec.prune.maxDeleteRatio"},
		{"webhook without auth", func(ts *operatorv1alpha1.TargetSource) {
			ts.Spec.Webhook = &operatorv1alpha1.WebhookSpec{Enabled: true}
		}, "spec.webhook.auth"},
		{"webhook with auth", func(ts *operatorv1alpha1.TargetSource) {
			ts.Spec.Webhook = &operatorv1alpha1.WebhookSpec{Enabled: true, Auth: &operatorv1alpha1.WebhookAuthSpec{
				Bearer: &operatorv1alpha1.WebhookBearerAuth{SecretRef: operatorv1alpha1.SecretKeyReference{Name: "h", Key: "t"}}}}
		}, ""},
		{"sha1 rejected", func(ts *operatorv1alpha1.TargetSource) {
			ts.Spec.Webhook = &operatorv1alpha1.WebhookSpec{Enabled: true, Auth: &operatorv1alpha1.WebhookAuthSpec{
				Signature: &operatorv1alpha1.WebhookSignatureAuth{SecretRef: operatorv1alpha1.SecretKeyReference{Name: "h", Key: "t"}, Algorithm: "sha1"}}}
		}, "spec.webhook.auth.signature.algorithm"},
		{"static device without address", func(ts *operatorv1alpha1.TargetSource) {
			ts.Spec.Source = operatorv1alpha1.SourceSpec{Type: operatorv1alpha1.SourceTypeStatic, Static: &operatorv1alpha1.StaticSource{
				Devices: []operatorv1alpha1.StaticDevice{{Name: "a"}}}}
		}, "spec.source.static.devices[0].address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := validHTTPTargetSource()
			if tc.mutate != nil {
				tc.mutate(ts)
			}
			errs := validateTargetSourceSpec(&ts.Spec)
			if tc.wantErr == "" {
				if len(errs) != 0 {
					t.Fatalf("expected valid, got %v", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatalf("expected an error on %s", tc.wantErr)
			}
			if !strings.Contains(errs.ToAggregate().Error(), tc.wantErr) {
				t.Fatalf("errors %v do not mention %s", errs, tc.wantErr)
			}
		})
	}
}

func TestTargetSourceValidatorWarnsAboutMissingReferences(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = operatorv1alpha1.AddToScheme(scheme)
	existing := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "tok", Namespace: "default"}}
	v := &TargetSourceCustomValidator{Reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()}

	ts := validHTTPTargetSource()
	warnings, err := v.ValidateCreate(context.Background(), ts)
	if err != nil {
		t.Fatal(err)
	}
	// The Secret exists; the TargetProfile does not.
	if len(warnings) != 1 || !strings.Contains(warnings[0], `TargetProfile "default"`) {
		t.Fatalf("warnings = %v", warnings)
	}

	ts.Spec.Source.HTTP.Auth.Token.SecretRef.Name = "absent"
	warnings, _ = v.ValidateCreate(context.Background(), ts)
	if len(warnings) != 2 || !strings.Contains(warnings[0], `Secret "absent"`) {
		t.Fatalf("warnings = %v", warnings)
	}

	// A rejection carries the field path so kubectl apply explains itself.
	ts.Spec.Source.HTTP.Mapping.Items = "self.["
	if _, err := v.ValidateCreate(context.Background(), ts); err == nil || !strings.Contains(err.Error(), "spec.source.http.mapping.items") {
		t.Fatalf("expected an invalid error naming the field, got %v", err)
	}
}
