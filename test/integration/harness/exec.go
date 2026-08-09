//go:build integration

package harness

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Exec runs a command inside a pod container and returns its stdout.
//
// Used to read what a collector actually has on disk, as opposed to what the
// ConfigMap says it should have.
func (k *K8s) Exec(t *testing.T, pod, container string, command ...string) string {
	t.Helper()
	req := k.Clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(pod).
		Namespace(k.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(k.RestCfg, http.MethodPost, req.URL())
	if err != nil {
		t.Fatalf("building executor for %s: %v", pod, err)
	}
	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(k.Ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatalf("exec %v in %s: %v (stderr: %s)", command, pod, err, stderr.String())
	}
	return stdout.String()
}

// CollectorAPI opens a port-forward to a collector pod's REST API and returns a
// GET function against it. The forward closes when the test ends.
func (k *K8s) CollectorAPI(t *testing.T, pod string, restPort int) func(path string) (int, string) {
	t.Helper()
	fw, err := k.ForwardPodPort(pod, restPort)
	if err != nil {
		t.Fatalf("port-forwarding %s:%d: %v", pod, restPort, err)
	}
	t.Cleanup(fw.Close)

	return func(path string) (int, string) {
		resp, err := http.Get(fw.URL + path)
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
}

// AssertOwnedBy checks that an object carries an owner reference to the named
// owner. Garbage collection depends on this, and a missing owner reference
// otherwise only surfaces much later as objects left behind.
func AssertOwnedBy(t *testing.T, obj client.Object, ownerKind, ownerName string) {
	t.Helper()
	refs := obj.GetOwnerReferences()
	for _, ref := range refs {
		if ref.Kind == ownerKind && ref.Name == ownerName {
			return
		}
	}
	var have []string
	for _, r := range refs {
		have = append(have, fmt.Sprintf("%s/%s", r.Kind, r.Name))
	}
	if len(have) == 0 {
		have = []string{"<none>"}
	}
	t.Fatalf("%s has no owner reference to %s/%s; owners are %v", obj.GetName(), ownerKind, ownerName, have)
}
