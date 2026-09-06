package discovery

// Existing is what the controller knows about a Target it already manages.
type Existing struct {
	Name string
	// Hash is the AnnotationHash value on the object, empty if missing.
	Hash string
}

// Diff decides what to write. A desired Target is applied when it does not
// exist or its hash differs; a managed Target that is not desired is a
// deletion candidate, subject to the prune guards.
func Diff(desired []Desired, existing []Existing) (apply []Desired, remove []string) {
	byName := make(map[string]Existing, len(existing))
	for _, e := range existing {
		byName[e.Name] = e
	}
	wanted := make(map[string]struct{}, len(desired))
	for _, d := range desired {
		wanted[d.Name] = struct{}{}
		if e, ok := byName[d.Name]; !ok || e.Hash != d.Hash {
			apply = append(apply, d)
		}
	}
	for _, e := range existing {
		if _, ok := wanted[e.Name]; !ok {
			remove = append(remove, e.Name)
		}
	}
	return apply, remove
}
