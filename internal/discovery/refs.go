package discovery

import (
	"sort"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// ReferencedSecrets lists every Secret a TargetSource spec names: source
// documents, endpoint credentials, client certificates and webhook secrets.
// The controller indexes on it so a Secret change maps back to its sources,
// and the webhook warns when one is missing.
func ReferencedSecrets(spec *gnmicv1alpha1.TargetSourceSpec) []string {
	set := map[string]struct{}{}
	add := func(name string) {
		if name != "" {
			set[name] = struct{}{}
		}
	}
	if spec.Source.Secret != nil {
		add(spec.Source.Secret.Name)
	}
	if spec.Source.HTTP != nil {
		if a := spec.Source.HTTP.Auth; a != nil {
			if a.Basic != nil {
				add(a.Basic.SecretRef.Name)
			}
			if a.Token != nil {
				add(a.Token.SecretRef.Name)
			}
			if a.Header != nil {
				add(a.Header.SecretRef.Name)
			}
		}
		if t := spec.Source.HTTP.TLS; t != nil && t.ClientCertRef != nil {
			add(t.ClientCertRef.Name)
		}
	}
	if w := spec.Webhook; w != nil && w.Auth != nil {
		if w.Auth.Bearer != nil {
			add(w.Auth.Bearer.SecretRef.Name)
		}
		if w.Auth.Signature != nil {
			add(w.Auth.Signature.SecretRef.Name)
		}
	}
	return sortedKeys(set)
}

// ReferencedConfigMaps lists every ConfigMap a TargetSource spec names.
func ReferencedConfigMaps(spec *gnmicv1alpha1.TargetSourceSpec) []string {
	set := map[string]struct{}{}
	if spec.Source.ConfigMap != nil && spec.Source.ConfigMap.Name != "" {
		set[spec.Source.ConfigMap.Name] = struct{}{}
	}
	if spec.Source.HTTP != nil && spec.Source.HTTP.TLS != nil && spec.Source.HTTP.TLS.CABundleRef != nil {
		set[spec.Source.HTTP.TLS.CABundleRef.Name] = struct{}{}
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
