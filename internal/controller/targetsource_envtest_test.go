package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/discovery"
)

// These run against a real kube-apiserver, which is what makes them worth
// having next to the fake-client tests: server-side apply field ownership and
// the CRD's CEL rules are both emulated or absent in the fake.
var _ = Describe("TargetSource against the API server", func() {
	ctx := context.Background()

	newSource := func(name string) *gnmicv1alpha1.TargetSource {
		return &gnmicv1alpha1.TargetSource{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: gnmicv1alpha1.TargetSourceSpec{
				Source: gnmicv1alpha1.SourceSpec{Type: gnmicv1alpha1.SourceTypeStatic, Static: &gnmicv1alpha1.StaticSource{
					Devices: []gnmicv1alpha1.StaticDevice{
						{Name: "leaf1", Address: "10.0.0.1", Labels: map[string]string{"site": "ams"}},
						{Name: "leaf2", Address: "10.0.0.2"},
					},
				}},
				Target: gnmicv1alpha1.TargetTemplateSpec{Profile: "default"},
			},
		}
	}

	reconcile := func(name string) {
		r := &TargetSourceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: name}})
		Expect(err).NotTo(HaveOccurred())
	}

	It("creates owned Targets and applies schema defaults", func() {
		ts := newSource("envtest-basic")
		Expect(k8sClient.Create(ctx, ts)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ts) })

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ts), ts)).To(Succeed())
		Expect(ts.Spec.Interval).NotTo(BeNil())
		Expect(ts.Spec.Interval.Duration).To(Equal(5*time.Minute), "CRD default for interval")
		Expect(ts.Spec.Target.Port).To(Equal(int32(57400)), "CRD default for target.port")
		Expect(ptr.Deref(ts.Spec.Prune.MaxDeleteRatio, 0)).To(Equal(int32(50)), "CRD default for prune.maxDeleteRatio")
		Expect(ptr.Deref(ts.Spec.MaxTargets, 0)).To(Equal(int32(10000)), "CRD default for maxTargets")

		reconcile(ts.Name)

		var target gnmicv1alpha1.Target
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "envtest-basic-leaf1"}, &target)).To(Succeed())
		Expect(target.Spec.Address).To(Equal("10.0.0.1:57400"))
		owner := metav1.GetControllerOf(&target)
		Expect(owner).NotTo(BeNil())
		Expect(owner.UID).To(Equal(ts.UID))
		Expect(target.ManagedFields).NotTo(BeEmpty())
		Expect(target.ManagedFields[0].Manager).To(Equal(discovery.FieldManagerPrefix + ts.Name))

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ts), ts)).To(Succeed())
		Expect(ts.Status.Managed).To(Equal(int32(2)))
		Expect(ts.Status.Conditions).NotTo(BeEmpty())
		for _, t := range []string{"envtest-basic-leaf1", "envtest-basic-leaf2"} {
			_ = k8sClient.Delete(ctx, &gnmicv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Name: t, Namespace: "default"}})
		}
	})

	It("owns only the fields it sets", func() {
		ts := newSource("envtest-ssa")
		Expect(k8sClient.Create(ctx, ts)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ts) })
		reconcile(ts.Name)

		key := types.NamespacedName{Namespace: "default", Name: "envtest-ssa-leaf1"}
		var target gnmicv1alpha1.Target
		Expect(k8sClient.Get(ctx, key, &target)).To(Succeed())
		target.Labels["team"] = "netops"
		target.Labels["site"] = "user-edited"
		Expect(k8sClient.Update(ctx, &target)).To(Succeed())

		// Change the source so the hash differs and the Target is re-applied.
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ts), ts)).To(Succeed())
		ts.Spec.Source.Static.Devices[0].Labels["site"] = "fra"
		Expect(k8sClient.Update(ctx, ts)).To(Succeed())
		reconcile(ts.Name)

		Expect(k8sClient.Get(ctx, key, &target)).To(Succeed())
		Expect(target.Labels["team"]).To(Equal("netops"), "a label the source does not set survives")
		Expect(target.Labels["site"]).To(Equal("fra"), "a key the source sets is the source's")
		for _, t := range []string{"envtest-ssa-leaf1", "envtest-ssa-leaf2"} {
			_ = k8sClient.Delete(ctx, &gnmicv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Name: t, Namespace: "default"}})
		}
	})

	It("rejects specs the CEL rules forbid", func() {
		byType := newSource("envtest-cel-1")
		byType.Spec.Source.Type = gnmicv1alpha1.SourceTypeHTTP
		err := k8sClient.Create(ctx, byType)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("if and only if type is"))

		timing := newSource("envtest-cel-2")
		timing.Spec.Interval = &metav1.Duration{Duration: 5 * time.Second}
		err = k8sClient.Create(ctx, timing)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("interval must be at least 10s"))

		hook := newSource("envtest-cel-3")
		hook.Spec.Webhook = &gnmicv1alpha1.WebhookSpec{Enabled: true}
		err = k8sClient.Create(ctx, hook)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("webhook.auth is required"))
	})
})
