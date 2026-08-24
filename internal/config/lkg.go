package config

import (
	"path/filepath"
)

// LastKnownGoodConfigPath is the fixed path of the most recent verified user
// config snapshot. Runtime loading never falls back to this snapshot; repair
// and tests use the path without weakening strict config parsing.
func LastKnownGoodConfigPath() string {
	root := MemoryUserDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "repair", "config.toml.last-known-good")
}
