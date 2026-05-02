package agent

import (
	"fmt"
	"strings"
)

func KnownBackendNames() []string {
	return []string{"codex", "claude"}
}

func ValidateBackend(name string) error {
	switch name {
	case "codex", "claude":
		return nil
	}
	return UnknownBackendError(name)
}

func UnknownBackendError(name string) error {
	return fmt.Errorf("unknown backend: %s (expected %s)", name, joinBackendNames(KnownBackendNames()))
}

func joinBackendNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " or " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
	}
}
