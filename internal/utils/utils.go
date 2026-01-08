package utils

import (
	"strings"
)

const Delimiter = "/"

// SplitNN splits a namespaced name (namespace/name) into namespace and name
func SplitNN(nn string) (namespace, name string) {
	parts := strings.SplitN(nn, Delimiter, 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", nn
}
