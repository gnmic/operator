package controller

import (
	"crypto/tls"
	"net/http"
	"sync"
	"time"
)

const (
	// applyRequestTimeout bounds a single config apply to a collector pod.
	applyRequestTimeout = 30 * time.Second

	// idleConnTimeout bounds how long a pooled connection is kept.
	//
	// A hand-built http.Transport leaves this at zero, meaning no limit at all --
	// unlike http.DefaultTransport, which sets 90s. Anything that outlives its
	// cluster would otherwise pin its connections for the life of the process.
	idleConnTimeout = 90 * time.Second
)

// clientKind selects how a cached client treats connections.
//
// The distinction is load-bearing, and was found the hard way: pooling a connection
// and later handing it to the long-lived SSE stream made that stream drop
// intermittently. Every drop invalidates the pod's ApplyCache entry, which makes the
// next reconcile re-POST the configuration, which makes gNMIc reload the target's
// subscription -- so a target would briefly report zero streams for no reason anyone
// asked for. Bisecting the integration suite pinned it to reuse on the streaming
// path specifically: reuse on the apply path is fine.
type clientKind int

const (
	// clientPooled is for short request/response calls -- the config applies. These
	// benefit from keep-alive: without it every apply pays a fresh TLS handshake.
	clientPooled clientKind = iota
	// clientStreaming is for the SSE target-state stream. The client is still cached
	// so transports do not churn, but keep-alive is off: a stream that lasts minutes
	// must never start on a connection that has been sitting in a pool.
	clientStreaming
)

func (k clientKind) timeout() time.Duration {
	if k == clientStreaming {
		// The stream is meant to stay open; the periodic poll bounds itself with a
		// context deadline instead.
		return 0
	}
	return applyRequestTimeout
}

// clientCache hands out one *http.Client per cluster.
//
// Both reconcilers used to construct a fresh http.Transport on every pass and drop
// it. Nothing called CloseIdleConnections and the transports carried no idle timeout,
// so every discarded one kept its sockets open for the life of the process: at one
// apply per cluster per backstop interval, and one per SSE reconnect, that is an
// unbounded file-descriptor leak. Reuse also stops an apply paying for a fresh TLS
// handshake every time.
//
// The client's own certificate is resolved per handshake by TLSMaterial, so a rotated
// keypair never invalidates anything here. Only material the tls.Config captures by
// value -- the trust pool -- carries a revision, and only a change to that rebuilds.
type clientCache struct {
	mu      sync.Mutex
	entries map[string]*cachedClient
}

type cachedClient struct {
	revision  string
	client    *http.Client
	transport *http.Transport
}

func newClientCache() *clientCache {
	return &clientCache{entries: make(map[string]*cachedClient)}
}

// get returns the cached client for key while revision still matches, and otherwise
// calls build to make a replacement.
//
// build is only invoked on a miss: parsing PEM and assembling a cert pool is the
// expensive part, and doing it before consulting the cache -- as the first version of
// this did -- gives up most of what the cache is for.
//
// A nil cache builds every time, so a reconciler constructed without one (tests)
// keeps working rather than panicking.
func (c *clientCache) get(key, revision string, kind clientKind, build func() (*tls.Config, error)) (*http.Client, error) {
	if c != nil {
		c.mu.Lock()
		if entry, ok := c.entries[key]; ok && entry.revision == revision {
			defer c.mu.Unlock()
			return entry.client, nil
		}
		c.mu.Unlock()
	}

	tlsConfig, err := build()
	if err != nil {
		return nil, err
	}
	client, transport := newTLSClient(tlsConfig, kind)
	if c == nil {
		return client, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]*cachedClient)
	}
	if previous, ok := c.entries[key]; ok {
		// Let the connections built on the old trust pool go rather than leaving
		// them open against material this operator no longer accepts.
		previous.transport.CloseIdleConnections()
	}
	c.entries[key] = &cachedClient{revision: revision, client: client, transport: transport}
	return client, nil
}

// evict drops one cluster's client and closes what it still holds. Called when a
// Cluster goes away, so entries do not accumulate for clusters that no longer exist.
func (c *clientCache) evict(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[key]; ok {
		entry.transport.CloseIdleConnections()
		delete(c.entries, key)
	}
}

func newTLSClient(tlsConfig *tls.Config, kind clientKind) (*http.Client, *http.Transport) {
	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
		IdleConnTimeout: idleConnTimeout,
		// Off for streaming: see clientKind. This prevents *reuse* of a connection
		// once a request finishes; it does not shorten the request itself, so the
		// SSE stream still stays open for as long as the server holds it.
		DisableKeepAlives: kind == clientStreaming,
	}
	return &http.Client{Timeout: kind.timeout(), Transport: transport}, transport
}
