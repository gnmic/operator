package provider

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/google/cel-go/cel"
	"sigs.k8s.io/yaml"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// Parse turns one document into devices. The document may be JSON or YAML.
//
// With no mapping the document must be the native list: an array of objects
// with name, address, port, profile and labels, of which name and address
// are required. With a mapping the document is whatever the source returns
// and the expressions say how to read it.
//
// Items that cannot be read are returned in the second value rather than
// dropped, unless mapping.onError is Fail, in which case the first such item
// fails the whole parse.
func Parse(data []byte, mapping *gnmicv1alpha1.MappingSpec) ([]Device, []Failure, error) {
	raw, err := Decode(data)
	if err != nil {
		return nil, nil, err
	}
	return Extract(raw, mapping)
}

// Decode reads a JSON or YAML document into generic Go values. An empty
// document decodes to nil.
func Decode(data []byte) (any, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	jsonData, err := yaml.YAMLToJSON(data)
	if err != nil {
		return nil, fmt.Errorf("document is not valid JSON or YAML: %w", err)
	}
	var raw any
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		return nil, fmt.Errorf("document is not valid JSON or YAML: %w", err)
	}
	return raw, nil
}

// Extract reads devices out of a decoded document. See Parse.
func Extract(raw any, mapping *gnmicv1alpha1.MappingSpec) ([]Device, []Failure, error) {
	if mapping == nil {
		return extractNative(raw)
	}
	return extractMapped(raw, mapping)
}

// extractNative reads the operator's own shape.
func extractNative(raw any) ([]Device, []Failure, error) {
	items, err := asList(raw)
	if err != nil {
		return nil, nil, err
	}
	devices := make([]Device, 0, len(items))
	var failures []Failure
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			failures = append(failures, Failure{Name: fmt.Sprintf("#%d", i), Reason: "NotAnObject"})
			continue
		}
		d, fail := deviceFromFields(
			obj["name"], obj["address"], obj["port"], obj["profile"], obj["labels"],
			fmt.Sprintf("#%d", i),
		)
		if fail != nil {
			failures = append(failures, *fail)
			continue
		}
		devices = append(devices, d)
	}
	return devices, failures, nil
}

// compiledMapping holds the programs for one MappingSpec. A nil program means
// the field falls back to the item's key of the same name.
type compiledMapping struct {
	items, name, address, port, profile, labels cel.Program
}

// compileMapping compiles every expression in a MappingSpec. A compile error
// is a spec error: the TargetSource has to change.
func compileMapping(m *gnmicv1alpha1.MappingSpec) (*compiledMapping, error) {
	cm := &compiledMapping{}
	fields := []struct {
		name string
		expr string
		dst  *cel.Program
	}{
		{"items", m.Items, &cm.items},
		{"name", m.Name, &cm.name},
		{"address", m.Address, &cm.address},
		{"port", m.Port, &cm.port},
		{"profile", m.Profile, &cm.profile},
		{"labels", m.Labels, &cm.labels},
	}
	for _, f := range fields {
		if f.expr == "" {
			continue
		}
		prog, err := CompileExpression(f.expr)
		if err != nil {
			return nil, Specf("mapping.%s: %w", f.name, err)
		}
		*f.dst = prog
	}
	return cm, nil
}

// extractMapped reads an arbitrary document through a MappingSpec.
func extractMapped(raw any, mapping *gnmicv1alpha1.MappingSpec) ([]Device, []Failure, error) {
	cm, err := compileMapping(mapping)
	if err != nil {
		return nil, nil, err
	}
	var items []any
	if cm.items != nil {
		out, err := evalExpression(cm.items, raw, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("mapping.items: %w", err)
		}
		items, err = asList(out)
		if err != nil {
			return nil, nil, fmt.Errorf("mapping.items: %w", err)
		}
	} else {
		items, err = asList(raw)
		if err != nil {
			return nil, nil, err
		}
	}

	failOnError := mapping.OnError == gnmicv1alpha1.MappingErrorFail
	devices := make([]Device, 0, len(items))
	var failures []Failure
	for i, item := range items {
		fallback := fmt.Sprintf("#%d", i)
		obj, ok := item.(map[string]any)
		if !ok {
			if failOnError {
				return nil, nil, fmt.Errorf("item %s is not an object", fallback)
			}
			failures = append(failures, Failure{Name: fallback, Reason: "NotAnObject"})
			continue
		}
		d, fail := mapItem(obj, raw, cm, fallback)
		if fail != nil {
			if failOnError {
				return nil, nil, fmt.Errorf("item %s: %s", fail.Name, fail.Reason)
			}
			failures = append(failures, *fail)
			continue
		}
		devices = append(devices, d)
	}
	return devices, failures, nil
}

// mapItem evaluates every field expression for one item, falling back to the
// item's own key when no expression is set.
func mapItem(obj map[string]any, self any, cm *compiledMapping, fallback string) (Device, *Failure) {
	get := func(field string, prog cel.Program, key string) (any, error) {
		if prog == nil {
			return obj[key], nil
		}
		v, err := evalExpression(prog, self, obj)
		if err != nil {
			return nil, fmt.Errorf("mapping.%s: %w", field, err)
		}
		return v, nil
	}
	name, err := get("name", cm.name, "name")
	if err != nil {
		return Device{}, &Failure{Name: fallback, Reason: reasonFor(err)}
	}
	// From here on the item has a name to report failures under, when it is a string.
	if s, ok := name.(string); ok && s != "" {
		fallback = s
	}
	address, err := get("address", cm.address, "address")
	if err != nil {
		return Device{}, &Failure{Name: fallback, Reason: reasonFor(err)}
	}
	port, err := get("port", cm.port, "port")
	if err != nil {
		return Device{}, &Failure{Name: fallback, Reason: reasonFor(err)}
	}
	profile, err := get("profile", cm.profile, "profile")
	if err != nil {
		return Device{}, &Failure{Name: fallback, Reason: reasonFor(err)}
	}
	labels, err := get("labels", cm.labels, "labels")
	if err != nil {
		return Device{}, &Failure{Name: fallback, Reason: reasonFor(err)}
	}
	return deviceFromFields(name, address, port, profile, labels, fallback)
}

// reasonFor turns an evaluation error into a short status reason that still
// names the field, so "MappingError(address)" tells the reader where to look.
func reasonFor(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ":"); i > 0 && strings.HasPrefix(msg, "mapping.") {
		return "MappingError(" + strings.TrimPrefix(msg[:i], "mapping.") + ")"
	}
	return "MappingError"
}

// deviceFromFields validates and converts the raw field values of one item.
func deviceFromFields(name, address, port, profile, labels any, fallback string) (Device, *Failure) {
	d := Device{}
	s, ok := name.(string)
	if !ok || s == "" {
		return d, &Failure{Name: fallback, Reason: "MissingName"}
	}
	d.Name = s
	s, ok = address.(string)
	if !ok || s == "" {
		return d, &Failure{Name: d.Name, Reason: "MissingAddress"}
	}
	d.Address = s
	p, ok := portOf(port)
	if !ok {
		return d, &Failure{Name: d.Name, Reason: "InvalidPort"}
	}
	d.Port = p
	if profile != nil {
		s, ok = profile.(string)
		if !ok {
			return d, &Failure{Name: d.Name, Reason: "InvalidProfile"}
		}
		d.Profile = s
	}
	if labels != nil {
		m, ok := labels.(map[string]any)
		if !ok {
			return d, &Failure{Name: d.Name, Reason: "InvalidLabels"}
		}
		d.Labels = make(map[string]string, len(m))
		for k, v := range m {
			d.Labels[k] = fmt.Sprintf("%v", v)
		}
	}
	return d, nil
}

// portOf reads a port from the number or numeric string a document is likely
// to carry. nil, 0 and "" mean unset, so the template default applies;
// anything else must be 1..65535.
func portOf(v any) (int32, bool) {
	var n int64
	switch p := v.(type) {
	case nil:
		return 0, true
	case float64:
		if p != math.Trunc(p) {
			return 0, false
		}
		n = int64(p)
	case int64:
		n = p
	case int:
		n = int64(p)
	case uint64:
		if p > math.MaxInt64 {
			return 0, false
		}
		n = int64(p)
	case string:
		if p == "" {
			return 0, true
		}
		parsed, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return 0, false
		}
		n = parsed
	default:
		return 0, false
	}
	if n == 0 {
		return 0, true
	}
	if n < 1 || n > 65535 {
		return 0, false
	}
	return int32(n), true
}

// asList requires a decoded value to be a list. nil is an empty list, which
// is what an empty document or an empty API response decodes to.
func asList(v any) ([]any, error) {
	if v == nil {
		return nil, nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("document must be a list of devices, got %T", v)
	}
	return items, nil
}
