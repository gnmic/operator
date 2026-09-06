package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"sort"
	"strconv"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/discovery/provider"
)

// Desired is one Target the controller wants to exist.
type Desired struct {
	// Name is the normalised resource name.
	Name string
	// Address is host:port as the Target spec wants it.
	Address string
	// Profile is the TargetProfile name.
	Profile string
	// Labels is the full, valid label set including the reserved ones.
	Labels map[string]string
	// Annotations carries the discovered name and any rewritten-label originals.
	Annotations map[string]string
	// Hash summarises everything above. It is stored on the Target so an
	// unchanged device costs no write on the next run.
	Hash string
}

// BuildResult is the outcome of turning devices into the desired set.
type BuildResult struct {
	Desired []Desired
	// Invalid lists devices that did not become a Target, with the reason.
	Invalid []provider.Failure
	// Sanitized counts devices whose labels had to be rewritten.
	Sanitized int
}

// Build turns devices into the desired Target set in a fixed order: defaults,
// labels, sanitise, name, dedupe. Devices are sorted first so that when two
// of them collide on a name, the winner does not depend on the order the
// source happened to return them in.
func Build(ts *gnmicv1alpha1.TargetSource, devices []provider.Device) BuildResult {
	sorted := make([]provider.Device, len(devices))
	copy(sorted, devices)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].Address < sorted[j].Address
	})

	var out BuildResult
	seen := make(map[string]struct{}, len(sorted))
	for _, d := range sorted {
		desired, fail, sanitized := buildOne(ts, d)
		if fail != nil {
			out.Invalid = append(out.Invalid, *fail)
			continue
		}
		if _, dup := seen[desired.Name]; dup {
			out.Invalid = append(out.Invalid, provider.Failure{Name: d.Name, Reason: "DuplicateName"})
			continue
		}
		seen[desired.Name] = struct{}{}
		if sanitized {
			out.Sanitized++
		}
		out.Desired = append(out.Desired, desired)
	}
	return out
}

func buildOne(ts *gnmicv1alpha1.TargetSource, d provider.Device) (Desired, *provider.Failure, bool) {
	if d.Name == "" {
		return Desired{}, &provider.Failure{Name: d.Address, Reason: "MissingName"}, false
	}
	if d.Address == "" {
		return Desired{}, &provider.Failure{Name: d.Name, Reason: "MissingAddress"}, false
	}
	port := d.Port
	if port == 0 {
		port = ts.Spec.Target.Port
	}
	if port == 0 {
		port = 57400
	}
	address, ok := hostPort(d.Address, port)
	if !ok {
		return Desired{}, &provider.Failure{Name: d.Name, Reason: "InvalidAddress"}, false
	}
	profile := d.Profile
	if profile == "" {
		profile = ts.Spec.Target.Profile
	}
	if profile == "" {
		return Desired{}, &provider.Failure{Name: d.Name, Reason: "NoProfile"}, false
	}
	name, ok := TargetName(ts.Name, d.Name)
	if !ok {
		return Desired{}, &provider.Failure{Name: d.Name, Reason: "InvalidName"}, false
	}

	// Lowest precedence first: template labels, then the source's, then the
	// reserved operator labels which always win.
	merged := make(map[string]string, len(ts.Spec.Target.Labels)+len(d.Labels)+2)
	maps.Copy(merged, ts.Spec.Target.Labels)
	maps.Copy(merged, d.Labels)
	san := SanitizeLabels(merged)
	san.Labels[LabelTargetSource] = ts.Name
	san.Labels[LabelManagedBy] = LabelManagedByValue

	annotations := map[string]string{AnnotationDiscoveredName: d.Name}
	maps.Copy(annotations, san.Annotations)

	desired := Desired{
		Name:        name,
		Address:     address,
		Profile:     profile,
		Labels:      san.Labels,
		Annotations: annotations,
	}
	desired.Hash = hashDesired(desired)
	return desired, nil, san.Changed
}

// hostPort joins an address and port unless the address already carries a
// valid port, in which case it is kept as-is.
func hostPort(address string, port int32) (string, bool) {
	if host, p, err := net.SplitHostPort(address); err == nil && host != "" {
		if n, err := strconv.Atoi(p); err == nil && n >= 1 && n <= 65535 {
			return address, true
		}
		return "", false
	}
	if port < 1 || port > 65535 {
		return "", false
	}
	return net.JoinHostPort(address, strconv.Itoa(int(port))), true
}

// hashDesired summarises the fields the controller applies. Map keys are
// sorted by the JSON encoder, so equal content gives equal hashes.
func hashDesired(d Desired) string {
	payload, _ := json.Marshal(struct {
		Address     string            `json:"address"`
		Profile     string            `json:"profile"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	}{d.Address, d.Profile, d.Labels, d.Annotations})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])[:16]
}

// Digest is a stable, order-independent hash of a device list, for
// status.sourceDigest. It is informational; the controller does not branch on it.
func Digest(devices []provider.Device) string {
	sorted := make([]provider.Device, len(devices))
	copy(sorted, devices)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].Address < sorted[j].Address
	})
	h := sha256.New()
	for _, d := range sorted {
		labels, _ := json.Marshal(d.Labels)
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00%s\x00%s\n", d.Name, d.Address, d.Port, d.Profile, labels)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
