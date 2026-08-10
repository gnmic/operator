//go:build integration

// Package outputs covers Output CR binding: Prometheus Services, file output
// delivery, dynamic edits, deletion, and per-pipeline Service naming.
package outputs

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/test/integration/harness"
)

var s *harness.Suite

const (
	cluster = "c1"
	leaf1   = "leaf1"
	leaf2   = "leaf2"
	spine1  = "spine1"
	pathIF  = "/interface/statistics/in-octets"
)

var allTargets = []string{leaf1, leaf2, spine1}

func TestMain(m *testing.M) {
	os.Exit(harness.RunSuite(m, harness.Options{
		Name:           "005-outputs",
		RequireTargets: allTargets,
		Baseline:       []string{"fixtures/baseline.yaml"},
	}, &s))
}

func waitClusterReady(t *testing.T) {
	t.Helper()
	harness.WaitClusterReady(t, s.K8s, cluster)
	s.K8s.WaitReadyPods(t, cluster, 1, harness.Long)
}

func waitIdle(t *testing.T) {
	t.Helper()
	var pipes gnmicv1alpha1.PipelineList
	if err := s.K8s.Client.List(s.Ctx, &pipes, client.InNamespace(s.Namespace)); err == nil {
		for i := range pipes.Items {
			_ = s.K8s.Client.Delete(s.Ctx, &pipes.Items[i])
		}
	}
	var outs gnmicv1alpha1.OutputList
	if err := s.K8s.Client.List(s.Ctx, &outs, client.InNamespace(s.Namespace)); err == nil {
		for i := range outs.Items {
			_ = s.K8s.Client.Delete(s.Ctx, &outs.Items[i])
		}
	}
	harness.Wait(t, harness.Medium, "pipelines gone", func() (bool, string) {
		var left gnmicv1alpha1.PipelineList
		if err := s.K8s.Client.List(s.Ctx, &left, client.InNamespace(s.Namespace)); err != nil {
			return false, err.Error()
		}
		return len(left.Items) == 0, fmt.Sprintf("%d remain", len(left.Items))
	})
	harness.Wait(t, harness.Medium, "outputs gone", func() (bool, string) {
		var left gnmicv1alpha1.OutputList
		if err := s.K8s.Client.List(s.Ctx, &left, client.InNamespace(s.Namespace)); err != nil {
			return false, err.Error()
		}
		return len(left.Items) == 0, fmt.Sprintf("%d remain", len(left.Items))
	})
	harness.Wait(t, harness.Medium, "targets idle", func() (bool, string) {
		for _, name := range allTargets {
			if n := s.GnmiGen.StreamCount(name); n != 0 {
				return false, fmt.Sprintf("%s has %d streams", name, n)
			}
		}
		return true, ""
	})
	s.K8s.WaitReadyPods(t, cluster, 1, harness.Long)
}

func applyPromOutput(t *testing.T, name string, vars map[string]any) {
	t.Helper()
	base := map[string]any{
		"Name":               name,
		"Expiration":         "",
		"ServiceType":        "",
		"ServiceLabels":      map[string]string{},
		"ServiceAnnotations": map[string]string{},
		"ExtraLabelKey":      "",
		"ExtraLabelValue":    "",
	}
	for k, v := range vars {
		base[k] = v
	}
	b, err := os.ReadFile("fixtures/output-prom.yaml")
	if err != nil {
		t.Fatalf("reading prom output fixture: %v", err)
	}
	if _, err := s.K8s.ApplyYAMLNoCleanup(string(b), base); err != nil {
		t.Fatalf("applying prom output %s: %v", name, err)
	}
}

func applyFileOutput(t *testing.T, name, filename string) {
	t.Helper()
	b, err := os.ReadFile("fixtures/output-file.yaml")
	if err != nil {
		t.Fatalf("reading file output fixture: %v", err)
	}
	if _, err := s.K8s.ApplyYAMLNoCleanup(string(b), map[string]any{
		"Name":     name,
		"Filename": filename,
	}); err != nil {
		t.Fatalf("applying file output %s: %v", name, err)
	}
}

func applyPipeline(t *testing.T, name string, outputRefs []string, targetRole string) {
	t.Helper()
	b, err := os.ReadFile("fixtures/pipeline.yaml")
	if err != nil {
		t.Fatalf("reading pipeline fixture: %v", err)
	}
	if _, err := s.K8s.ApplyYAMLNoCleanup(string(b), map[string]any{
		"Name":       name,
		"OutputRefs": outputRefs,
		"TargetRole": targetRole,
	}); err != nil {
		t.Fatalf("applying pipeline %s: %v", name, err)
	}
}

func waitPromService(t *testing.T, pipe, out string) *corev1.Service {
	t.Helper()
	name := harness.PromServiceName(cluster, pipe, out)
	svc := &corev1.Service{}
	s.K8s.WaitExists(t, name, svc)
	return s.K8s.Service(t, name)
}

func startPromPipeline(t *testing.T, pipe, out string) {
	t.Helper()
	applyPromOutput(t, out, nil)
	applyPipeline(t, pipe, []string{out}, "")
	waitClusterReady(t)
	harness.WaitConfigApplied(t, s.K8s, cluster)
	waitPromService(t, pipe, out)
	s.GnmiGen.AssertCollectedOnce(t, 1, allTargets...)
}

func TestOut001_PrometheusServiceShape(t *testing.T) {
	waitIdle(t)
	startPromPipeline(t, "p1", "out1")

	svcName := harness.PromServiceName(cluster, "p1", "out1")
	svc := s.K8s.Service(t, svcName)
	if svc.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("type=%s want ClusterIP", svc.Spec.Type)
	}
	if got := svc.Spec.Selector[harness.LabelClusterName]; got != cluster {
		t.Errorf("selector cluster=%q", got)
	}
	if len(svc.Spec.Ports) == 0 || svc.Spec.Ports[0].Port == 0 {
		t.Fatal("expected allocated metrics port")
	}
	wantLabels := map[string]string{
		harness.LabelServiceType:  harness.ValueServiceTypePrometheus,
		harness.LabelOutputName:   "out1",
		harness.LabelPipelineName: "p1",
		harness.LabelClusterName:  cluster,
	}
	for k, v := range wantLabels {
		if got := svc.Labels[k]; got != v {
			t.Errorf("label %s=%q want %q", k, got, v)
		}
	}
	harness.AssertOwnedBy(t, svc, "Cluster", cluster)
}

func TestOut002_PrometheusServesCollectedData(t *testing.T) {
	waitIdle(t)
	startPromPipeline(t, "p1", "out1")

	s.K8s.WaitClusterPrometheusSources(t, cluster, "p1", "out1", allTargets, harness.Medium)
	body1 := s.K8s.ScrapeClusterPrometheus(t, cluster, "p1", "out1")
	n1 := harness.SampleCount(body1)
	if n1 == 0 {
		t.Fatal("expected samples")
	}
	if !strings.Contains(body1, "interface_statistics") && !strings.Contains(body1, "in_octets") {
		t.Errorf("scrape missing interface path metrics; samples=%d", n1)
	}
	time.Sleep(10 * time.Second)
	body2 := s.K8s.ScrapeClusterPrometheus(t, cluster, "p1", "out1")
	n2 := harness.SampleCount(body2)
	if n2 < n1 {
		t.Errorf("sample count shrank between scrapes: %d -> %d", n1, n2)
	}
	s.K8s.WaitClusterPrometheusSources(t, cluster, "p1", "out1", allTargets, harness.Short)
}

func TestOut003_ServiceTypeHonored(t *testing.T) {
	waitIdle(t)
	applyPromOutput(t, "out1", map[string]any{
		"ServiceType": "NodePort",
		"ServiceLabels": map[string]string{
			"team":                          "telemetry",
			harness.LabelServiceType:        "should-not-win",
			"app.kubernetes.io/managed-by":  "user",
		},
		"ServiceAnnotations": map[string]string{
			"custom.annotation":    "yes",
			"prometheus.io/scrape": "false",
		},
	})
	applyPipeline(t, "p1", []string{"out1"}, "")
	waitClusterReady(t)

	svc := s.K8s.Service(t, harness.PromServiceName(cluster, "p1", "out1"))
	if svc.Spec.Type != corev1.ServiceTypeNodePort {
		t.Fatalf("type=%s want NodePort", svc.Spec.Type)
	}
	if svc.Spec.Ports[0].NodePort == 0 {
		t.Fatal("expected allocated node port")
	}
	if svc.Labels["team"] != "telemetry" {
		t.Errorf("user label missing: %v", svc.Labels)
	}
	if svc.Labels[harness.LabelServiceType] != harness.ValueServiceTypePrometheus {
		t.Errorf("operator service-type overwritten: %q", svc.Labels[harness.LabelServiceType])
	}
	if svc.Labels["app.kubernetes.io/managed-by"] != harness.ValueManagedBy {
		t.Errorf("operator managed-by overwritten: %q", svc.Labels["app.kubernetes.io/managed-by"])
	}
	if svc.Annotations["custom.annotation"] != "yes" {
		t.Errorf("user annotation missing: %v", svc.Annotations)
	}
	if svc.Annotations["prometheus.io/scrape"] != "true" {
		t.Errorf("operator scrape annotation overwritten: %q", svc.Annotations["prometheus.io/scrape"])
	}
}

func TestOut004_FileOutputWritesInPod(t *testing.T) {
	waitIdle(t)
	const file = "/tmp/005-out.jsonl"
	applyFileOutput(t, "file1", file)
	applyPipeline(t, "p1", []string{"file1"}, "")
	waitClusterReady(t)

	events := s.K8s.WaitEventsHaveSources(t, cluster, file, allTargets...)
	if len(events) == 0 {
		t.Fatal("no parseable events in file output")
	}

	// No Prometheus Service as a side effect of a file output.
	svcName := harness.PromServiceName(cluster, "p1", "file1")
	var svc corev1.Service
	err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: svcName}, &svc)
	if err == nil {
		t.Fatalf("unexpected prometheus service %s for file output", svcName)
	}
	if !apierrors.IsNotFound(err) {
		t.Fatalf("getting %s: %v", svcName, err)
	}
}

func TestOut005_EditingOutputNoRestart(t *testing.T) {
	waitIdle(t)
	const oldFile = "/tmp/005-old.jsonl"
	const newFile = "/tmp/005-new.jsonl"
	applyFileOutput(t, "file1", oldFile)
	applyPromOutput(t, "out1", map[string]any{"Expiration": "60s"})
	applyPipeline(t, "p1", []string{"file1", "out1"}, "")
	waitClusterReady(t)
	s.K8s.WaitCollectorFileNonEmpty(t, cluster, oldFile)
	s.K8s.WaitClusterPrometheusSources(t, cluster, "p1", "out1", allTargets, harness.Medium)

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	oldLen := len(s.K8s.ReadCollectorFile(t, cluster, oldFile))

	out := &gnmicv1alpha1.Output{}
	s.K8s.WaitExists(t, "file1", out)
	s.K8s.Patch(t, out, fmt.Sprintf(`{"spec":{"config":{"filename":%q,"format":"event"}}}`, newFile))

	prom := &gnmicv1alpha1.Output{}
	s.K8s.WaitExists(t, "out1", prom)
	s.K8s.Patch(t, prom, `{"spec":{"config":{"expiration":"120s"}}}`)

	harness.WaitConfigApplied(t, s.K8s, cluster)
	s.K8s.WaitCollectorFileNonEmpty(t, cluster, newFile)
	s.K8s.WaitClusterPrometheusSources(t, cluster, "p1", "out1", allTargets, harness.Medium)

	// Old file should stop growing once the output points elsewhere.
	time.Sleep(3 * time.Second)
	if got := len(s.K8s.ReadCollectorFile(t, cluster, oldFile)); got > oldLen+64 {
		t.Errorf("old file still growing: was %d now %d", oldLen, got)
	}
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
}

func TestOut006_DeletingOutputRemovesService(t *testing.T) {
	waitIdle(t)
	startPromPipeline(t, "p1", "out1")
	svcName := harness.PromServiceName(cluster, "p1", "out1")
	_ = s.K8s.Service(t, svcName)

	restartsBefore := s.K8s.RestartCounts(t, cluster)
	out := &gnmicv1alpha1.Output{}
	s.K8s.WaitExists(t, "out1", out)
	if err := s.K8s.Client.Delete(s.Ctx, out); err != nil {
		t.Fatalf("deleting output: %v", err)
	}
	s.K8s.WaitAbsent(t, "out1", &gnmicv1alpha1.Output{})
	s.K8s.WaitAbsent(t, svcName, &corev1.Service{})

	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		OutputsCount: harness.I32(0),
	})
	s.GnmiGen.ConsistentlyCollectedOnce(t, 10*time.Second, 1, allTargets...)
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
}

func TestOut007_SharedOutputTwoServices(t *testing.T) {
	waitIdle(t)
	applyPromOutput(t, "out1", nil)
	applyPipeline(t, "p1", []string{"out1"}, "leaf")
	applyPipeline(t, "p2", []string{"out1"}, "spine")
	waitClusterReady(t)
	harness.WaitConfigApplied(t, s.K8s, cluster)
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		PipelinesCount: harness.I32(2),
		TargetsCount:   harness.I32(3),
		OutputsCount:   harness.I32(2), // per-pipeline output bindings
	})

	// Cluster Ready can still be true from the idle empty plan; wait for the
	// Services themselves before asserting on them.
	svc1 := waitPromService(t, "p1", "out1")
	svc2 := waitPromService(t, "p2", "out1")
	if svc1.Spec.Ports[0].Port == svc2.Spec.Ports[0].Port {
		t.Fatalf("expected distinct ports, both %d", svc1.Spec.Ports[0].Port)
	}

	s.K8s.WaitClusterPrometheusSources(t, cluster, "p1", "out1", []string{leaf1, leaf2}, harness.Medium)
	s.K8s.WaitClusterPrometheusSources(t, cluster, "p2", "out1", []string{spine1}, harness.Medium)

	// Isolation: each pipeline's prometheus port must not carry the other
	// pipeline's targets (shared Subscription CR, disjoint target selectors).
	harness.Wait(t, harness.Medium, "p1 lacks spine1", func() (bool, string) {
		body, err := s.K8s.ScrapeClusterPrometheusQuiet(cluster, "p1", "out1")
		if err != nil {
			return false, err.Error()
		}
		if harness.HasLabel(body, spine1) {
			return false, "spine1 still present"
		}
		return true, ""
	})
	harness.Wait(t, harness.Medium, "p2 lacks leaves", func() (bool, string) {
		body, err := s.K8s.ScrapeClusterPrometheusQuiet(cluster, "p2", "out1")
		if err != nil {
			return false, err.Error()
		}
		if harness.HasLabel(body, leaf1) || harness.HasLabel(body, leaf2) {
			return false, "leaf still present"
		}
		return true, ""
	})

	pipe := s.K8s.Pipeline(t, "p2")
	if err := s.K8s.Client.Delete(s.Ctx, pipe); err != nil {
		t.Fatalf("deleting p2: %v", err)
	}
	s.K8s.WaitAbsent(t, "p2", &gnmicv1alpha1.Pipeline{})
	s.K8s.WaitAbsent(t, harness.PromServiceName(cluster, "p2", "out1"), &corev1.Service{})
	_ = s.K8s.Service(t, harness.PromServiceName(cluster, "p1", "out1"))
	s.K8s.WaitClusterPrometheusSources(t, cluster, "p1", "out1", []string{leaf1, leaf2}, harness.Medium)
}

func TestOut008_ServiceRefResolvesAddress(t *testing.T) {
	t.Skip("prometheus_write sink fixture deferred; see Planned 005-8")
}
