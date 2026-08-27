package controller

import (
	"context"
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/gnmic"
)

// promOutput builds a Prometheus Output with the given raw config (empty for none).
func promOutput(name, rawConfig string) *gnmicv1alpha1.Output {
	spec := gnmicv1alpha1.OutputSpec{Type: gnmic.PrometheusOutputType}
	if rawConfig != "" {
		spec.Config = apiextensionsv1.JSON{Raw: []byte(rawConfig)}
	}
	return &gnmicv1alpha1.Output{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       spec,
	}
}

func promPipelineCR(outputName string) *gnmicv1alpha1.Pipeline {
	return &gnmicv1alpha1.Pipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Spec: gnmicv1alpha1.PipelineSpec{
			ClusterRef:       "c1",
			Enabled:          true,
			TargetRefs:       []string{"t1"},
			SubscriptionRefs: []string{"s1"},
			Outputs:          gnmicv1alpha1.OutputSelector{OutputRefs: []string{outputName}},
		},
	}
}

func TestPrometheusServicePortMatchesCollectorListen(t *testing.T) {
	tests := []struct {
		name      string
		rawConfig string
		wantPort  int32 // 0 means "whatever the plan assigned"
	}{
		{
			// The case that used to drift: the collector was reconfigured onto a
			// hash-assigned port while the Service was built from this value.
			name:      "explicit listen",
			rawConfig: `{"listen": ":9999"}`,
			wantPort:  9999,
		},
		{
			name:      "explicit listen with host",
			rawConfig: `{"listen": "0.0.0.0:9123", "path": "/custom"}`,
			wantPort:  9123,
		},
		{
			name:      "no listen, port assigned from the pool",
			rawConfig: "",
			wantPort:  0,
		},
		{
			name:      "config without a listen key",
			rawConfig: `{"path": "/custom"}`,
			wantPort:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, cl, _ := reconcileCluster(t,
				readyCluster(),
				statefulSet(0),
				promPipelineCR("o1"),
				&gnmicv1alpha1.Target{
					ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
					Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "prof"},
				},
				&gnmicv1alpha1.TargetProfile{ObjectMeta: metav1.ObjectMeta{Name: "prof", Namespace: "default"}},
				&gnmicv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "default"}},
				promOutput("o1", tt.rawConfig),
			)

			plan, err := r.GetClusterPlan("default", "c1")
			if err != nil {
				t.Fatal(err)
			}
			const outputNN = "default/p1/o1"

			// What the collector is told to bind.
			listen, _ := plan.Outputs[outputNN]["listen"].(string)
			bound, err := gnmic.ParseListenPort(listen)
			if err != nil {
				t.Fatalf("unparseable listen %q: %v", listen, err)
			}
			if bound == 0 {
				t.Fatalf("no listen address in the plan: %v", plan.Outputs[outputNN])
			}
			if tt.wantPort != 0 && bound != tt.wantPort {
				t.Errorf("collector binds %d, want %d (listen=%q)", bound, tt.wantPort, listen)
			}

			// What the Service points at.
			var svc corev1.Service
			svcNN := types.NamespacedName{Name: "gnmic-c1-prom-p1-o1", Namespace: "default"}
			if err := cl.Get(context.Background(), svcNN, &svc); err != nil {
				t.Fatalf("prometheus service not created: %v", err)
			}
			if len(svc.Spec.Ports) != 1 {
				t.Fatalf("service ports = %+v", svc.Spec.Ports)
			}

			if svc.Spec.Ports[0].Port != bound {
				t.Errorf("service port %d but the collector binds %d", svc.Spec.Ports[0].Port, bound)
			}
			if got := svc.Spec.Ports[0].TargetPort.IntVal; got != bound {
				t.Errorf("service targetPort %d but the collector binds %d", got, bound)
			}
			if got := svc.Annotations["prometheus.io/port"]; got != strconv.Itoa(int(bound)) {
				t.Errorf("prometheus.io/port = %q, want %q", got, strconv.Itoa(int(bound)))
			}
			if got := plan.PrometheusPorts[outputNN]; got != bound {
				t.Errorf("PrometheusPorts = %d, want %d", got, bound)
			}
		})
	}
}

// The scrape path still comes from the CR, independently of the port.
func TestPrometheusServicePathAnnotation(t *testing.T) {
	for raw, want := range map[string]string{
		`{"listen": ":9999", "path": "/custom"}`: "/custom",
		`{"listen": ":9999"}`:                    gnmic.PrometheusDefaultPath,
		``:                                       gnmic.PrometheusDefaultPath,
	} {
		_, cl, _ := reconcileCluster(t,
			readyCluster(),
			statefulSet(0),
			promPipelineCR("o1"),
			&gnmicv1alpha1.Target{
				ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
				Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "prof"},
			},
			&gnmicv1alpha1.TargetProfile{ObjectMeta: metav1.ObjectMeta{Name: "prof", Namespace: "default"}},
			&gnmicv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "default"}},
			promOutput("o1", raw),
		)

		var svc corev1.Service
		svcNN := types.NamespacedName{Name: "gnmic-c1-prom-p1-o1", Namespace: "default"}
		if err := cl.Get(context.Background(), svcNN, &svc); err != nil {
			t.Fatalf("config %s: service not created: %v", raw, err)
		}
		if got := svc.Annotations["prometheus.io/path"]; got != want {
			t.Errorf("config %s: prometheus.io/path = %q, want %q", raw, got, want)
		}
	}
}
