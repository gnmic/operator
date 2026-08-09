//go:build integration

package harness

import (
	"encoding/json"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// PodUsage is instantaneous CPU/memory usage for one pod, summed across
// containers, as reported by the kubelet stats summary.
type PodUsage struct {
	Name        string
	CPUCores    float64 // usageNanoCores / 1e9
	MemoryBytes uint64  // working set
}

// PodUsageOf fetches usage for a single pod via the node kubelet stats proxy.
// kind exposes this without metrics-server.
func (k *K8s) PodUsageOf(t *testing.T, pod corev1.Pod) PodUsage {
	t.Helper()
	if pod.Spec.NodeName == "" {
		t.Fatalf("pod %s has no nodeName", pod.Name)
	}
	summary, err := k.nodeStatsSummary(pod.Spec.NodeName)
	if err != nil {
		t.Fatalf("node stats for %s: %v", pod.Spec.NodeName, err)
	}
	for _, p := range summary.Pods {
		if p.PodRef.Namespace != pod.Namespace || p.PodRef.Name != pod.Name {
			continue
		}
		var cpu uint64
		var mem uint64
		for _, c := range p.Containers {
			cpu += c.CPU.UsageNanoCores
			mem += c.Memory.WorkingSetBytes
		}
		return PodUsage{
			Name:        pod.Name,
			CPUCores:    float64(cpu) / 1e9,
			MemoryBytes: mem,
		}
	}
	t.Fatalf("pod %s/%s not present in node stats summary", pod.Namespace, pod.Name)
	return PodUsage{}
}

// LogPodUsage writes a one-line usage summary for diagnostics / trend lines.
func LogPodUsage(t *testing.T, u PodUsage) {
	t.Helper()
	t.Logf("usage %s: cpu=%.3f cores mem=%.1f MiB", u.Name, u.CPUCores, float64(u.MemoryBytes)/(1024*1024))
}

// AssertPodUsageWithinLimits fails if usage exceeds the pod's container limits
// (when set). CPU without a limit is checked against a soft ceiling of 4 cores
// so a runaway process still fails the suite.
func AssertPodUsageWithinLimits(t *testing.T, pod corev1.Pod, u PodUsage) {
	t.Helper()
	LogPodUsage(t, u)

	var memLimit, cpuLimit int64
	for _, c := range pod.Spec.Containers {
		if q := c.Resources.Limits.Memory(); !q.IsZero() {
			memLimit += q.Value()
		}
		if q := c.Resources.Limits.Cpu(); !q.IsZero() {
			cpuLimit += q.MilliValue()
		}
	}
	if memLimit > 0 && u.MemoryBytes > uint64(memLimit) {
		t.Fatalf("%s memory %.1f MiB exceeds limit %s",
			u.Name, float64(u.MemoryBytes)/(1024*1024), resource.NewQuantity(memLimit, resource.BinarySI))
	}
	if cpuLimit > 0 {
		limitCores := float64(cpuLimit) / 1000
		if u.CPUCores > limitCores {
			t.Fatalf("%s cpu %.3f cores exceeds limit %.3f", u.Name, u.CPUCores, limitCores)
		}
	} else if u.CPUCores > 4 {
		t.Fatalf("%s cpu %.3f cores exceeds soft ceiling of 4 (no limit set)", u.Name, u.CPUCores)
	}
}

type nodeStatsSummary struct {
	Pods []nodeStatsPod `json:"pods"`
}

type nodeStatsPod struct {
	PodRef     nodeStatsPodRef      `json:"podRef"`
	Containers []nodeStatsContainer `json:"containers"`
}

type nodeStatsPodRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type nodeStatsContainer struct {
	Name string `json:"name"`
	CPU  struct {
		UsageNanoCores uint64 `json:"usageNanoCores"`
	} `json:"cpu"`
	Memory struct {
		WorkingSetBytes uint64 `json:"workingSetBytes"`
	} `json:"memory"`
}

func (k *K8s) nodeStatsSummary(node string) (*nodeStatsSummary, error) {
	path := fmt.Sprintf("/api/v1/nodes/%s/proxy/stats/summary", node)
	raw, err := k.Clientset.CoreV1().RESTClient().Get().AbsPath(path).DoRaw(k.Ctx)
	if err != nil {
		return nil, err
	}
	var out nodeStatsSummary
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
