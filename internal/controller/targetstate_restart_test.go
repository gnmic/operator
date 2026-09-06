package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Reproduces the sequence seen on a kind cluster: the SSE stream failed to
// connect before the collector was ready and went into reconnect backoff; the
// Cluster controller then applied and recorded the plan; the collector crashed
// and its container restarted empty, all while no stream was attached; the
// stream then connected to the new container as if nothing had happened. With
// invalidation tied only to a stream drop, the record survived and every
// reconcile short-circuited until the operator was restarted.
//
// The next reconcile's decision is exactly ApplyCache.Unchanged on the pod's
// key (see applyConfigToPods), so that is what must flip back to false.
func TestRunStream_ConnectAfterUnseenRestartInvalidatesApplyRecord(t *testing.T) {
	var ready atomic.Bool
	connects := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			// The container is not serving yet (or is mid-restart): the
			// operator sees a failed connect, never a stream drop.
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		connects <- struct{}{}
		<-r.Context().Done()
	}))
	defer srv.Close()

	cache := NewApplyCache()
	key := streamKey("ns", "c1", 0)
	cluster := &gnmicv1alpha1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "ns"}}
	r := &TargetStateReconciler{Applied: cache}

	ctx, cancel := context.WithCancel(logf.IntoContext(context.Background(), logf.Log))
	defer cancel()
	go r.runStream(ctx, cluster, "gnmic-c1", 0, srv.URL+sseTargetsPath)

	// Let the first attempt fail so the loop is sitting in its backoff.
	time.Sleep(200 * time.Millisecond)

	// T+0: the Cluster controller applies and records; T+2s the collector
	// crashes and comes back empty. Nothing was attached, so no drop is seen.
	cache.Record(key, "hashA")
	if !cache.Unchanged(key, "hashA") {
		t.Fatal("precondition: record not held")
	}
	ready.Store(true)

	select {
	case <-connects:
	case <-time.After(reconnectMinDelay + 5*time.Second):
		t.Fatal("stream never reconnected")
	}

	deadline := time.Now().Add(2 * time.Second)
	for cache.Unchanged(key, "hashA") {
		if time.Now().After(deadline) {
			t.Fatal("apply record survived a connect to a container that may have restarted; the next reconcile would skip the re-send")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The re-send then records again, and the record is trusted while this
	// container stays attached: no further connect, no further invalidation.
	cache.Record(key, "hashA")
	time.Sleep(100 * time.Millisecond)
	if !cache.Unchanged(key, "hashA") {
		t.Fatal("record applied after the connect was dropped without a drop or reconnect")
	}

	// A crash that is observed as a drop still invalidates.
	srv.CloseClientConnections()
	deadline = time.Now().Add(2 * time.Second)
	for cache.Unchanged(key, "hashA") {
		if time.Now().After(deadline) {
			t.Fatal("stream drop did not invalidate")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
