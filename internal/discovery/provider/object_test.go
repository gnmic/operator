package provider

import (
	"context"
	"errors"
	"testing"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func TestConfigMapProviderReadsOneKeyOrAll(t *testing.T) {
	resolver := fakeResolver{configMaps: map[string]map[string][]byte{"inv": {
		"b.yaml": []byte("- name: b\n  address: 10.0.0.2\n"),
		"a.json": []byte(`[{"name":"a","address":"10.0.0.1"}]`),
	}}}
	p, _ := Lookup(gnmicv1alpha1.SourceTypeConfigMap)

	res, err := p.Fetch(context.Background(), Request{Namespace: "default", Objects: resolver,
		Source: gnmicv1alpha1.SourceSpec{Type: gnmicv1alpha1.SourceTypeConfigMap, ConfigMap: &gnmicv1alpha1.ObjectSource{Name: "inv"}}})
	if err != nil || len(res.Devices) != 2 || res.Devices[0].Name != "a" || res.Devices[1].Name != "b" {
		t.Fatalf("all keys: res=%+v err=%v", res, err)
	}

	res, err = p.Fetch(context.Background(), Request{Namespace: "default", Objects: resolver,
		Source: gnmicv1alpha1.SourceSpec{Type: gnmicv1alpha1.SourceTypeConfigMap, ConfigMap: &gnmicv1alpha1.ObjectSource{Name: "inv", Key: "b.yaml"}}})
	if err != nil || len(res.Devices) != 1 || res.Devices[0].Name != "b" {
		t.Fatalf("one key: res=%+v err=%v", res, err)
	}

	_, err = p.Fetch(context.Background(), Request{Namespace: "default", Objects: resolver,
		Source: gnmicv1alpha1.SourceSpec{Type: gnmicv1alpha1.SourceTypeConfigMap, ConfigMap: &gnmicv1alpha1.ObjectSource{Name: "inv", Key: "nope"}}})
	var nf *NotFoundError
	if !IsSpecError(err) || !errors.As(err, &nf) || nf.Kind != "ConfigMap" || nf.Key != "nope" {
		t.Fatalf("missing key should be a typed ConfigMap not-found spec error, got %v", err)
	}
	_, err = p.Fetch(context.Background(), Request{Namespace: "default", Objects: resolver,
		Source: gnmicv1alpha1.SourceSpec{Type: gnmicv1alpha1.SourceTypeConfigMap, ConfigMap: &gnmicv1alpha1.ObjectSource{Name: "absent"}}})
	if !IsSpecError(err) {
		t.Fatalf("missing ConfigMap should be a spec error, got %v", err)
	}
}

func TestSecretProviderWithMapping(t *testing.T) {
	resolver := fakeResolver{secrets: map[string]map[string][]byte{"inv": {
		"devices.json": []byte(`{"hosts":[{"id":"x","ip":"10.0.0.9"}]}`),
	}}}
	p, _ := Lookup(gnmicv1alpha1.SourceTypeSecret)
	res, err := p.Fetch(context.Background(), Request{Namespace: "default", Objects: resolver,
		Source: gnmicv1alpha1.SourceSpec{Type: gnmicv1alpha1.SourceTypeSecret, Secret: &gnmicv1alpha1.ObjectSource{
			Name: "inv", Key: "devices.json",
			Mapping: &gnmicv1alpha1.MappingSpec{Items: "self.hosts", Name: "item.id", Address: "item.ip"},
		}}})
	if err != nil || len(res.Devices) != 1 || res.Devices[0].Address != "10.0.0.9" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestConfigMapProviderBadDocumentIsSpecError(t *testing.T) {
	resolver := fakeResolver{configMaps: map[string]map[string][]byte{"inv": {"d": []byte("{{{")}}}
	p, _ := Lookup(gnmicv1alpha1.SourceTypeConfigMap)
	_, err := p.Fetch(context.Background(), Request{Namespace: "default", Objects: resolver,
		Source: gnmicv1alpha1.SourceSpec{Type: gnmicv1alpha1.SourceTypeConfigMap, ConfigMap: &gnmicv1alpha1.ObjectSource{Name: "inv"}}})
	if !IsSpecError(err) {
		t.Fatalf("want spec error, got %v", err)
	}
}

func TestStaticProvider(t *testing.T) {
	p, _ := Lookup(gnmicv1alpha1.SourceTypeStatic)
	res, err := p.Fetch(context.Background(), Request{Source: gnmicv1alpha1.SourceSpec{
		Type: gnmicv1alpha1.SourceTypeStatic,
		Static: &gnmicv1alpha1.StaticSource{Devices: []gnmicv1alpha1.StaticDevice{
			{Name: "leaf1", Address: "10.0.0.1", Port: 57401, Profile: "srl", Labels: map[string]string{"role": "leaf"}},
		}},
	}})
	if err != nil || len(res.Devices) != 1 || res.Devices[0].Port != 57401 || res.Devices[0].Labels["role"] != "leaf" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestRegistryHasTheFourProviders(t *testing.T) {
	got := Types()
	want := []gnmicv1alpha1.SourceType{"ConfigMap", "HTTP", "Secret", "Static"}
	if len(got) != len(want) {
		t.Fatalf("types = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("types = %v, want %v", got, want)
		}
	}
}
