//go:build integration

// Package harness provides the shared machinery for the operator integration
// suites under test/integration/suite.
//
// A suite is a Go package with a TestMain that calls RunSuite. The harness
// gives it an isolated namespace, an in-cluster gnmi-gen simulator with a REST
// client for device-side ground truth, polling assertion helpers, and
// deterministic teardown.
//
// Everything here is behind the "integration" build tag and expects a running
// cluster with the operator deployed: see `make integration-env-up`.
package harness

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed manifests/gnmi-gen.yaml
var gnmiGenManifests string

// Options configures a suite. Set by each suite's TestMain.
type Options struct {
	// Name is the suite directory name, e.g. "003-targets". The namespace is
	// derived from its numeric prefix.
	Name string
	// GnmiGenConfig is the simulator config, relative to the suite directory.
	// Defaults to "gnmi-gen.yaml". Empty string disables the simulator, for
	// suites that do not need one.
	GnmiGenConfig string
	// GnmiGenConfigData if non-nil is used as the simulator config instead of
	// reading GnmiGenConfig from disk. Suites that size the expand range from
	// an env var use this.
	GnmiGenConfigData []byte
	// Baseline fixtures applied after the simulator is up, before m.Run.
	Baseline []string
	// BaselineVars are merged into every baseline fixture template (on top of
	// the harness base vars). Used for suite-wide knobs like replica count.
	BaselineVars map[string]any
	// RequireTargets are simulated targets that must report "up" before tests
	// start.
	RequireTargets []string
	// AfterBaseline runs after baseline fixtures are applied, before m.Run.
	// Used by suites that generate large numbers of CRs (e.g. 013-scale).
	AfterBaseline func(*Suite) error
}

// Suite is the per-suite context, created once in TestMain and shared by every
// test in the package.
type Suite struct {
	Name      string
	Namespace string
	K8s       *K8s
	GnmiGen   *GnmiGen
	Ctx       context.Context

	forward *Forward
	cancel  context.CancelFunc
}

// New brings a suite up: namespace, simulator, baseline fixtures.
func New(opts Options) (*Suite, error) {
	if opts.Name == "" {
		return nil, fmt.Errorf("suite name is required")
	}
	if opts.GnmiGenConfig == "" {
		opts.GnmiGenConfig = "gnmi-gen.yaml"
	}

	ctx, cancel := context.WithCancel(context.Background())
	namespace := namespaceFor(opts.Name)

	k, err := NewK8s(ctx, namespace)
	if err != nil {
		cancel()
		return nil, err
	}

	s := &Suite{Name: opts.Name, Namespace: namespace, K8s: k, Ctx: ctx, cancel: cancel}

	if err := k.createNamespace(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("creating namespace: %w", err)
	}
	logf("suite %s: namespace %s ready", opts.Name, namespace)

	if err := s.deployGnmiGen(opts.GnmiGenConfig, opts.GnmiGenConfigData); err != nil {
		return s, fmt.Errorf("deploying gnmi-gen: %w", err)
	}

	if len(opts.RequireTargets) > 0 {
		if err := s.waitTargetsUp(opts.RequireTargets); err != nil {
			return s, err
		}
	}

	for _, f := range opts.Baseline {
		if err := s.applyBaseline(f, opts.BaselineVars); err != nil {
			return s, fmt.Errorf("applying baseline %s: %w", f, err)
		}
	}
	logf("suite %s: baseline applied", opts.Name)
	if opts.AfterBaseline != nil {
		if err := opts.AfterBaseline(s); err != nil {
			return s, fmt.Errorf("after baseline: %w", err)
		}
	}
	return s, nil
}

// RunSuite is the standard TestMain body:
//
//	func TestMain(m *testing.M) {
//	    os.Exit(harness.RunSuite(m, harness.Options{Name: "003-targets", ...}, &s))
//	}
//
// The suite pointer is populated before m.Run so tests can reach it.
func RunSuite(m *testing.M, opts Options, out **Suite) int {
	s, err := New(opts)
	if s != nil {
		*out = s
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "suite setup failed: %v\n", err)
		if s != nil {
			s.Dump(os.Stderr)
			s.teardown()
		}
		return 1
	}

	code := m.Run()
	if code != 0 {
		s.Dump(os.Stderr)
	}
	s.teardown()
	return code
}

func (s *Suite) teardown() {
	if s.forward != nil {
		s.forward.Close()
	}
	if os.Getenv("KEEP_NS") == "1" {
		logf("KEEP_NS=1, leaving namespace %s in place", s.Namespace)
		logf("  kubectl -n %s get cluster,pipeline,target,pods", s.Namespace)
		logf("  kubectl -n %s port-forward svc/gnmi-gen 8080:8080", s.Namespace)
		s.cancel()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.K8s.deleteNamespace(ctx); err != nil {
		logf("deleting namespace %s: %v", s.Namespace, err)
	}
	s.cancel()
}

// deployGnmiGen creates the simulator ConfigMap from the suite's config file
// (or inline data), applies the Deployment and headless Service, waits for
// readiness, and opens a port-forward to the REST control plane.
func (s *Suite) deployGnmiGen(configPath string, data []byte) error {
	cfg := data
	if len(cfg) == 0 {
		var err error
		cfg, err = os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", configPath, err)
		}
	}

	// The simulator config contains its own Go template syntax, which gnmi-gen
	// expands at startup. It must not go through the fixture renderer.
	sum := sha256.Sum256(cfg)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "gnmi-gen-config", Namespace: s.Namespace},
		Data:       map[string]string{"gnmi-gen.yaml": string(cfg)},
	}
	cm.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	if err := s.K8s.ApplyNoCleanup(cm); err != nil {
		return fmt.Errorf("applying config: %w", err)
	}

	vars := map[string]any{"ConfigHash": hex.EncodeToString(sum[:8])}
	if _, err := s.K8s.applyYAML(gnmiGenManifests, vars); err != nil {
		return err
	}

	if err := s.waitDeploymentAvailable("gnmi-gen"); err != nil {
		return err
	}

	fw, err := s.K8s.ForwardService(s.Ctx, map[string]string{"app": "gnmi-gen"}, 8080)
	if err != nil {
		return fmt.Errorf("port-forwarding gnmi-gen: %w", err)
	}
	s.forward = fw
	s.GnmiGen = NewGnmiGen(fw.URL)
	logf("suite %s: gnmi-gen REST at %s", s.Name, fw.URL)
	return nil
}

func (s *Suite) waitDeploymentAvailable(name string) error {
	deadline := time.Now().Add(Medium)
	for {
		var d appsv1.Deployment
		err := s.K8s.Client.Get(s.Ctx, types.NamespacedName{Namespace: s.Namespace, Name: name}, &d)
		if err == nil && d.Status.ReadyReplicas > 0 {
			return nil
		}
		// A bad simulator config crash-loops rather than hanging. Waiting the
		// full timeout to then report "not available" hides the actual error,
		// which is in the container's log.
		if crash := s.crashingPod(map[string]string{"app": name}); crash != "" {
			return fmt.Errorf("%s is crash-looping:\n%s", name, crash)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("deployment %s not available within %s", name, Medium)
		}
		time.Sleep(time.Second)
	}
}

// crashingPod returns a diagnostic for the first pod stuck restarting, or "".
func (s *Suite) crashingPod(selector map[string]string) string {
	var pods corev1.PodList
	if err := s.K8s.Client.List(s.Ctx, &pods,
		client.InNamespace(s.Namespace), client.MatchingLabels(selector)); err != nil {
		return ""
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		for _, cs := range p.Status.ContainerStatuses {
			waiting := cs.State.Waiting
			if waiting == nil || waiting.Reason != "CrashLoopBackOff" {
				continue
			}
			logs := s.podLogs(p.Name, cs.Name)
			return fmt.Sprintf("pod %s container %s: %s (restarts=%d)\n%s",
				p.Name, cs.Name, waiting.Reason, cs.RestartCount, logs)
		}
	}
	return ""
}

func (s *Suite) podLogs(pod, container string) string {
	tail := int64(20)
	req := s.K8s.Clientset.CoreV1().Pods(s.Namespace).GetLogs(pod, &corev1.PodLogOptions{
		Container: container,
		Previous:  true,
		TailLines: &tail,
	})
	rc, err := req.Stream(s.Ctx)
	if err != nil {
		return fmt.Sprintf("  <logs unavailable: %v>", err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	return string(b)
}

func (s *Suite) waitTargetsUp(names []string) error {
	timeout := Medium
	if len(names) > 50 {
		timeout = Long
	}
	deadline := time.Now().Add(timeout)
	for {
		targets, err := s.GnmiGen.Targets()
		if err == nil {
			missing := ""
			for _, n := range names {
				t, ok := targets[n]
				if !ok {
					missing = n + " not registered"
					break
				}
				if t.Status != "up" {
					missing = n + " is " + t.Status
					break
				}
			}
			if missing == "" {
				logf("suite %s: simulated targets %v are up", s.Name, names)
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("simulated targets not up: %s", missing)
			}
		} else if time.Now().After(deadline) {
			return fmt.Errorf("gnmi-gen API not answering: %w", err)
		}
		time.Sleep(time.Second)
	}
}

func (s *Suite) applyBaseline(path string, vars map[string]any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, err = s.K8s.applyYAML(string(b), vars)
	return err
}

// TargetAddress is the dial address of simulated target n, 0-based.
func (s *Suite) TargetAddress(n int) string {
	return GnmiGenAddress(s.Namespace, n)
}

// Dump writes a diagnostic bundle for the suite namespace.
func (s *Suite) Dump(w io.Writer) {
	fmt.Fprintf(w, "\n===== diagnostics for %s (namespace %s) =====\n", s.Name, s.Namespace)
	if s.K8s == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var pods corev1.PodList
	if err := s.K8s.Client.List(ctx, &pods, client.InNamespace(s.Namespace)); err == nil {
		fmt.Fprintf(w, "\n-- pods --\n")
		for _, p := range pods.Items {
			restarts := int32(0)
			for _, cs := range p.Status.ContainerStatuses {
				restarts += cs.RestartCount
			}
			fmt.Fprintf(w, "  %-40s %-10s restarts=%d\n", p.Name, p.Status.Phase, restarts)
		}
	}

	if s.GnmiGen != nil {
		fmt.Fprintf(w, "\n-- gnmi-gen targets --\n")
		if targets, err := s.GnmiGen.Targets(); err == nil {
			for name, t := range targets {
				subs, _ := s.GnmiGen.Subscriptions(name)
				fmt.Fprintf(w, "  %-12s status=%-10s clients=%d paths=%d\n", name, t.Status, len(subs), t.PathCount)
				for _, sub := range subs {
					fmt.Fprintf(w, "      mode=%s interval=%s notifications=%d paths=%v\n",
						sub.Mode, sub.EffectiveSampleInterval, sub.NotificationsSent, sub.Paths)
				}
			}
		} else {
			fmt.Fprintf(w, "  unavailable: %v\n", err)
		}
	}
	fmt.Fprintf(w, "\nRe-run with KEEP_NS=1 to inspect the namespace live.\n")
	fmt.Fprintf(w, "===== end diagnostics =====\n\n")
}

// RequireGnmiGen skips a test when no simulator is present.
func (s *Suite) RequireGnmiGen(t *testing.T) {
	t.Helper()
	if s.GnmiGen == nil {
		t.Skip("test requires gnmi-gen")
	}
}

// namespaceFor derives a suite's namespace from its numeric prefix, so
// "003-targets" runs in gnmic-it-003 and suites never collide.
func namespaceFor(suiteName string) string {
	prefix := suiteName
	if i := strings.IndexByte(suiteName, '-'); i > 0 {
		prefix = suiteName[:i]
	}
	return "gnmic-it-" + prefix
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[harness] "+format+"\n", args...)
}
