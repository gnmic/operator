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
	if !c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("recorded hash reported changed")
	}
	if c.Unchanged("ns/c1/0", "hashB") {
		t.Fatal("different hash reported unchanged")
	}
	if c.Unchanged("ns/c1/1", "hashA") {
		t.Fatal("records must not leak between pods of the same cluster")
	}
}

// The TTL is the backstop for a pod losing its config without dropping its SSE
// stream, which nothing else can detect. It is measured from verification,
// since an unverified hash is on its own, shorter clock (applyVerifyDelay).
func TestApplyCache_ExpiresAfterRefreshInterval(t *testing.T) {
	now := time.Unix(1000, 0)
	c := testCache(t, &now)
	c.Record("ns/c1/0", "hashA")
	now = now.Add(applyVerifyDelay)
	c.Record("ns/c1/0", "hashA") // simulate the safety-net repost verifying it

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
	c.Record("ns/c1/0", "h")
	c.Record("ns/c1/1", "h")
	c.Record("ns/c10/0", "h") // prefix-adjacent, must survive
	c.Record("ns/c2/0", "h")

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

// A reconciler built without a cache must apply unconditionally rather than
// silently skip, so the short-circuit can never be enabled by accident.
func TestApplyCache_NilIsAlwaysChanged(t *testing.T) {
	var c *ApplyCache
	c.Record("ns/c1/0", "hashA")
	if c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("nil cache reported unchanged")
	}
	c.Invalidate("ns/c1/0")
	c.InvalidateCluster("ns", "c1")
}

// A freshly applied hash is unverified but must still read as unchanged
// during the grace period: an unrelated reconcile arriving in that window
// (e.g. a Target status update) must not trigger a redundant repost.
func TestApplyCache_UnverifiedUnchangedDuringGrace(t *testing.T) {
	now := time.Unix(1000, 0)
	c := testCache(t, &now)
	c.Record("ns/c1/0", "hashA")

	now = now.Add(applyVerifyDelay - time.Second)
	if !c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("unverified hash reported changed before the grace period elapsed")
	}
	if !c.NeedsVerify("ns/c1/0", "hashA") {
		t.Fatal("NeedsVerify should stay true until the follow-up Record")
	}
}

// Once the grace period elapses with no newer hash recorded, the safety-net
// repost is due: Unchanged must report changed so the reconciler re-POSTs.
func TestApplyCache_UnverifiedNeedsRepostAfterGrace(t *testing.T) {
	now := time.Unix(1000, 0)
	c := testCache(t, &now)
	c.Record("ns/c1/0", "hashA")

	now = now.Add(applyVerifyDelay)
	if c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("unverified hash still reported unchanged after the grace period")
	}
}

// The repost's Record call marks the hash verified, so it settles for good
// (until the TTL) instead of triggering yet another repost.
func TestApplyCache_SecondRecordVerifies(t *testing.T) {
	now := time.Unix(1000, 0)
	c := testCache(t, &now)
	c.Record("ns/c1/0", "hashA")

	now = now.Add(applyVerifyDelay)
	c.Record("ns/c1/0", "hashA")
	if c.NeedsVerify("ns/c1/0", "hashA") {
		t.Fatal("hash still reported as needing verify after the follow-up Record")
	}
	now = now.Add(applyVerifyDelay)
	if !c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("verified hash reported changed")
	}
}

// A real edit before the grace period elapses must reset verification for
// the new hash rather than leave a stale repost pending for the old one.
func TestApplyCache_NewHashResetsVerification(t *testing.T) {
	now := time.Unix(1000, 0)
	c := testCache(t, &now)
	c.Record("ns/c1/0", "hashA")

	now = now.Add(time.Second)
	c.Record("ns/c1/0", "hashB")
	if c.Unchanged("ns/c1/0", "hashA") {
		t.Fatal("superseded hash still reported unchanged")
	}
	if !c.NeedsVerify("ns/c1/0", "hashB") {
		t.Fatal("new hash should start its own unverified grace period")
	}
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
