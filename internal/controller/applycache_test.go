package controller

import (
	"testing"
	"time"
)

func testCache(t *testing.T, now *time.Time) *ApplyCache {
	t.Helper()
	c := NewApplyCache()
	c.now = func() time.Time { return *now }
	return c
}

func TestApplyCache_RecordThenUnchanged(t *testing.T) {
	now := time.Unix(1000, 0)
	c := testCache(t, &now)

	if c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("empty cache reported unchanged; a pod we know nothing about must be applied to")
	}
	c.Record("ns/c1/0", "hashA")
	if c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("first record must stay unverified so a follow-up apply still runs")
	}
	if !c.NeedsVerify("ns/c1/0", "hashA") {
		t.Fatal("first record should need verify")
	}
	c.Record("ns/c1/0", "hashA")
	if !c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("verified hash reported changed")
	}
	if c.NeedsVerify("ns/c1/0", "hashA") {
		t.Fatal("verified record should not need verify")
	}
	if c.Unchanged("ns/c1/0", "hashB") {
		t.Fatal("different hash reported unchanged")
	}
	if c.Unchanged("ns/c1/1", "hashA") {
		t.Fatal("records must not leak between pods of the same cluster")
	}
}

// The TTL is the backstop for a pod losing its config without dropping its SSE
// stream, which nothing else can detect.
func TestApplyCache_ExpiresAfterRefreshInterval(t *testing.T) {
	now := time.Unix(1000, 0)
	c := testCache(t, &now)
	c.Record("ns/c1/0", "hashA")
	c.Record("ns/c1/0", "hashA") // verify

	now = now.Add(applyRefreshInterval - time.Second)
	if !c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("expired early")
	}
	now = now.Add(2 * time.Second)
	if c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("did not expire at the refresh interval")
	}
}

// An SSE disconnect means the pod may have restarted empty. Only that pod's
// record may be dropped: invalidating its neighbours would re-POST the whole
// cluster on any single-pod blip.
func TestApplyCache_InvalidateIsPerPod(t *testing.T) {
	now := time.Unix(1000, 0)
	c := testCache(t, &now)
	c.Record("ns/c1/0", "hashA")
	c.Record("ns/c1/0", "hashA")
	c.Record("ns/c1/1", "hashB")
	c.Record("ns/c1/1", "hashB")

	c.Invalidate("ns/c1/0")
	if c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("invalidated pod still reported unchanged")
	}
	if !c.Unchanged("ns/c1/1", "hashB") {
		t.Fatal("invalidating one pod dropped another")
	}
}

func TestApplyCache_InvalidateCluster(t *testing.T) {
	now := time.Unix(1000, 0)
	c := testCache(t, &now)
	for _, key := range []string{"ns/c1/0", "ns/c1/1", "ns/c10/0", "ns/c2/0"} {
		c.Record(key, "h")
		c.Record(key, "h")
	}

	c.InvalidateCluster("ns", "c1")
	if c.Unchanged("ns/c1/0", "h") || c.Unchanged("ns/c1/1", "h") {
		t.Fatal("cluster records survived deletion")
	}
	if !c.Unchanged("ns/c10/0", "h") {
		t.Fatal("cluster c10 was dropped by a prefix match on c1")
	}
	if !c.Unchanged("ns/c2/0", "h") {
		t.Fatal("unrelated cluster was dropped")
	}
}

func TestApplyCache_NewHashResetsVerification(t *testing.T) {
	now := time.Unix(1000, 0)
	c := testCache(t, &now)
	c.Record("ns/c1/0", "hashA")
	c.Record("ns/c1/0", "hashA")
	if !c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("setup: hashA should be verified")
	}
	c.Record("ns/c1/0", "hashB")
	if c.Unchanged("ns/c1/0", "hashB") {
		t.Fatal("new hash must not be verified after a single record")
	}
	if !c.NeedsVerify("ns/c1/0", "hashB") {
		t.Fatal("new hash should need verify")
	}
}

// A reconciler built without a cache must apply unconditionally rather than
// silently skip, so the short-circuit can never be enabled by accident.
func TestApplyCache_NilIsAlwaysChanged(t *testing.T) {
	var c *ApplyCache
	c.Record("ns/c1/0", "hashA")
	if c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("nil cache reported unchanged")
	}
	if c.NeedsVerify("ns/c1/0", "hashA") {
		t.Fatal("nil cache reported needs verify")
	}
	c.Invalidate("ns/c1/0")
	c.InvalidateCluster("ns", "c1")
}

// The fingerprint covers the bytes on the wire, so equal payloads compare equal
// and any difference in what is sent is a difference here.
func TestFingerprint(t *testing.T) {
	a := fingerprint([]byte(`{"targets":{}}`))
	if a != fingerprint([]byte(`{"targets":{}}`)) {
		t.Fatal("identical bodies produced different fingerprints")
	}
	if a == fingerprint([]byte(`{"targets":{"x":null}}`)) {
		t.Fatal("different bodies produced the same fingerprint")
	}
	if a == "" {
		t.Fatal("empty fingerprint")
	}
}
