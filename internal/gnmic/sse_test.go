package gnmic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseTargetStateObject(t *testing.T) {
	raw, _ := json.Marshal(TargetStateObject{
		State:           "running",
		ConnectionState: "READY",
		LastUpdated:     time.Now(),
	})
	obj, err := ParseTargetStateObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	if obj.State != "running" {
		t.Fatalf("state = %q", obj.State)
	}
	if _, err := ParseTargetStateObject(json.RawMessage(`{`)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestStreamTargetState(t *testing.T) {
	body := ": keepalive\n\nevent: update\ndata: {\"store\":\"config\",\"kind\":\"targets\"}\n\nevent: update\ndata: {\"store\":\"state\",\"kind\":\"targets\",\"name\":\"default/t1\",\"object\":{\"state\":\"running\",\"connection-state\":\"READY\",\"last-updated\":\"2020-01-01T00:00:00Z\"}}\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	events := make(chan SSEEvent, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- StreamTargetState(ctx, srv.Client(), srv.URL, events)
	}()

	select {
	case ev := <-events:
		if ev.Data.Kind != "targets" || ev.EventType != "update" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SSE event")
	}
	cancel()
}

func TestPollTargetState(t *testing.T) {
	entries := []PollTargetEntry{{
		Name:  "default/t1",
		State: &TargetStateObject{State: "running"},
	}}
	payload, _ := json.Marshal(entries)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	got, err := PollTargetState(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "default/t1" {
		t.Fatalf("unexpected entries: %+v", got)
	}

	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badSrv.Close()
	if _, err := PollTargetState(context.Background(), badSrv.Client(), badSrv.URL); err == nil {
		t.Fatal("expected poll error on bad status")
	}
}

// The connect hook must fire exactly when the pod has accepted the stream, and
// never for an attempt that did not reach a serving pod: a failed connect is
// not evidence of which process is on the other end.
func TestStreamTargetStateWithConnect_HookFiresOnlyOnAcceptedStream(t *testing.T) {
	var connected atomic.Int32

	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer refused.Close()
	ch := make(chan SSEEvent, 1)
	if err := StreamTargetStateWithConnect(context.Background(), refused.Client(), refused.URL, ch, func() { connected.Add(1) }); err == nil {
		t.Fatal("expected error for non-200 status")
	}
	if got := connected.Load(); got != 0 {
		t.Fatalf("hook fired %d times on a rejected connect", got)
	}

	if err := StreamTargetStateWithConnect(context.Background(), &http.Client{}, "http://127.0.0.1:1/sse", ch, func() { connected.Add(1) }); err == nil {
		t.Fatal("expected error for connection refused")
	}
	if got := connected.Load(); got != 0 {
		t.Fatalf("hook fired %d times on a refused connect", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	accepted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer accepted.Close()
	done := make(chan error, 1)
	go func() {
		done <- StreamTargetStateWithConnect(ctx, accepted.Client(), accepted.URL, ch, func() { connected.Add(1) })
	}()

	deadline := time.Now().Add(2 * time.Second)
	for connected.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("hook did not fire on an accepted stream")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if got := connected.Load(); got != 1 {
		t.Fatalf("hook fired %d times for one connect", got)
	}
}
