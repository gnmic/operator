//go:build integration

package harness

import (
	"bytes"
	"fmt"
	"text/template"
)

// baseVars are available to every fixture template. Target addresses embed the
// suite namespace, which is why fixtures are templates rather than static files.
func (k *K8s) baseVars() map[string]any {
	return map[string]any{
		"Namespace":    k.Namespace,
		"GnmiGenHost":  GnmiGenHost(k.Namespace),
		"GnmiGenImage": GnmiGenImage(),
		"GnmicImage":   GnmicImage(),
	}
}

// render executes a fixture template with the suite variables plus any
// per-call overrides.
//
// Missing keys are an error rather than an empty string: a typo in a fixture
// should surface as a failure, not as a Target with no address.
func (k *K8s) render(text string, vars map[string]any) (string, error) {
	data := k.baseVars()
	for key, v := range vars {
		data[key] = v
	}
	tmpl, err := template.New("fixture").Option("missingkey=error").Parse(text)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering template: %w", err)
	}
	return buf.String(), nil
}

// GnmiGenHost is the in-cluster DNS name of a suite's simulator. Because the
// Service is headless this resolves to the pod IP, so any simulated target port
// is reachable without being declared on the Service.
func GnmiGenHost(namespace string) string {
	return fmt.Sprintf("gnmi-gen.%s.svc.cluster.local", namespace)
}

// GnmiGenAddress is the dial address of simulated target n, 0-based.
func GnmiGenAddress(namespace string, n int) string {
	return fmt.Sprintf("%s:%d", GnmiGenHost(namespace), 57400+n)
}

func GnmiGenImage() string {
	return getenv("GNMIGEN_IMAGE", "registry.kmrd.dev/gnmic/gnmigen:0.0.0")
}

func GnmicImage() string {
	return getenv("GNMIC_IMAGE", "ghcr.io/openconfig/gnmic:0.46.0")
}
