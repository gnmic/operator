package gnmic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// streamBody serves body as one SSE response and collects everything the
// reader forwards until the stream ends.
func streamBody(t *testing.T, body string) []SSEEvent {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events := make(chan SSEEvent, 16)
	if err := StreamTargetState(ctx, srv.Client(), srv.URL, events); err != nil {
		t.Fatalf("stream: %v", err)
	}
	close(events)
	var got []SSEEvent
	for ev := range events {
		got = append(got, ev)
	}
	return got
}

const stateEvent = `{"store":"state","kind":"targets","name":"default/t1","object":{"state":"running"}}`

// #33: the space after the colon is optional per the SSE spec; the reader
// required it and silently skipped events without one.
func TestSSEDataWithoutSpaceIsAccepted(t *testing.T) {
	got := streamBody(t, "event:update\ndata:"+stateEvent+"\n\n")
	if len(got) != 1 || got[0].EventType != "update" || got[0].Data.Name != "default/t1" {
		t.Fatalf("events = %+v", got)
	}
}

// #33: several data: lines are one payload joined by newlines.
func TestSSEMultiLineDataIsJoined(t *testing.T) {
	part1 := `{"store":"state","kind":"targets",`
	part2 := `"name":"default/t1","object":{"state":"running"}}`
	got := streamBody(t, "event: create\ndata: "+part1+"\ndata: "+part2+"\n\n")
	if len(got) != 1 || got[0].EventType != "create" || got[0].Data.Name != "default/t1" {
		t.Fatalf("events = %+v", got)
	}
}

// #33: event: after data: belongs to the same frame; the reader used to
// attribute it to the next one.
func TestSSEEventFieldAfterDataIsAttributed(t *testing.T) {
	got := streamBody(t, "data: "+stateEvent+"\nevent: delete\n\n")
	if len(got) != 1 || got[0].EventType != "delete" {
		t.Fatalf("events = %+v", got)
	}
}

// #33: bufio.Scanner's default 64 KiB line cap turned a large event into a
// stream error and a reconnect.
func TestSSELargeEventDoesNotBreakTheStream(t *testing.T) {
	big := `{"store":"state","kind":"targets","name":"default/big","object":{"state":"running","subscriptions":{` +
		`"pad":"` + strings.Repeat("x", 200*1024) + `"}}}`
	got := streamBody(t, "event: update\ndata: "+big+"\n\nevent: update\ndata: "+stateEvent+"\n\n")
	if len(got) != 2 || got[0].Data.Name != "default/big" || got[1].Data.Name != "default/t1" {
		names := make([]string, 0, len(got))
		for _, ev := range got {
			names = append(names, ev.Data.Name)
		}
		t.Fatalf("got %d events %v, want big then t1", len(got), names)
	}
}

// id:, retry:, comments and unknown fields are ignored; config-store and
// non-target events are filtered as before; a trailing event without a blank
// line is still delivered.
func TestSSEFilteringAndFramingUnchanged(t *testing.T) {
	body := ": keepalive\n" +
		"id: 1\nretry: 5000\nevent: update\ndata: " + stateEvent + "\n\n" +
		"event: update\ndata: {\"store\":\"config\",\"kind\":\"targets\",\"name\":\"default/cfg\"}\n\n" +
		"event: update\ndata: {\"store\":\"state\",\"kind\":\"outputs\",\"name\":\"default/out\"}\n\n" +
		"event: update\ndata: not json\n\n" +
		"event: delete\ndata: " + strings.Replace(stateEvent, "default/t1", "default/last", 1) + "\n" // no blank line
	got := streamBody(t, body)
	if len(got) != 2 {
		t.Fatalf("events = %+v", got)
	}
	if got[0].Data.Name != "default/t1" || got[1].Data.Name != "default/last" || got[1].EventType != "delete" {
		t.Fatalf("events = %+v", got)
	}
}
