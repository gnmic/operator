package discovery

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/discovery/provider"
)

func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"Leaf_1":                 "leaf-1",
		"spine--2":               "spine-2",
		"-edge.-":                "edge",
		"a..b":                   "a.b",
		"x.-y":                   "x.y",
		"Röuter 1":               "r-uter-1",
		"ok.name-1":              "ok.name-1",
		"___":                    "",
		strings.Repeat("a", 300): "",
	}
	for in, want := range cases {
		got, ok := NormalizeName(in)
		if want == "" && in == strings.Repeat("a", 300) {
			if !ok || len(got) != MaxNameLength || !strings.Contains(got, "-") {
				t.Errorf("long name: got %q ok=%v", got, ok)
			}
			continue
		}
		if (want == "") == ok || got != want {
			t.Errorf("NormalizeName(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	// Truncation is deterministic and keeps distinct inputs distinct.
	a, _ := NormalizeName(strings.Repeat("a", 300) + "1")
	b, _ := NormalizeName(strings.Repeat("a", 300) + "2")
	if a == b {
		t.Error("two long names collapsed onto one")
	}
}

func TestSanitizeLabels(t *testing.T) {
	out := SanitizeLabels(map[string]string{
		"site":                   "Amsterdam 1",
		"Rack/Position":          "7",
		"too_long":               strings.Repeat("v", 70),
		"app.kubernetes.io/name": "fine",
		"!!!":                    "dropped",
		"trailing-":              "-edges-",
	})
	if out.Labels["site"] != "Amsterdam-1" {
		t.Errorf("site = %q", out.Labels["site"])
	}
	if out.Labels["rack/Position"] != "7" {
		t.Errorf("prefixed key: %+v", out.Labels)
	}
	if len(out.Labels["too_long"]) != 63 {
		t.Errorf("too_long len = %d", len(out.Labels["too_long"]))
	}
	if out.Labels["app.kubernetes.io/name"] != "fine" {
		t.Errorf("valid label changed: %+v", out.Labels)
	}
	if _, ok := out.Labels["!!!"]; ok || len(out.Dropped) != 1 {
		t.Errorf("invalid key should be dropped: %+v dropped=%v", out.Labels, out.Dropped)
	}
	if out.Labels["trailing"] != "edges" {
		t.Errorf("edges: %+v", out.Labels)
	}
	if out.Annotations[AnnotationLabelPrefix+"site"] != "Amsterdam 1" {
		t.Errorf("original not recorded: %+v", out.Annotations)
	}
	if out.Annotations[AnnotationLabelPrefix+"rack.Position"] != "Rack/Position=7" {
		t.Errorf("rewritten key original: %+v", out.Annotations)
	}
	if !out.Changed {
		t.Error("Changed should be true")
	}
	clean := SanitizeLabels(map[string]string{"a": "b"})
	if clean.Changed || len(clean.Annotations) != 0 {
		t.Errorf("clean labels reported as changed: %+v", clean)
	}
}

func source(name string) *gnmicv1alpha1.TargetSource {
	return &gnmicv1alpha1.TargetSource{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: gnmicv1alpha1.TargetSourceSpec{Target: gnmicv1alpha1.TargetTemplateSpec{
			Port: 57400, Profile: "default", Labels: map[string]string{"region": "emea", "site": "template"},
		}},
	}
}

func TestBuild(t *testing.T) {
	ts := source("inv")
	res := Build(ts, []provider.Device{
		{Name: "Leaf_1", Address: "10.0.0.1", Labels: map[string]string{"site": "ams 1"}},
		{Name: "leaf-1", Address: "10.0.0.2"}, // collides with Leaf_1 after normalisation
		{Name: "v6", Address: "2001:db8::1", Port: 57401, Profile: "srl"},
		{Name: "withport", Address: "10.0.0.3:9339"},
		{Name: "", Address: "10.0.0.4"},
		{Name: "noaddr"},
	})
	if len(res.Desired) != 3 {
		t.Fatalf("desired = %+v", res.Desired)
	}
	byName := map[string]Desired{}
	for _, d := range res.Desired {
		byName[d.Name] = d
	}
	leaf := byName["inv-leaf-1"]
	// sorted by discovered name: "Leaf_1" < "leaf-1" (uppercase first), so Leaf_1 wins
	if leaf.Address != "10.0.0.1:57400" || leaf.Profile != "default" {
		t.Errorf("leaf = %+v", leaf)
	}
	if leaf.Labels["site"] != "ams-1" || leaf.Labels["region"] != "emea" || leaf.Labels[LabelTargetSource] != "inv" || leaf.Labels[LabelManagedBy] != LabelManagedByValue {
		t.Errorf("leaf labels = %+v", leaf.Labels)
	}
	if leaf.Annotations[AnnotationDiscoveredName] != "Leaf_1" || leaf.Annotations[AnnotationLabelPrefix+"site"] != "ams 1" {
		t.Errorf("leaf annotations = %+v", leaf.Annotations)
	}
	if byName["inv-v6"].Address != "[2001:db8::1]:57401" || byName["inv-v6"].Profile != "srl" {
		t.Errorf("v6 = %+v", byName["inv-v6"])
	}
	if byName["inv-withport"].Address != "10.0.0.3:9339" {
		t.Errorf("withport = %+v", byName["inv-withport"])
	}
	reasons := map[string]string{}
	for _, f := range res.Invalid {
		reasons[f.Name] = f.Reason
	}
	if reasons["leaf-1"] != "DuplicateName" || reasons["10.0.0.4"] != "MissingName" || reasons["noaddr"] != "MissingAddress" {
		t.Errorf("invalid = %+v", res.Invalid)
	}
	if res.Sanitized != 1 {
		t.Errorf("sanitized = %d", res.Sanitized)
	}
	if leaf.Hash == "" || leaf.Hash == byName["inv-v6"].Hash {
		t.Error("hashes should be set and distinct")
	}
}

func TestBuildIsOrderIndependent(t *testing.T) {
	ts := source("inv")
	a := Build(ts, []provider.Device{{Name: "Leaf_1", Address: "10.0.0.1"}, {Name: "leaf-1", Address: "10.0.0.2"}})
	b := Build(ts, []provider.Device{{Name: "leaf-1", Address: "10.0.0.2"}, {Name: "Leaf_1", Address: "10.0.0.1"}})
	if a.Desired[0].Address != b.Desired[0].Address {
		t.Fatalf("winner depends on input order: %s vs %s", a.Desired[0].Address, b.Desired[0].Address)
	}
}

func TestBuildRequiresAProfile(t *testing.T) {
	ts := source("inv")
	ts.Spec.Target.Profile = ""
	res := Build(ts, []provider.Device{{Name: "a", Address: "10.0.0.1"}, {Name: "b", Address: "10.0.0.2", Profile: "srl"}})
	if len(res.Desired) != 1 || len(res.Invalid) != 1 || res.Invalid[0].Reason != "NoProfile" {
		t.Fatalf("res = %+v", res)
	}
}

func TestDiff(t *testing.T) {
	desired := []Desired{{Name: "a", Hash: "1"}, {Name: "b", Hash: "2"}, {Name: "c", Hash: "3"}}
	existing := []Existing{{Name: "a", Hash: "1"}, {Name: "b", Hash: "old"}, {Name: "gone", Hash: "x"}}
	apply, remove := Diff(desired, existing)
	if len(apply) != 2 || apply[0].Name != "b" || apply[1].Name != "c" {
		t.Errorf("apply = %+v", apply)
	}
	if len(remove) != 1 || remove[0] != "gone" {
		t.Errorf("remove = %v", remove)
	}
}

func TestPruneGuard(t *testing.T) {
	spec := gnmicv1alpha1.PruneSpec{MaxDeleteRatio: ptr.To(int32(50))}
	if d := PruneGuard(spec, false, 10, 10, 0); !d.Allowed {
		t.Error("no deletions is always allowed")
	}
	if d := PruneGuard(spec, true, 10, 10, 1); d.Allowed || d.Reason != "Truncated" {
		t.Errorf("truncated: %+v", d)
	}
	if d := PruneGuard(spec, false, 0, 10, 10); d.Allowed || d.Reason != "EmptySource" {
		t.Errorf("empty: %+v", d)
	}
	allowEmpty := gnmicv1alpha1.PruneSpec{MaxDeleteRatio: ptr.To(int32(100)), AllowEmptySource: true}
	if d := PruneGuard(allowEmpty, false, 0, 10, 10); !d.Allowed {
		t.Errorf("empty allowed: %+v", d)
	}
	if d := PruneGuard(spec, false, 5, 10, 5); !d.Allowed {
		t.Errorf("exactly at the ratio is allowed: %+v", d)
	}
	if d := PruneGuard(spec, false, 4, 10, 6); d.Allowed || d.Reason != "PruneGuard" {
		t.Errorf("over the ratio: %+v", d)
	}
	if d := PruneGuard(gnmicv1alpha1.PruneSpec{MaxDeleteRatio: ptr.To(int32(100))}, false, 1, 10, 9); !d.Allowed {
		t.Errorf("100%% disables the guard: %+v", d)
	}
	if d := PruneGuard(gnmicv1alpha1.PruneSpec{MaxDeleteRatio: ptr.To(int32(0))}, false, 9, 10, 1); d.Allowed {
		t.Errorf("0%% holds every deletion: %+v", d)
	}
	if d := PruneGuard(gnmicv1alpha1.PruneSpec{}, false, 5, 10, 5); !d.Allowed {
		t.Errorf("nil ratio defaults to 50: %+v", d)
	}
}

func TestRetryAfterAndJitter(t *testing.T) {
	interval := 5 * time.Minute
	want := []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute, 50 * time.Minute, 50 * time.Minute}
	for i, w := range want {
		if got := RetryAfter(interval, int32(i+1)); got != w {
			t.Errorf("RetryAfter(%d) = %s, want %s", i+1, got, w)
		}
	}
	if got := RetryAfter(interval, 0); got != interval {
		t.Errorf("RetryAfter(0) = %s", got)
	}
	if got := Jitter(time.Minute, func() float64 { return 0.5 }); got != 63*time.Second {
		t.Errorf("Jitter = %s", got)
	}
}

func TestDigestIsOrderIndependent(t *testing.T) {
	a := []provider.Device{{Name: "a", Address: "1"}, {Name: "b", Address: "2", Labels: map[string]string{"x": "y"}}}
	b := []provider.Device{a[1], a[0]}
	if Digest(a) != Digest(b) {
		t.Error("digest depends on order")
	}
	if Digest(a) == Digest(a[:1]) {
		t.Error("digest ignores content")
	}
}

func TestReferencedObjects(t *testing.T) {
	spec := &gnmicv1alpha1.TargetSourceSpec{
		Source: gnmicv1alpha1.SourceSpec{Type: gnmicv1alpha1.SourceTypeHTTP, HTTP: &gnmicv1alpha1.HTTPSource{
			EndpointSpec: gnmicv1alpha1.EndpointSpec{
				URL:  "https://x",
				Auth: &gnmicv1alpha1.AuthSpec{Token: &gnmicv1alpha1.TokenAuth{SecretRef: gnmicv1alpha1.SecretKeyReference{Name: "tok", Key: "t"}}},
				TLS: &gnmicv1alpha1.ClientTLSSpec{
					CABundleRef:   &gnmicv1alpha1.ConfigMapKeyReference{Name: "ca", Key: "ca.crt"},
					ClientCertRef: &gnmicv1alpha1.LocalSecretReference{Name: "cert"},
				},
			},
		}},
		Webhook: &gnmicv1alpha1.WebhookSpec{Enabled: true, Auth: &gnmicv1alpha1.WebhookAuthSpec{
			Bearer: &gnmicv1alpha1.WebhookBearerAuth{SecretRef: gnmicv1alpha1.SecretKeyReference{Name: "hook", Key: "k"}},
		}},
	}
	secrets := ReferencedSecrets(spec)
	if strings.Join(secrets, ",") != "cert,hook,tok" {
		t.Errorf("secrets = %v", secrets)
	}
	if cms := ReferencedConfigMaps(spec); strings.Join(cms, ",") != "ca" {
		t.Errorf("configmaps = %v", cms)
	}
	cmSpec := &gnmicv1alpha1.TargetSourceSpec{Source: gnmicv1alpha1.SourceSpec{Type: gnmicv1alpha1.SourceTypeConfigMap, ConfigMap: &gnmicv1alpha1.ObjectSource{Name: "inv"}}}
	if cms := ReferencedConfigMaps(cmSpec); strings.Join(cms, ",") != "inv" {
		t.Errorf("configmap source = %v", cms)
	}
}
