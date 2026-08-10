//go:build integration

package harness

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var labelValueRE = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)="([^"]*)"`)

// Scrape fetches a metrics endpoint. url is typically a port-forward base plus
// path, e.g. http://127.0.0.1:12345/metrics.
func Scrape(t *testing.T, url string) string {
	t.Helper()
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("scraping %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading scrape body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("scraping %s: %s: %s", url, resp.Status, string(body))
	}
	return string(body)
}

// WaitMetrics polls until the endpoint answers 200 with at least one sample.
func WaitMetrics(t *testing.T, url string) {
	t.Helper()
	Wait(t, Medium, "metrics endpoint answering", func() (bool, string) {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			return false, err.Error()
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return false, resp.Status
		}
		if SampleCount(string(body)) == 0 {
			return false, "no samples yet"
		}
		return true, ""
	})
}

// SampleCount is the number of non-comment, non-empty lines in a scrape body.
func SampleCount(body string) int {
	n := 0
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n++
	}
	return n
}

// HasLabel reports whether body contains the label fragment, e.g. source="dev-1".
func HasLabel(body, fragment string) bool {
	return strings.Contains(body, fragment)
}

// LabelValues returns sorted unique values of label across the scrape body.
func LabelValues(body, label string) []string {
	seen := map[string]struct{}{}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, m := range labelValueRE.FindAllStringSubmatch(line, -1) {
			if m[1] == label {
				seen[m[2]] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// WaitHasLabel polls until a scrape of url contains fragment.
func WaitHasLabel(t *testing.T, url, fragment string) {
	t.Helper()
	Wait(t, Medium, fmt.Sprintf("metrics to contain %s", fragment), func() (bool, string) {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			return false, err.Error()
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return false, resp.Status
		}
		if HasLabel(string(body), fragment) {
			return true, ""
		}
		return false, fmt.Sprintf("%d samples, label absent", SampleCount(string(body)))
	})
}

// WaitLacksLabel polls until a scrape of url no longer contains fragment.
func WaitLacksLabel(t *testing.T, url, fragment string) {
	t.Helper()
	Wait(t, Medium, fmt.Sprintf("metrics to lack %s", fragment), func() (bool, string) {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			return false, err.Error()
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return false, resp.Status
		}
		if !HasLabel(string(body), fragment) {
			return true, ""
		}
		return false, "still present"
	})
}

// PromListenPort returns the Service port for a Prometheus output Service.
func (k *K8s) PromListenPort(t *testing.T, service string) int {
	t.Helper()
	var svc corev1.Service
	if err := k.Client.Get(k.Ctx, types.NamespacedName{Namespace: k.Namespace, Name: service}, &svc); err != nil {
		t.Fatalf("getting prometheus service %s: %v", service, err)
	}
	if len(svc.Spec.Ports) == 0 {
		t.Fatalf("prometheus service %s has no ports", service)
	}
	return int(svc.Spec.Ports[0].Port)
}

// ScrapeClusterPrometheus scrapes /metrics on every ready collector pod for
// the cluster (multi-replica outputs cannot be covered by a single Service
// forward) and returns the concatenated body.
func (k *K8s) ScrapeClusterPrometheus(t *testing.T, cluster, pipeline, output string) string {
	t.Helper()
	body, err := k.ScrapeClusterPrometheusQuiet(cluster, pipeline, output)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// ScrapeClusterPrometheusQuiet is the non-fatal form for polling helpers.
func (k *K8s) ScrapeClusterPrometheusQuiet(cluster, pipeline, output string) (string, error) {
	return k.scrapeClusterPrometheus(cluster, pipeline, output)
}

func (k *K8s) scrapeClusterPrometheus(cluster, pipeline, output string) (string, error) {
	svc := PromServiceName(cluster, pipeline, output)
	var svcObj corev1.Service
	if err := k.Client.Get(k.Ctx, types.NamespacedName{Namespace: k.Namespace, Name: svc}, &svcObj); err != nil {
		return "", fmt.Errorf("getting prometheus service %s: %w", svc, err)
	}
	if len(svcObj.Spec.Ports) == 0 {
		return "", fmt.Errorf("prometheus service %s has no ports", svc)
	}
	port := int(svcObj.Spec.Ports[0].Port)

	var list corev1.PodList
	if err := k.Client.List(k.Ctx, &list,
		client.InNamespace(k.Namespace),
		client.MatchingLabels{LabelClusterName: cluster}); err != nil {
		return "", fmt.Errorf("listing collector pods: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	var b strings.Builder
	scraped := 0
	for i := range list.Items {
		pod := &list.Items[i]
		if !podReady(pod) {
			continue
		}
		fw, err := k.ForwardPodPort(pod.Name, port)
		if err != nil {
			return "", fmt.Errorf("port-forward %s:%d: %w", pod.Name, port, err)
		}
		resp, err := client.Get(fw.URL + "/metrics")
		if err != nil {
			fw.Close()
			return "", fmt.Errorf("scraping %s: %w", fw.URL+"/metrics", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		fw.Close()
		if err != nil {
			return "", err
		}
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("scraping %s: %s", fw.URL+"/metrics", resp.Status)
		}
		b.Write(body)
		b.WriteByte('\n')
		scraped++
	}
	if scraped == 0 {
		return "", fmt.Errorf("no ready collector pods to scrape for %s", svc)
	}
	return b.String(), nil
}

// WaitClusterPrometheusSources waits until the union of source label values
// across all collector pods includes every want fragment (substring match).
func (k *K8s) WaitClusterPrometheusSources(t *testing.T, cluster, pipeline, output string, want []string, timeout time.Duration) {
	t.Helper()
	Wait(t, timeout, fmt.Sprintf("prometheus sources for %d targets", len(want)), func() (bool, string) {
		body, err := k.ScrapeClusterPrometheusQuiet(cluster, pipeline, output)
		if err != nil {
			return false, err.Error()
		}
		sources := LabelValues(body, "source")
		missing := 0
		for _, w := range want {
			found := false
			for _, src := range sources {
				if strings.Contains(src, w) {
					found = true
					break
				}
			}
			if !found && !HasLabel(body, w) {
				missing++
			}
		}
		if missing == 0 {
			return true, ""
		}
		return false, fmt.Sprintf("missing=%d sources=%d samples=%d", missing, len(sources), SampleCount(body))
	})
}
