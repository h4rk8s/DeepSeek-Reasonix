package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func uiBoolPtr(v bool) *bool { return &v }

func TestUIPresentationHybridPresetAndOverrides(t *testing.T) {
	cfg := Default()
	cfg.UI.Transcript.Profile = "hybrid"
	p := cfg.UIPresentation()
	if p.Profile != "hybrid" || p.Density != "balanced" || p.TurnSeparator != "space" || p.UserPrompt != "band" || p.AssistantMarker != "dot" {
		t.Fatalf("hybrid structure = %+v", p)
	}
	if !p.ShowRole || !p.ShowActivity || !p.ShowImageUnderstanding || !p.ShowRecap || !p.ShowTurnMetrics {
		t.Fatalf("hybrid visibility = %+v", p)
	}
	if p.ComposerPrefix != "› " || !p.ComposerFrame || p.StatusLayout != "two" || !p.StatusCache || !p.StatusPath || !p.StatusCost {
		t.Fatalf("hybrid chrome = %+v", p)
	}

	cfg.UI.Transcript.Density = "compact"
	cfg.UI.Transcript.Show.Activity = uiBoolPtr(false)
	cfg.UI.Composer.Prefix = "❯"
	cfg.UI.Composer.Frame = uiBoolPtr(false)
	cfg.UI.Status.Layout = "one"
	p = cfg.UIPresentation()
	if p.Density != "compact" || p.ShowActivity || p.ComposerPrefix != "❯ " || p.ComposerFrame || p.StatusLayout != "one" {
		t.Fatalf("explicit overrides = %+v", p)
	}
}

func TestUIPresentationTOMLRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	path := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `[ui]
input_prompt = "> "

[ui.transcript]
profile = "hybrid"
density = "balanced"
turn_separator = "space"
user_prompt = "band"
assistant_marker = "dot"

[ui.transcript.show]
role = true
activity = true
image_understanding = true
recap = true
turn_metrics = true

[ui.composer]
prefix = "›"
frame = true

[ui.status]
layout = "two"
cache = true
path = true
cost = true
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadUserConfigReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.UIPresentation()
	if p.Profile != "hybrid" || p.ComposerPrefix != "› " || !p.ComposerFrame || p.StatusLayout != "two" {
		t.Fatalf("loaded presentation = %+v", p)
	}
	rendered := RenderTOMLForScope(cfg, RenderScopeUser)
	for _, want := range []string{"[ui.transcript]", `profile = "hybrid"`, "[ui.transcript.show]", "[ui.composer]", "[ui.status]"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, rendered)
		}
	}
}

func TestUIPresentationRejectsInvalidConfigValue(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	path := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[ui.transcript]\ndensity = \"crowded\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadUserConfigReadOnly()
	if err == nil || !strings.Contains(err.Error(), "ui.transcript.density") {
		t.Fatalf("invalid presentation error = %v", err)
	}
}
