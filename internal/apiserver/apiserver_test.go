package apiserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gapi "github.com/openconfig/gnmic/pkg/api/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/controller"
	"github.com/gnmic/operator/internal/discovery"
	"github.com/gnmic/operator/internal/gnmic"
)

func newScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := gnmicv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func TestGetClusterPlan(t *testing.T) {
	plan := &gnmic.ApplyPlan{Targets: map[string]*gapi.TargetConfig{"default/t1": {
		Name:     "default/t1",
		Username: ptr.To("admin"),
		Password: ptr.To("plaintext-password"),
		Token:    ptr.To("plaintext-token"),
	}}}
	reconciler := controller.NewClusterReconcilerForTest()
	reconciler.CachePlan("default", "cluster-a", plan)
	srv := New(":0", reconciler, fake.NewClientBuilder().WithScheme(newScheme(t)).Build())
	ts := httptest.NewServer(srv.Server.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/clusters/default/cluster-a/plan")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var got gnmic.ApplyPlan
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	tc, ok := got.Targets["default/t1"]
	if !ok {
		t.Fatalf("plan = %+v", got)
	}
	// The endpoint is unauthenticated; what it serves must not include the
	// device credentials the plan carries for the collectors.
	for _, secret := range []string{"plaintext-password", "plaintext-token"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("plan response leaks %q: %s", secret, body)
		}
	}
	if tc.Password == nil || *tc.Password != gnmic.RedactedSecret || tc.Token == nil || *tc.Token != gnmic.RedactedSecret {
		t.Fatalf("credentials not masked: %+v", tc)
	}
	if tc.Username == nil || *tc.Username != "admin" {
		t.Fatalf("username should survive redaction: %+v", tc)
	}
	if *plan.Targets["default/t1"].Password != "plaintext-password" {
		t.Fatal("serving the plan modified the cached copy")
	}
	resp2, err := http.Get(ts.URL + "/clusters/default/missing/plan")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("missing plan status = %d", resp2.StatusCode)
	}
}

func refreshFixture(t *testing.T, webhook *gnmicv1alpha1.WebhookSpec) (*APIServer, client.Client) {
	scheme := newScheme(t)
	ts := &gnmicv1alpha1.TargetSource{
		ObjectMeta: metav1.ObjectMeta{Name: "inv", Namespace: "default"},
		Spec: gnmicv1alpha1.TargetSourceSpec{
			Source:  gnmicv1alpha1.SourceSpec{Type: gnmicv1alpha1.SourceTypeStatic, Static: &gnmicv1alpha1.StaticSource{Devices: []gnmicv1alpha1.StaticDevice{{Name: "a", Address: "1.1.1.1"}}}},
			Webhook: webhook,
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "hook", Namespace: "default"},
		Data:       map[string][]byte{"token": []byte("t0ken"), "hmac": []byte("k3y")},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ts, secret).Build()
	srv := New(":0", controller.NewClusterReconcilerForTest(), c)
	return srv, c
}

func post(t *testing.T, h http.Handler, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const refreshPath = "/api/v1/namespaces/default/targetsources/inv/refresh"

func TestRefreshBearer(t *testing.T) {
	srv, c := refreshFixture(t, &gnmicv1alpha1.WebhookSpec{
		Enabled:  true,
		Auth:     &gnmicv1alpha1.WebhookAuthSpec{Bearer: &gnmicv1alpha1.WebhookBearerAuth{SecretRef: gnmicv1alpha1.SecretKeyReference{Name: "hook", Key: "token"}}},
		Debounce: &metav1.Duration{Duration: 5 * time.Second},
	})
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	srv.now = func() time.Time { return now }

	if rec := post(t, srv.Router(), refreshPath, "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(t, srv.Router(), refreshPath, "", map[string]string{"Authorization": "Bearer wrong"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", rec.Code)
	}
	rec := post(t, srv.Router(), refreshPath, "", map[string]string{"Authorization": "Bearer t0ken"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("good token: %d %s", rec.Code, rec.Body.String())
	}
	var ts gnmicv1alpha1.TargetSource
	if err := c.Get(t.Context(), types.NamespacedName{Namespace: "default", Name: "inv"}, &ts); err != nil {
		t.Fatal(err)
	}
	if got := ts.Annotations[discovery.AnnotationRequestedAt]; got != now.Format(time.RFC3339Nano) {
		t.Fatalf("annotation = %q", got)
	}

	// Within the debounce window: acknowledged, not re-annotated.
	srv.now = func() time.Time { return now.Add(2 * time.Second) }
	rec = post(t, srv.Router(), refreshPath, "", map[string]string{"Authorization": "Bearer t0ken"})
	var resp RefreshResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusOK || !resp.Debounced {
		t.Fatalf("debounced call: %d %s", rec.Code, rec.Body.String())
	}
	// After the window: a new run.
	srv.now = func() time.Time { return now.Add(6 * time.Second) }
	if rec = post(t, srv.Router(), refreshPath, "", map[string]string{"Authorization": "Bearer t0ken"}); rec.Code != http.StatusAccepted {
		t.Fatalf("after debounce: %d", rec.Code)
	}
}

func TestRefreshSignature(t *testing.T) {
	srv, _ := refreshFixture(t, &gnmicv1alpha1.WebhookSpec{
		Enabled: true,
		Auth:    &gnmicv1alpha1.WebhookAuthSpec{Signature: &gnmicv1alpha1.WebhookSignatureAuth{SecretRef: gnmicv1alpha1.SecretKeyReference{Name: "hook", Key: "hmac"}}},
	})
	body := `{"event":"device.updated"}`
	mac := hmac.New(sha256.New, []byte("k3y"))
	mac.Write([]byte(body))
	sig := hex.EncodeToString(mac.Sum(nil))

	if rec := post(t, srv.Router(), refreshPath, body, map[string]string{"X-Hook-Signature": "sha256=" + sig}); rec.Code != http.StatusAccepted {
		t.Fatalf("valid signature: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(t, srv.Router(), refreshPath, body+" ", map[string]string{"X-Hook-Signature": sig}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered body: %d", rec.Code)
	}
	if rec := post(t, srv.Router(), refreshPath, body, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing signature: %d", rec.Code)
	}
}

func TestRefreshNotFoundAndDisabled(t *testing.T) {
	srv, _ := refreshFixture(t, &gnmicv1alpha1.WebhookSpec{Enabled: false})
	if rec := post(t, srv.Router(), refreshPath, "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("disabled webhook: %d", rec.Code)
	}
	if rec := post(t, srv.Router(), "/api/v1/namespaces/default/targetsources/absent/refresh", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("absent TargetSource: %d", rec.Code)
	}
	srv, _ = refreshFixture(t, &gnmicv1alpha1.WebhookSpec{Enabled: true})
	if rec := post(t, srv.Router(), refreshPath, "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("enabled without auth must reject: %d", rec.Code)
	}
}
