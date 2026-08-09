/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// watchedNamespaces mirrors the manager's --watch-namespaces setting. A nil map means the
// operator watches every namespace, which is the default.
//
// The webhook configurations are registered cluster-wide and carry no namespaceSelector, so
// admission requests arrive for namespaces this instance does not reconcile. Without this,
// a resource created in an unwatched namespace is accepted and then silently ignored: no
// error, no status, no event. That is a worse experience than an outright failure.
var watchedNamespaces map[string]struct{}

// SetWatchedNamespaces records the namespaces this operator reconciles. Pass nil or an empty
// slice for "all namespaces". Call once during setup, before webhooks are registered.
func SetWatchedNamespaces(namespaces []string) {
	if len(namespaces) == 0 {
		watchedNamespaces = nil
		return
	}
	watchedNamespaces = make(map[string]struct{}, len(namespaces))
	for _, ns := range namespaces {
		watchedNamespaces[ns] = struct{}{}
	}
}

// unwatchedNamespaceWarning returns an admission warning when the object's namespace is not
// reconciled by this operator, and nil otherwise.
//
// This deliberately warns rather than rejects. Several namespace-scoped operator instances
// can share a cluster, each with its own cluster-wide webhook registration; rejecting would
// mean every instance blocks resources intended for the others, and nothing could be created
// at all. A spurious warning is noise, a spurious rejection is an outage.
func unwatchedNamespaceWarning(kind, namespace string) admission.Warnings {
	if watchedNamespaces == nil {
		return nil
	}
	if _, ok := watchedNamespaces[namespace]; ok {
		return nil
	}
	watched := make([]string, 0, len(watchedNamespaces))
	for ns := range watchedNamespaces {
		watched = append(watched, ns)
	}
	sort.Strings(watched)
	return admission.Warnings{
		fmt.Sprintf(
			"namespace %q is not watched by this gnmic-operator instance (watching: %s); this %s will be accepted but never reconciled",
			namespace, strings.Join(watched, ", "), kind,
		),
	}
}
