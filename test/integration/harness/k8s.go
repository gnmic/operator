//go:build integration

package harness

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// FieldOwner identifies this suite's server-side apply writes.
const FieldOwner = client.FieldOwner("gnmic-integration-tests")

// K8s is the suite's handle on the cluster. Every helper is namespace-scoped to
// the suite namespace unless it says otherwise.
type K8s struct {
	Client    client.Client
	Clientset *kubernetes.Clientset
	RestCfg   *rest.Config
	Namespace string
	Ctx       context.Context
}

// Scheme returns the scheme used by the harness client: core Kubernetes, the
// operator's CRDs, and cert-manager.
func Scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	must(clientgoscheme.AddToScheme(s))
	must(gnmicv1alpha1.AddToScheme(s))
	must(certmanagerv1.AddToScheme(s))
	return s
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// NewK8s connects to the integration cluster.
//
// The context is pinned rather than inherited from whatever the shell happens
// to have selected: these tests create and delete namespaces, and pointing them
// at the wrong cluster is not a mistake worth allowing. IT_CONTEXT overrides.
func NewK8s(ctx context.Context, namespace string) (*K8s, error) {
	kubeContext := getenv("IT_CONTEXT", "kind-"+getenv("IT_CLUSTER_NAME", "gnmic-it"))

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules,
		&clientcmd.ConfigOverrides{CurrentContext: kubeContext},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("building client config for context %q: %w", kubeContext, err)
	}
	cfg.QPS, cfg.Burst = 50, 100

	c, err := client.New(cfg, client.Options{Scheme: Scheme()})
	if err != nil {
		return nil, fmt.Errorf("building client: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building clientset: %w", err)
	}
	return &K8s{Client: c, Clientset: cs, RestCfg: cfg, Namespace: namespace, Ctx: ctx}, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Apply server-side applies an object into the suite namespace and registers
// cleanup so the test does not have to.
func (k *K8s) Apply(t *testing.T, obj client.Object) {
	t.Helper()
	if obj.GetNamespace() == "" {
		obj.SetNamespace(k.Namespace)
	}
	if err := k.Client.Patch(k.Ctx, obj, client.Apply, FieldOwner, client.ForceOwnership); err != nil {
		t.Fatalf("applying %T %s: %v", obj, obj.GetName(), err)
	}
	t.Cleanup(func() { k.deleteQuietly(obj) })
}

// ApplyNoCleanup applies without registering cleanup, for suite baselines whose
// lifetime is the whole package rather than one test.
func (k *K8s) ApplyNoCleanup(obj client.Object) error {
	if obj.GetNamespace() == "" {
		obj.SetNamespace(k.Namespace)
	}
	return k.Client.Patch(k.Ctx, obj, client.Apply, FieldOwner, client.ForceOwnership)
}

// ApplyYAMLNoCleanup renders and applies YAML without registering cleanup.
func (k *K8s) ApplyYAMLNoCleanup(yaml string, vars map[string]any) ([]client.Object, error) {
	return k.applyYAML(yaml, vars)
}

// ApplyYAML renders a multi-document YAML string as a template, applies every
// document, and registers cleanup in reverse order.
func (k *K8s) ApplyYAML(t *testing.T, yaml string, vars map[string]any) []client.Object {
	t.Helper()
	objs, err := k.applyYAML(yaml, vars)
	if err != nil {
		t.Fatalf("applying YAML: %v", err)
	}
	t.Cleanup(func() {
		for i := len(objs) - 1; i >= 0; i-- {
			k.deleteQuietly(objs[i])
		}
	})
	return objs
}

// ApplyFile is ApplyYAML for a file on disk, relative to the suite directory.
func (k *K8s) ApplyFile(t *testing.T, path string, vars map[string]any) []client.Object {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	return k.ApplyYAML(t, string(b), vars)
}

func (k *K8s) applyYAML(yamlText string, vars map[string]any) ([]client.Object, error) {
	rendered, err := k.render(yamlText, vars)
	if err != nil {
		return nil, err
	}
	docs, err := splitYAML(rendered)
	if err != nil {
		return nil, err
	}
	var applied []client.Object
	for _, doc := range docs {
		u := &unstructured.Unstructured{Object: doc}
		if u.GetNamespace() == "" {
			u.SetNamespace(k.Namespace)
		}
		if err := k.Client.Patch(context.Background(), u, client.Apply, FieldOwner, client.ForceOwnership); err != nil {
			return applied, fmt.Errorf("applying %s/%s: %w", u.GetKind(), u.GetName(), err)
		}
		applied = append(applied, u)
	}
	return applied, nil
}

func splitYAML(text string) ([]map[string]any, error) {
	var out []map[string]any
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(text)), 4096)
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(doc) == 0 {
			continue
		}
		out = append(out, doc)
	}
	return out, nil
}

func (k *K8s) deleteQuietly(obj client.Object) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := k.Client.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		fmt.Fprintf(os.Stderr, "cleanup: deleting %s/%s: %v\n", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), err)
	}
}

// Delete removes an object and waits for it to actually disappear. Cleanup that
// does not verify removal poisons the next test.
func (k *K8s) Delete(t *testing.T, obj client.Object) {
	t.Helper()
	if err := k.Client.Delete(k.Ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("deleting %s: %v", obj.GetName(), err)
	}
	key := client.ObjectKeyFromObject(obj)
	Wait(t, Short, fmt.Sprintf("%s to be gone", obj.GetName()), func() (bool, string) {
		err := k.Client.Get(k.Ctx, key, obj)
		if apierrors.IsNotFound(err) {
			return true, ""
		}
		return false, "still present"
	})
}

// Patch applies a raw merge patch to a named object.
func (k *K8s) Patch(t *testing.T, obj client.Object, patch string) {
	t.Helper()
	if err := k.Client.Patch(k.Ctx, obj, client.RawPatch(types.MergePatchType, []byte(patch))); err != nil {
		t.Fatalf("patching %s: %v", obj.GetName(), err)
	}
}

// --- typed getters -------------------------------------------------------

func (k *K8s) get(t *testing.T, name string, obj client.Object) {
	t.Helper()
	if err := k.Client.Get(k.Ctx, types.NamespacedName{Namespace: k.Namespace, Name: name}, obj); err != nil {
		t.Fatalf("getting %T %s: %v", obj, name, err)
	}
}

func (k *K8s) Cluster(t *testing.T, name string) *gnmicv1alpha1.Cluster {
	t.Helper()
	o := &gnmicv1alpha1.Cluster{}
	k.get(t, name, o)
	return o
}

func (k *K8s) Pipeline(t *testing.T, name string) *gnmicv1alpha1.Pipeline {
	t.Helper()
	o := &gnmicv1alpha1.Pipeline{}
	k.get(t, name, o)
	return o
}

func (k *K8s) Target(t *testing.T, name string) *gnmicv1alpha1.Target {
	t.Helper()
	o := &gnmicv1alpha1.Target{}
	k.get(t, name, o)
	return o
}

// WaitExists blocks until a named object exists, then leaves it in obj.
func (k *K8s) WaitExists(t *testing.T, name string, obj client.Object) {
	t.Helper()
	kind := obj.GetObjectKind().GroupVersionKind().Kind
	if kind == "" {
		kind = fmt.Sprintf("%T", obj)
	}
	Wait(t, Medium, fmt.Sprintf("%s %s to exist", kind, name), func() (bool, string) {
		err := k.Client.Get(k.Ctx, types.NamespacedName{Namespace: k.Namespace, Name: name}, obj)
		if err == nil {
			return true, ""
		}
		if apierrors.IsNotFound(err) {
			return false, "not created yet"
		}
		return false, err.Error()
	})
}

// WaitAbsent blocks until a named object is gone. Garbage-collection
// assertions need this as much as creation assertions need WaitExists.
func (k *K8s) WaitAbsent(t *testing.T, name string, obj client.Object) {
	t.Helper()
	Wait(t, Medium, fmt.Sprintf("%s to be deleted", name), func() (bool, string) {
		err := k.Client.Get(k.Ctx, types.NamespacedName{Namespace: k.Namespace, Name: name}, obj)
		if apierrors.IsNotFound(err) {
			return true, ""
		}
		if err != nil {
			return false, err.Error()
		}
		return false, "still present"
	})
}

// clusterQuiet and targetQuiet return an error instead of failing the test, so
// polling helpers can retry a not-found or a stale cache.
func (k *K8s) clusterQuiet(name string) (*gnmicv1alpha1.Cluster, error) {
	o := &gnmicv1alpha1.Cluster{}
	err := k.Client.Get(k.Ctx, types.NamespacedName{Namespace: k.Namespace, Name: name}, o)
	return o, err
}

func (k *K8s) targetQuiet(name string) (*gnmicv1alpha1.Target, error) {
	o := &gnmicv1alpha1.Target{}
	err := k.Client.Get(k.Ctx, types.NamespacedName{Namespace: k.Namespace, Name: name}, o)
	return o, err
}

// TargetQuiet is the non-fatal form of Target, for polling helpers.
func (k *K8s) TargetQuiet(name string) (*gnmicv1alpha1.Target, error) {
	return k.targetQuiet(name)
}

func (k *K8s) pipelineQuiet(name string) (*gnmicv1alpha1.Pipeline, error) {
	o := &gnmicv1alpha1.Pipeline{}
	err := k.Client.Get(k.Ctx, types.NamespacedName{Namespace: k.Namespace, Name: name}, o)
	return o, err
}

// DeleteNow deletes without waiting for absence. Use for objects the operator
// or a controller will recreate immediately (e.g. a StatefulSet pod).
func (k *K8s) DeleteNow(t *testing.T, obj client.Object) {
	t.Helper()
	if obj.GetNamespace() == "" {
		obj.SetNamespace(k.Namespace)
	}
	if err := k.Client.Delete(k.Ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("deleting %s: %v", obj.GetName(), err)
	}
}

func (k *K8s) StatefulSet(t *testing.T, name string) *appsv1.StatefulSet {
	t.Helper()
	o := &appsv1.StatefulSet{}
	k.get(t, name, o)
	return o
}

func (k *K8s) Service(t *testing.T, name string) *corev1.Service {
	t.Helper()
	o := &corev1.Service{}
	k.get(t, name, o)
	return o
}

func (k *K8s) ConfigMap(t *testing.T, name string) *corev1.ConfigMap {
	t.Helper()
	o := &corev1.ConfigMap{}
	k.get(t, name, o)
	return o
}

// --- pods ----------------------------------------------------------------

// Pods lists pods in the suite namespace matching labels.
func (k *K8s) Pods(t *testing.T, labels map[string]string) []corev1.Pod {
	t.Helper()
	var list corev1.PodList
	if err := k.Client.List(k.Ctx, &list, client.InNamespace(k.Namespace), client.MatchingLabels(labels)); err != nil {
		t.Fatalf("listing pods: %v", err)
	}
	return list.Items
}

// ClusterPods lists the collector pods of a gNMIc cluster.
func (k *K8s) ClusterPods(t *testing.T, cluster string) []corev1.Pod {
	t.Helper()
	return k.Pods(t, map[string]string{LabelClusterName: cluster})
}

// WaitReadyPods blocks until a cluster has exactly want Ready collector pods.
// Exactly, not at least: scale-down is only proven by the surplus going away.
func (k *K8s) WaitReadyPods(t *testing.T, cluster string, want int, timeout time.Duration) []corev1.Pod {
	t.Helper()
	var ready []corev1.Pod
	Wait(t, timeout, fmt.Sprintf("%d ready pod(s) in cluster %s", want, cluster), func() (bool, string) {
		var list corev1.PodList
		err := k.Client.List(k.Ctx, &list,
			client.InNamespace(k.Namespace),
			client.MatchingLabels{LabelClusterName: cluster})
		if err != nil {
			return false, err.Error()
		}
		ready = nil
		for i := range list.Items {
			if podReady(&list.Items[i]) {
				ready = append(ready, list.Items[i])
			}
		}
		return len(ready) == want, fmt.Sprintf("%d ready of %d total", len(ready), len(list.Items))
	})
	return ready
}

// WaitPodGone blocks until a named pod no longer exists.
func (k *K8s) WaitPodGone(t *testing.T, name string) {
	t.Helper()
	k.WaitAbsent(t, name, &corev1.Pod{})
}

// RestartCounts snapshots container restart counts, so a test can prove a
// config change was applied dynamically rather than by restarting pods.
func (k *K8s) RestartCounts(t *testing.T, cluster string) map[string]int32 {
	t.Helper()
	out := map[string]int32{}
	for _, p := range k.ClusterPods(t, cluster) {
		var total int32
		for _, cs := range p.Status.ContainerStatuses {
			total += cs.RestartCount
		}
		out[p.Name] = total
	}
	return out
}

// AssertNoRestarts fails if any pod present in both snapshots restarted.
func AssertNoRestarts(t *testing.T, before, after map[string]int32) {
	t.Helper()
	for pod, b := range before {
		a, ok := after[pod]
		if !ok {
			continue // pod replaced; scaling tests assert on that separately
		}
		if a != b {
			t.Fatalf("pod %s restarted during the change: %d -> %d", pod, b, a)
		}
	}
}

// Logs returns recent logs for a pod.
func (k *K8s) Logs(t *testing.T, pod string, since time.Duration) string {
	t.Helper()
	sec := int64(since.Seconds())
	req := k.Clientset.CoreV1().Pods(k.Namespace).GetLogs(pod, &corev1.PodLogOptions{SinceSeconds: &sec})
	rc, err := req.Stream(k.Ctx)
	if err != nil {
		return fmt.Sprintf("<logs unavailable: %v>", err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	return string(b)
}

// AssertNoPanics fails if a log body contains a Go panic.
func AssertNoPanics(t *testing.T, logs string) {
	t.Helper()
	if strings.Contains(logs, "panic: ") || strings.Contains(logs, "runtime error:") {
		t.Fatalf("panic found in logs:\n%s", logs)
	}
}

// --- namespaces ----------------------------------------------------------

func (k *K8s) createNamespace(ctx context.Context) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   k.Namespace,
		Labels: map[string]string{"gnmic.dev/integration-suite": "true"},
	}}
	// A previous run may have left the namespace terminating.
	deadline := time.Now().Add(2 * time.Minute)
	for {
		err := k.Client.Create(ctx, ns)
		if err == nil {
			return nil
		}
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		var existing corev1.Namespace
		if getErr := k.Client.Get(ctx, types.NamespacedName{Name: k.Namespace}, &existing); getErr == nil &&
			existing.Status.Phase != corev1.NamespaceTerminating {
			return nil // usable as-is, e.g. a KEEP_NS re-run
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("namespace %s still terminating after 2m", k.Namespace)
		}
		time.Sleep(2 * time.Second)
	}
}

func (k *K8s) deleteNamespace(ctx context.Context) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: k.Namespace}}
	if err := k.Client.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
