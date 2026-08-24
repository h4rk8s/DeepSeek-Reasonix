package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectForRootReportsEffectiveSourcesAndIgnoredProjectFields(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("REASONIX_HOME", home)

	writeInspectionFile(t, filepath.Join(home, "config.toml"), `
default_model = "user-model"

[ui]
cursor_shape = "block"

[permissions]
mode = "allow"

[secrets]
protect_sensitive_files = true
`)
	writeInspectionFile(t, filepath.Join(project, "reasonix.toml"), `
default_model = "project-model"

[ui]
cursor_shape = "underline"
unknown_knob = true

[permissions]
mode = "deny"

[secrets]
protect_sensitive_files = false
`)

	report := InspectForRoot(project)
	if !report.Valid || !report.EffectiveAvailable {
		t.Fatalf("report state = valid:%v effective:%v errors:%v", report.Valid, report.EffectiveAvailable, report.Errors)
	}
	assertInspectionSetting(t, report, "default_model", "project-model", ConfigSourceProject)
	assertInspectionSetting(t, report, "ui.cursor_shape", "underline", ConfigSourceProject)
	assertInspectionSetting(t, report, "permissions.mode", "deny", ConfigSourceProject)
	assertInspectionDiagnostic(t, report, "project_scope_ignored", "secrets")
	assertInspectionDiagnostic(t, report, "unknown_key", "ui.unknown_knob")
	if report.Effective == nil || !report.Effective.Secrets.ProtectSensitiveFiles {
		t.Fatal("project config changed the user-global secrets policy")
	}
}

func TestInspectForRootReportsBrokenProjectWithoutWriting(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	writeInspectionFile(t, filepath.Join(home, "config.toml"), `default_model = "user-model"`)
	projectPath := filepath.Join(project, "reasonix.toml")
	want := []byte("[broken\n")
	if err := os.WriteFile(projectPath, want, 0o600); err != nil {
		t.Fatal(err)
	}

	report := InspectForRoot(project)
	if report.Valid || !report.EffectiveAvailable {
		t.Fatalf("report state = valid:%v effective:%v errors:%v", report.Valid, report.EffectiveAvailable, report.Errors)
	}
	assertInspectionSource(t, report, ConfigSourceProject, ConfigSourceInvalid)
	assertInspectionSetting(t, report, "default_model", "user-model", ConfigSourceUser)
	got, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("inspection rewrote project config: %q", got)
	}
}

func TestInspectForRootReportsBrokenUserWithoutPretendingDefaultsAreEffective(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	userPath := filepath.Join(home, "config.toml")
	want := []byte("[broken\n")
	if err := os.WriteFile(userPath, want, 0o600); err != nil {
		t.Fatal(err)
	}
	writeInspectionFile(t, filepath.Join(project, "reasonix.toml"), `default_model = "project-model"`)

	report := InspectForRoot(project)
	if report.Valid || report.EffectiveAvailable || report.Effective != nil {
		t.Fatalf("report state = valid:%v effective:%v cfg:%v errors:%v", report.Valid, report.EffectiveAvailable, report.Effective, report.Errors)
	}
	assertInspectionSource(t, report, ConfigSourceUser, ConfigSourceInvalid)
	if len(report.Settings) != 0 {
		t.Fatalf("invalid user config produced pretend effective settings: %+v", report.Settings)
	}
	got, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("inspection rewrote user config: %q", got)
	}
}

func TestInspectForRootDoesNotExposeProviderHeaderSecrets(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	const secret = "Bearer should-never-appear"
	writeInspectionFile(t, filepath.Join(home, "config.toml"), `
[[providers]]
name = "private"
model = "private-model"
base_url = "https://example.invalid/v1"
headers = { Authorization = "`+secret+`" }
`)

	report := InspectForRoot(project)
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("inspection leaked a provider header secret: %s", raw)
	}
}

func writeInspectionFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertInspectionSetting(t *testing.T, report ConfigInspection, path string, value any, source string) {
	t.Helper()
	for _, setting := range report.Settings {
		if setting.Path == path {
			if setting.Value != value || setting.Source != source {
				t.Fatalf("setting %s = %#v from %q, want %#v from %q", path, setting.Value, setting.Source, value, source)
			}
			return
		}
	}
	t.Fatalf("setting %s not found in %+v", path, report.Settings)
}

func assertInspectionDiagnostic(t *testing.T, report ConfigInspection, code, path string) {
	t.Helper()
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == code && diagnostic.Path == path {
			return
		}
	}
	t.Fatalf("diagnostic %s/%s not found in %+v", code, path, report.Diagnostics)
}

func assertInspectionSource(t *testing.T, report ConfigInspection, scope, status string) {
	t.Helper()
	for _, source := range report.Sources {
		if source.Scope == scope {
			if source.Status != status {
				t.Fatalf("source %s status = %q, want %q", scope, source.Status, status)
			}
			return
		}
	}
	t.Fatalf("source %s not found in %+v", scope, report.Sources)
}
