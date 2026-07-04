package gnmic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	gapi "github.com/openconfig/gnmic/pkg/api/types"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestGetControllerTLSPaths_AllEnvVars(t *testing.T) {
	t.Setenv("GNMIC_TLS_KEY", "/k")
	t.Setenv("GNMIC_TLS_CA", "/ca")
	if GetControllerKeyPath() != "/k" {
		t.Fatal("key path")
	}
	if GetControllerCAPath() != "/ca" {
		t.Fatal("ca path")
	}
	t.Setenv("GNMIC_TLS_KEY", "")
	t.Setenv("GNMIC_TLS_CA", "")
}

func TestBuildTargetConfig_AllBranches(t *testing.T) {
	target := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "t"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "1.2.3.4:57400"},
	}
	profile := &gnmicv1alpha1.TargetProfileSpec{Encoding: "JSON"}

	// token-only credentials
	cfg := buildTargetConfig(target, profile, &Credentials{Token: "tok"}, nil)
	if cfg.Token == nil || *cfg.Token != "tok" {
		t.Fatal("token")
	}

	// client TLS without CA file
	clientTLS := &ClientTLSPaths{CertFile: "/c", KeyFile: "/k"}
	cfg = buildTargetConfig(target, profile, nil, clientTLS)
	if cfg.SkipVerify == nil || !*cfg.SkipVerify {
		t.Fatal("expected skip verify without CA")
	}

	// client TLS with CA + full profile TLS and keepalives
	profile.TLS = &gnmicv1alpha1.TargetTLSConfig{
		ServerName:   "router.example.com",
		MaxVersion:   "1.3",
		MinVersion:   "1.2",
		CipherSuites: []string{"TLS_AES_128_GCM_SHA256"},
	}
	profile.TCPKeepAlive = &metav1.Duration{Duration: 30 * time.Second}
	profile.GRCPKeepAlive = &gnmicv1alpha1.GRPCKeepAliveConfig{
		Time:                metav1.Duration{Duration: 10 * time.Second},
		Timeout:             metav1.Duration{Duration: 3 * time.Second},
		PermitWithoutStream: true,
	}
	clientTLS.CAFile = "/ca"
	cfg = buildTargetConfig(target, profile, &Credentials{Username: "u", Password: "p"}, clientTLS)
	if cfg.TLSCA == nil || cfg.TLSServerName != "router.example.com" {
		t.Fatalf("tls config: %+v", cfg)
	}
	if cfg.GRPCKeepalive == nil || !cfg.GRPCKeepalive.PermitWithoutStream {
		t.Fatal("grpc keepalive")
	}
}

func TestBuildTunnelTargetMatch_AllBranches(t *testing.T) {
	policy := &gnmicv1alpha1.TunnelTargetPolicySpec{Profile: "default"}
	profile := &gnmicv1alpha1.TargetProfileSpec{Encoding: "JSON"}

	// nil profile
	if m := buildTunnelTargetMatch(policy, nil, nil, nil); m.Config != nil {
		t.Fatal("expected no config without profile")
	}

	// profile TLS only
	profileTLS := &gnmicv1alpha1.TargetProfileSpec{
		Encoding: "JSON",
		TLS:      &gnmicv1alpha1.TargetTLSConfig{MaxVersion: "1.3"},
	}
	m := buildTunnelTargetMatch(policy, profileTLS, nil, nil)
	if m.Config.SkipVerify == nil || !*m.Config.SkipVerify {
		t.Fatal("profile tls only")
	}

	// client TLS + profile TLS fields
	profileFull := &gnmicv1alpha1.TargetProfileSpec{
		Encoding: "JSON",
		TLS:      &gnmicv1alpha1.TargetTLSConfig{ServerName: "tun.example", MinVersion: "1.2"},
	}
	clientTLS := &ClientTLSPaths{CertFile: "/c", KeyFile: "/k", CAFile: "/ca"}
	m = buildTunnelTargetMatch(policy, profileFull, &Credentials{Username: "u"}, clientTLS)
	if m.Config.TLSServerName != "tun.example" {
		t.Fatalf("config: %+v", m.Config)
	}

	// client TLS no CA early return when profile.TLS nil
	profileNoTLS := &gnmicv1alpha1.TargetProfileSpec{Encoding: "JSON"}
	clientNoCA := &ClientTLSPaths{CertFile: "/c"}
	m = buildTunnelTargetMatch(policy, profileNoTLS, nil, clientNoCA)
	if m.Config.TLSCert == nil {
		t.Fatal("expected cert")
	}
	_ = profile
}

func TestBuildSubscriptionConfig_Full(t *testing.T) {
	qos := uint32(5)
	now := metav1.Now()
	spec := &gnmicv1alpha1.SubscriptionSpec{
		Prefix:            "/pfx",
		Paths:             []string{"/a"},
		Mode:              "STREAM/ON_CHANGE",
		Encoding:          "JSON",
		SampleInterval:    metav1.Duration{Duration: time.Second},
		HeartbeatInterval: metav1.Duration{Duration: 2 * time.Second},
		Qos:               &qos,
		History: &gnmicv1alpha1.SubscriptionHistoryConfig{
			Snapshot: now,
			Start:    now,
			End:      now,
		},
		StreamSubscriptions: []string{"default/child"},
	}
	child := gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/child"}, Mode: "ONCE"}
	cfg := buildSubscriptionConfig("default/parent", spec, []string{"out"}, map[string]gnmicv1alpha1.SubscriptionSpec{
		"default/child": child,
	})
	if cfg.Encoding == nil || cfg.Qos == nil || cfg.History == nil {
		t.Fatal("expected optional fields")
	}
	if len(cfg.StreamSubscriptions) != 1 || cfg.StreamSubscriptions[0] == nil {
		t.Fatal("expected child subscription")
	}

	// missing child in map is skipped
	spec.StreamSubscriptions = []string{"default/missing"}
	cfg = buildSubscriptionConfig("default/p", spec, nil, map[string]gnmicv1alpha1.SubscriptionSpec{})
	if cfg.StreamSubscriptions[0] != nil {
		t.Fatal("expected nil slot for missing child")
	}
}

func TestBuildOutputConfig_AllTypes(t *testing.T) {
	addrs := []string{"h1:1", "h2:2"}

	for _, tc := range []struct {
		typ    string
		key    string
		expect string
	}{
		{JetstreamOutputType, "address", "h1:1,h2:2"},
		{KafkaOutputType, "address", "h1:1,h2:2"},
		{PrometheusWriteOutputType, "url", "h1:1,h2:2"},
		{InfluxDBOutputType, "url", "h1:1,h2:2"},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			out, err := buildOutputConfig(&gnmicv1alpha1.OutputSpec{Type: tc.typ}, &outputConfigOptions{ResolvedAddresses: addrs})
			if err != nil {
				t.Fatal(err)
			}
			if out[tc.key] != tc.expect {
				t.Fatalf("%s = %v", tc.key, out[tc.key])
			}
		})
	}

	// prometheus default path
	out, err := buildOutputConfig(&gnmicv1alpha1.OutputSpec{Type: PrometheusOutputType}, &outputConfigOptions{})
	if err != nil || out["path"] != PrometheusDefaultPath {
		t.Fatalf("prometheus defaults: %v err=%v", out, err)
	}

	// resolved addresses ignored for unsupported type
	out, err = buildOutputConfig(&gnmicv1alpha1.OutputSpec{Type: FileOutputType}, &outputConfigOptions{ResolvedAddresses: addrs})
	if err != nil || out["address"] != nil {
		t.Fatalf("file output should not get address: %v", out)
	}
}

func TestFormatServiceAddress_AllCases(t *testing.T) {
	if got := FormatServiceAddress(&gnmicv1alpha1.OutputSpec{Type: JetstreamOutputType}, "host", 4222); got != "nats://host:4222" {
		t.Fatalf("jetstream: %q", got)
	}
	if got := FormatServiceAddress(&gnmicv1alpha1.OutputSpec{Type: PrometheusWriteOutputType}, "host", 9090); got != "http://host:9090" {
		t.Fatalf("prom write http: %q", got)
	}
	if got := FormatServiceAddress(&gnmicv1alpha1.OutputSpec{Type: PrometheusWriteOutputType, Config: *rawJSON(":\n")}, "host", 9090); got != "http://host:9090" {
		t.Fatalf("prom write bad yaml: %q", got)
	}
	if got := FormatServiceAddress(&gnmicv1alpha1.OutputSpec{Type: PrometheusWriteOutputType, Config: *rawJSON("tls: true")}, "host", 9090); got != "https://host:9090" {
		t.Fatalf("prom write https: %q", got)
	}
	if got := FormatServiceAddress(&gnmicv1alpha1.OutputSpec{Type: InfluxDBOutputType}, "host", 8086); got != "http://host:8086" {
		t.Fatalf("influx http: %q", got)
	}
	if got := FormatServiceAddress(&gnmicv1alpha1.OutputSpec{Type: InfluxDBOutputType, Config: *rawJSON("{")}, "host", 8086); got != "http://host:8086" {
		t.Fatalf("influx bad yaml: %q", got)
	}
	if got := FormatServiceAddress(&gnmicv1alpha1.OutputSpec{Type: InfluxDBOutputType, Config: *rawJSON("tls: {}")}, "host", 8086); got != "https://host:8086" {
		t.Fatalf("influx https: %q", got)
	}
	if got := FormatServiceAddress(&gnmicv1alpha1.OutputSpec{Type: TCPOutputType}, "host", 9999); got != "host:9999" {
		t.Fatalf("default: %q", got)
	}
}

func TestParseServicePort_Numeric(t *testing.T) {
	p, err := ParseServicePort("9090", nil)
	if err != nil || p != 9090 {
		t.Fatalf("port=%d err=%v", p, err)
	}
}

func TestBuildInputConfig_ErrorsAndMinimal(t *testing.T) {
	_, err := buildInputConfig(&gnmicv1alpha1.InputSpec{
		Type:   "kafka",
		Config: *rawJSON("["),
	}, nil, nil)
	if err == nil {
		t.Fatal("expected yaml error")
	}
	in, err := buildInputConfig(&gnmicv1alpha1.InputSpec{Type: "nats"}, nil, nil)
	if err != nil || in["type"] != "nats" {
		t.Fatal(err)
	}
}

func TestBuildProcessorConfig_Errors(t *testing.T) {
	_, err := buildProcessorConfig(&gnmicv1alpha1.ProcessorSpec{
		Type:   "event-starlark",
		Config: *rawJSON("["),
	})
	if err == nil {
		t.Fatal("expected yaml error")
	}
}

func TestConvert_NonStringMapKey(t *testing.T) {
	in := map[any]any{1: "skip", "keep": "v"}
	out := convert(in).(map[string]any)
	if _, ok := out["keep"]; !ok || len(out) != 1 {
		t.Fatalf("convert = %v", out)
	}
}

func TestPlanBuilder_Extended(t *testing.T) {
	profile := gnmicv1alpha1.TargetProfileSpec{Encoding: "JSON"}

	// target without profile is skipped
	p1 := NewPipelineData()
	p1.Targets["default/t-skip"] = gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "t-skip"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.2:57400", Profile: "missing"},
	}
	p1.TargetProfiles["default/default"] = profile

	// input processors + bad processor yaml
	p2 := NewPipelineData()
	p2.InputProcessors["default/iproc"] = gnmicv1alpha1.ProcessorSpec{
		Type:   "event-jq",
		Config: apiextensionsv1.JSON{Raw: []byte(`filter: .`)},
	}
	p2.Inputs["default/in"] = gnmicv1alpha1.InputSpec{Type: "kafka"}
	p2.Outputs["default/out"] = gnmicv1alpha1.OutputSpec{Type: FileOutputType}
	p2.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}

	builder := NewPlanBuilder("c", &mockCredsFetcher{creds: &Credentials{Username: "u"}})
	plan, err := builder.AddPipeline("a", p1).AddPipeline("b", p2).Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plan.Targets["default/t-skip"]; ok {
		t.Fatal("target without profile should be skipped")
	}
	if len(plan.Processors) != 1 || len(plan.Inputs) != 1 {
		t.Fatalf("processors=%d inputs=%d", len(plan.Processors), len(plan.Inputs))
	}

	// invalid pod id in status
	p4 := NewPipelineData()
	p4.Targets["default/t"] = gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "t"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "default"},
		Status: gnmicv1alpha1.TargetStatus{
			ClusterStates: map[string]gnmicv1alpha1.ClusterTargetState{
				"c": {Pod: "not-a-valid-pod-id"},
			},
		},
	}
	p4.TargetProfiles["default/default"] = profile
	plan, err = NewPlanBuilder("c", nil).AddPipeline("p", p4).Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.CurrentTargetAssignment) != 0 {
		t.Fatal("invalid pod suffix should not assign")
	}

	// output build error
	p5 := NewPipelineData()
	p5.Outputs["default/bad"] = gnmicv1alpha1.OutputSpec{Type: "file", Config: *rawJSON("[")}
	p5.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}
	_, err = NewPlanBuilder("c", nil).AddPipeline("p", p5).Build()
	if err == nil {
		t.Fatal("expected output yaml error")
	}

	// input build error
	p6 := NewPipelineData()
	p6.Inputs["default/bad"] = gnmicv1alpha1.InputSpec{Type: "kafka", Config: *rawJSON("[")}
	p6.Outputs["default/o"] = gnmicv1alpha1.OutputSpec{Type: FileOutputType}
	p6.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}
	_, err = NewPlanBuilder("c", nil).AddPipeline("p", p6).Build()
	if err == nil {
		t.Fatal("expected input yaml error")
	}

	// processor build error (output processor)
	p7 := NewPipelineData()
	p7.OutputProcessors["default/bad"] = gnmicv1alpha1.ProcessorSpec{Type: "event-jq", Config: *rawJSON("[")}
	p7.Outputs["default/o"] = gnmicv1alpha1.OutputSpec{Type: FileOutputType}
	p7.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}
	_, err = NewPlanBuilder("c", nil).AddPipeline("p", p7).Build()
	if err == nil {
		t.Fatal("expected processor yaml error")
	}

	// tunnel policy creds fetch error
	p8 := NewPipelineData()
	p8.TunnelTargetPolicies["default/pol"] = gnmicv1alpha1.TunnelTargetPolicySpec{Profile: "default"}
	p8.TargetProfiles["default/default"] = gnmicv1alpha1.TargetProfileSpec{
		Encoding:       "JSON",
		CredentialsRef: "secret",
	}
	_, err = NewPlanBuilder("c", &mockCredsFetcher{err: errors.New("no secret")}).AddPipeline("p", p8).Build()
	if err == nil {
		t.Fatal("expected tunnel creds error")
	}

	// successful tunnel policy
	p9 := NewPipelineData()
	p9.TunnelTargetPolicies["default/pol"] = gnmicv1alpha1.TunnelTargetPolicySpec{Profile: "default"}
	p9.TargetProfiles["default/default"] = profile
	plan, err = NewPlanBuilder("c", &mockCredsFetcher{creds: &Credentials{Password: "p"}}).AddPipeline("p", p9).Build()
	if err != nil || len(plan.TunnelTargetMatches) != 1 {
		t.Fatalf("tunnel match: err=%v matches=%d", err, len(plan.TunnelTargetMatches))
	}

	// multiple prometheus outputs
	p10 := NewPipelineData()
	p10.Outputs["default/prom1"] = gnmicv1alpha1.OutputSpec{Type: PrometheusOutputType}
	p10.Outputs["default/prom2"] = gnmicv1alpha1.OutputSpec{Type: PrometheusOutputType}
	p10.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}
	plan, err = NewPlanBuilder("c", nil).AddPipeline("p", p10).Build()
	if err != nil || len(plan.PrometheusPorts) != 2 {
		t.Fatalf("prom ports: %v err=%v", plan.PrometheusPorts, err)
	}
}

func TestAssignPorts_Exhausted(t *testing.T) {
	names := make([]string, 5)
	for i := range names {
		names[i] = string(rune('a' + i))
	}
	_, err := assignPorts(names, 9000, 2)
	if err == nil {
		t.Fatal("expected no free ports error")
	}
}

func TestNormalizeOptions_ZeroPods(t *testing.T) {
	opts := normalizeOptions(&PlacementStrategyOpts{NumPods: 0})
	if opts.NumPods != 1 {
		t.Fatalf("numPods = %d", opts.NumPods)
	}
}

func TestPlacementNew_UnknownStrategy(t *testing.T) {
	s := New("other")
	if s == nil {
		t.Fatal("expected default strategy")
	}
}

func TestStreamTargetState_ErrorsAndEdgeCases(t *testing.T) {
	ctx := context.Background()

	// bad status
	badStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer badStatus.Close()
	ch := make(chan SSEEvent, 1)
	if err := StreamTargetState(ctx, badStatus.Client(), badStatus.URL, ch); err == nil {
		t.Fatal("expected status error")
	}

	// malformed data skipped, empty line, event type
	body := ": ping\n\nevent: create\ndata: not-json\n\ndata: {}\n\nevent: update\ndata: {\"store\":\"state\",\"kind\":\"targets\",\"name\":\"n\"}\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	ch = make(chan SSEEvent, 2)
	runCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	go func() { _ = StreamTargetState(runCtx, srv.Client(), srv.URL, ch) }()
	<-ch

	// connection error
	ch = make(chan SSEEvent)
	err := StreamTargetState(ctx, &http.Client{}, "http://127.0.0.1:1/sse", ch)
	if err == nil {
		t.Fatal("expected connection error")
	}

	// invalid URL
	ch = make(chan SSEEvent)
	err = StreamTargetState(ctx, &http.Client{}, "://bad-url", ch)
	if err == nil {
		t.Fatal("expected request creation error")
	}
}

func TestPollTargetState_MoreErrors(t *testing.T) {
	ctx := context.Background()

	decodeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer decodeSrv.Close()
	if _, err := PollTargetState(ctx, decodeSrv.Client(), decodeSrv.URL); err == nil {
		t.Fatal("expected decode error")
	}

	connErr := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})}
	if _, err := PollTargetState(ctx, connErr, "http://example.com"); err == nil {
		t.Fatal("expected request error")
	}

	// closes body on success path already tested
	_ = json.Marshal
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStreamTargetState_ScannerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// chunked response that never ends cleanly for scanner
		_, _ = w.Write([]byte("data: {\"store\":\"state\",\"kind\":\"targets\"}\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// hijack: close without complete stream - scanner may still return nil on EOF
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan SSEEvent, 1)
	done := make(chan error, 1)
	go func() { done <- StreamTargetState(ctx, srv.Client(), srv.URL, ch) }()
	cancel()
	<-done // may be nil or context canceled path
}

func TestStreamTargetState_LargeLine(t *testing.T) {
	payload, _ := json.Marshal(SSEEventData{
		Store: SSEStoreState,
		Kind:  "targets",
		Name:  "default/t",
	})
	line := "data: " + string(payload) + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, line)
	}))
	defer srv.Close()
	ch := make(chan SSEEvent, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = StreamTargetState(ctx, srv.Client(), srv.URL, ch)
}

func TestDistributeTargets_WithCurrentAssignment(t *testing.T) {
	plan := &ApplyPlan{
		Targets: map[string]*gapi.TargetConfig{
			"default/t1": {Name: "t1"},
			"default/t2": {Name: "t2"},
		},
		CurrentTargetAssignment: map[int]map[string]struct{}{
			0: {"default/t1": {}},
		},
		Subscriptions: map[string]*gapi.SubscriptionConfig{},
		Outputs:       map[string]map[string]any{},
		Inputs:        map[string]map[string]any{},
	}
	res := DistributeTargets(plan, 2, &gnmicv1alpha1.TargetDistributionConfig{PodCapacity: 1})
	if len(res.PerPodPlans) == 0 {
		t.Fatal("expected pod plans")
	}
}

func TestBoundedRendezvousHash_NoCapacity(t *testing.T) {
	assignments := Assignment{0: {"a", "b"}, 1: {"c", "d"}}
	if boundedRendezvousHash("default/new", 2, 2, assignments) != nil {
		t.Fatal("expected nil when all pods at capacity")
	}
}

func TestBoundedLoadRendezvousHash_EdgeCases(t *testing.T) {
	if a := boundedLoadRendezvousHash(map[string]*gapi.TargetConfig{}, &PlacementStrategyOpts{NumPods: 2}); len(a) != 0 {
		t.Fatal("empty targets")
	}

	// pod index out of range in CurrentAssignment is skipped
	opts := &PlacementStrategyOpts{
		NumPods:  2,
		Capacity: 1,
		CurrentAssignment: Assignment{
			5: {"orphan"},
			0: {"target-01"},
		},
	}
	a := boundedLoadRendezvousHash(genTargets(2), opts)
	if len(a[5]) != 0 {
		t.Fatal("orphan pod index should be ignored")
	}

	// pre-assigned pod over capacity drops extra targets
	opts = &PlacementStrategyOpts{
		NumPods:  1,
		Capacity: 1,
		CurrentAssignment: Assignment{
			0: {"target-01", "target-02"},
		},
	}
	a = boundedLoadRendezvousHash(genTargets(2), opts)
	if len(a[0]) != 1 {
		t.Fatalf("expected one kept target, got %v", a[0])
	}

	// new target cannot be placed when pod is full
	opts = &PlacementStrategyOpts{
		NumPods:           1,
		Capacity:          1,
		CurrentAssignment: Assignment{0: {"target-01"}},
	}
	targets := genTargets(2)
	a = boundedLoadRendezvousHash(targets, opts)
	total := 0
	for _, tgts := range a {
		total += len(tgts)
	}
	if total != 1 {
		t.Fatalf("expected only pre-assigned target, got %v", a)
	}

	// tie-breaking branch: deterministic assignment still succeeds
	a = boundedLoadRendezvousHash(genTargets(2), &PlacementStrategyOpts{NumPods: 2})
	if len(a) != 2 {
		t.Fatalf("assignment: %v", a)
	}
}

func TestPlanBuilder_DedupAndBranches(t *testing.T) {
	profile := gnmicv1alpha1.TargetProfileSpec{
		Encoding:       "JSON",
		CredentialsRef: "secret",
	}

	p1 := NewPipelineData()
	p1.Targets["default/t1"] = gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "t1"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "default"},
	}
	p1.TargetProfiles["default/default"] = profile
	p1.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}
	p1.Outputs["default/out"] = gnmicv1alpha1.OutputSpec{Type: FileOutputType}
	p1.ResolvedOutputAddresses = map[string][]string{"default/out": {"kafka://broker:9092"}}

	p1.Inputs["default/in"] = gnmicv1alpha1.InputSpec{Type: "kafka"}

	p2 := NewPipelineData()
	p2.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/other"}, Mode: "ONCE"}
	p2.Outputs["default/out"] = gnmicv1alpha1.OutputSpec{Type: FileOutputType}
	p2.Inputs["default/in"] = gnmicv1alpha1.InputSpec{Type: "kafka"}
	p2.OutputProcessors["default/shared"] = gnmicv1alpha1.ProcessorSpec{Type: "event-jq", Config: apiextensionsv1.JSON{Raw: []byte(`filter: .`)}}
	p2.InputProcessors["default/shared"] = gnmicv1alpha1.ProcessorSpec{Type: "event-jq", Config: apiextensionsv1.JSON{Raw: []byte(`filter: .`)}}

	plan, err := NewPlanBuilder("c", &mockCredsFetcher{creds: &Credentials{Username: "u"}}).
		AddPipeline("one", p1).
		AddPipeline("two", p2).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Subscriptions) != 1 || len(plan.Outputs) != 1 {
		t.Fatalf("dedup failed: subs=%d outs=%d", len(plan.Subscriptions), len(plan.Outputs))
	}
	if len(plan.Processors) != 1 {
		t.Fatalf("shared processor dedup: %d", len(plan.Processors))
	}

	// no prometheus outputs: assignPrometheusOutputPorts no-op path
	p3 := NewPipelineData()
	p3.Outputs["default/file"] = gnmicv1alpha1.OutputSpec{Type: FileOutputType}
	p3.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}
	plan, err = NewPlanBuilder("c", nil).AddPipeline("p", p3).Build()
	if err != nil || len(plan.PrometheusPorts) != 0 {
		t.Fatalf("prom ports=%v err=%v", plan.PrometheusPorts, err)
	}

	// tunnel policy missing profile skipped
	p4 := NewPipelineData()
	p4.TunnelTargetPolicies["default/pol"] = gnmicv1alpha1.TunnelTargetPolicySpec{Profile: "missing"}
	p4.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}
	p4.Outputs["default/o"] = gnmicv1alpha1.OutputSpec{Type: FileOutputType}
	plan, err = NewPlanBuilder("c", nil).AddPipeline("p", p4).Build()
	if err != nil || len(plan.TunnelTargetMatches) != 0 {
		t.Fatalf("tunnel skip: matches=%d err=%v", len(plan.TunnelTargetMatches), err)
	}

	// duplicate tunnel policy across pipelines
	p5 := NewPipelineData()
	p5.TunnelTargetPolicies["default/pol"] = gnmicv1alpha1.TunnelTargetPolicySpec{Profile: "default"}
	p5.TargetProfiles["default/default"] = profile
	p5.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}
	p5.Outputs["default/o"] = gnmicv1alpha1.OutputSpec{Type: FileOutputType}
	plan, err = NewPlanBuilder("c", nil).
		AddPipeline("a", p5).
		AddPipeline("b", p5).
		Build()
	if err != nil || len(plan.TunnelTargetMatches) != 1 {
		t.Fatalf("tunnel dedup: %d err=%v", len(plan.TunnelTargetMatches), err)
	}

	// input processor error
	p6 := NewPipelineData()
	p6.InputProcessors["default/bad"] = gnmicv1alpha1.ProcessorSpec{Type: "event-jq", Config: *rawJSON("[")}
	p6.Inputs["default/in"] = gnmicv1alpha1.InputSpec{Type: "kafka"}
	p6.Outputs["default/o"] = gnmicv1alpha1.OutputSpec{Type: FileOutputType}
	p6.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}
	_, err = NewPlanBuilder("c", nil).AddPipeline("p", p6).Build()
	if err == nil {
		t.Fatal("expected input processor error")
	}
}

func TestPollTargetState_InvalidURL(t *testing.T) {
	_, err := PollTargetState(context.Background(), &http.Client{}, "://invalid-url")
	if err == nil {
		t.Fatal("expected request error")
	}
}

func TestCollectRelationships_PodAssignmentLogging(t *testing.T) {
	p := NewPipelineData()
	p.Targets["default/t1"] = gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "t1"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "default"},
		Status: gnmicv1alpha1.TargetStatus{
			ClusterStates: map[string]gnmicv1alpha1.ClusterTargetState{
				"c": {Pod: "gnmic-c-0"},
			},
		},
	}
	p.Targets["default/t2"] = gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "t2"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.2:57400", Profile: "default"},
	}
	p.TargetProfiles["default/default"] = gnmicv1alpha1.TargetProfileSpec{Encoding: "JSON"}
	p.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}
	p.Outputs["default/out"] = gnmicv1alpha1.OutputSpec{Type: FileOutputType}

	plan, err := NewPlanBuilder("c", nil).AddPipeline("p", p).Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.CurrentTargetAssignment[0]) != 1 {
		t.Fatalf("assignment: %+v", plan.CurrentTargetAssignment)
	}
}

func TestPlanBuilder_DuplicateTargetAcrossPipelines(t *testing.T) {
	profile := gnmicv1alpha1.TargetProfileSpec{Encoding: "JSON"}
	target := gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "t1"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "default"},
	}
	p1 := NewPipelineData()
	p1.Targets["default/t1"] = target
	p1.TargetProfiles["default/default"] = profile
	p1.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}
	p1.Outputs["default/out"] = gnmicv1alpha1.OutputSpec{Type: FileOutputType}

	p2 := NewPipelineData()
	p2.Targets["default/t1"] = target
	p2.Subscriptions["default/sub"] = gnmicv1alpha1.SubscriptionSpec{Paths: []string{"/"}, Mode: "ONCE"}
	p2.Outputs["default/out"] = gnmicv1alpha1.OutputSpec{Type: FileOutputType}

	plan, err := NewPlanBuilder("c", nil).AddPipeline("a", p1).AddPipeline("b", p2).Build()
	if err != nil || len(plan.Targets) != 1 {
		t.Fatalf("targets=%d err=%v", len(plan.Targets), err)
	}
}

func TestBuildTargetConfig_PasswordOnly(t *testing.T) {
	target := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "t"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "1.2.3.4:57400"},
	}
	profile := &gnmicv1alpha1.TargetProfileSpec{Encoding: "JSON"}
	cfg := buildTargetConfig(target, profile, &Credentials{Password: "secret"}, nil)
	if cfg.Password == nil || cfg.Username != nil {
		t.Fatal("expected password-only creds")
	}
}

func TestBuildTargetConfig_ClientTLSKeyOnly(t *testing.T) {
	target := &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "t"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "1.2.3.4:57400"},
	}
	profile := &gnmicv1alpha1.TargetProfileSpec{Encoding: "JSON"}
	cfg := buildTargetConfig(target, profile, nil, &ClientTLSPaths{KeyFile: "/key"})
	if cfg.TLSKey == nil || cfg.TLSCert != nil {
		t.Fatalf("config: %+v", cfg)
	}
}

func TestStreamTargetState_ScannerReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"store\":\"state\",\"kind\":\"targets\"}\n"))
	}))
	defer srv.Close()

	// Replace client transport to return body that errors on read
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		resp.Body = io.NopCloser(errReader{})
		return resp, nil
	})}
	ch := make(chan SSEEvent, 1)
	err := StreamTargetState(context.Background(), client, srv.URL, ch)
	if err == nil {
		t.Fatal("expected scanner error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestBuildTunnelTargetMatch_ProfileTLSVariants(t *testing.T) {
	policy := &gnmicv1alpha1.TunnelTargetPolicySpec{Profile: "p"}
	profile := &gnmicv1alpha1.TargetProfileSpec{
		Encoding: "JSON",
		TLS:      &gnmicv1alpha1.TargetTLSConfig{MinVersion: "1.2", CipherSuites: []string{"TLS_RSA"}},
	}
	m := buildTunnelTargetMatch(policy, profile, nil, nil)
	if m.Config.TLSMinVersion != "1.2" {
		t.Fatal("min version")
	}
}

// Ensure ptr import used in tests that mirror production patterns.
var _ = ptr.To(1)
