package controller

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gnmic/operator/internal/gnmic"
)

// caReloadBackstop re-reads the CA even without an fsnotify event.
//
// A missed event would otherwise leave the operator trusting a retired CA
// indefinitely, and the read is a few KB off local disk.
const caReloadBackstop = time.Minute

// TLSMaterial owns the operator's own TLS material — the client certificate it
// presents to collector pods, and the CA bundle it trusts — and keeps both current
// from disk so nothing on the reconcile path has to read them.
//
// Before this, every call to createHTTPClient re-read both files and re-parsed the
// keypair, purely so a rotation would eventually be noticed. That made rotation
// detection a side effect of polling: nothing watched the files, so a new certificate
// took up to a full reconcile interval to apply, and every reconcile in between paid
// for the check. Both files are mounted Secrets, so fsnotify sees the change the
// moment kubelet swaps them in.
type TLSMaterial struct {
	certWatcher *certwatcher.CertWatcher
	caPath      string

	mu    sync.RWMutex
	caPEM []byte
	// caRevision changes whenever the CA bytes do. Clients built from an older
	// revision are rebuilt; the value itself is not meaningful.
	caRevision string
}

// NewTLSMaterial prepares the watchers. Missing files are not an error: an operator
// running without TLS to its collectors has nothing to watch, and every accessor
// degrades to "no material".
func NewTLSMaterial() *TLSMaterial {
	m := &TLSMaterial{caPath: gnmic.GetControllerCAPath()}

	certPath, keyPath := gnmic.GetControllerCertPath(), gnmic.GetControllerKeyPath()
	if fileExists(certPath) && fileExists(keyPath) {
		// certwatcher reads once up front, so a construction error here means the
		// material on disk is unusable rather than absent.
		cw, err := certwatcher.New(certPath, keyPath)
		if err != nil {
			log.Log.Error(err, "controller client certificate is present but unusable; continuing without one",
				"cert", certPath, "key", keyPath)
		} else {
			m.certWatcher = cw
		}
	}

	m.reloadCA(context.Background())
	return m
}

// Start runs the watchers until ctx is cancelled. It satisfies manager.Runnable.
func (m *TLSMaterial) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("tls-material")

	if m.certWatcher != nil {
		go func() {
			if err := m.certWatcher.Start(ctx); err != nil {
				logger.Error(err, "client certificate watcher stopped")
			}
		}()
	}
	return m.watchCA(ctx, logger)
}

// NeedLeaderElection reports false: both files are local to this pod, and a standby
// replica still needs its own material current.
func (m *TLSMaterial) NeedLeaderElection() bool { return false }

// ClientCertificate returns a callback for tls.Config.GetClientCertificate, or nil
// when no certificate is configured.
//
// Resolving the certificate per handshake rather than per client is what removes
// rotation from the reconcile path entirely: a rotated keypair is picked up by the
// next handshake without rebuilding anything.
func (m *TLSMaterial) ClientCertificate() func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	if m == nil || m.certWatcher == nil {
		return nil
	}
	return func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		return m.certWatcher.GetCertificate(nil)
	}
}

// CA returns the current CA bundle and a revision that changes with it.
func (m *TLSMaterial) CA() (pem []byte, revision string) {
	if m == nil {
		return nil, ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.caPEM, m.caRevision
}

// watchCA follows the directory rather than the file: kubelet updates a mounted
// Secret by swapping a `..data` symlink, which arrives as a rename of the directory
// entry and never as a write to the path being watched.
func (m *TLSMaterial) watchCA(ctx context.Context, logger logr.Logger) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	dir := filepath.Dir(m.caPath)
	if err := watcher.Add(dir); err != nil {
		// The directory may not exist when TLS is not configured. The backstop
		// still picks the file up if it appears later.
		logger.Error(err, "cannot watch the CA directory; falling back to periodic reload", "dir", dir)
	}

	ticker := time.NewTicker(caReloadBackstop)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			m.reloadCA(ctx)
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			logger.Error(err, "CA watch error")
		case <-ticker.C:
			m.reloadCA(ctx)
		}
	}
}

// reloadCA re-reads the CA and updates the revision only when the bytes changed, so
// an unchanged file never invalidates a cached client.
func (m *TLSMaterial) reloadCA(ctx context.Context) {
	pem, err := os.ReadFile(m.caPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.FromContext(ctx).Error(err, "failed to read the controller CA", "path", m.caPath)
		}
		pem = nil
	}

	sum := sha256.Sum256(pem)
	revision := hex.EncodeToString(sum[:])

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.caRevision == revision {
		return
	}
	m.caPEM = pem
	m.caRevision = revision
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
