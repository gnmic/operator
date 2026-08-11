//go:build integration

// Package scale covers fleet-size operator behavior: convergence, placement,
// single-target churn cost, sustained membership change, and mass reboot.
//
// Gated behind RUN_SCALE=1 (see TestMain). Not part of the default CI lane.
// Tunables (env vars, with defaults):
//
//	SCALE_TARGETS=200
//	SCALE_REPLICAS=4
//	SCALE_CPU_REQUEST=500m
//	SCALE_CPU_LIMIT=2
//	SCALE_MEMORY_REQUEST=512Mi
//	SCALE_MEMORY_LIMIT=2Gi
package scale

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/gnmic/operator/test/integration/harness"
)

const (
	cluster  = "c1"
	pipeline = "fleet"
	output   = "prom"
)

var (
	s         *harness.Suite
	targets   []string
	fleetN    int
	replicas  int
	cpuReq    string
	cpuLim    string
	memReq    string
	memLim    string
	spareSim  string
	sparePort int
	minPerPod int
	maxPerPod int
)

func TestMain(m *testing.M) {
	if os.Getenv("RUN_SCALE") != "1" {
		fmt.Fprintln(os.Stderr, "013-scale: skipped (set RUN_SCALE=1 to enable)")
		os.Exit(0)
	}
	fleetN = envInt("SCALE_TARGETS", 200)
	replicas = envInt("SCALE_REPLICAS", 4)
	cpuReq = envStr("SCALE_CPU_REQUEST", "500m")
	cpuLim = envStr("SCALE_CPU_LIMIT", "2")
	memReq = envStr("SCALE_MEMORY_REQUEST", "512Mi")
	memLim = envStr("SCALE_MEMORY_LIMIT", "2Gi")
	if replicas < 1 {
		fmt.Fprintf(os.Stderr, "013-scale: SCALE_REPLICAS=%d too small (min 1)\n", replicas)
		os.Exit(1)
	}
	if fleetN < replicas {
		fmt.Fprintf(os.Stderr, "013-scale: SCALE_TARGETS=%d < SCALE_REPLICAS=%d\n", fleetN, replicas)
		os.Exit(1)
	}
	spareSim = fmt.Sprintf("dev-%d", fleetN+1)
	sparePort = 57400 + fleetN // 0-based offset for index fleetN+1
	minPerPod, maxPerPod = placementBand(fleetN, replicas)
	targets = make([]string, fleetN)
	for i := 1; i <= fleetN; i++ {
		targets[i-1] = fmt.Sprintf("dev-%d", i)
	}
	fmt.Fprintf(os.Stderr, "[harness] suite 013-scale: SCALE_TARGETS=%d SCALE_REPLICAS=%d cpu=%s/%s mem=%s/%s spare=%s placement=%d..%d per pod\n",
		fleetN, replicas, cpuReq, cpuLim, memReq, memLim, spareSim, minPerPod, maxPerPod)

	require := append(append([]string{}, targets...), spareSim)
	os.Exit(harness.RunSuite(m, harness.Options{
		Name:              "013-scale",
		GnmiGenConfigData: renderGnmiGenConfig(fleetN + 1),
		RequireTargets:    require,
		Baseline:          []string{"fixtures/baseline.yaml"},
		BaselineVars: map[string]any{
			"Replicas":      replicas,
			"CPURequest":    cpuReq,
			"CPULimit":      cpuLim,
			"MemoryRequest": memReq,
			"MemoryLimit":   memLim,
		},
		AfterBaseline: applyFleetTargets,
	}, &s))
}

func envInt(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func envStr(name, def string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	return v
}

// placementBand is avg±20% with a floor that matches the design's 40–60 band
// at the default 200/4 fleet.
func placementBand(n, r int) (int, int) {
	avg := n / r
	band := (n + r - 1) / (r * 5)
	if band < 1 {
		band = 1
	}
	if n >= 100 && band < 10 {
		band = 10
	}
	min := avg - band
	if min < 0 {
		min = 0
	}
	return min, avg + band
}

func renderGnmiGenConfig(expandEnd int) []byte {
	// expandEnd includes the spare simulator used for address-move tests.
	raw, err := os.ReadFile("gnmi-gen.yaml")
	if err != nil {
		panic(fmt.Sprintf("reading gnmi-gen.yaml: %v", err))
	}
	const placeholder = "__SCALE_END__"
	if !strings.Contains(string(raw), placeholder) {
		panic("gnmi-gen.yaml missing " + placeholder + " placeholder for SCALE_TARGETS")
	}
	return []byte(strings.ReplaceAll(string(raw), placeholder, strconv.Itoa(expandEnd)))
}

func applyFleetTargets(suite *harness.Suite) error {
	var b strings.Builder
	host := harness.GnmiGenHost(suite.Namespace)
	for i := 1; i <= fleetN; i++ {
		fmt.Fprintf(&b, `---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: dev-%d
  labels:
    suite: "013"
spec:
  address: %s:%d
  profile: default
`, i, host, 57399+i)
	}
	_, err := suite.K8s.ApplyYAMLNoCleanup(b.String(), nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[harness] suite 013-scale: applied %d Target CRs\n", fleetN)
	return nil
}

func waitFleetReady(t *testing.T) time.Duration {
	t.Helper()
	return waitFleetReadyWithin(t, harness.Long)
}

func waitFleetReadyWithin(t *testing.T, timeout time.Duration) time.Duration {
	t.Helper()
	restoreFleetAddresses(t)
	start := time.Now()
	s.K8s.WaitReadyPods(t, cluster, replicas, timeout)
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		TargetsCount:      harness.I32(int32(fleetN)),
		UnassignedTargets: harness.I32(0),
		ReadyReplicas:     harness.I32(int32(replicas)),
	})
	s.GnmiGen.WaitFleetStreams(t, timeout, 1, targets)
	s.GnmiGen.WaitStreams(t, spareSim, 0)
	elapsed := time.Since(start)
	t.Logf("fleet converged in %s (SCALE_TARGETS=%d SCALE_REPLICAS=%d)", elapsed, fleetN, replicas)
	return elapsed
}

func restoreFleetAddresses(t *testing.T) {
	t.Helper()
	host := harness.GnmiGenHost(s.Namespace)
	for i := 1; i <= fleetN; i++ {
		name := fmt.Sprintf("dev-%d", i)
		want := fmt.Sprintf("%s:%d", host, 57399+i)
		tgt, err := s.K8s.TargetQuiet(name)
		if err != nil {
			continue
		}
		if tgt.Spec.Address == want {
			continue
		}
		s.K8s.Patch(t, tgt, fmt.Sprintf(`{"spec":{"address":%q}}`, want))
	}
}

func assertSuiteResourceUsage(t *testing.T) {
	t.Helper()
	gens := s.K8s.Pods(t, map[string]string{"app": "gnmi-gen"})
	if len(gens) == 0 {
		t.Fatal("no gnmi-gen pods")
	}
	genUsages := s.K8s.PodUsagesOf(t, gens)
	for _, p := range gens {
		u := genUsages[p.Name]
		harness.LogPodUsage(t, u)
		harness.AssertPodUsageWithinLimitsQuiet(t, p, u)
	}

	collectors := s.K8s.ClusterPods(t, cluster)
	colUsages := s.K8s.PodUsagesOf(t, collectors)
	var maxCPU, sumCPU float64
	var maxMem, sumMem uint64
	var maxCPUName, maxMemName string
	for _, p := range collectors {
		u := colUsages[p.Name]
		harness.AssertPodUsageWithinLimitsQuiet(t, p, u)
		sumCPU += u.CPUCores
		sumMem += u.MemoryBytes
		if u.CPUCores >= maxCPU {
			maxCPU, maxCPUName = u.CPUCores, p.Name
		}
		if u.MemoryBytes >= maxMem {
			maxMem, maxMemName = u.MemoryBytes, p.Name
		}
	}
	n := float64(len(collectors))
	if n == 0 {
		t.Fatal("no collector pods")
	}
	t.Logf("usage collectors: n=%d cpu[avg=%.3f max=%.3f %s] cores mem[avg=%.1f max=%.1f %s] MiB",
		len(collectors), sumCPU/n, maxCPU, maxCPUName,
		float64(sumMem)/(n*1024*1024), float64(maxMem)/(1024*1024), maxMemName)

	ops := s.K8s.OperatorPods(t)
	opUsages := s.K8s.PodUsagesOf(t, ops)
	for _, p := range ops {
		u := opUsages[p.Name]
		harness.LogPodUsage(t, u)
		harness.AssertPodUsageWithinLimitsQuiet(t, p, u)
	}
}

// assertOperatorMemStable fails if working-set grew more than maxGrowthBytes
// across a sample window (leak / runaway cache growth smoke check).
func assertOperatorMemStable(t *testing.T, windows []harness.UsageWindow, maxGrowthBytes uint64) {
	t.Helper()
	for _, w := range windows {
		if w.Samples < 2 {
			t.Fatalf("%s: need ≥2 samples for stability, got %d", w.Name, w.Samples)
		}
		if w.MemMax < w.MemMin {
			continue
		}
		growth := w.MemMax - w.MemMin
		if growth > maxGrowthBytes {
			t.Fatalf("%s memory grew %.1f MiB over soak (min=%.1f max=%.1f; allow ≤%.1f MiB)",
				w.Name,
				float64(growth)/(1024*1024),
				float64(w.MemMin)/(1024*1024),
				float64(w.MemMax)/(1024*1024),
				float64(maxGrowthBytes)/(1024*1024))
		}
	}
}

func assignments(t *testing.T) map[string]string {
	t.Helper()
	out := make(map[string]string, len(targets))
	for _, name := range targets {
		tgt, err := s.K8s.TargetQuiet(name)
		if err != nil {
			continue
		}
		out[name] = tgt.Status.ClusterStates[cluster].Pod
	}
	return out
}

func loadPerPod(as map[string]string) map[string]int {
	loads := map[string]int{}
	for _, pod := range as {
		if pod == "" {
			continue
		}
		loads[pod]++
	}
	return loads
}

func establishedSnapshot() map[string]time.Time {
	out := make(map[string]time.Time, len(targets))
	for _, name := range targets {
		out[name] = s.GnmiGen.EstablishedAt(name)
	}
	return out
}

func TestScale001_FleetConverges(t *testing.T) {
	const timeout = 10 * time.Minute
	// Sample through converge so peak CPU is visible, not only the idle
	// post-Ready reading that AssertPodUsageWithinLimits would see.
	opSampler := s.K8s.StartOperatorUsageSampler(2 * time.Second)
	elapsed := waitFleetReadyWithin(t, timeout)
	harness.WaitClusterCondition(t, s.K8s, cluster, harness.CondReady, metav1.ConditionTrue, timeout)

	promSvc := harness.PromServiceName(cluster, pipeline, output)
	s.K8s.WaitExists(t, promSvc, &corev1.Service{})
	s.K8s.WaitClusterPrometheusSources(t, cluster, pipeline, output, targets, timeout)
	convergeWindows := opSampler.StopAndLog(t, "converge")
	for _, p := range s.K8s.OperatorPods(t) {
		for _, w := range convergeWindows {
			if w.Name == p.Name {
				harness.AssertUsageWindowWithinLimits(t, p, w)
			}
		}
	}

	assertSuiteResourceUsage(t)
	// Short soak: steady-state memory should not climb after Ready.
	soak := s.K8s.SampleOperatorUsageFor(t, 30*time.Second, 2*time.Second, "post-converge soak")
	assertOperatorMemStable(t, soak, 32*1024*1024)
	t.Logf("013-1 converge_seconds=%.1f scale_targets=%d", elapsed.Seconds(), fleetN)
}

func TestScale002_PlacementBalanced(t *testing.T) {
	waitFleetReady(t)

	harness.Wait(t, harness.Medium, fmt.Sprintf("balanced placement %d-%d per pod", minPerPod, maxPerPod), func() (bool, string) {
		as := assignments(t)
		loads := loadPerPod(as)
		if len(loads) != replicas {
			return false, fmt.Sprintf("owners=%v", loads)
		}
		if len(as) != fleetN {
			return false, fmt.Sprintf("cover=%d want %d", len(as), fleetN)
		}
		for name, pod := range as {
			if pod == "" {
				return false, name + " unassigned"
			}
		}
		for pod, n := range loads {
			if n < minPerPod || n > maxPerPod {
				return false, fmt.Sprintf("%s has %d (want %d-%d); loads=%v", pod, n, minPerPod, maxPerPod, loads)
			}
		}
		return true, ""
	})
}

// TestScale003_SingleTargetChangeIsCheap moves one target's address and checks
// the rest of the fleet is not re-established.
//
// gnmi-gen keys streams by simulator server name (bound to a listen port), not
// by Target CR name. Pointing CR dev-1 at the spare port moves the Subscribe
// stream onto the spare simulator.
func TestScale003_SingleTargetChangeIsCheap(t *testing.T) {
	waitFleetReady(t)
	before := establishedSnapshot()

	changed := "dev-1"
	host := harness.GnmiGenHost(s.Namespace)
	origAddr := fmt.Sprintf("%s:%d", host, 57400)
	spareAddr := fmt.Sprintf("%s:%d", host, sparePort)

	t.Cleanup(func() {
		s.K8s.Patch(t, s.K8s.Target(t, changed), fmt.Sprintf(`{"spec":{"address":%q}}`, origAddr))
		s.GnmiGen.WaitFleetStreams(t, harness.Long, 1, targets)
		s.GnmiGen.WaitStreams(t, spareSim, 0)
	})

	s.K8s.Patch(t, s.K8s.Target(t, changed), fmt.Sprintf(`{"spec":{"address":%q}}`, spareAddr))

	s.GnmiGen.WaitStreams(t, changed, 0)
	s.GnmiGen.WaitStreams(t, spareSim, 1)

	rest := targets[1:]
	s.GnmiGen.WaitFleetStreams(t, harness.Long, 1, rest)
	if n := s.GnmiGen.StreamCount(spareSim); n != 1 {
		t.Fatalf("spare %s: want 1 stream, got %d", spareSim, n)
	}
	total := 1
	for _, name := range rest {
		total += s.GnmiGen.StreamCount(name)
	}
	if total != fleetN {
		t.Fatalf("total streams=%d want %d", total, fleetN)
	}

	reestablished := 0
	for _, name := range rest {
		was, now := before[name], s.GnmiGen.EstablishedAt(name)
		if was.IsZero() || now.IsZero() {
			continue
		}
		if now.Sub(was).Abs() > time.Second {
			reestablished++
		}
	}
	if reestablished > 2 {
		t.Fatalf("single-target address change re-established %d other targets (want ≤ 2)", reestablished)
	}
	t.Logf("013-3 changed=%s -> %s others_reestablished=%d", changed, spareSim, reestablished)
}

func TestScale004_ChurnKeepsInvariant(t *testing.T) {
	waitFleetReady(t)
	opSampler := s.K8s.StartOperatorUsageSampler(2 * time.Second)

	var overHits atomic.Int32
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				st := s.GnmiGen.SampleFleetStreams(1, targets)
				if st.Over > 0 {
					overHits.Add(1)
					t.Logf("churn sample: over-collected=%d under=%d", st.Over, st.Under)
				}
			}
		}
	}()

	batchSize := 10
	if batchSize > fleetN {
		batchSize = fleetN
	}
	deadline := time.Now().Add(5 * time.Minute)
	host := harness.GnmiGenHost(s.Namespace)
	for time.Now().Before(deadline) {
		perm := rand.Perm(fleetN)
		batch := perm[:batchSize]
		for _, idx := range batch {
			s.K8s.Delete(t, s.K8s.Target(t, targets[idx]))
		}
		var b strings.Builder
		for _, idx := range batch {
			i := idx + 1
			fmt.Fprintf(&b, `---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: dev-%d
  labels:
    suite: "013"
spec:
  address: %s:%d
  profile: default
`, i, host, 57399+i)
		}
		if _, err := s.K8s.ApplyYAMLNoCleanup(b.String(), nil); err != nil {
			close(stop)
			wg.Wait()
			_ = opSampler.Stop()
			t.Fatalf("recreating targets: %v", err)
		}
		time.Sleep(5 * time.Second)
	}
	close(stop)
	wg.Wait()

	windows := opSampler.StopAndLog(t, "target churn")
	for _, p := range s.K8s.OperatorPods(t) {
		for _, w := range windows {
			if w.Name == p.Name {
				harness.AssertUsageWindowWithinLimits(t, p, w)
			}
		}
	}

	if overHits.Load() > 0 {
		t.Fatalf("observed %d samples with >1 stream during churn", overHits.Load())
	}
	s.GnmiGen.WaitFleetStreams(t, harness.Long, 1, targets)
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		TargetsCount:      harness.I32(int32(fleetN)),
		UnassignedTargets: harness.I32(0),
	})
	harness.AssertNoPanics(t, s.K8s.OperatorLogs(t, 10*time.Minute))

	harness.Consistently(t, 30*time.Second, 2*time.Second, "cluster Ready after churn", func() (bool, string) {
		c := s.K8s.Cluster(t, cluster)
		cond := meta.FindStatusCondition(c.Status.Conditions, harness.CondReady)
		if cond == nil {
			return false, "Ready missing"
		}
		return cond.Status == metav1.ConditionTrue, fmt.Sprintf("status=%s", cond.Status)
	})
}

func TestScale005_ChaosRebootsRecover(t *testing.T) {
	waitFleetReady(t)
	restartsBefore := s.K8s.RestartCounts(t, cluster)
	opSampler := s.K8s.StartOperatorUsageSampler(2 * time.Second)

	var overHits atomic.Int32
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				st := s.GnmiGen.SampleFleetStreams(1, targets)
				if st.Over > 0 {
					overHits.Add(1)
				}
			}
		}
	}()

	// Waves of up to ~10% of the fleet (capped at 32). Unbounded subsets mostly
	// hit already-rebooting targets and leave a reconnect storm that misses the
	// 2m recovery window at SCALE_TARGETS>=200.
	maxWave := fleetN / 10
	if maxWave < 5 {
		maxWave = 5
	}
	if maxWave > 32 {
		maxWave = 32
	}
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		if err := s.GnmiGen.RebootRandom(targets, 5*time.Second, 30*time.Second, maxWave); err != nil {
			t.Logf("reboot: %v", err)
		}
		time.Sleep(10 * time.Second)
	}
	close(stop)
	wg.Wait()

	s.GnmiGen.WaitTargetsUp(t, targets...)
	// Design: within 2 minutes of the last reboot (targets already up above).
	s.GnmiGen.WaitFleetStreams(t, harness.Medium, 1, targets)
	if overHits.Load() > 0 {
		_ = opSampler.Stop()
		t.Fatalf("observed %d samples with >1 stream during/after chaos", overHits.Load())
	}
	harness.WaitClusterCounts(t, s.K8s, cluster, harness.ClusterCounts{
		TargetsCount:      harness.I32(int32(fleetN)),
		UnassignedTargets: harness.I32(0),
	})
	// Target CR READY is mirrored by TargetStateReconciler (SSE + 15s poll) and
	// lags the wire-level stream check at large fleets after a reconnect storm.
	readyTimeout := harness.Medium
	if fleetN > 200 {
		readyTimeout = harness.Long
	}
	harness.WaitTargetsReady(t, s.K8s, readyTimeout, targets)
	harness.AssertNoRestarts(t, restartsBefore, s.K8s.RestartCounts(t, cluster))
	harness.AssertNoPanics(t, s.K8s.OperatorLogs(t, 15*time.Minute))

	windows := opSampler.StopAndLog(t, "chaos reboots")
	for _, p := range s.K8s.OperatorPods(t) {
		for _, w := range windows {
			if w.Name == p.Name {
				harness.AssertUsageWindowWithinLimits(t, p, w)
			}
		}
	}
	assertSuiteResourceUsage(t)
}
