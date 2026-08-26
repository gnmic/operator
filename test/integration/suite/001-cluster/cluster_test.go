//go:build integration

// Package cluster covers the Cluster CR: the objects the operator builds from
// it, the status it reports, scaling, and garbage collection.
//
// It deliberately does not cover what the collectors collect. Pipelines appear
// only where a test needs a cluster to be doing something; target behavior
// belongs to 003-targets and placement to 008-distribution.
//
// Each test creates its own cluster rather than sharing one, because the
// cluster is the object under test. Resources are tagged per test so a
// pipeline never selects another test's targets.
package cluster

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/gnmic/operator/test/integration/harness"
)

var s *harness.Suite

const (
	defaultRestPort = 7890
	gnmicConfigPath = "/etc/gnmic/config.yaml"
	collectorCtr    = "gnmic"
)

func TestMain(m *testing.M) {
	os.Exit(harness.RunSuite(m, harness.Options{
		Name:           "001-cluster",
		RequireTargets: []string{"leaf1", "leaf2", "spine1"},
		Baseline:       []string{"fixtures/baseline.yaml"},
	}, &s))
}

// newCluster applies a Cluster and waits for it to report Ready. The name is
// per-test so tests never contend for one.
func newCluster(t *testing.T, name string, replicas int, restPort, gnmiPort int) {
	t.Helper()
	s.K8s.ApplyFile(t, "fixtures/cluster.yaml", map[string]any{
		"Name":     name,
		"Image":    harness.GnmicImage(),
		"Replicas": replicas,
		"RestPort": restPort,
		"GnmiPort": gnmiPort,
	})
	harness.WaitClusterReady(t, s.K8s, name)
}

// TestCluster001_CreatesStatefulSet checks a minimal Cluster produces a
// correctly shaped StatefulSet.
func TestCluster001_CreatesStatefulSet(t *testing.T) {
	const name = "sts"
	newCluster(t, name, 1, defaultRestPort, 0)

	sts := &appsv1.StatefulSet{}
	s.K8s.WaitExists(t, harness.StatefulSetName(name), sts)

	if got := *sts.Spec.Replicas; got != 1 {
		t.Errorf("replicas: want 1, got %d", got)
	}
	harness.AssertOwnedBy(t, sts, "Cluster", name)

	ctr := containerNamed(t, sts, collectorCtr)
	if ctr.Image != harness.GnmicImage() {
		t.Errorf("container image: want %s, got %s", harness.GnmicImage(), ctr.Image)
	}
	if !argsContain(ctr.Command, "--config", gnmicConfigPath) {
		t.Errorf("container command does not point at %s: %v", gnmicConfigPath, ctr.Command)
	}

	labels := sts.Spec.Template.Labels
	if got := labels[harness.LabelClusterName]; got != name {
		t.Errorf("pod label %s: want %s, got %q", harness.LabelClusterName, name, got)
	}
	if got := labels["app.kubernetes.io/managed-by"]; got != harness.ValueManagedBy {
		t.Errorf("pod label managed-by: want %s, got %q", harness.ValueManagedBy, got)
	}

	s.K8s.WaitReadyPods(t, name, 1, harness.Long)
}

// TestCluster002_HeadlessServiceExposesAPI checks the API Service is headless
// and carries the configured ports.
//
// Headless is asserted explicitly rather than incidentally: the operator
// addresses individual pods by DNS to apply config, which only works without a
// cluster IP.
func TestCluster002_HeadlessServiceExposesAPI(t *testing.T) {
	newCluster(t, "svca", 1, defaultRestPort, 0)
	newCluster(t, "svcb", 1, 7891, 57400)

	svcA := s.K8s.Service(t, harness.HeadlessServiceName("svca"))
	if svcA.Spec.ClusterIP != corev1.ClusterIPNone {
		t.Errorf("service gnmic-svca is not headless: clusterIP=%q", svcA.Spec.ClusterIP)
	}
	if got := svcA.Labels[harness.LabelServiceType]; got != harness.ValueServiceTypeHeadless {
		t.Errorf("service-type label: want %s, got %q", harness.ValueServiceTypeHeadless, got)
	}
	if got := svcA.Spec.Selector[harness.LabelClusterName]; got != "svca" {
		t.Errorf("service selector: want cluster=svca, got %q", got)
	}
	assertPort(t, svcA, defaultRestPort)

	svcB := s.K8s.Service(t, harness.HeadlessServiceName("svcb"))
	if svcB.Spec.ClusterIP != corev1.ClusterIPNone {
		t.Errorf("service gnmic-svcb is not headless: clusterIP=%q", svcB.Spec.ClusterIP)
	}
	assertPort(t, svcB, 7891)
	assertPort(t, svcB, 57400)

	// The port is only proven usable by talking to it.
	pods := s.K8s.WaitReadyPods(t, "svca", 1, harness.Long)
	get := s.K8s.CollectorAPI(t, pods[0].Name, defaultRestPort)
	harness.Wait(t, harness.Short, "collector REST API to answer", func() (bool, string) {
		code, body := get("/api/v1/targets")
		if code == 200 {
			return true, ""
		}
		return false, fmt.Sprintf("status %d: %s", code, truncate(body))
	})
}

// TestCluster003_BootstrapConfigMapIsMounted checks the rendered config reaches
// the pods at the path the collector reads.
func TestCluster003_BootstrapConfigMapIsMounted(t *testing.T) {
	const name = "cfg"
	newCluster(t, name, 1, defaultRestPort, 0)

	cm := s.K8s.ConfigMap(t, harness.ConfigMapName(name))
	harness.AssertOwnedBy(t, cm, "Cluster", name)

	content, ok := cm.Data["config.yaml"]
	if !ok {
		t.Fatalf("ConfigMap has no config.yaml key; keys are %v", keysOf(cm.Data))
	}

	var parsed struct {
		APIServer struct {
			Address string `json:"address"`
		} `json:"api-server"`
	}
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("config.yaml does not parse: %v\n%s", err, content)
	}
	if want := fmt.Sprintf(":%d", defaultRestPort); parsed.APIServer.Address != want {
		t.Errorf("api-server address: want %s, got %q", want, parsed.APIServer.Address)
	}

	// What the pod has on disk, rather than what the ConfigMap claims.
	pods := s.K8s.WaitReadyPods(t, name, 1, harness.Long)
	onDisk := s.K8s.Exec(t, pods[0].Name, collectorCtr, "cat", gnmicConfigPath)
	if strings.TrimSpace(onDisk) != strings.TrimSpace(content) {
		t.Errorf("file at %s differs from the ConfigMap\n--- pod ---\n%s\n--- configmap ---\n%s",
			gnmicConfigPath, onDisk, content)
	}
}

// TestCluster004_ImageChangeRollsStatefulSet checks a spec.image edit triggers
// a rollout and converges.
func TestCluster004_ImageChangeRollsStatefulSet(t *testing.T) {
	const name = "img"
	newCluster(t, name, 1, defaultRestPort, 0)

	before := &appsv1.StatefulSet{}
	s.K8s.WaitExists(t, harness.StatefulSetName(name), before)
	genBefore := before.Generation

	newImage := altGnmicImage()
	s.K8s.Patch(t, s.K8s.Cluster(t, name), fmt.Sprintf(`{"spec":{"image":%q}}`, newImage))

	harness.Wait(t, harness.Medium, "StatefulSet to pick up the new image", func() (bool, string) {
		sts := s.K8s.StatefulSet(t, harness.StatefulSetName(name))
		ctr := containerNamed(t, sts, collectorCtr)
		if ctr.Image != newImage {
			return false, "image is still " + ctr.Image
		}
		if sts.Generation <= genBefore {
			return false, fmt.Sprintf("generation did not advance past %d", genBefore)
		}
		return true, ""
	})

	// Waiting for "one ready pod" is not enough: during a single-replica
	// rollout the old pod can still be the ready one. Wait for the ready pod to
	// be running the new image.
	harness.Wait(t, harness.Long, "the ready pod to be running the new image", func() (bool, string) {
		for _, p := range s.K8s.ClusterPods(t, name) {
			if p.DeletionTimestamp != nil {
				continue
			}
			img := p.Spec.Containers[0].Image
			if img == newImage {
				return true, ""
			}
			return false, fmt.Sprintf("pod %s still runs %s", p.Name, img)
		}
		return false, "no pods"
	})
	s.K8s.WaitReadyPods(t, name, 1, harness.Long)
	harness.WaitClusterReady(t, s.K8s, name)
	harness.WaitClusterCounts(t, s.K8s, name, harness.ClusterCounts{ReadyReplicas: harness.I32(1)})
}

// TestCluster005_ScalingChangesReplicaCount checks replica edits produce and
// remove pods, and that status follows in both directions.
func TestCluster005_ScalingChangesReplicaCount(t *testing.T) {
	const name = "scale"
	newCluster(t, name, 1, defaultRestPort, 0)
	s.K8s.WaitReadyPods(t, name, 1, harness.Long)

	s.K8s.Patch(t, s.K8s.Cluster(t, name), `{"spec":{"replicas":3}}`)
	harness.Wait(t, harness.Medium, "StatefulSet to request 3 replicas", func() (bool, string) {
		sts := s.K8s.StatefulSet(t, harness.StatefulSetName(name))
		return *sts.Spec.Replicas == 3, fmt.Sprintf("spec.replicas=%d", *sts.Spec.Replicas)
	})
	s.K8s.WaitReadyPods(t, name, 3, harness.Long)
	harness.WaitClusterCounts(t, s.K8s, name, harness.ClusterCounts{ReadyReplicas: harness.I32(3)})
	harness.WaitClusterReady(t, s.K8s, name)
	assertStaysReady(t, name)

	s.K8s.Patch(t, s.K8s.Cluster(t, name), `{"spec":{"replicas":1}}`)
	s.K8s.WaitPodGone(t, harness.PodName(name, 1))
	s.K8s.WaitPodGone(t, harness.PodName(name, 2))
	s.K8s.WaitReadyPods(t, name, 1, harness.Long)
	harness.WaitClusterCounts(t, s.K8s, name, harness.ClusterCounts{ReadyReplicas: harness.I32(1)})
	harness.WaitClusterReady(t, s.K8s, name)
	assertStaysReady(t, name)
}

// TestCluster006_ConfigAppliedWithoutRestart checks that adding collection work
// to a running cluster reconfigures pods over the REST API instead of rolling
// them.
//
// This is the claim every dynamic-edit test in the suite rests on, so it is
// asserted here in its simplest form.
func TestCluster006_ConfigAppliedWithoutRestart(t *testing.T) {
	const name = "dyn"
	newCluster(t, name, 1, defaultRestPort, 0)
	s.K8s.WaitReadyPods(t, name, 1, harness.Long)

	restartsBefore := s.K8s.RestartCounts(t, name)
	genBefore := s.K8s.StatefulSet(t, harness.StatefulSetName(name)).Generation

	s.K8s.ApplyFile(t, "fixtures/collection.yaml", map[string]any{
		"Tag":     name,
		"Cluster": name,
	})

	s.GnmiGen.WaitStreams(t, "leaf1", 1)
	harness.WaitConfigApplied(t, s.K8s, name)

	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, name))
	if genAfter := s.K8s.StatefulSet(t, harness.StatefulSetName(name)).Generation; genAfter != genBefore {
		t.Errorf("StatefulSet generation changed: %d -> %d; the config was applied by rolling pods", genBefore, genAfter)
	}
}

// TestCluster007_StatusCountersReflectResources checks the counters describe
// what the cluster is actually running, and that they follow a deletion.
//
// The counters are paired with a device-side check, because status on its own
// is only the operator agreeing with itself.
func TestCluster007_StatusCountersReflectResources(t *testing.T) {
	const name = "counts"
	newCluster(t, name, 1, defaultRestPort, 0)

	s.K8s.ApplyFile(t, "fixtures/counters.yaml", map[string]any{
		"Tag":     name,
		"Cluster": name,
	})

	harness.WaitClusterCounts(t, s.K8s, name, harness.ClusterCounts{
		PipelinesCount:     harness.I32(1),
		TargetsCount:       harness.I32(2),
		SubscriptionsCount: harness.I32(3),
		OutputsCount:       harness.I32(1),
		InputsCount:        harness.I32(0),
		UnassignedTargets:  harness.I32(0),
	})
	// Three subscriptions are bound here, so one collector opens three streams
	// per target.
	s.GnmiGen.AssertCollectedOnce(t, 3, "leaf1", "leaf2")

	s.K8s.Delete(t, s.K8s.Target(t, name+"-leaf2"))

	harness.WaitClusterCounts(t, s.K8s, name, harness.ClusterCounts{TargetsCount: harness.I32(1)})
	s.GnmiGen.WaitStreams(t, "leaf2", 0)
	s.GnmiGen.WaitStreams(t, "leaf1", 3)
}

// TestCluster008_DeletingClusterGarbageCollects checks owner references are set
// correctly, so deleting a Cluster leaves nothing behind.
//
// The device-side check matters: an object can be deleted while its process
// keeps streaming for a while, so "the StatefulSet is gone" does not by itself
// prove collection stopped.
func TestCluster008_DeletingClusterGarbageCollects(t *testing.T) {
	const name = "gc"
	newCluster(t, name, 1, defaultRestPort, 0)

	s.K8s.ApplyFile(t, "fixtures/prom.yaml", map[string]any{
		"Tag":     name,
		"Cluster": name,
	})
	s.GnmiGen.WaitStreams(t, "leaf1", 1)

	promService := harness.PromServiceName(name, name, name+"-prom")
	s.K8s.WaitExists(t, promService, &corev1.Service{})

	s.K8s.Delete(t, s.K8s.Cluster(t, name))

	s.K8s.WaitAbsent(t, harness.StatefulSetName(name), &appsv1.StatefulSet{})
	s.K8s.WaitAbsent(t, harness.HeadlessServiceName(name), &corev1.Service{})
	s.K8s.WaitAbsent(t, harness.ConfigMapName(name), &corev1.ConfigMap{})
	s.K8s.WaitAbsent(t, promService, &corev1.Service{})

	harness.Wait(t, harness.Medium, "collector pods to be gone", func() (bool, string) {
		pods := s.K8s.ClusterPods(t, name)
		return len(pods) == 0, fmt.Sprintf("%d pod(s) remain", len(pods))
	})
	s.GnmiGen.WaitStreams(t, "leaf1", 0)

	// The Pipeline outlives the Cluster it referenced; it just has nowhere to run.
	if p := s.K8s.Pipeline(t, name); p.Name == "" {
		t.Error("Pipeline was garbage-collected along with the Cluster")
	}
}

// --- helpers -------------------------------------------------------------

func assertStaysReady(t *testing.T, name string) {
	t.Helper()
	harness.Consistently(t, 10*time.Second, time.Second, "Cluster "+name+" staying Ready", func() (bool, string) {
		c := s.K8s.Cluster(t, name)
		for _, cond := range c.Status.Conditions {
			if cond.Type == harness.CondReady {
				return cond.Status == metav1.ConditionTrue,
					fmt.Sprintf("Ready=%s reason=%s", cond.Status, cond.Reason)
			}
		}
		return false, "Ready condition absent"
	})
}

func containerNamed(t *testing.T, sts *appsv1.StatefulSet, name string) corev1.Container {
	t.Helper()
	var names []string
	for _, c := range sts.Spec.Template.Spec.Containers {
		if c.Name == name {
			return c
		}
		names = append(names, c.Name)
	}
	t.Fatalf("no container named %s in %s; containers are %v", name, sts.Name, names)
	return corev1.Container{}
}

func argsContain(args []string, flag, value string) bool {
	for i, a := range args {
		if a == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
		if a == flag+"="+value {
			return true
		}
	}
	return false
}

func assertPort(t *testing.T, svc *corev1.Service, want int32) {
	t.Helper()
	var have []int32
	for _, p := range svc.Spec.Ports {
		if p.Port == want {
			return
		}
		have = append(have, p.Port)
	}
	t.Errorf("service %s has no port %d; ports are %v", svc.Name, want, have)
}

// altGnmicImage is a second pinned tag, used to prove a rollout happened.
func altGnmicImage() string {
	if v := os.Getenv("GNMIC_IMAGE_ALT"); v != "" {
		return v
	}
	return "ghcr.io/openconfig/gnmic:0.47.0-amd64"
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
