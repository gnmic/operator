// Package discovery turns the devices a provider returns into the desired set
// of Target resources, and decides what may be deleted. Everything here is a
// pure function of its inputs so it can be tested without a cluster.
package discovery

const (
	// LabelTargetSource names the TargetSource that manages a Target. It is a
	// claim: the controller owner reference is the fact.
	LabelTargetSource = "operator.gnmic.dev/targetsource"
	// LabelManagedBy marks a Target as written by a TargetSource.
	LabelManagedBy = "operator.gnmic.dev/managed-by"
	// LabelManagedByValue is the value of LabelManagedBy.
	LabelManagedByValue = "targetsource"

	// AnnotationDiscoveredName keeps the name the source reported, so a device
	// can be found by its real name after normalisation.
	AnnotationDiscoveredName = "operator.gnmic.dev/discovered-name"
	// AnnotationLabelPrefix prefixes annotations holding the original value of
	// a label that had to be rewritten to be valid.
	AnnotationLabelPrefix = "operator.gnmic.dev/label."
	// AnnotationHash records what the controller last applied, so an unchanged
	// device costs no write.
	AnnotationHash = "operator.gnmic.dev/targetsource-hash"
	// AnnotationRequestedAt is set by the webhook handler to ask for a run.
	AnnotationRequestedAt = "operator.gnmic.dev/requested-at"

	// FieldManagerPrefix prefixes the server-side apply field manager, one per
	// TargetSource, so two sources never fight over one Target's fields silently.
	FieldManagerPrefix = "targetsource.operator.gnmic.dev/"

	// MaxNameLength is the DNS-1123 subdomain limit a Target name must fit.
	MaxNameLength = 253
	// maxFailedDevices caps the sample kept in status.
	MaxFailedDevices = 10
)
