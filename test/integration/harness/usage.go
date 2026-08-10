//go:build integration

package harness

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

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

// UsageWindow aggregates instantaneous kubelet samples for one pod.
type UsageWindow struct {
	Name    string
	Samples int
	CPUMin  float64
	CPUMax  float64
	CPULast float64
	MemMin  uint64
	MemMax  uint64
	MemLast uint64
}

// UsageSampler periodically samples selected pods via kubelet stats.
// Sampling runs off the test goroutine; failures are recorded and checked on Stop.
type UsageSampler struct {
	k        *K8s
	interval time.Duration
	stop     chan struct{}
	wg       sync.WaitGroup

	mu       sync.Mutex
	windows  map[string]*UsageWindow
	lastErr  error
	errCount int
}

// PodUsageOf fetches usage for a single pod via the node kubelet stats proxy.
// kind exposes this without metrics-server.
func (k *K8s) PodUsageOf(t *testing.T, pod corev1.Pod) PodUsage {
	t.Helper()
	usages := k.PodUsagesOf(t, []corev1.Pod{pod})
	u, ok := usages[pod.Name]
	if !ok {
		t.Fatalf("pod %s/%s not present in node stats summary", pod.Namespace, pod.Name)
	}
	return u
}

// PodUsagesOf fetches usage for many pods, caching one node stats summary per
// distinct nodeName so large collector fleets do not re-fetch the same payload.
func (k *K8s) PodUsagesOf(t *testing.T, pods []corev1.Pod) map[string]PodUsage {
	t.Helper()
	out := make(map[string]PodUsage, len(pods))
	summaries := map[string]*nodeStatsSummary{}
	for _, pod := range pods {
		if pod.Spec.NodeName == "" {
			t.Fatalf("pod %s has no nodeName", pod.Name)
		}
		summary, ok := summaries[pod.Spec.NodeName]
		if !ok {
			var err error
			summary, err = k.nodeStatsSummary(pod.Spec.NodeName)
			if err != nil {
				t.Fatalf("node stats for %s: %v", pod.Spec.NodeName, err)
			}
			summaries[pod.Spec.NodeName] = summary
		}
		u, found := usageFromSummary(summary, pod)
		if !found {
			t.Fatalf("pod %s/%s not present in node stats summary", pod.Namespace, pod.Name)
		}
		out[pod.Name] = u
	}
	return out
}

func usageFromSummary(summary *nodeStatsSummary, pod corev1.Pod) (PodUsage, bool) {
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
		}, true
	}
	return PodUsage{}, false
}

// LogPodUsage writes a one-line usage summary for diagnostics / trend lines.
func LogPodUsage(t *testing.T, u PodUsage) {
	t.Helper()
	t.Logf("usage %s: cpu=%.3f cores mem=%.1f MiB", u.Name, u.CPUCores, float64(u.MemoryBytes)/(1024*1024))
}

// LogUsageWindow writes min/max/last for a sampled pod.
func LogUsageWindow(t *testing.T, w UsageWindow) {
	t.Helper()
	if w.Samples == 0 {
		t.Logf("usage-window %s: no samples", w.Name)
		return
	}
	t.Logf("usage-window %s: samples=%d cpu[min=%.3f max=%.3f last=%.3f] cores mem[min=%.1f max=%.1f last=%.1f] MiB",
		w.Name, w.Samples,
		w.CPUMin, w.CPUMax, w.CPULast,
		float64(w.MemMin)/(1024*1024), float64(w.MemMax)/(1024*1024), float64(w.MemLast)/(1024*1024))
}

// AssertPodUsageWithinLimits fails if usage exceeds the pod's container limits
// (when set). CPU without a limit is checked against a soft ceiling of 4 cores
// so a runaway process still fails the suite.
func AssertPodUsageWithinLimits(t *testing.T, pod corev1.Pod, u PodUsage) {
	t.Helper()
	AssertPodUsageWithinLimitsQuiet(t, pod, u)
}

// AssertPodUsageWithinLimitsQuiet is AssertPodUsageWithinLimits without logging.
func AssertPodUsageWithinLimitsQuiet(t *testing.T, pod corev1.Pod, u PodUsage) {
	t.Helper()
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

// AssertUsageWindowWithinLimits checks peak CPU/memory from a sample window.
func AssertUsageWindowWithinLimits(t *testing.T, pod corev1.Pod, w UsageWindow) {
	t.Helper()
	if w.Samples == 0 {
		t.Fatalf("%s: usage window has no samples", pod.Name)
	}
	AssertPodUsageWithinLimitsQuiet(t, pod, PodUsage{
		Name:        w.Name,
		CPUCores:    w.CPUMax,
		MemoryBytes: w.MemMax,
	})
}

// StartOperatorUsageSampler samples controller-manager pods every interval
// until Stop. Use around converge/churn/chaos to capture peak compute, not
// only the idle post-Ready reading.
func (k *K8s) StartOperatorUsageSampler(interval time.Duration) *UsageSampler {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	s := &UsageSampler{
		k:        k,
		interval: interval,
		stop:     make(chan struct{}),
		windows:  map[string]*UsageWindow{},
	}
	s.sampleOnce()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				s.sampleOnce()
			}
		}
	}()
	return s
}

func (s *UsageSampler) sampleOnce() {
	pods, err := s.k.listOperatorPods()
	if err != nil {
		s.recordErr(err)
		return
	}
	if len(pods) == 0 {
		s.recordErr(fmt.Errorf("no controller-manager pods"))
		return
	}
	summaries := map[string]*nodeStatsSummary{}
	for _, pod := range pods {
		if pod.Spec.NodeName == "" {
			s.recordErr(fmt.Errorf("pod %s has no nodeName", pod.Name))
			continue
		}
		summary, ok := summaries[pod.Spec.NodeName]
		if !ok {
			summary, err = s.k.nodeStatsSummary(pod.Spec.NodeName)
			if err != nil {
				s.recordErr(fmt.Errorf("node stats for %s: %w", pod.Spec.NodeName, err))
				continue
			}
			summaries[pod.Spec.NodeName] = summary
		}
		u, found := usageFromSummary(summary, pod)
		if !found {
			s.recordErr(fmt.Errorf("pod %s/%s missing from node stats", pod.Namespace, pod.Name))
			continue
		}
		s.record(u)
	}
}

func (s *UsageSampler) recordErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastErr = err
	s.errCount++
}

func (s *UsageSampler) record(u PodUsage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.windows[u.Name]
	if w == nil {
		w = &UsageWindow{
			Name:   u.Name,
			CPUMin: u.CPUCores,
			MemMin: u.MemoryBytes,
		}
		s.windows[u.Name] = w
	}
	if w.Samples == 0 || u.CPUCores < w.CPUMin {
		w.CPUMin = u.CPUCores
	}
	if u.CPUCores > w.CPUMax {
		w.CPUMax = u.CPUCores
	}
	if w.Samples == 0 || u.MemoryBytes < w.MemMin {
		w.MemMin = u.MemoryBytes
	}
	if u.MemoryBytes > w.MemMax {
		w.MemMax = u.MemoryBytes
	}
	w.CPULast = u.CPUCores
	w.MemLast = u.MemoryBytes
	w.Samples++
}

// Stop ends sampling and returns one window per observed pod name.
func (s *UsageSampler) Stop() []UsageWindow {
	close(s.stop)
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]UsageWindow, 0, len(s.windows))
	for _, w := range s.windows {
		out = append(out, *w)
	}
	return out
}

// StopAndLog stops sampling and logs each window. Fails the test if no samples
// were collected (kubelet stats unavailable).
func (s *UsageSampler) StopAndLog(t *testing.T, label string) []UsageWindow {
	t.Helper()
	windows := s.Stop()
	s.mu.Lock()
	lastErr, errCount := s.lastErr, s.errCount
	s.mu.Unlock()
	if label != "" {
		t.Logf("operator usage during %s:", label)
	}
	if len(windows) == 0 {
		t.Fatalf("operator usage sampler collected no samples (errors=%d last=%v)", errCount, lastErr)
	}
	if errCount > 0 {
		t.Logf("operator usage sampler: %d sample errors (last: %v)", errCount, lastErr)
	}
	for _, w := range windows {
		LogUsageWindow(t, w)
	}
	return windows
}

// SampleOperatorUsageFor samples operator pods for dur, logging the window.
// Useful for a short post-converge stability soak.
func (k *K8s) SampleOperatorUsageFor(t *testing.T, dur, interval time.Duration, label string) []UsageWindow {
	t.Helper()
	s := k.StartOperatorUsageSampler(interval)
	time.Sleep(dur)
	return s.StopAndLog(t, label)
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
