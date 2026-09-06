package discovery

import (
	"fmt"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
)

// PruneDecision says whether this run may delete, and if not, why.
type PruneDecision struct {
	Allowed bool
	// Reason is a condition reason: Truncated, EmptySource or PruneGuard.
	Reason string
	// Message explains the numbers behind the decision.
	Message string
}

// PruneGuard runs the three checks that stand between a diff and a deletion.
// None of them is permanent: each clears as soon as the source returns a
// complete, non-empty, plausible answer.
func PruneGuard(spec gnmicv1alpha1.PruneSpec, truncated bool, discovered, managed, deletions int) PruneDecision {
	if deletions == 0 {
		return PruneDecision{Allowed: true}
	}
	if truncated {
		return PruneDecision{
			Reason:  "Truncated",
			Message: fmt.Sprintf("the source could not be read completely; %d deletion(s) skipped", deletions),
		}
	}
	if discovered == 0 && !spec.AllowEmptySource {
		return PruneDecision{
			Reason:  "EmptySource",
			Message: fmt.Sprintf("the source returned no devices; %d managed Target(s) kept (set prune.allowEmptySource to prune)", managed),
		}
	}
	ratio := int32(50)
	if spec.MaxDeleteRatio != nil {
		ratio = *spec.MaxDeleteRatio
	}
	if ratio < 100 && managed > 0 && deletions*100 > int(ratio)*managed {
		return PruneDecision{
			Reason: "PruneGuard",
			Message: fmt.Sprintf("this run would delete %d of %d managed Target(s), over prune.maxDeleteRatio %d%%; deletions skipped",
				deletions, managed, ratio),
		}
	}
	return PruneDecision{Allowed: true}
}
