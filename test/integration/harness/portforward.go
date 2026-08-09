//go:build integration

package harness

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Forward is an active port-forward into the cluster.
type Forward struct {
	LocalPort int
	URL       string

	stop chan struct{}
}

// Close tears the forward down.
func (f *Forward) Close() {
	select {
	case <-f.stop:
	default:
		close(f.stop)
	}
}

// ForwardPodPort forwards a container port on a specific pod.
//
// This uses client-go directly rather than shelling out to kubectl: no child
// process to leak, failures arrive as Go errors, and the forward closes
// deterministically when the suite ends.
func (k *K8s) ForwardPodPort(pod string, remotePort int) (*Forward, error) {
	transport, upgrader, err := spdy.RoundTripperFor(k.RestCfg)
	if err != nil {
		return nil, fmt.Errorf("building spdy transport: %w", err)
	}
	url := k.Clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Namespace(k.Namespace).
		Name(pod).
		SubResource("portforward").
		URL()

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, url)

	stop := make(chan struct{})
	ready := make(chan struct{})
	fw, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", remotePort)}, stop, ready, io.Discard, io.Discard)
	if err != nil {
		close(stop)
		return nil, fmt.Errorf("creating port forwarder: %w", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- fw.ForwardPorts() }()

	select {
	case <-ready:
	case err := <-errCh:
		return nil, fmt.Errorf("port forward to %s:%d failed: %w", pod, remotePort, err)
	case <-time.After(30 * time.Second):
		close(stop)
		return nil, fmt.Errorf("port forward to %s:%d timed out", pod, remotePort)
	}

	ports, err := fw.GetPorts()
	if err != nil || len(ports) == 0 {
		close(stop)
		return nil, fmt.Errorf("resolving forwarded port: %w", err)
	}
	local := int(ports[0].Local)
	return &Forward{
		LocalPort: local,
		URL:       fmt.Sprintf("http://127.0.0.1:%d", local),
		stop:      stop,
	}, nil
}

// ForwardService forwards a port on the first ready pod backing a Service.
//
// It targets a pod rather than the Service because port-forward is a pod-level
// operation; picking the pod here keeps callers from having to know that.
func (k *K8s) ForwardService(ctx context.Context, selector map[string]string, remotePort int) (*Forward, error) {
	pod, err := k.firstReadyPod(ctx, selector)
	if err != nil {
		return nil, err
	}
	return k.ForwardPodPort(pod, remotePort)
}

func (k *K8s) firstReadyPod(ctx context.Context, selector map[string]string) (string, error) {
	deadline := time.Now().Add(Medium)
	for {
		var pods corev1.PodList
		err := k.Client.List(ctx, &pods, client.InNamespace(k.Namespace), client.MatchingLabels(selector))
		if err == nil {
			for _, p := range pods.Items {
				if podReady(&p) {
					return p.Name, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("no ready pod matching %v in %s", selector, k.Namespace)
		}
		time.Sleep(time.Second)
	}
}

func podReady(p *corev1.Pod) bool {
	if p.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
