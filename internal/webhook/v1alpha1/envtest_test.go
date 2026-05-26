package v1alpha1

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	operatorv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

func webhookEnvtestAssetsDir() string {
	candidates := []string{
		filepath.Join("..", "..", "..", "bin", "k8s",
			fmt.Sprintf("1.28.3-%s-%s", runtime.GOOS, runtime.GOARCH)),
		filepath.Join(os.Getenv("HOME"), ".local", "share", "kubebuilder-envtest", "k8s",
			fmt.Sprintf("1.28.3-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}
	if fromEnv := os.Getenv("KUBEBUILDER_ASSETS"); fromEnv != "" {
		candidates = append([]string{fromEnv}, candidates...)
	}
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "etcd")); err == nil {
			return dir
		}
	}
	return ""
}

func TestSetupWebhooksWithManager(t *testing.T) {
	assets := webhookEnvtestAssetsDir()
	if assets == "" {
		t.Skip("envtest binaries not installed")
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: false,
		BinaryAssetsDirectory: assets,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	defer func() { _ = testEnv.Stop() }()

	if err := operatorv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:         scheme.Scheme,
		LeaderElection: false,
		Metrics:        metricsserver.Options{BindAddress: "0"},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    9443,
			Host:    "127.0.0.1",
			CertDir: t.TempDir(),
		}),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	setups := []struct {
		name string
		fn   func(ctrl.Manager) error
	}{
		{"cluster", SetupClusterWebhookWithManager},
		{"pipeline", SetupPipelineWebhookWithManager},
		{"target", SetupTargetWebhookWithManager},
		{"subscription", SetupSubscriptionWebhookWithManager},
		{"output", SetupOutputWebhookWithManager},
		{"input", SetupInputWebhookWithManager},
		{"processor", SetupProcessorWebhookWithManager},
		{"targetprofile", SetupTargetProfileWebhookWithManager},
		{"tunneltargetpolicy", SetupTunnelTargetPolicyWebhookWithManager},
		{"targetsource", SetupTargetSourceWebhookWithManager},
	}
	for _, s := range setups {
		t.Run(s.name, func(t *testing.T) {
			if err := s.fn(mgr); err != nil {
				t.Fatal(err)
			}
		})
	}
}
