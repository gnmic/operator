package gnmic

import (
	"testing"
	"time"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func rawJSON(s string) *apiextensionsv1.JSON {
	return &apiextensionsv1.JSON{Raw: []byte(s)}
}

func TestBuildTargetConfig(t *testing.T) {
	target := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "router1"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "default"},
	}
	profile := &gnmicv1alpha1.TargetProfileSpec{
		Encoding:   "JSON",
		Timeout:    metav1.Duration{Duration: 15 * time.Second},
		RetryTimer: metav1.Duration{Duration: 3 * time.Second},
	}

	cfg := buildTargetConfig(target, profile, &Credentials{Username: "u", Password: "p"}, nil)
	if cfg.Insecure == nil || !*cfg.Insecure {
		t.Fatal("expected insecure when TLS unset")
	}
	if cfg.Username == nil || *cfg.Username != "u" {
		t.Fatal("expected username")
	}

	profile.TLS = &gnmicv1alpha1.TargetTLSConfig{MaxVersion: "1.3", MinVersion: "1.2"}
	cfg = buildTargetConfig(target, profile, nil, nil)
	if cfg.SkipVerify == nil || !*cfg.SkipVerify {
		t.Fatal("expected skip verify with profile TLS only")
	}

	profileOnlyMax := &gnmicv1alpha1.TargetProfileSpec{
		Encoding: "JSON",
		TLS:      &gnmicv1alpha1.TargetTLSConfig{MaxVersion: "1.3"},
	}
	cfg = buildTargetConfig(target, profileOnlyMax, nil, nil)
	if cfg.TLSMaxVersion != "1.3" {
		t.Fatal("expected max version only")
	}

	clientTLS := &ClientTLSPaths{CertFile: "/cert", KeyFile: "/key", CAFile: "/ca"}
	cfg = buildTargetConfig(target, profile, nil, clientTLS)
	if cfg.TLSCert == nil || *cfg.TLSCert != "/cert" {
		t.Fatal("expected client cert paths")
	}
}

func TestDurationOrDefault(t *testing.T) {
	d := metav1.Duration{Duration: 5 * time.Second}
	if durationOrDefault(&d, time.Second) != 5*time.Second {
		t.Fatal("expected custom duration")
	}
	if durationOrDefault(nil, time.Second) != time.Second {
		t.Fatal("expected default duration")
	}
}

func TestBuildOutputConfig(t *testing.T) {
	spec := &gnmicv1alpha1.OutputSpec{
		Type:   PrometheusOutputType,
		Config: *rawJSON(`path: /custom`),
	}
	out, err := buildOutputConfig(spec, &outputConfigOptions{
		Processors:        []string{"proc1"},
		ResolvedAddresses: []string{"nats://svc:4222"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["type"] != PrometheusOutputType {
		t.Fatalf("type = %v", out["type"])
	}
	if out["path"] != "/custom" {
		t.Fatalf("path = %v", out["path"])
	}

	natsSpec := &gnmicv1alpha1.OutputSpec{Type: NATSOutputType}
	out, err = buildOutputConfig(natsSpec, &outputConfigOptions{ResolvedAddresses: []string{"nats://a:4222", "nats://b:4222"}})
	if err != nil {
		t.Fatal(err)
	}
	if out["address"] != "nats://a:4222,nats://b:4222" {
		t.Fatalf("address = %v", out["address"])
	}

	_, err = buildOutputConfig(&gnmicv1alpha1.OutputSpec{
		Type:   "file",
		Config: *rawJSON(":\ninvalid"),
	}, &outputConfigOptions{})
	if err == nil {
		t.Fatal("expected yaml error")
	}
}

func TestFormatServiceAddressAndParsePort(t *testing.T) {
	ports := []ServicePort{{Name: "grpc", Port: 4222}}

	if got := FormatServiceAddress(&gnmicv1alpha1.OutputSpec{Type: NATSOutputType}, "svc", 4222); got != "nats://svc:4222" {
		t.Fatalf("nats address = %q", got)
	}
	if got := FormatServiceAddress(&gnmicv1alpha1.OutputSpec{Type: KafkaOutputType}, "svc", 9092); got != "svc:9092" {
		t.Fatalf("kafka address = %q", got)
	}
	if got := FormatServiceAddress(&gnmicv1alpha1.OutputSpec{
		Type:   PrometheusWriteOutputType,
		Config: *rawJSON("tls: {}"),
	}, "svc", 9090); got != "https://svc:9090" {
		t.Fatalf("prom write tls address = %q", got)
	}

	p, err := ParseServicePort("", ports)
	if err != nil || p != 4222 {
		t.Fatalf("default port: %d, err=%v", p, err)
	}
	p, err = ParseServicePort("grpc", ports)
	if err != nil || p != 4222 {
		t.Fatalf("named port: %d, err=%v", p, err)
	}
	if _, err := ParseServicePort("missing", ports); err == nil {
		t.Fatal("expected error for unknown port")
	}
}

func TestBuildInputAndProcessorConfig(t *testing.T) {
	in, err := buildInputConfig(&gnmicv1alpha1.InputSpec{
		Type:   "kafka",
		Config: *rawJSON(`topic: events`),
	}, []string{"out1"}, []string{"proc1"})
	if err != nil {
		t.Fatal(err)
	}
	if in["outputs"].([]string)[0] != "out1" {
		t.Fatal("expected outputs")
	}

	proc, err := buildProcessorConfig(&gnmicv1alpha1.ProcessorSpec{
		Type:   "event-starlark",
		Config: *rawJSON(`script: "true"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if proc == nil {
		t.Fatal("expected processor config map")
	}
}

func TestConvert(t *testing.T) {
	in := map[any]any{"k": map[any]any{"nested": []any{"a"}}}
	out := convert(in).(map[string]any)
	if out["k"].(map[string]any)["nested"].([]any)[0] != "a" {
		t.Fatal("convert failed")
	}
}

func TestBuildSubscriptionConfig(t *testing.T) {
	spec := &gnmicv1alpha1.SubscriptionSpec{
		Prefix: "/interfaces",
		Paths:  []string{"/interfaces/interface"},
		Mode:   "STREAM/SAMPLE",
	}
	cfg := buildSubscriptionConfig("default/sub1", spec, []string{"default/out1"}, nil)
	if cfg.Mode != "STREAM" || cfg.StreamMode != "SAMPLE" {
		t.Fatalf("mode = %s/%s", cfg.Mode, cfg.StreamMode)
	}

	mode, stream := specModeToConfig("ONCE")
	if mode != "ONCE" || stream != "" {
		t.Fatalf("specModeToConfig = %s/%s", mode, stream)
	}
}

func TestBuildTunnelTargetMatch(t *testing.T) {
	policy := &gnmicv1alpha1.TunnelTargetPolicySpec{Profile: "default"}
	profile := &gnmicv1alpha1.TargetProfileSpec{Encoding: "JSON"}
	match := buildTunnelTargetMatch(policy, profile, &Credentials{Token: "t"}, nil)
	if match.Config == nil || match.Config.Insecure == nil || !*match.Config.Insecure {
		t.Fatalf("unexpected match: %+v", match)
	}
	if match.Config.Token == nil || *match.Config.Token != "t" {
		t.Fatal("expected token on tunnel target config")
	}
}
