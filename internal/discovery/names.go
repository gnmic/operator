package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

var (
	invalidNameChars = regexp.MustCompile(`[^a-z0-9.-]+`)
	dashRuns         = regexp.MustCompile(`-{2,}`)
	dotRuns          = regexp.MustCompile(`\.{2,}`)
	dashDot          = regexp.MustCompile(`-\.|\.-`)
)

// TargetName builds the resource name for a device: <source>-<discovered>,
// normalised to a DNS-1123 subdomain. The rule is fixed rather than a
// template because renaming Targets renames gNMIc targets and therefore the
// source label on every exported metric.
func TargetName(source, discovered string) (string, bool) {
	return NormalizeName(source + "-" + discovered)
}

// NormalizeName lowercases, replaces anything outside [a-z0-9.-] with "-",
// collapses runs, strips leading and trailing separators, and truncates with
// a hash suffix when the result is too long. ok is false when nothing valid
// is left.
func NormalizeName(s string) (string, bool) {
	original := s
	s = strings.ToLower(s)
	s = invalidNameChars.ReplaceAllString(s, "-")
	s = dashRuns.ReplaceAllString(s, "-")
	s = dotRuns.ReplaceAllString(s, ".")
	for dashDot.MatchString(s) {
		s = dashDot.ReplaceAllString(s, ".")
		s = dotRuns.ReplaceAllString(s, ".")
	}
	s = strings.Trim(s, "-.")
	if len(s) > MaxNameLength {
		sum := sha256.Sum256([]byte(original))
		s = strings.TrimRight(s[:MaxNameLength-9], "-.") + "-" + hex.EncodeToString(sum[:])[:8]
	}
	if s == "" || len(validation.IsDNS1123Subdomain(s)) > 0 {
		return "", false
	}
	return s, true
}
