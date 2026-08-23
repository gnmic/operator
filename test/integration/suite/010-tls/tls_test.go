//go:build integration

// Package tls010 covers Cluster clientTLS / api.tls certificate issuance and
// TargetProfile TLS collection against mixed plaintext and TLS gnmi-gen targets.
//
// Test -> ID:
//
//	TestTLS001_ClientCertsMounted          -> 010-1
//	TestTLS002_MissingIssuerGatesReady     -> 010-2
//	TestTLS003_SkipVerifyCollectsTLS       -> 010-3
//	TestTLS004_CAVerification              -> 010-4
//	TestTLS005_APITLSKeepsConfigPath      -> 010-5
//	TestTLS006_ClientCertRotation          -> 010-6
//
// Run:
//
//	make integration-test-010-tls
package tls010

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/test/integration/harness"
)

// Paths the operator mounts for Cluster.spec.clientTLS (mirrors internal/gnmic/tls.go).
const (
	clientTLSCertPath = "/etc/gnmic/client-tls/tls.crt"
	clientTLSKeyPath  = "/etc/gnmic/client-tls/tls.key"
)

var s *harness.Suite

const (
	cluster = "c1"
	issuer  = "suite-ca-issuer"

	leaf1    = "leaf1"
	leaf2    = "leaf2"
	leafTLS1 = "leaf-tls1"
	leafTLS2 = "leaf-tls2"

	portPlain1 = 57400
	portPlain2 = 57401
	portTLS1   = 57410
	portTLS2   = 57411

	caBundleName    = "target-ca-bundle"
	wrongBundleName = "wrong-ca-bundle"
	serverTLSSecret = "gnmi-gen-server-tls"
)

var allSimTargets = []string{leaf1, leaf2, leafTLS1, leafTLS2}

func TestMain(m *testing.M) {
	os.Exit(harness.RunSuite(m, harness.Options{
		Name:           "010-tls",
		RequireTargets: allSimTargets,
		Baseline:       []string{"fixtures/baseline.yaml"},
		BeforeGnmiGen:  stageCerts,
	}, &s))
}

// stageCerts creates the suite CA, signs a server cert for gnmi-gen, and publishes
// trust bundles the Cluster can mount via clientTLS.bundleRef.
func stageCerts(suite *harness.Suite) error {
	b, err := os.ReadFile("fixtures/issuers.yaml")
	if err != nil {
		return err
	}
	if _, err := suite.K8s.ApplyYAMLNoCleanup(string(b), nil); err != nil {
		return fmt.Errorf("applying issuers: %w", err)
	}
	if err := waitCertificateReady(suite, "suite-ca"); err != nil {
		return err
	}
	if err := waitCertificateReady(suite, "wrong-ca"); err != nil {
		return err
	}

	sb, err := os.ReadFile("fixtures/gnmi-gen-server-cert.yaml")
	if err != nil {
		return err
	}
	if _, err := suite.K8s.ApplyYAMLNoCleanup(string(sb), nil); err != nil {
		return fmt.Errorf("applying gnmi-gen server cert: %w", err)
	}
	if err := waitCertificateReady(suite, "gnmi-gen-server"); err != nil {
		return err
	}

	if err := publishCABundle(suite, "suite-ca-secret", caBundleName); err != nil {
		return err
	}
	if err := publishCABundle(suite, "wrong-ca-secret", wrongBundleName); err != nil {
		return err
	}

	suite.GnmiGenTLSSecret = serverTLSSecret
	return nil
}

func waitCertificateReady(suite *harness.Suite, name string) error {
	deadline := time.Now().Add(harness.Long)
	for {
		var cert certmanagerv1.Certificate
		err := suite.K8s.Client.Get(suite.Ctx, types.NamespacedName{
			Namespace: suite.Namespace, Name: name,
		}, &cert)
		if err == nil {
			for _, c := range cert.Status.Conditions {
				if c.Type == certmanagerv1.CertificateConditionReady && c.Status == cmmeta.ConditionTrue {
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("certificate %s not Ready: %v", name, err)
		}
		time.Sleep(time.Second)
	}
}

func publishCABundle(suite *harness.Suite, secretName, cmName string) error {
	var sec corev1.Secret
	if err := suite.K8s.Client.Get(suite.Ctx, types.NamespacedName{
		Namespace: suite.Namespace, Name: secretName,
	}, &sec); err != nil {
		return fmt.Errorf("reading CA secret %s: %w", secretName, err)
	}
	pem := sec.Data["ca.crt"]
	if len(pem) == 0 {
		pem = sec.Data["tls.crt"]
	}
	if len(pem) == 0 {
		return fmt.Errorf("CA secret %s has neither ca.crt nor tls.crt", secretName)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: suite.Namespace},
		Data:       map[string]string{"ca.crt": string(pem)},
	}
	cm.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	return suite.K8s.ApplyNoCleanup(cm)
}

func waitClusterReady(t *testing.T, name string) {
	t.Helper()
	harness.WaitClusterReady(t, s.K8s, name)
	s.K8s.WaitReadyPods(t, name, 1, harness.Long)
}

// waitPodFile blocks until path exists and is non-empty in a ready collector pod.
// Used after clientTLS / api.tls patches: Cluster Ready can stay True on the old
// pod while the STS rolls to pick up new volume mounts.
func waitPodFile(t *testing.T, clusterName, path string) {
	t.Helper()
	harness.Wait(t, harness.Long, fmt.Sprintf("pod file %s", path), func() (bool, string) {
		pods := s.K8s.ClusterPods(t, clusterName)
		for i := range pods {
			p := &pods[i]
			ready := false
			for _, c := range p.Status.Conditions {
				if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
					ready = true
					break
				}
			}
			if !ready {
				continue
			}
			out, err := s.K8s.ExecQuiet(p.Name, harness.CollectorContainer, "sh", "-c",
				fmt.Sprintf("test -s %q && echo ok || echo missing", path))
			if err != nil {
				return false, err.Error()
			}
			if strings.Contains(out, "ok") {
				return true, ""
			}
			return false, out
		}
		return false, "no ready pods"
	})
}

func patchClientTLS(t *testing.T, issuerRef, bundleRef string) {
	t.Helper()
	patch := fmt.Sprintf(`{"spec":{"clientTLS":{"issuerRef":%q`, issuerRef)
	if bundleRef != "" {
		patch += fmt.Sprintf(`,"bundleRef":%q`, bundleRef)
	}
	patch += `}}}`
	s.K8s.Patch(t, s.K8s.Cluster(t, cluster), patch)
	waitCertReady(t, harness.ClientCertName(cluster))
	waitClusterReady(t, cluster)
	waitPodFile(t, cluster, clientTLSCertPath)
	if bundleRef != "" {
		waitPodFile(t, cluster, "/etc/gnmic/client-ca/ca.crt")
	}
	harness.WaitConfigApplied(t, s.K8s, cluster)
}

func patchAPITLS(t *testing.T, issuerRef string) {
	t.Helper()
	s.K8s.Patch(t, s.K8s.Cluster(t, cluster), fmt.Sprintf(
		`{"spec":{"api":{"restPort":7890,"tls":{"issuerRef":%q}}}}`, issuerRef))
	waitCertReady(t, harness.APICertName(cluster, 0))
	harness.WaitClusterCondition(t, s.K8s, cluster, harness.CondCertificatesReady, metav1.ConditionTrue, harness.Long)
	waitClusterReady(t, cluster)
	waitPodFile(t, cluster, "/etc/gnmic/tls/tls.crt")
	harness.WaitConfigApplied(t, s.K8s, cluster)
}

func waitIdle(t *testing.T) {
	t.Helper()
	var targets gnmicv1alpha1.TargetList
	if err := s.K8s.Client.List(s.Ctx, &targets, client.InNamespace(s.Namespace)); err == nil {
		for i := range targets.Items {
			_ = s.K8s.Client.Delete(s.Ctx, &targets.Items[i])
		}
	}
	var profiles gnmicv1alpha1.TargetProfileList
	if err := s.K8s.Client.List(s.Ctx, &profiles, client.InNamespace(s.Namespace)); err == nil {
		for i := range profiles.Items {
			_ = s.K8s.Client.Delete(s.Ctx, &profiles.Items[i])
		}
	}
	// Drop extra clusters created by 010-1 / 010-2; keep baseline c1.
	var clusters gnmicv1alpha1.ClusterList
	if err := s.K8s.Client.List(s.Ctx, &clusters, client.InNamespace(s.Namespace)); err == nil {
		for i := range clusters.Items {
			if clusters.Items[i].Name == cluster {
				continue
			}
			_ = s.K8s.Client.Delete(s.Ctx, &clusters.Items[i])
		}
	}
	harness.Wait(t, harness.Medium, "extra clusters gone", func() (bool, string) {
		var left gnmicv1alpha1.ClusterList
		if err := s.K8s.Client.List(s.Ctx, &left, client.InNamespace(s.Namespace)); err != nil {
			return false, err.Error()
		}
		n := 0
		for _, c := range left.Items {
			if c.Name != cluster {
				n++
			}
		}
		return n == 0, fmt.Sprintf("%d extra", n)
	})
	harness.Wait(t, harness.Medium, "targets gone", func() (bool, string) {
		var left gnmicv1alpha1.TargetList
		if err := s.K8s.Client.List(s.Ctx, &left, client.InNamespace(s.Namespace)); err != nil {
			return false, err.Error()
		}
		return len(left.Items) == 0, fmt.Sprintf("%d remain", len(left.Items))
	})
	harness.Wait(t, harness.Medium, "simulators idle", func() (bool, string) {
		for _, name := range allSimTargets {
			if n := s.GnmiGen.StreamCount(name); n != 0 {
				return false, fmt.Sprintf("%s=%d", name, n)
			}
		}
		return true, ""
	})
	// Reset c1 TLS knobs so later tests start from a known plaintext API.
	resetClusterTLS(t)
}

func resetClusterTLS(t *testing.T) {
	t.Helper()
	c := s.K8s.Cluster(t, cluster)
	if c.Spec.ClientTLS == nil && (c.Spec.API == nil || c.Spec.API.TLS == nil) {
		waitClusterReady(t, cluster)
		return
	}
	s.K8s.Patch(t, c, `{"spec":{"clientTLS":null,"api":{"restPort":7890,"tls":null}}}`)
	harness.Wait(t, harness.Long, "c1 TLS cleared", func() (bool, string) {
		var cur gnmicv1alpha1.Cluster
		if err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: cluster}, &cur); err != nil {
			return false, err.Error()
		}
		if cur.Spec.ClientTLS != nil {
			return false, "clientTLS still set"
		}
		if cur.Spec.API != nil && cur.Spec.API.TLS != nil {
			return false, "api.tls still set"
		}
		return true, ""
	})
	waitClusterReady(t, cluster)
	// Ensure the rolled pod no longer has API TLS material mounted.
	harness.Wait(t, harness.Long, "api TLS unmounted", func() (bool, string) {
		pods := s.K8s.ClusterPods(t, cluster)
		for i := range pods {
			p := &pods[i]
			ready := false
			for _, c := range p.Status.Conditions {
				if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
					ready = true
					break
				}
			}
			if !ready {
				continue
			}
			out, err := s.K8s.ExecQuiet(p.Name, harness.CollectorContainer, "sh", "-c",
				`test -e /etc/gnmic/tls/tls.crt && echo present || echo absent`)
			if err != nil {
				return false, err.Error()
			}
			return strings.Contains(out, "absent"), out
		}
		return false, "no ready pods"
	})
	harness.WaitConfigApplied(t, s.K8s, cluster)
}

func applyProfile(t *testing.T, name string, tls bool) {
	t.Helper()
	s.K8s.ApplyFile(t, "fixtures/profile.yaml", map[string]any{
		"Name": name,
		"TLS":  tls,
	})
}

func applyTarget(t *testing.T, name string, port int, profile, role string) {
	t.Helper()
	s.K8s.ApplyFile(t, "fixtures/target.yaml", map[string]any{
		"Name":    name,
		"Port":    port,
		"Profile": profile,
		"Role":    role,
	})
}

func applyCluster(t *testing.T, name string, vars map[string]any) {
	t.Helper()
	base := map[string]any{
		"Name":         name,
		"Replicas":     1,
		"RestPort":     7890,
		"APIIssuer":    "",
		"ClientIssuer": "",
		"BundleRef":    "",
	}
	for k, v := range vars {
		base[k] = v
	}
	s.K8s.ApplyFile(t, "fixtures/cluster.yaml", base)
}

func waitCertReady(t *testing.T, name string) *certmanagerv1.Certificate {
	t.Helper()
	var cert certmanagerv1.Certificate
	harness.Wait(t, harness.Long, fmt.Sprintf("Certificate %s Ready", name), func() (bool, string) {
		err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: name}, &cert)
		if err != nil {
			return false, err.Error()
		}
		for _, c := range cert.Status.Conditions {
			if c.Type == certmanagerv1.CertificateConditionReady && c.Status == cmmeta.ConditionTrue {
				return true, ""
			}
		}
		return false, "not ready"
	})
	return &cert
}

func targetNotReady(t *testing.T, name string) {
	t.Helper()
	harness.Wait(t, harness.Medium, name+" not READY", func() (bool, string) {
		tgt, err := s.K8s.TargetQuiet(name)
		if err != nil {
			return false, err.Error()
		}
		if tgt.Status.State == "READY" {
			return false, "reached READY"
		}
		cs := tgt.Status.ClusterStates[cluster]
		if cs.State == "" && cs.ConnectionState == "" {
			return false, "no cluster state yet"
		}
		return cs.State != "running" || cs.ConnectionState != "READY",
			fmt.Sprintf("state=%q connection=%q reason=%q", cs.State, cs.ConnectionState, cs.FailedReason)
	})
}

// 010-1: clientTLS produces a cert-manager Certificate, Secret, and mount.
func TestTLS001_ClientCertsMounted(t *testing.T) {
	waitIdle(t)
	const name = "clienttls"
	applyCluster(t, name, map[string]any{"ClientIssuer": issuer})
	waitClusterReady(t, name)

	certName := harness.ClientCertName(name)
	cert := waitCertReady(t, certName)
	harness.AssertOwnedBy(t, cert, "Cluster", name)

	var sec corev1.Secret
	s.K8s.WaitExists(t, certName, &sec)
	if len(sec.Data["tls.crt"]) == 0 || len(sec.Data["tls.key"]) == 0 {
		t.Fatalf("client TLS secret missing tls.crt/tls.key keys: %v", keysOf(sec.Data))
	}

	pod := harness.PodName(name, 0)
	crt := s.K8s.Exec(t, pod, harness.CollectorContainer, "sh", "-c",
		fmt.Sprintf("test -s %s && wc -c <%s", clientTLSCertPath, clientTLSCertPath))
	key := s.K8s.Exec(t, pod, harness.CollectorContainer, "sh", "-c",
		fmt.Sprintf("test -s %s && wc -c <%s", clientTLSKeyPath, clientTLSKeyPath))
	if crt == "" || key == "" {
		t.Fatalf("client TLS files not readable in pod: crt=%q key=%q", crt, key)
	}

	// CertificatesReady is only published for api.tls today; clientTLS gates
	// reconcile by requeue instead. Ready on the Cluster is the readiness signal.
	c := s.K8s.Cluster(t, name)
	if !conditionTrue(c, harness.CondReady) {
		t.Fatal("Cluster Ready is not True after clientTLS issuance")
	}
}

// 010-2: a missing issuer keeps the Cluster from becoming Ready until fixed.
func TestTLS002_MissingIssuerGatesReady(t *testing.T) {
	waitIdle(t)
	const name = "badissuer"
	applyCluster(t, name, map[string]any{"ClientIssuer": "does-not-exist"})

	certName := harness.ClientCertName(name)
	harness.Wait(t, harness.Medium, "Certificate exists but not Ready", func() (bool, string) {
		var cert certmanagerv1.Certificate
		err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: certName}, &cert)
		if err != nil {
			return false, err.Error()
		}
		for _, c := range cert.Status.Conditions {
			if c.Type == certmanagerv1.CertificateConditionReady && c.Status == cmmeta.ConditionTrue {
				return false, "unexpectedly Ready"
			}
		}
		return true, "not ready yet"
	})

	harness.Consistently(t, 5*time.Second, time.Second, "Cluster stays not Ready", func() (bool, string) {
		var c gnmicv1alpha1.Cluster
		if err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: name}, &c); err != nil {
			return false, err.Error()
		}
		if conditionTrue(&c, harness.CondReady) {
			return false, "Ready became True"
		}
		return true, ""
	})
	for _, sim := range allSimTargets {
		if n := s.GnmiGen.StreamCount(sim); n != 0 {
			t.Fatalf("expected no collection while certs blocked, %s has %d streams", sim, n)
		}
	}

	// Creating the missing issuer as an alias of the suite CA lets cert-manager
	// issue the cert; the Cluster should converge without further edits.
	alias := []byte(fmt.Sprintf(`
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: does-not-exist
spec:
  ca:
    secretName: suite-ca-secret
`))
	if _, err := s.K8s.ApplyYAMLNoCleanup(string(alias), nil); err != nil {
		t.Fatalf("creating alias issuer: %v", err)
	}
	t.Cleanup(func() {
		_ = s.K8s.Client.Delete(s.Ctx, &certmanagerv1.Issuer{
			ObjectMeta: metav1.ObjectMeta{Name: "does-not-exist", Namespace: s.Namespace},
		})
	})

	waitCertReady(t, certName)
	waitClusterReady(t, name)
}

// 010-3: TargetProfile tls:{} collects TLS targets; same profile fails on plaintext.
func TestTLS003_SkipVerifyCollectsTLS(t *testing.T) {
	waitIdle(t)
	applyProfile(t, "skip-verify", true)
	applyTarget(t, leafTLS1, portTLS1, "skip-verify", "tls")
	applyTarget(t, leafTLS2, portTLS2, "skip-verify", "tls")
	applyTarget(t, "plain-mismatch", portPlain1, "skip-verify", "plain")

	s.GnmiGen.WaitStreams(t, leafTLS1, 1)
	s.GnmiGen.WaitStreams(t, leafTLS2, 1)
	harness.WaitTargetState(t, s.K8s, leafTLS1, "READY")
	harness.WaitTargetState(t, s.K8s, leafTLS2, "READY")

	targetNotReady(t, "plain-mismatch")
	s.GnmiGen.WaitStreams(t, leaf1, 0)
	s.GnmiGen.ConsistentlyCollectedOnce(t, 10*time.Second, 1, leafTLS1, leafTLS2)
}

// 010-4: clientTLS.bundleRef verifies the suite CA and rejects an unrelated CA.
func TestTLS004_CAVerification(t *testing.T) {
	waitIdle(t)

	patchClientTLS(t, issuer, caBundleName)

	// With clientTLS set, targets get TLS even without profile.tls.
	applyProfile(t, "ca-ok", false)
	applyTarget(t, leafTLS1, portTLS1, "ca-ok", "tls")
	s.GnmiGen.WaitStreams(t, leafTLS1, 1)
	harness.WaitTargetState(t, s.K8s, leafTLS1, "READY")

	// Flip to the wrong CA; verification must fail without silent downgrade.
	// bundleRef change updates the ConfigMap volume name → pod rollout.
	patchClientTLS(t, issuer, wrongBundleName)

	applyTarget(t, leafTLS2, portTLS2, "ca-ok", "tls")
	targetNotReady(t, leafTLS2)
	harness.Wait(t, harness.Medium, "leaf-tls2 has no stream", func() (bool, string) {
		n := s.GnmiGen.StreamCount(leafTLS2)
		return n == 0, fmt.Sprintf("streams=%d", n)
	})
	// leaf-tls1 may flap while the plan re-applies; it must not stay collected
	// against a CA that cannot verify the server cert.
	harness.Wait(t, harness.Medium, "leaf-tls1 loses stream under wrong CA", func() (bool, string) {
		n := s.GnmiGen.StreamCount(leafTLS1)
		return n == 0, fmt.Sprintf("streams=%d", n)
	})
}

// 010-5: enabling api.tls must not break the operator's apply path.
func TestTLS005_APITLSKeepsConfigPath(t *testing.T) {
	waitIdle(t)
	applyProfile(t, "skip-verify", true)
	applyTarget(t, leafTLS1, portTLS1, "skip-verify", "tls")
	s.GnmiGen.WaitStreams(t, leafTLS1, 1)
	harness.WaitTargetState(t, s.K8s, leafTLS1, "READY")

	patchAPITLS(t, issuer)

	// A live subscription edit must still reach the wire through the TLS API.
	sub := &gnmicv1alpha1.Subscription{}
	s.K8s.WaitExists(t, "if-counters", sub)
	s.K8s.Patch(t, sub, `{"spec":{"sampleInterval":"2s"}}`)
	harness.WaitConfigApplied(t, s.K8s, cluster)

	s.GnmiGen.WaitStreams(t, leafTLS1, 1)
	s.GnmiGen.WaitNotificationsAdvance(t, leafTLS1)
	harness.WaitTargetState(t, s.K8s, leafTLS1, "READY")
	s.GnmiGen.ConsistentlyCollectedOnce(t, 10*time.Second, 1, leafTLS1)
}

// 010-6: deleting the clientTLS Secret forces reissue without lasting outage.
func TestTLS006_ClientCertRotation(t *testing.T) {
	waitIdle(t)

	patchClientTLS(t, issuer, "")
	certName := harness.ClientCertName(cluster)

	applyProfile(t, "skip-verify", true)
	applyTarget(t, leafTLS1, portTLS1, "skip-verify", "tls")
	applyTarget(t, leafTLS2, portTLS2, "skip-verify", "tls")
	s.GnmiGen.AssertCollectedOnce(t, 1, leafTLS1, leafTLS2)

	var before corev1.Secret
	s.K8s.WaitExists(t, certName, &before)
	beforeRV := before.ResourceVersion
	if err := s.K8s.Client.Delete(s.Ctx, &before); err != nil {
		t.Fatalf("deleting client TLS secret: %v", err)
	}

	zeroSince := map[string]time.Time{}
	deadline := time.Now().Add(harness.Long)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for clientTLS secret reissue + recovery")
		}
		for _, name := range []string{leafTLS1, leafTLS2} {
			n := s.GnmiGen.StreamCount(name)
			if n > 1 {
				t.Fatalf("%s has %d streams during rotation", name, n)
			}
			if n == 0 {
				if zeroSince[name].IsZero() {
					zeroSince[name] = time.Now()
				} else if time.Since(zeroSince[name]) > 60*time.Second {
					t.Fatalf("%s at zero streams for >60s during rotation", name)
				}
			} else {
				delete(zeroSince, name)
			}
		}

		var sec corev1.Secret
		err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: certName}, &sec)
		if err == nil && sec.ResourceVersion != beforeRV && len(sec.Data["tls.crt"]) > 0 {
			if s.GnmiGen.StreamCount(leafTLS1) == 1 && s.GnmiGen.StreamCount(leafTLS2) == 1 {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	waitCertReady(t, certName)
	s.GnmiGen.AssertCollectedOnce(t, 1, leafTLS1, leafTLS2)
	// skip-verify lets streams recover before kubelet has projected the new
	// Secret into the mount. Wait for the file; a one-shot exec races that.
	waitPodFile(t, cluster, clientTLSCertPath)
}

func conditionTrue(c *gnmicv1alpha1.Cluster, typ string) bool {
	for _, cond := range c.Status.Conditions {
		if cond.Type == typ {
			return cond.Status == metav1.ConditionTrue
		}
	}
	return false
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
