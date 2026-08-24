package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadForRootRejectsBrokenUserConfigEvenWithLastKnownGood(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	// Broken live config.
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Valid LKG snapshot.
	lkgDir := filepath.Join(home, "repair")
	if err := os.MkdirAll(lkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	good := []byte("default_model = \"from-lkg\"\n")
	if err := os.WriteFile(LastKnownGoodConfigPath(), good, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadForRoot(t.TempDir()); err == nil {
		t.Fatal("LoadForRoot accepted a broken user config via last-known-good fallback")
	}
	// Original file must remain untouched.
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || string(raw) != "[broken\n" {
		t.Fatalf("original config rewritten: %q err=%v", raw, err)
	}
}

func TestLoadForRootRejectsBrokenUserConfigWithoutLastKnownGood(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadForRoot(t.TempDir()); err == nil {
		t.Fatal("LoadForRoot accepted a broken user config via built-in defaults")
	}
}

func TestLoadForRootTypeErrorDoesNotPartiallyApplyUserConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	body := "default_model = \"must-not-survive\"\n[ui]\nshow_reasoning = \"not-a-bool\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadForRoot(t.TempDir()); err == nil {
		t.Fatal("LoadForRoot accepted a partially decoded user config")
	}
}

func TestLoadForRootIsolatesBrokenProjectConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("default_model = \"user-model\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "user-model" {
		t.Fatalf("user model lost: %q", cfg.DefaultModel)
	}
	if !cfg.HasLoadWarnings() {
		t.Fatal("expected project warning")
	}
}

func TestLoadForRootTypeErrorDoesNotPartiallyApplyProjectConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("default_model = \"user-model\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	body := "default_model = \"project-model\"\n[ui]\nshow_reasoning = \"not-a-bool\"\n"
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "user-model" {
		t.Fatalf("partially decoded project model=%q", cfg.DefaultModel)
	}
	if !cfg.HasLoadWarnings() {
		t.Fatal("expected project config warning")
	}
}
