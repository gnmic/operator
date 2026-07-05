package gnmic

import (
	"bufio"
	"io"
	"os"
	"strings"
)

const defaultClusterDomain = "cluster.local"

// clusterDomain is resolved once at process start from /etc/resolv.conf's
// search domains (falling back to defaultClusterDomain).
//
// The operator builds in-cluster FQDNs for gNMIc pods
// (<pod>.<service>.<namespace>.svc.<cluster-domain>) to reach their REST
// API (config-apply, SSE target-state streaming) and to build cert SANs.
// These were previously hardcoded to "cluster.local", which breaks every
// one of those lookups on any cluster configured with a different DNS
// domain (kubelet --cluster-domain) -- confirmed as the root cause of
// persistent "no such host" errors on a cluster using a custom domain.
var clusterDomain = detectClusterDomain()

// ClusterDomain returns the cluster's DNS domain (e.g. "cluster.local"),
// used to build in-cluster FQDNs for gNMIc pods. Can be overridden with
// the CLUSTER_DOMAIN environment variable for clusters where
// auto-detection from /etc/resolv.conf isn't reliable.
func ClusterDomain() string {
	if v := os.Getenv("CLUSTER_DOMAIN"); v != "" {
		return v
	}
	return clusterDomain
}

func detectClusterDomain() string {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return defaultClusterDomain
	}
	defer f.Close()
	return parseClusterDomain(f)
}

// parseClusterDomain extracts the cluster domain from resolv.conf content
// (split out from detectClusterDomain so the parsing logic is unit-testable
// without depending on the host's actual /etc/resolv.conf).
func parseClusterDomain(r io.Reader) string {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[0] != "search" {
			continue
		}
		// Kubernetes orders search domains most-specific to least-specific, e.g.:
		//   <namespace>.svc.<cluster-domain> svc.<cluster-domain> <cluster-domain>
		// the "svc.<cluster-domain>" entry gives us the domain directly;
		// fall back to the shortest entry if it's absent for some reason.
		var shortest string
		for _, d := range fields[1:] {
			if strings.HasPrefix(d, "svc.") {
				return strings.TrimPrefix(d, "svc.")
			}
			if shortest == "" || len(d) < len(shortest) {
				shortest = d
			}
		}
		if shortest != "" {
			return shortest
		}
	}
	return defaultClusterDomain
}
