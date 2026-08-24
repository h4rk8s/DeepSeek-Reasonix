package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestDoctorConfigBypassesBrokenStartupConfigAndRemainsReadOnly(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	want := []byte("[broken\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := captureCLIOutput(t, func() {
		if code := Run([]string{"doctor", "config", "--root", root, "--json"}, "test"); code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})
	if stderr != "" {
		t.Fatalf("stderr = %q, want JSON-only diagnostic on stdout", stderr)
	}
	var report config.ConfigInspection
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, stdout)
	}
	if report.Valid || report.EffectiveAvailable {
		t.Fatalf("report = %+v", report)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("doctor config rewrote user config: %q", got)
	}
}

func TestDoctorConfigTextShowsSourceAndIgnoredReason(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[ui]\ncursor_shape = \"block\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[secrets]\nprotect_sensitive_files = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := captureCLIOutput(t, func() {
		if code := Run([]string{"doctor", "config", "--root", root}, "test"); code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	})
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	for _, want := range []string{"ui.cursor_shape", "block", "user", "project_scope_ignored", "secrets"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("text report missing %q:\n%s", want, stdout)
		}
	}
}

func TestDoctorConfigCommandRecognition(t *testing.T) {
	if !isDoctorConfigCommand([]string{"doctor", "config", "--json"}) {
		t.Fatal("doctor config was not recognized")
	}
	for _, args := range [][]string{{"doctor"}, {"doctor", "capabilities"}, {"config"}} {
		if isDoctorConfigCommand(args) {
			t.Fatalf("unexpected doctor config match for %v", args)
		}
	}
}
