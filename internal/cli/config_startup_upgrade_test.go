package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestRunIgnoresLegacyGlobalConfigWithoutPersistingMigration(t *testing.T) {
	isolateCLIConfigHome(t)
	legacyPath := filepath.Join(filepath.Dir(config.UserConfigPath()), "reasonix.toml")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`
default_model = "deepseek-flash"

[[plugins]]
name = "legacy-cli"
command = "legacy-bin"
`)
	if err := os.WriteFile(legacyPath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if rc := Run([]string{"mcp", "list"}, "test-version"); rc != 0 {
			t.Fatalf("mcp list rc = %d, want 0", rc)
		}
	})
	if strings.Contains(out, "legacy-cli") {
		t.Fatalf("ordinary CLI startup should not import legacy global config:\n%s", out)
	}
	if _, err := os.Stat(config.UserConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("ordinary CLI startup created a migrated user config: %v", err)
	}
	body, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("read legacy config: %v", err)
	}
	if !bytes.Equal(body, original) {
		t.Fatalf("ordinary CLI startup rewrote legacy config:\n%s", body)
	}
}

func TestRunDoesNotPersistUserConfigUpgradesOnStartup(t *testing.T) {
	isolateCLIConfigHome(t)
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("config_version = 2\ndefault_model = \"deepseek-flash\"\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	captureStdout(t, func() {
		if rc := Run([]string{"mcp", "list"}, "test-version"); rc != 0 {
			t.Fatalf("mcp list rc = %d, want 0", rc)
		}
	})

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read upgraded user config: %v", err)
	}
	if !bytes.Equal(body, original) {
		t.Fatalf("ordinary CLI startup rewrote user config:\n%s", body)
	}
}
