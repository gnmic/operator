package core

const (
	// Kubernetes Side Labels
	LabelTargetSourceName = "operator.gnmic.dev/targetsource"
)

const (
	// Prefix and Labels for external systems
	ExternalLabelPrefix = "gnmic_operator_"

	ExternalLabelTargetProfile = ExternalLabelPrefix + "target_profile"
)
