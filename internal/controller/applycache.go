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

// applyVerifyDelay is how soon to re-POST the same payload after the first
// successful apply. gNMIc's config/apply can ACK before every Subscribe stream
// has re-established; without a second pass, ApplyCache would short-circuit
// until applyRefreshInterval and leave targets with paths=[] for minutes.
const applyVerifyDelay = 2 * time.Second

// ApplyCache records what was last successfully applied to each collector pod,
// so a reconcile that changes nothing does not re-POST the whole configuration
// to every pod.
//
// It is deliberately in-memory and unshared between operator instances: losing
// it costs one redundant apply per pod, which is the safe direction to fail.
// Persisting it would make a stale entry survive the restart that might have
// been the only thing left to clear it.
//
// Each hash is applied twice before the short-circuit engages: the first POST
// installs the plan, the second (after applyVerifyDelay) recovers targets that
// missed the reconnect window. Only then is the record marked verified.
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

// Unchanged reports whether the pod already holds this exact configuration,
// the record has been verified by a second successful apply, and the record
// is still inside the refresh interval.
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
	if !ok || rec.hash != hash || !rec.verified {
		return false
	}
	return c.timeNow().Sub(rec.at) < c.ttl
}

// Record marks a configuration as successfully POSTed to a pod. The first
// Record for a hash leaves the entry unverified so the next reconcile re-applies
// once; the second Record of the same hash marks it verified and enables the
// short-circuit. It must only be called after the POST succeeds.
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
// reconciler should requeue for the follow-up apply.
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
