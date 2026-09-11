//go:build integration

// Package gnmiserver covers the collector's northbound gNMI server: a Cluster
// with api.gnmiPort answers Capabilities, proxies Get to the targets it
// collects, and serves Subscribe from the cache its own subscriptions fill.
//
// The operator's part is the gnmi-server section of the bootstrap config; the
// listener itself is gnmic's (openconfig/gnmic#995). This suite needs a
// collector image that has it -- an older collector accepts the section and
// starts nothing, which is what the negative cases here would look like.
//
// Test -> ID:
//
//	TestGNMI001_ServiceAndConfigRendered   -> 011-1
//	TestGNMI002_CapabilitiesOverPodPort    -> 011-2
//	TestGNMI003_GetIsProxiedToEveryTarget  -> 011-3
//	TestGNMI004_SubscribeOnceServesCache   -> 011-4
//	TestGNMI005_UnknownTargetIsNotFound    -> 011-5
//	TestGNMI006_SetIsRefusedReadOnly       -> 011-6
//	TestGNMI007_NoPortNoListener           -> 011-7
//	TestGNMI008_TLSFollowsAPITLS           -> 011-8
//	TestGNMI009_EachPodServesItsOwnTargets -> 011-9
//
// Run:
//
//	GNMIC_GIT_REF= GNMIC_IMAGE=<image with #995> make integration-test-011-gnmi-server
package gnmiserver

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/target"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"

	"github.com/gnmic/operator/test/integration/harness"
)

var s *harness.Suite

const (
	cluster  = "c1"
	gnmiPort = 9393
	restPort = 7890

	leaf1 = "leaf1"
	leaf2 = "leaf2"

	pathOctets = "/interface/statistics/in-octets"

	// Clusters created by individual tests.
	clusterNoPort = "noport"
	clusterTLS    = "tls"
	gnmiPortTLS   = 9394
	issuer        = "suite-ca-issuer"
)

var leaves = []string{leaf1, leaf2}

func TestMain(m *testing.M) {
	os.Exit(harness.RunSuite(m, harness.Options{
		Name:           "011-gnmi-server",
		RequireTargets: leaves,
		Baseline:       []string{"fixtures/baseline.yaml"},
		BeforeGnmiGen:  stageIssuer,
	}, &s))
}

func stageIssuer(suite *harness.Suite) error {
	b, err := os.ReadFile("fixtures/issuers.yaml")
	if err != nil {
		return err
	}
	if _, err := suite.K8s.ApplyYAMLNoCleanup(string(b), nil); err != nil {
		return fmt.Errorf("applying issuers: %w", err)
	}
	deadline := time.Now().Add(harness.Long)
	for {
		var cert certmanagerv1.Certificate
		err := suite.K8s.Client.Get(suite.Ctx, types.NamespacedName{Namespace: suite.Namespace, Name: "suite-ca"}, &cert)
		if err == nil {
			for _, c := range cert.Status.Conditions {
				if c.Type == certmanagerv1.CertificateConditionReady && c.Status == cmmeta.ConditionTrue {
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("certificate suite-ca not Ready: %v", err)
		}
		time.Sleep(time.Second)
	}
}

// --- gNMI client -----------------------------------------------------------

// gnmiClient forwards a collector pod's gNMI port and returns a connected gnmic
// client for it. opts override the default plaintext connection.
func gnmiClient(t *testing.T, pod string, port int, opts ...api.TargetOption) *target.Target {
	t.Helper()
	fw, err := s.K8s.ForwardPodPort(pod, port)
	if err != nil {
		t.Fatalf("port-forwarding %s:%d: %v", pod, port, err)
	}
	t.Cleanup(fw.Close)

	all := append([]api.TargetOption{
		api.Name("northbound"),
		api.Address(fmt.Sprintf("127.0.0.1:%d", fw.LocalPort)),
		api.Timeout(10 * time.Second),
		api.Insecure(true),
	}, opts...)
	tg, err := api.NewTarget(all...)
	if err != nil {
		t.Fatalf("building gNMI target: %v", err)
	}
	ctx, cancel := context.WithTimeout(s.Ctx, 15*time.Second)
	defer cancel()
	if err := tg.CreateGNMIClient(ctx); err != nil {
		t.Fatalf("connecting to %s:%d over the forward: %v", pod, port, err)
	}
	t.Cleanup(func() { _ = tg.Close() })
	return tg
}

// tryGNMIClient is gnmiClient for cases where connecting is expected to fail.
func tryGNMIClient(t *testing.T, pod string, port int, opts ...api.TargetOption) (*target.Target, error) {
	t.Helper()
	fw, err := s.K8s.ForwardPodPort(pod, port)
	if err != nil {
		return nil, err
	}
	t.Cleanup(fw.Close)
	all := append([]api.TargetOption{
		api.Name("northbound"),
		api.Address(fmt.Sprintf("127.0.0.1:%d", fw.LocalPort)),
		api.Timeout(5 * time.Second),
	}, opts...)
	tg, err := api.NewTarget(all...)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.Ctx, 8*time.Second)
	defer cancel()
	if err := tg.CreateGNMIClient(ctx); err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = tg.Close() })
	return tg, nil
}

func capabilities(ctx context.Context, tg *target.Target) (*gnmi.CapabilityResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return tg.Capabilities(ctx)
}

// targetName is how the collector names an operator-declared Target.
func targetName(name string) string { return s.Namespace + "/" + name }

// notificationTargets returns the distinct prefix targets in a set of
// notifications, sorted.
func notificationTargets(ns []*gnmi.Notification) []string {
	set := map[string]struct{}{}
	for _, n := range ns {
		set[n.GetPrefix().GetTarget()] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func hasOctetsUpdate(n *gnmi.Notification) bool {
	for _, u := range n.GetUpdate() {
		var parts []string
		for _, e := range u.GetPath().GetElem() {
			parts = append(parts, e.GetName())
		}
		if strings.Contains(strings.Join(parts, "/"), "in-octets") && u.GetVal() != nil {
			return true
		}
	}
	return false
}

// waitCollecting waits until the collector streams from every leaf, so the
// cache has content.
func waitCollecting(t *testing.T) {
	t.Helper()
	harness.WaitClusterReady(t, s.K8s, cluster)
	harness.WaitConfigApplied(t, s.K8s, cluster)
	for _, l := range leaves {
		s.GnmiGen.WaitStreams(t, l, 1)
	}
}

// --- tests ------------------------------------------------------------------

// TestGNMI001_ServiceAndConfigRendered checks the plumbing: the gnmi-server
// section in the bootstrap config, the Service port, and the container port.
func TestGNMI001_ServiceAndConfigRendered(t *testing.T) {
	harness.WaitClusterReady(t, s.K8s, cluster)

	cm := s.K8s.ConfigMap(t, harness.ConfigMapName(cluster))
	cfg := cm.Data["config.yaml"]
	for _, want := range []string{"gnmi-server:", fmt.Sprintf("address: :%d", gnmiPort), "type: oc"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("bootstrap config lacks %q:\n%s", want, cfg)
		}
	}
	if strings.Contains(strings.SplitN(cfg, "gnmi-server:", 2)[1], "client-auth") {
		t.Errorf("gnmi-server must not require client certificates:\n%s", cfg)
	}

	svc := s.K8s.Service(t, harness.HeadlessServiceName(cluster))
	found := false
	for _, p := range svc.Spec.Ports {
		if p.Port == gnmiPort && p.Name == "gnmi" {
			found = true
		}
	}
	if !found {
		t.Errorf("headless Service lacks a gnmi port %d: %+v", gnmiPort, svc.Spec.Ports)
	}

	pods := s.K8s.WaitReadyPods(t, cluster, 1, harness.Long)
	exposed := false
	for _, c := range pods[0].Spec.Containers {
		for _, p := range c.Ports {
			if p.ContainerPort == gnmiPort {
				exposed = true
			}
		}
	}
	if !exposed {
		t.Errorf("pod does not declare container port %d", gnmiPort)
	}
}

// TestGNMI002_CapabilitiesOverPodPort is the proof that the section produced a
// listener: Capabilities answers on the pod's gNMI port.
func TestGNMI002_CapabilitiesOverPodPort(t *testing.T) {
	s.K8s.WaitReadyPods(t, cluster, 1, harness.Long)
	tg := gnmiClient(t, harness.PodName(cluster, 0), gnmiPort)

	caps, err := capabilities(s.Ctx, tg)
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if caps.GetGNMIVersion() == "" {
		t.Errorf("empty gNMI version in %v", caps)
	}
	hasJSONIETF := false
	for _, e := range caps.GetSupportedEncodings() {
		if e == gnmi.Encoding_JSON_IETF {
			hasJSONIETF = true
		}
	}
	if !hasJSONIETF {
		t.Errorf("JSON_IETF not among supported encodings: %v", caps.GetSupportedEncodings())
	}
}

// TestGNMI003_GetIsProxiedToEveryTarget sends one Get with target "*" and
// expects one answer per collected target, each attributed to its target name.
func TestGNMI003_GetIsProxiedToEveryTarget(t *testing.T) {
	waitCollecting(t)
	tg := gnmiClient(t, harness.PodName(cluster, 0), gnmiPort)

	req, err := api.NewGetRequest(api.Target("*"), api.Path(pathOctets), api.Encoding("json_ietf"))
	if err != nil {
		t.Fatal(err)
	}
	var resp *gnmi.GetResponse
	harness.Wait(t, harness.Medium, "Get proxied to both targets", func() (bool, string) {
		ctx, cancel := context.WithTimeout(s.Ctx, 15*time.Second)
		defer cancel()
		resp, err = tg.Get(ctx, req)
		if err != nil {
			return false, err.Error()
		}
		got := notificationTargets(resp.GetNotification())
		want := []string{targetName(leaf1), targetName(leaf2)}
		return strings.Join(got, ",") == strings.Join(want, ","), fmt.Sprintf("targets %v", got)
	})
	for _, n := range resp.GetNotification() {
		if !hasOctetsUpdate(n) {
			t.Errorf("notification for %s carries no in-octets value: %v", n.GetPrefix().GetTarget(), n)
		}
	}
}

// TestGNMI004_SubscribeOnceServesCache reads what the collector's own STREAM
// subscription has been putting in the cache: a ONCE subscription with target
// "*" returns the counter for every collected target.
func TestGNMI004_SubscribeOnceServesCache(t *testing.T) {
	waitCollecting(t)
	tg := gnmiClient(t, harness.PodName(cluster, 0), gnmiPort)

	req, err := api.NewSubscribeRequest(
		api.Target("*"),
		api.Encoding("json_ietf"),
		api.SubscriptionListMode("once"),
		api.Subscription(api.Path(pathOctets)),
	)
	if err != nil {
		t.Fatal(err)
	}
	harness.Wait(t, harness.Medium, "ONCE subscription answered for both targets from cache", func() (bool, string) {
		ctx, cancel := context.WithTimeout(s.Ctx, 20*time.Second)
		defer cancel()
		rsps, err := tg.SubscribeOnce(ctx, req)
		if err != nil {
			return false, err.Error()
		}
		var notifs []*gnmi.Notification
		for _, r := range rsps {
			if u := r.GetUpdate(); u != nil && hasOctetsUpdate(u) {
				notifs = append(notifs, u)
			}
		}
		got := notificationTargets(notifs)
		want := []string{targetName(leaf1), targetName(leaf2)}
		return strings.Join(got, ",") == strings.Join(want, ","), fmt.Sprintf("%d responses, targets %v", len(rsps), got)
	})
}

// TestGNMI005_UnknownTargetIsNotFound: a Get for a target the collector does
// not have is a clean NotFound, not a hang or an empty 200.
func TestGNMI005_UnknownTargetIsNotFound(t *testing.T) {
	s.K8s.WaitReadyPods(t, cluster, 1, harness.Long)
	tg := gnmiClient(t, harness.PodName(cluster, 0), gnmiPort)

	req, err := api.NewGetRequest(api.Target("no-such-target"), api.Path(pathOctets), api.Encoding("json_ietf"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(s.Ctx, 15*time.Second)
	defer cancel()
	_, err = tg.Get(ctx, req)
	if status.Code(err) != codes.NotFound {
		t.Fatalf("Get for an unknown target: code=%s err=%v, want NotFound", status.Code(err), err)
	}
}

// TestGNMI006_SetIsRefusedReadOnly: the operator does not enable writes, so a
// Set through the collector is refused rather than forwarded to a device.
func TestGNMI006_SetIsRefusedReadOnly(t *testing.T) {
	s.K8s.WaitReadyPods(t, cluster, 1, harness.Long)
	tg := gnmiClient(t, harness.PodName(cluster, 0), gnmiPort)

	req, err := api.NewSetRequest(
		api.Target(targetName(leaf1)),
		api.Update(api.Path("/system/name"), api.Value("renamed", "json_ietf")),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(s.Ctx, 15*time.Second)
	defer cancel()
	_, err = tg.Set(ctx, req)
	if err == nil {
		t.Fatal("Set succeeded through a read-only proxy")
	}
	switch status.Code(err) {
	case codes.Unimplemented, codes.PermissionDenied, codes.FailedPrecondition:
	default:
		t.Fatalf("Set refused with code %s (%v); want Unimplemented/PermissionDenied/FailedPrecondition", status.Code(err), err)
	}
}

// TestGNMI007_NoPortNoListener: without api.gnmiPort nothing is rendered and
// nothing listens -- the feature is opt-in.
func TestGNMI007_NoPortNoListener(t *testing.T) {
	s.K8s.ApplyFile(t, "fixtures/cluster.yaml", map[string]any{"Name": clusterNoPort, "GnmiPort": 0, "TLSIssuer": ""})
	harness.WaitClusterReady(t, s.K8s, clusterNoPort)
	s.K8s.WaitReadyPods(t, clusterNoPort, 1, harness.Long)

	cm := s.K8s.ConfigMap(t, harness.ConfigMapName(clusterNoPort))
	if strings.Contains(cm.Data["config.yaml"], "gnmi-server") {
		t.Errorf("gnmi-server rendered without gnmiPort:\n%s", cm.Data["config.yaml"])
	}
	svc := s.K8s.Service(t, harness.HeadlessServiceName(clusterNoPort))
	for _, p := range svc.Spec.Ports {
		if p.Name == "gnmi" {
			t.Errorf("headless Service has a gnmi port without gnmiPort: %+v", svc.Spec.Ports)
		}
	}

	tg, err := tryGNMIClient(t, harness.PodName(clusterNoPort, 0), gnmiPort, api.Insecure(true))
	if err == nil {
		if _, err = capabilities(s.Ctx, tg); err == nil {
			t.Fatal("Capabilities answered on a Cluster with no gnmiPort")
		}
	}
	t.Logf("no listener, as expected: %v", err)
}

// TestGNMI008_TLSFollowsAPITLS: with api.tls the gNMI server presents the API
// server certificate. A client that skips verification connects; a plaintext
// client does not. No client certificate is required.
func TestGNMI008_TLSFollowsAPITLS(t *testing.T) {
	s.K8s.ApplyFile(t, "fixtures/cluster.yaml", map[string]any{"Name": clusterTLS, "GnmiPort": gnmiPortTLS, "TLSIssuer": issuer})
	harness.WaitClusterReady(t, s.K8s, clusterTLS)
	s.K8s.WaitReadyPods(t, clusterTLS, 1, harness.Long)

	cm := s.K8s.ConfigMap(t, harness.ConfigMapName(clusterTLS))
	gs := strings.SplitN(cm.Data["config.yaml"], "gnmi-server:", 2)
	if len(gs) != 2 || !strings.Contains(gs[1], "cert-file") || strings.Contains(gs[1], "client-auth") {
		t.Fatalf("gnmi-server TLS block wrong:\n%s", cm.Data["config.yaml"])
	}

	pod := harness.PodName(clusterTLS, 0)
	tg := gnmiClient(t, pod, gnmiPortTLS, api.Insecure(false), api.SkipVerify(true))
	if _, err := capabilities(s.Ctx, tg); err != nil {
		t.Fatalf("Capabilities over TLS (skip-verify): %v", err)
	}

	plain, err := tryGNMIClient(t, pod, gnmiPortTLS, api.Insecure(true))
	if err == nil {
		if _, err = capabilities(s.Ctx, plain); err == nil {
			t.Fatal("plaintext Capabilities succeeded against a TLS listener")
		}
	}
	t.Logf("plaintext refused, as expected: %v", err)
}

// TestGNMI009_EachPodServesItsOwnTargets: at two replicas every target is
// distributed to exactly one pod, and a Get with target "*" on each pod answers
// for that pod's targets only -- together they cover the set exactly once.
func TestGNMI009_EachPodServesItsOwnTargets(t *testing.T) {
	waitCollecting(t)
	s.K8s.Patch(t, s.K8s.Cluster(t, cluster), `{"spec":{"replicas":2}}`)
	t.Cleanup(func() {
		s.K8s.Patch(t, s.K8s.Cluster(t, cluster), `{"spec":{"replicas":1}}`)
		s.K8s.WaitPodGone(t, harness.PodName(cluster, 1))
	})
	s.K8s.WaitReadyPods(t, cluster, 2, harness.Long)
	harness.WaitConfigApplied(t, s.K8s, cluster)
	s.GnmiGen.WaitFleetStreams(t, harness.Medium, 1, leaves)

	req, err := api.NewGetRequest(api.Target("*"), api.Path(pathOctets), api.Encoding("json_ietf"))
	if err != nil {
		t.Fatal(err)
	}
	clients := []*target.Target{
		gnmiClient(t, harness.PodName(cluster, 0), gnmiPort),
		gnmiClient(t, harness.PodName(cluster, 1), gnmiPort),
	}
	harness.Wait(t, harness.Medium, "the two pods together answer for each target exactly once", func() (bool, string) {
		owners := map[string]int{}
		var perPod []string
		for i, tg := range clients {
			ctx, cancel := context.WithTimeout(s.Ctx, 15*time.Second)
			resp, err := tg.Get(ctx, req)
			cancel()
			if err != nil && status.Code(err) != codes.NotFound {
				return false, fmt.Sprintf("pod %d: %v", i, err)
			}
			names := notificationTargets(resp.GetNotification())
			perPod = append(perPod, fmt.Sprintf("pod%d=%v", i, names))
			for _, n := range names {
				owners[n]++
			}
		}
		for _, l := range leaves {
			if owners[targetName(l)] != 1 {
				return false, fmt.Sprintf("%s answered by %d pod(s): %s", l, owners[targetName(l)], strings.Join(perPod, " "))
			}
		}
		return true, ""
	})
}
