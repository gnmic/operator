package gnmic

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// fullProfile sets every TargetProfile field that describes how to reach a target,
// so a test can assert none of them is dropped.
func fullProfile() *gnmicv1alpha1.TargetProfileSpec {
	return &gnmicv1alpha1.TargetProfileSpec{
		Encoding:        "JSON_IETF",
		Timeout:         metav1.Duration{Duration: 7 * time.Second},
		RetryTimer:      metav1.Duration{Duration: 3 * time.Second},
		Proxy:           "socks5://jump.example.com:1080",
		GzipCompression: true,
		Labels:          map[string]string{"site": "ams1", "tier": "leaf"},
		TCPKeepAlive:    &metav1.Duration{Duration: 30 * time.Second},
		GRCPKeepAlive: &gnmicv1alpha1.GRPCKeepAliveConfig{
			Time:                metav1.Duration{Duration: 10 * time.Second},
			Timeout:             metav1.Duration{Duration: 3 * time.Second},
			PermitWithoutStream: true,
		},
	}
}

func testTarget() *gnmicv1alpha1.Target {
	return &gnmicv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "t1"},
		Spec:       gnmicv1alpha1.TargetSpec{Address: "10.0.0.1:57400", Profile: "prof"},
	}
}

// The profile fields below used to be applied only after the TLS decision, every
// branch of which returned early — so keepalives reached the collector only when
// cluster clientTLS and profile TLS were both set, which excludes the plaintext
// default. proxy, gzipCompression and labels were never mapped at all.
func TestProfileConnectionFieldsSurviveEveryTLSCombination(t *testing.T) {
	tests := []struct {
		name       string
		profileTLS *gnmicv1alpha1.TargetTLSConfig
		clientTLS  *ClientTLSPaths
	}{
		{name: "plaintext"},
		{
			name:       "profile TLS only",
			profileTLS: &gnmicv1alpha1.TargetTLSConfig{MinVersion: "1.2"},
		},
		{
			name:      "cluster clientTLS only",
			clientTLS: &ClientTLSPaths{CertFile: "/c", KeyFile: "/k", CAFile: "/ca"},
		},
		{
			name:       "both",
			profileTLS: &gnmicv1alpha1.TargetTLSConfig{MinVersion: "1.2"},
			clientTLS:  &ClientTLSPaths{CertFile: "/c", KeyFile: "/k", CAFile: "/ca"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := fullProfile()
			profile.TLS = tt.profileTLS

			cfg := buildTargetConfig(testTarget(), profile, nil, tt.clientTLS)

			if cfg.Proxy != profile.Proxy {
				t.Errorf("proxy = %q, want %q", cfg.Proxy, profile.Proxy)
			}
			if cfg.Gzip == nil || !*cfg.Gzip {
				t.Errorf("gzip = %v, want true", cfg.Gzip)
			}
			if got := cfg.EventTags["site"]; got != "ams1" {
				t.Errorf("event-tags = %v, want the profile labels", cfg.EventTags)
			}
			if cfg.TCPKeepalive != 30*time.Second {
				t.Errorf("tcp keepalive = %v, want 30s", cfg.TCPKeepalive)
			}
			if cfg.GRPCKeepalive == nil {
				t.Fatal("grpc keepalive dropped")
			}
			if cfg.GRPCKeepalive.Time != 10*time.Second ||
				cfg.GRPCKeepalive.Timeout != 3*time.Second ||
				!cfg.GRPCKeepalive.PermitWithoutStream {
				t.Errorf("grpc keepalive = %+v", cfg.GRPCKeepalive)
			}
			// The non-TLS basics still come through.
			if cfg.Timeout != 7*time.Second || cfg.RetryTimer != 3*time.Second {
				t.Errorf("timeout=%v retry=%v", cfg.Timeout, cfg.RetryTimer)
			}
		})
	}
}

// The labels map must not be shared with the CR the informer cache holds.
func TestProfileLabelsAreCloned(t *testing.T) {
	profile := fullProfile()
	cfg := buildTargetConfig(testTarget(), profile, nil, nil)

	cfg.EventTags["site"] = "mutated"
	if profile.Labels["site"] != "ams1" {
		t.Fatal("mutating the target config changed the profile's labels map")
	}
}

// serverName selects the SNI the collector sends, which is meaningful with or without
// verification. It used to be applied only when cluster clientTLS was configured.
func TestProfileServerNameAppliesWithoutClusterClientTLS(t *testing.T) {
	profile := fullProfile()
	profile.TLS = &gnmicv1alpha1.TargetTLSConfig{
		ServerName:   "router.example.com",
		MinVersion:   "1.2",
		MaxVersion:   "1.3",
		CipherSuites: []string{"TLS_AES_128_GCM_SHA256"},
	}

	cfg := buildTargetConfig(testTarget(), profile, nil, nil)
	if cfg.TLSServerName != "router.example.com" {
		t.Errorf("tls-server-name = %q, want router.example.com", cfg.TLSServerName)
	}
	if cfg.TLSMinVersion != "1.2" || cfg.TLSMaxVersion != "1.3" || len(cfg.CipherSuites) != 1 {
		t.Errorf("tls options dropped: %+v", cfg)
	}
	if cfg.SkipVerify == nil || !*cfg.SkipVerify {
		t.Error("expected skip-verify with no CA to verify against")
	}
}

// The TLS decision itself must be unchanged by the restructuring.
func TestTargetTLSDecisionUnchanged(t *testing.T) {
	target := testTarget()

	plain := buildTargetConfig(target, &gnmicv1alpha1.TargetProfileSpec{}, nil, nil)
	if plain.Insecure == nil || !*plain.Insecure {
		t.Error("plaintext: want insecure")
	}
	if plain.SkipVerify != nil {
		t.Error("plaintext: skip-verify should be unset")
	}

	withCA := buildTargetConfig(target, &gnmicv1alpha1.TargetProfileSpec{}, nil,
		&ClientTLSPaths{CertFile: "/c", KeyFile: "/k", CAFile: "/ca"})
	if withCA.TLSCA == nil || withCA.SkipVerify == nil || *withCA.SkipVerify {
		t.Errorf("with CA: want verification on, got %+v", withCA)
	}
	if withCA.Insecure != nil {
		t.Error("with CA: insecure should be unset")
	}

	noCA := buildTargetConfig(target, &gnmicv1alpha1.TargetProfileSpec{}, nil,
		&ClientTLSPaths{CertFile: "/c", KeyFile: "/k"})
	if noCA.SkipVerify == nil || !*noCA.SkipVerify {
		t.Errorf("without CA: want skip-verify, got %+v", noCA)
	}
}

// A tunnel target built from the same profile must get the same connection settings
// as a declared one. The inlined copy this replaces never mapped the keepalives.
func TestTunnelTargetMatchGetsTheSameProfileFields(t *testing.T) {
	profile := fullProfile()

	// TunnelTargetPolicySpec.Match points at an unexported type, so it cannot be
	// constructed from outside api/v1alpha1 -- decode the spec the way the API server
	// does instead.
	var policy gnmicv1alpha1.TunnelTargetPolicySpec
	if err := json.Unmarshal([]byte(
		`{"profile":"prof","match":{"type":"nokia_srlinux","id":"leaf.*"}}`), &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Match == nil {
		t.Fatal("match did not decode")
	}

	match := buildTunnelTargetMatch(&policy, profile, &Credentials{Username: "u", Password: "p"}, nil)
	if match.Type != "nokia_srlinux" || match.ID != "leaf.*" {
		t.Errorf("match criteria = %+v", match)
	}
	cfg := match.Config
	if cfg == nil {
		t.Fatal("no target config built")
	}
	if cfg.TCPKeepalive != 30*time.Second {
		t.Errorf("tcp keepalive = %v, want 30s", cfg.TCPKeepalive)
	}
	if cfg.GRPCKeepalive == nil || cfg.GRPCKeepalive.Time != 10*time.Second {
		t.Errorf("grpc keepalive = %+v", cfg.GRPCKeepalive)
	}
	if cfg.Proxy != profile.Proxy {
		t.Errorf("proxy = %q", cfg.Proxy)
	}
	if cfg.Gzip == nil || !*cfg.Gzip {
		t.Error("gzip dropped")
	}
	if cfg.EventTags["tier"] != "leaf" {
		t.Errorf("event-tags = %v", cfg.EventTags)
	}
	if cfg.Username == nil || *cfg.Username != "u" {
		t.Error("credentials dropped")
	}
	if cfg.Insecure == nil || !*cfg.Insecure {
		t.Error("expected plaintext with no TLS configured")
	}
}

// ---------------------------------------------------------------- #12

// streamSubscriptions names siblings in the same pipeline. The lookup used the bare
// name against a map keyed namespace/pipeline/name, so it never matched, and the
// pre-sized slice left a nil per entry that marshalled as a literal null.
func TestStreamSubscriptionsResolveByPipelineScopedKey(t *testing.T) {
	parent := &gnmicv1alpha1.SubscriptionSpec{
		Mode:                "STREAM/SAMPLE",
		Paths:               []string{"/parent"},
		StreamSubscriptions: []string{"childA", "childB"},
	}
	allSubs := map[string]gnmicv1alpha1.SubscriptionSpec{
		"ns/pipe/parent": *parent,
		"ns/pipe/childA": {Mode: "STREAM/ON_CHANGE", Paths: []string{"/a"}},
		"ns/pipe/childB": {Mode: "STREAM/SAMPLE", Paths: []string{"/b"}},
	}

	cfg := buildSubscriptionConfig("ns/pipe/parent", parent, []string{"ns/pipe/out"}, allSubs)

	if len(cfg.StreamSubscriptions) != 2 {
		t.Fatalf("resolved %d stream subscriptions, want 2: %+v", len(cfg.StreamSubscriptions), cfg.StreamSubscriptions)
	}
	for i, sub := range cfg.StreamSubscriptions {
		if sub == nil {
			t.Fatalf("stream subscription %d is nil; it would serialize as null", i)
		}
	}
	if cfg.StreamSubscriptions[0].Name != "ns/pipe/childA" {
		t.Errorf("child name = %q, want the pipeline-scoped key", cfg.StreamSubscriptions[0].Name)
	}
	if cfg.StreamSubscriptions[0].StreamMode != "ON_CHANGE" {
		t.Errorf("child stream mode = %q", cfg.StreamSubscriptions[0].StreamMode)
	}
}

// A name that is not in the pipeline is left out rather than leaving a hole.
func TestUnknownStreamSubscriptionLeavesNoNull(t *testing.T) {
	parent := &gnmicv1alpha1.SubscriptionSpec{
		Mode:                "STREAM/SAMPLE",
		StreamSubscriptions: []string{"present", "absent"},
	}
	allSubs := map[string]gnmicv1alpha1.SubscriptionSpec{
		"ns/pipe/present": {Mode: "ONCE", Paths: []string{"/p"}},
	}

	cfg := buildSubscriptionConfig("ns/pipe/parent", parent, nil, allSubs)
	if len(cfg.StreamSubscriptions) != 1 || cfg.StreamSubscriptions[0] == nil {
		t.Fatalf("stream subscriptions = %+v, want exactly one non-nil", cfg.StreamSubscriptions)
	}

	// The payload the collectors receive must not contain a null entry.
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "null") {
		t.Errorf("serialized config contains null: %s", body)
	}
}

func TestSiblingSubscriptionKey(t *testing.T) {
	for _, tc := range []struct{ subNN, name, want string }{
		{"ns/pipe/parent", "child", "ns/pipe/child"},
		{"ns/parent", "child", "ns/child"},
		{"parent", "child", "child"},
	} {
		if got := siblingSubscriptionKey(tc.subNN, tc.name); got != tc.want {
			t.Errorf("siblingSubscriptionKey(%q, %q) = %q, want %q", tc.subNN, tc.name, got, tc.want)
		}
	}
}
