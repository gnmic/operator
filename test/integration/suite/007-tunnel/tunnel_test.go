//go:build integration

// Package tunnel covers gRPC tunnel dial-in: a Cluster running gnmic's tunnel
// server, simulated devices that dial into it, and TunnelTargetPolicy plus
// TargetProfile turning those registrations into collected targets.
//
// Every other suite has the collector dial the simulator. Here the simulator
// dials the collector, so nothing about a peer is known until it registers:
// no Target CR exists, the target's name is the ID the device sent, and the
// only way to see what happened is the collector's own REST API and the
// simulator's stream counts.
//
// Test -> ID:
//
//	TestTunnel001_ServiceAndConfigRendered      -> 007-1
//	TestTunnel002_NoPolicyPeersAreIgnored       -> 007-2 (issue #102's signature)
//	TestTunnel003_PolicyRegistersDialedInPeers  -> 007-3
//	TestTunnel004_NarrowingMatchDropsPeers      -> 007-4
//	TestTunnel005_ProfileCredentialsReachPeers  -> 007-5
//	TestTunnel006_EveryPodReceivesMatches       -> 007-6
//	TestTunnel007_ServiceAnnotationsFollowSpec  -> 007-7
//
// Run:
//
//	make integration-test-007-tunnel
package tunnel

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/gnmic/operator/test/integration/harness"
)

var s *harness.Suite

const (
	cluster  = "c1"
	pipeline = "tunnel"
	policy   = "all"

	tunnelPort = 57401
	restPort   = 7890

	profileDefault = "default"
	profileWrong   = "wrong-creds"

	// Tunnel target IDs are the simulator server names (gnmi-gen.yaml).
	leaf1  = "leaf1"
	leaf2  = "leaf2"
	spine1 = "spine1"

	tunnelTargetType = "GNMI_GNOI"

	// What gnmic logs when a peer registers and no rule matches it. This is
	// the line issue #102 reported.
	ignoredLogLine = "not matching any rule"
)

var peers = []string{leaf1, leaf2, spine1}

func TestMain(m *testing.M) {
	os.Exit(harness.RunSuite(m, harness.Options{
		Name:           "007-tunnel",
		RequireTargets: peers,
		Baseline:       []string{"fixtures/baseline.yaml"},
		BeforeGnmiGen:  stageIssuer,
	}, &s))
}

// stageIssuer creates the suite CA before anything else, so the baseline
// Cluster's tunnel certificates can be issued as soon as it is applied.
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

// --- collector REST helpers -------------------------------------------------

// targetCfg is the slice of gnmic's TargetConfig this suite reads.
type targetCfg struct {
	Name             string `json:"name"`
	TunnelTargetType string `json:"tunnel-target-type"`
}

// collectorAPI returns a GET against one collector pod's REST API, once that
// pod is Ready. It waits on the named pod rather than on a ready count, so it
// works the same at one replica and at two.
func collectorAPI(t *testing.T, podIndex int) func(string) (int, string) {
	t.Helper()
	name := harness.PodName(cluster, podIndex)
	harness.Wait(t, harness.Long, "pod "+name+" ready", func() (bool, string) {
		var pod corev1.Pod
		if err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: name}, &pod); err != nil {
			return false, err.Error()
		}
		for _, c := range pod.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return true, ""
			}
		}
		return false, string(pod.Status.Phase)
	})
	return s.K8s.CollectorAPI(t, name, restPort)
}

// tunnelTargets returns the targets a collector created from tunnel
// registrations: the ones carrying a tunnel-target-type. A target the operator
// declared would not have one.
func tunnelTargets(get func(string) (int, string)) (map[string]targetCfg, error) {
	code, body := get("/api/v1/config/targets")
	if code != 200 {
		return nil, fmt.Errorf("GET /config/targets: %d %s", code, truncate(body))
	}
	var all map[string]targetCfg
	if err := json.Unmarshal([]byte(body), &all); err != nil {
		return nil, fmt.Errorf("decoding /config/targets: %w: %s", err, truncate(body))
	}
	out := make(map[string]targetCfg)
	for name, tc := range all {
		if tc.TunnelTargetType != "" {
			out[name] = tc
		}
	}
	return out, nil
}

// matchKeys returns the tunnel-target-match rules a collector holds, keyed the
// way the operator names them: <namespace>/<policy>. Unlike subscriptions and
// outputs, a policy is not scoped under the pipeline that binds it.
func matchKeys(get func(string) (int, string)) ([]string, error) {
	code, body := get("/api/v1/config/tunnel-target-matches")
	if code != 200 {
		return nil, fmt.Errorf("GET /config/tunnel-target-matches: %d %s", code, truncate(body))
	}
	var all map[string]any
	if err := json.Unmarshal([]byte(body), &all); err != nil {
		return nil, fmt.Errorf("decoding matches: %w: %s", err, truncate(body))
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

func policyKey() string { return s.Namespace + "/" + policy }

func names(m map[string]targetCfg) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// waitTunnelTargets waits until the collector's tunnel-created targets are
// exactly want.
func waitTunnelTargets(t *testing.T, get func(string) (int, string), want ...string) map[string]targetCfg {
	t.Helper()
	sort.Strings(want)
	var last map[string]targetCfg
	harness.Wait(t, harness.Medium, fmt.Sprintf("tunnel targets == %v", want), func() (bool, string) {
		tts, err := tunnelTargets(get)
		if err != nil {
			return false, err.Error()
		}
		last = tts
		got := names(tts)
		return strings.Join(got, ",") == strings.Join(want, ","), fmt.Sprintf("have %v", got)
	})
	return last
}

func truncate(s string) string {
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}

// --- fixtures ---------------------------------------------------------------

// applyPolicy creates or updates the suite's TunnelTargetPolicy. Empty matchID
// means match everything.
func applyPolicy(t *testing.T, profile, matchType, matchID string) {
	t.Helper()
	s.K8s.ApplyFile(t, "fixtures/policy.yaml", map[string]any{
		"Name":      policy,
		"Profile":   profile,
		"MatchType": matchType,
		"MatchID":   matchID,
	})
}

func applyPipeline(t *testing.T) {
	t.Helper()
	s.K8s.ApplyFile(t, "fixtures/pipeline.yaml", map[string]any{
		"Name":   pipeline,
		"Policy": policy,
	})
}

// waitIdle waits for the previous test's Pipeline to be gone and every peer to
// have been dropped, so each test starts from "peers connected, nothing
// matching them".
func waitIdle(t *testing.T, get func(string) (int, string)) {
	t.Helper()
	harness.Wait(t, harness.Medium, "no tunnel targets on the collector", func() (bool, string) {
		tts, err := tunnelTargets(get)
		if err != nil {
			return false, err.Error()
		}
		return len(tts) == 0, fmt.Sprintf("have %v", names(tts))
	})
	harness.Wait(t, harness.Medium, "all peers idle", func() (bool, string) {
		for _, p := range peers {
			if n := s.GnmiGen.StreamCount(p); n != 0 {
				return false, fmt.Sprintf("%s has %d streams", p, n)
			}
		}
		return true, ""
	})
}

// --- tests ------------------------------------------------------------------

// TestTunnel001_ServiceAndConfigRendered checks the plumbing a device needs to
// dial in: a Service on the tunnel port, the tunnel-server block in the
// collector's bootstrap config with the issued certificate, and the container
// port.
func TestTunnel001_ServiceAndConfigRendered(t *testing.T) {
	harness.WaitClusterReady(t, s.K8s, cluster)

	svc := s.K8s.Service(t, harness.TunnelServiceName(cluster))
	if svc.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("tunnel service type = %s, want ClusterIP as specified", svc.Spec.Type)
	}
	if got := svc.Labels[harness.LabelServiceType]; got != harness.ValueServiceTypeTunnel {
		t.Errorf("service-type label = %q, want %s", got, harness.ValueServiceTypeTunnel)
	}
	if got := svc.Spec.Selector[harness.LabelClusterName]; got != cluster {
		t.Errorf("service selector cluster = %q, want %s", got, cluster)
	}
	hasPort := false
	for _, p := range svc.Spec.Ports {
		if p.Port == tunnelPort {
			hasPort = true
		}
	}
	if !hasPort {
		t.Errorf("tunnel service does not expose port %d: %+v", tunnelPort, svc.Spec.Ports)
	}

	cm := s.K8s.ConfigMap(t, harness.ConfigMapName(cluster))
	cfg := cm.Data["config.yaml"]
	for _, want := range []string{"tunnel-server", fmt.Sprintf(":%d", tunnelPort), "cert-file", "key-file"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("bootstrap config lacks %q:\n%s", want, cfg)
		}
	}

	pods := s.K8s.WaitReadyPods(t, cluster, 1, harness.Long)
	exposed := false
	for _, c := range pods[0].Spec.Containers {
		for _, p := range c.Ports {
			if p.ContainerPort == tunnelPort {
				exposed = true
			}
		}
	}
	if !exposed {
		t.Errorf("pod %s does not declare container port %d", pods[0].Name, tunnelPort)
	}
}

// TestTunnel002_NoPolicyPeersAreIgnored reproduces issue #102's report: peers
// dial in, the collector accepts the tunnel, and every registration is turned
// away with "target ignored, not matching any rule" because no
// TunnelTargetPolicy is bound to the Cluster. Nothing is collected. The next
// test is the fix.
func TestTunnel002_NoPolicyPeersAreIgnored(t *testing.T) {
	get := collectorAPI(t, 0)

	// The peers have been retrying since before the Cluster existed; once the
	// tunnel server is up they register and are refused.
	pod := harness.PodName(cluster, 0)
	harness.Wait(t, harness.Medium, "collector logs the refused registrations", func() (bool, string) {
		logs := s.K8s.Logs(t, pod, 30*time.Minute)
		if strings.Contains(logs, ignoredLogLine) {
			return true, ""
		}
		return false, "no " + ignoredLogLine + " yet"
	})

	harness.Consistently(t, 10*time.Second, 2*time.Second, "no tunnel target without a policy", func() (bool, string) {
		tts, err := tunnelTargets(get)
		if err != nil {
			return false, err.Error()
		}
		return len(tts) == 0, fmt.Sprintf("have %v", names(tts))
	})
	for _, p := range peers {
		if n := s.GnmiGen.StreamCount(p); n != 0 {
			t.Errorf("%s has %d stream(s) with no policy bound", p, n)
		}
	}
	keys, err := matchKeys(get)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Errorf("collector holds match rules with no Pipeline: %v", keys)
	}
}

// TestTunnel003_PolicyRegistersDialedInPeers binds a match-all policy through a
// Pipeline and checks the whole path: the rule reaches the collector under the
// operator's key, every connected peer becomes a target of the tunnel type, and
// each is collected exactly once with the profile's subscription.
func TestTunnel003_PolicyRegistersDialedInPeers(t *testing.T) {
	get := collectorAPI(t, 0)
	waitIdle(t, get)

	applyPolicy(t, profileDefault, "", "")
	applyPipeline(t)
	harness.WaitPipelineCondition(t, s.K8s, pipeline, "Ready", metav1.ConditionTrue)
	harness.WaitConfigApplied(t, s.K8s, cluster)

	harness.Wait(t, harness.Medium, "match rule on the collector", func() (bool, string) {
		keys, err := matchKeys(get)
		if err != nil {
			return false, err.Error()
		}
		return len(keys) == 1 && keys[0] == policyKey(), fmt.Sprintf("have %v", keys)
	})

	// Registration happened before the rule existed; the collector must
	// re-evaluate connected peers rather than wait for them to reconnect.
	tts := waitTunnelTargets(t, get, peers...)
	for _, p := range peers {
		if tts[p].TunnelTargetType != tunnelTargetType {
			t.Errorf("%s tunnel-target-type = %q, want %s", p, tts[p].TunnelTargetType, tunnelTargetType)
		}
	}
	for _, p := range peers {
		s.GnmiGen.WaitStreams(t, p, 1)
	}
	s.GnmiGen.ConsistentlyCollectedOnce(t, 10*time.Second, 1, peers...)

	// No Target CR is involved, so the Cluster counts none.
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{TargetsCount: harness.I32(0), PipelinesCount: harness.I32(1)})
}

// TestTunnel004_NarrowingMatchDropsPeers changes the policy's match from
// everything to "leaf.*". The collector re-evaluates its connected peers:
// spine1 is dropped and stops streaming, the leaves are untouched.
func TestTunnel004_NarrowingMatchDropsPeers(t *testing.T) {
	get := collectorAPI(t, 0)
	waitIdle(t, get)

	applyPolicy(t, profileDefault, "", "")
	applyPipeline(t)
	waitTunnelTargets(t, get, peers...)
	for _, p := range peers {
		s.GnmiGen.WaitStreams(t, p, 1)
	}
	before := map[string]time.Time{leaf1: s.GnmiGen.EstablishedAt(leaf1), leaf2: s.GnmiGen.EstablishedAt(leaf2)}

	applyPolicy(t, profileDefault, tunnelTargetType, "leaf.*")
	harness.WaitConfigApplied(t, s.K8s, cluster)

	waitTunnelTargets(t, get, leaf1, leaf2)
	s.GnmiGen.WaitStreams(t, spine1, 0)
	harness.Consistently(t, 10*time.Second, 2*time.Second, "spine1 stays dropped, leaves keep one stream", func() (bool, string) {
		if n := s.GnmiGen.StreamCount(spine1); n != 0 {
			return false, fmt.Sprintf("spine1 has %d streams", n)
		}
		for _, p := range []string{leaf1, leaf2} {
			if n := s.GnmiGen.StreamCount(p); n != 1 {
				return false, fmt.Sprintf("%s has %d streams", p, n)
			}
		}
		return true, ""
	})
	// Narrowing must not have restarted the peers that still match.
	for p, was := range before {
		if now := s.GnmiGen.EstablishedAt(p); !now.Equal(was) {
			t.Errorf("%s stream was re-established (%s -> %s) by a match change that kept it", p, was, now)
		}
	}
}

// TestTunnel005_ProfileCredentialsReachPeers proves the TargetProfile named by
// the policy is what the collector authenticates with: a profile with the wrong
// password yields registered targets that never stream; switching the policy
// back to the right profile recovers them without any change on the device.
func TestTunnel005_ProfileCredentialsReachPeers(t *testing.T) {
	get := collectorAPI(t, 0)
	waitIdle(t, get)

	applyPolicy(t, profileWrong, "", "")
	applyPipeline(t)
	harness.WaitPipelineCondition(t, s.K8s, pipeline, "Ready", metav1.ConditionTrue)

	// Registered -- the match does not depend on credentials...
	waitTunnelTargets(t, get, peers...)
	// ...but nothing streams, because the subscribe is refused.
	harness.Consistently(t, 15*time.Second, 2*time.Second, "no stream with the wrong password", func() (bool, string) {
		for _, p := range peers {
			if n := s.GnmiGen.StreamCount(p); n != 0 {
				return false, fmt.Sprintf("%s has %d streams", p, n)
			}
		}
		return true, ""
	})

	applyPolicy(t, profileDefault, "", "")
	harness.WaitConfigApplied(t, s.K8s, cluster)
	for _, p := range peers {
		s.GnmiGen.WaitStreams(t, p, 1)
	}
	s.GnmiGen.ConsistentlyCollectedOnce(t, 10*time.Second, 1, peers...)
}

// TestTunnel006_EveryPodReceivesMatches scales the Cluster to two pods. A
// tunnel-only Cluster has no declared targets, so the distributor hands every
// pod an empty target set -- and the match rules must still reach each of
// them, or a peer that dials into the second pod is refused. This is the
// failure #106 described and #110/#114 hit from the other direction.
func TestTunnel006_EveryPodReceivesMatches(t *testing.T) {
	get0 := collectorAPI(t, 0)
	waitIdle(t, get0)

	applyPolicy(t, profileDefault, "", "")
	applyPipeline(t)
	waitTunnelTargets(t, get0, peers...)

	s.K8s.Patch(t, s.K8s.Cluster(t, cluster), `{"spec":{"replicas":2}}`)
	t.Cleanup(func() {
		s.K8s.Patch(t, s.K8s.Cluster(t, cluster), `{"spec":{"replicas":1}}`)
		s.K8s.WaitPodGone(t, harness.PodName(cluster, 1))
	})
	// With grpcTunnel.tls the pod template enumerates one certificate source
	// per ordinal, so a replica change also rolls the existing pod -- after the
	// new ordinal is Ready. Wait for the whole rollout, or a forward opened to
	// old pod-0 dies seconds later (seen in CI). The forward from before the
	// scale is stale for the same reason.
	s.K8s.WaitStatefulSetRolledOut(t, cluster, 2)
	harness.WaitConfigApplied(t, s.K8s, cluster)
	get0 = collectorAPI(t, 0)
	get1 := collectorAPI(t, 1)
	for i, get := range []func(string) (int, string){get0, get1} {
		harness.Wait(t, harness.Medium, fmt.Sprintf("pod %d holds the match rule", i), func() (bool, string) {
			keys, err := matchKeys(get)
			if err != nil {
				return false, err.Error()
			}
			return len(keys) == 1 && keys[0] == policyKey(), fmt.Sprintf("have %v", keys)
		})
	}

	// Wherever each peer's tunnel landed, it is collected by exactly one pod.
	harness.Wait(t, harness.Medium, "every peer registered on exactly one pod", func() (bool, string) {
		owners := map[string]int{}
		for _, get := range []func(string) (int, string){get0, get1} {
			tts, err := tunnelTargets(get)
			if err != nil {
				return false, err.Error()
			}
			for name := range tts {
				owners[name]++
			}
		}
		for _, p := range peers {
			if owners[p] != 1 {
				return false, fmt.Sprintf("%s registered on %d pod(s)", p, owners[p])
			}
		}
		return true, ""
	})
	s.GnmiGen.ConsistentlyCollectedOnce(t, 10*time.Second, 1, peers...)
}

// TestTunnel007_ServiceAnnotationsFollowSpec edits spec.grpcTunnel.service on a
// live Cluster. Annotations are how a cloud load balancer is configured and
// used to be ignored on update; the change must land without disturbing the
// Service's type, port or selector.
func TestTunnel007_ServiceAnnotationsFollowSpec(t *testing.T) {
	c := s.K8s.Cluster(t, cluster)
	s.K8s.Patch(t, c, `{"spec":{"grpcTunnel":{"service":{"annotations":{"example.com/lb":"internal"},"labels":{"tier":"edge"}}}}}`)
	t.Cleanup(func() {
		s.K8s.Patch(t, s.K8s.Cluster(t, cluster), `{"spec":{"grpcTunnel":{"service":{"annotations":null,"labels":null}}}}`)
	})

	name := harness.TunnelServiceName(cluster)
	harness.Wait(t, harness.Medium, "tunnel Service carries the new annotation and label", func() (bool, string) {
		svc := s.K8s.Service(t, name)
		return svc.Annotations["example.com/lb"] == "internal" && svc.Labels["tier"] == "edge",
			fmt.Sprintf("annotations=%v labels=%v", svc.Annotations, svc.Labels)
	})
	svc := s.K8s.Service(t, name)
	if svc.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("type changed to %s", svc.Spec.Type)
	}
	if got := svc.Spec.Selector[harness.LabelClusterName]; got != cluster {
		t.Errorf("selector lost: %v", svc.Spec.Selector)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != tunnelPort {
		t.Errorf("ports changed: %+v", svc.Spec.Ports)
	}
	if got := svc.Labels[harness.LabelServiceType]; got != harness.ValueServiceTypeTunnel {
		t.Errorf("operator label overwritten by user labels: %q", got)
	}

	s.K8s.Patch(t, s.K8s.Cluster(t, cluster), `{"spec":{"grpcTunnel":{"service":{"annotations":null,"labels":null}}}}`)
	harness.Wait(t, harness.Medium, "annotation removed again", func() (bool, string) {
		svc := s.K8s.Service(t, name)
		_, hasAnn := svc.Annotations["example.com/lb"]
		_, hasLabel := svc.Labels["tier"]
		return !hasAnn && !hasLabel, fmt.Sprintf("annotations=%v labels=%v", svc.Annotations, svc.Labels)
	})
}
