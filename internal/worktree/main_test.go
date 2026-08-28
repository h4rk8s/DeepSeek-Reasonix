package worktree

import (
	"os"
	"testing"
)

// TestMain isolates the package's Git subprocesses from the developer's
// global and system Git configuration. Without this, settings such as
// core.hooksPath make `git rev-parse --git-path hooks/...` resolve outside the
// temporary repository, and hook-writing tests overwrite the user's real hooks.
func TestMain(m *testing.M) {
	if err := os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull); err != nil {
		panic(err)
	}
	if err := os.Setenv("GIT_CONFIG_NOSYSTEM", "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
