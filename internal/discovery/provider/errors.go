package provider

import "fmt"

// NotFoundError says a referenced Secret or ConfigMap, or a key in one, does
// not exist. Resolvers return it wrapped with Spec, and the controller picks
// the condition reason from Kind.
type NotFoundError struct {
	Kind      string
	Namespace string
	Name      string
	Key       string
}

func (e *NotFoundError) Error() string {
	if e.Key != "" {
		return fmt.Sprintf("%s %s/%s has no key %q", e.Kind, e.Namespace, e.Name, e.Key)
	}
	return fmt.Sprintf("%s %s/%s not found", e.Kind, e.Namespace, e.Name)
}
