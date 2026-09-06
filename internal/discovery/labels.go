package discovery

import (
	"regexp"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

var (
	invalidLabelChars  = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	invalidPrefixChars = regexp.MustCompile(`[^a-z0-9.-]+`)
	nonAlnumEdges      = regexp.MustCompile(`^[^A-Za-z0-9]+|[^A-Za-z0-9]+$`)
)

// SanitizedLabels is the outcome of making a label set valid.
type SanitizedLabels struct {
	// Labels is the valid set.
	Labels map[string]string
	// Annotations records the original of every key or value that changed,
	// under AnnotationLabelPrefix, so nothing is lost.
	Annotations map[string]string
	// Dropped lists keys that could not be made valid at all.
	Dropped []string
	// Changed is true when any key or value was rewritten.
	Changed bool
}

// SanitizeLabels makes every key and value a valid Kubernetes label. Invalid
// characters become "-", values are truncated to 63 characters, and a key
// that is still invalid afterwards is dropped. There is no policy field: one
// behaviour, always reported.
func SanitizeLabels(in map[string]string) SanitizedLabels {
	out := SanitizedLabels{Labels: make(map[string]string, len(in))}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic collisions
	for _, key := range keys {
		value := in[key]
		newKey := sanitizeLabelKey(key)
		if newKey == "" {
			out.Dropped = append(out.Dropped, key)
			out.Changed = true
			continue
		}
		newValue := sanitizeLabelValue(value)
		if _, taken := out.Labels[newKey]; taken {
			// Two keys collapsed onto one; the sorted order makes the winner stable.
			out.Dropped = append(out.Dropped, key)
			out.Changed = true
			continue
		}
		out.Labels[newKey] = newValue
		if newKey != key || newValue != value {
			out.Changed = true
			if out.Annotations == nil {
				out.Annotations = make(map[string]string)
			}
			original := value
			if newKey != key {
				original = key + "=" + value
			}
			out.Annotations[LabelOriginalAnnotation(newKey)] = original
		}
	}
	return out
}

// LabelOriginalAnnotation is the annotation key holding the original of a
// rewritten label. A "/" in the label key becomes "." so the annotation key
// keeps a single prefix.
func LabelOriginalAnnotation(labelKey string) string {
	name := strings.ReplaceAll(labelKey, "/", ".")
	limit := 63 - len("label.")
	if len(name) > limit {
		name = strings.TrimRight(name[:limit], ".-_")
	}
	return AnnotationLabelPrefix + name
}

func sanitizeLabelKey(key string) string {
	prefix, name := "", key
	if i := strings.LastIndex(key, "/"); i >= 0 {
		prefix, name = key[:i], key[i+1:]
	}
	name = invalidLabelChars.ReplaceAllString(name, "-")
	if len(name) > 63 {
		name = name[:63]
	}
	name = nonAlnumEdges.ReplaceAllString(name, "")
	if name == "" {
		return ""
	}
	if prefix != "" {
		prefix = strings.ToLower(prefix)
		prefix = invalidPrefixChars.ReplaceAllString(prefix, "-")
		prefix = strings.Trim(prefix, "-.")
		if len(prefix) > 253 {
			prefix = strings.TrimRight(prefix[:253], "-.")
		}
		if prefix != "" {
			name = prefix + "/" + name
		}
	}
	if len(validation.IsQualifiedName(name)) > 0 {
		return ""
	}
	return name
}

func sanitizeLabelValue(value string) string {
	if value == "" {
		return ""
	}
	value = invalidLabelChars.ReplaceAllString(value, "-")
	if len(value) > 63 {
		value = value[:63]
	}
	value = nonAlnumEdges.ReplaceAllString(value, "")
	if len(validation.IsValidLabelValue(value)) > 0 {
		return ""
	}
	return value
}
