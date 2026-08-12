package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// applyRefreshInterval bounds how long the short-circuit will trust its own
// record of what a pod holds.
//
// The cache is invalidated by evidence — an apply failure, or the pod's SSE
// stream dropping — but a pod can also lose its configuration without either:
// a gNMIc bug, or somebody calling DELETE on its REST API. Nothing tells the
// operator that happened, and gNMIc exposes no cheap way to ask (GET
// /api/v1/config returns the entire configuration, so verifying costs as much
// as re-applying). Re-applying unconditionally past this interval bounds that
// blind spot without giving up the reduction: a reconcile storm still collapses
// to one apply per pod per interval.
const applyRefreshInterval = 5 * time.Minute

// applyVerifyDelay is the grace period before a safety-net re-POST of an
// unverified hash. gNMIc's config/apply can ACK before every Subscribe stream
// has re-established, and occasionally one never comes back on its own; a
// second POST of the same config reliably kicks it loose. The delay has to
// clear two things: every unrelated reconcile trigger that fires in the
// meantime (Target status, Secret changes, periodic resync — all check
// Unchanged and must not treat a still-settling apply as stale), and every
// test's own tight "stays stable" window, so the safety net never lands mid
// assertion. Firing it immediately (as a first attempt did) instead doubled
// every apply back-to-back and made pods work harder to reconnect right when
// they were already struggling, which was worse than the bug it targeted.
const applyVerifyDelay = 30 * time.Second

// ApplyCache records what was last successfully applied to each collector pod,
// so a reconcile that changes nothing does not re-POST the whole configuration
// to every pod.
//
// It is deliberately in-memory and unshared between operator instances: losing
// it costs one redundant apply per pod, which is the safe direction to fail.
// Persisting it would make a stale entry survive the restart that might have
// been the only thing left to clear it.
type ApplyCache struct {
	mu      sync.Mutex
	entries map[string]applyRecord
	ttl     time.Duration
	// now is swappable for tests; nil means time.Now.
	now func() time.Time
}

type applyRecord struct {
	hash     string
	at       time.Time
	verified bool
}

// NewApplyCache returns a cache using the default refresh interval.
func NewApplyCache() *ApplyCache {
	return &ApplyCache{entries: make(map[string]applyRecord), ttl: applyRefreshInterval}
}

func (c *ApplyCache) timeNow() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// Unchanged reports whether the pod already holds this exact configuration and
// no apply is due right now.
//
// An unverified hash still reports unchanged until applyVerifyDelay elapses —
// otherwise every unrelated reconcile in that window would re-POST the same
// config, compounding the very reconnect problem the verify pass exists to
// fix. Once the grace period passes with no newer hash recorded, the next
// reconcile (organic or the deliberate requeue below) is due its one
// safety-net repost.
//
// A nil cache always reports false, so a reconciler constructed without one
// (tests, the API server) applies unconditionally rather than silently skipping.
func (c *ApplyCache) Unchanged(key, hash string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	rec, ok := c.entries[key]
	if !ok || rec.hash != hash {
		return false
	}
	age := c.timeNow().Sub(rec.at)
	if !rec.verified && age >= applyVerifyDelay {
		return false
	}
	return age < c.ttl
}

// Record marks a configuration as successfully POSTed to a pod. The first
// Record for a hash leaves the entry unverified; if Unchanged later reports
// changed because the grace period elapsed, the resulting repost's Record
// call marks it verified so the short-circuit engages. It must only be called
// after the POST succeeds: recording an attempt would make a failed apply
// look applied until the plan changes again.
func (c *ApplyCache) Record(key, hash string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.timeNow()
	if rec, ok := c.entries[key]; ok && rec.hash == hash && !rec.verified {
		c.entries[key] = applyRecord{hash: hash, at: now, verified: true}
		return
	}
	c.entries[key] = applyRecord{hash: hash, at: now, verified: false}
}

// NeedsVerify reports whether key holds an unverified record for hash, so the
// reconciler should requeue after applyVerifyDelay for the follow-up POST.
func (c *ApplyCache) NeedsVerify(key, hash string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	rec, ok := c.entries[key]
	return ok && rec.hash == hash && !rec.verified
}

// Invalidate drops one pod's record, forcing the next reconcile to re-apply to
// that pod alone.
func (c *ApplyCache) Invalidate(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// InvalidateCluster drops every pod record belonging to a cluster. Used when
// the cluster is deleted, so entries do not accumulate for clusters that no
// longer exist.
func (c *ApplyCache) InvalidateCluster(namespace, name string) {
	if c == nil {
		return
	}
	prefix := namespace + "/" + name + "/"
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if strings.HasPrefix(key, prefix) {
			delete(c.entries, key)
		}
	}
}

// fingerprint hashes the exact bytes that go on the wire rather than the plan
// struct, so two structs that marshal identically compare equal and any change
// in what is actually sent is a change here.
func fingerprint(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
