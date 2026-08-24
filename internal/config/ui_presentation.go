package config

import (
	"fmt"
	"slices"
	"strings"
)

// UIPresentation resolves a named information-architecture preset and then
// applies explicit field overrides. It is presentation-only and never enters
// provider requests or the cache-stable prompt prefix.
func (c *Config) UIPresentation() UIPresentation {
	p := currentPresentation()
	if c == nil {
		return p
	}

	raw := c.UI.Transcript
	switch normalizeChoice(raw.Profile, "current", "hybrid", "claude", "codex", "grok") {
	case "hybrid":
		p = hybridPresentation()
	case "claude":
		p = claudePresentation()
	case "codex":
		p = codexPresentation()
	case "grok":
		p = grokPresentation()
	}
	p.Profile = normalizeChoice(raw.Profile, "current", "hybrid", "claude", "codex", "grok")
	if raw.Profile == "" {
		p.Profile = "current"
	}
	if v := normalizeChoice(raw.Density, "compact", "balanced", "comfortable"); raw.Density != "" {
		p.Density = v
	}
	if v := normalizeChoice(raw.TurnSeparator, "none", "space", "rule"); raw.TurnSeparator != "" {
		p.TurnSeparator = v
	}
	if v := normalizeChoice(raw.UserPrompt, "plain", "band", "boxed"); raw.UserPrompt != "" {
		p.UserPrompt = v
	}
	if v := normalizeChoice(raw.AssistantMarker, "none", "dot", "diamond", "name"); raw.AssistantMarker != "" {
		p.AssistantMarker = v
	}
	applyBool(&p.ShowRole, raw.Show.Role)
	applyBool(&p.ShowActivity, raw.Show.Activity)
	applyBool(&p.ShowImageUnderstanding, raw.Show.ImageUnderstanding)
	applyBool(&p.ShowRecap, raw.Show.Recap)
	if raw.Show.TurnMetrics != nil {
		p.ShowTurnMetrics = *raw.Show.TurnMetrics
	} else {
		p.ShowTurnMetrics = c.UIShowUsage()
	}

	if legacyPrefix := c.UIInputPrompt(); legacyPrefix != "" || p.Profile == "current" {
		p.ComposerPrefix = legacyPrefix
	}
	if c.UI.Composer.Prefix != "" {
		p.ComposerPrefix = normalizeComposerPrefix(c.UI.Composer.Prefix)
	}
	applyBool(&p.ComposerFrame, c.UI.Composer.Frame)
	if c.UI.Status.Layout != "" {
		p.StatusLayout = normalizeChoice(c.UI.Status.Layout, "one", "two")
	}
	applyBool(&p.StatusCache, c.UI.Status.Cache)
	applyBool(&p.StatusPath, c.UI.Status.Path)
	applyBool(&p.StatusCost, c.UI.Status.Cost)
	return p
}

func normalizeChoice(value string, allowed ...string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if slices.Contains(allowed, v) {
		return v
	}
	return allowed[0]
}

func validateUIPresentationConfig(ui UIConfig) error {
	checks := []struct {
		name    string
		value   string
		allowed []string
	}{
		{"ui.transcript.profile", ui.Transcript.Profile, []string{"current", "hybrid", "claude", "codex", "grok"}},
		{"ui.transcript.density", ui.Transcript.Density, []string{"compact", "balanced", "comfortable"}},
		{"ui.transcript.turn_separator", ui.Transcript.TurnSeparator, []string{"none", "space", "rule"}},
		{"ui.transcript.user_prompt", ui.Transcript.UserPrompt, []string{"plain", "band", "boxed"}},
		{"ui.transcript.assistant_marker", ui.Transcript.AssistantMarker, []string{"none", "dot", "diamond", "name"}},
		{"ui.status.layout", ui.Status.Layout, []string{"one", "two"}},
	}
	for _, check := range checks {
		value := strings.ToLower(strings.TrimSpace(check.value))
		if value == "" {
			continue
		}
		valid := false
		for _, allowed := range check.allowed {
			valid = valid || value == allowed
		}
		if !valid {
			return fmt.Errorf("%s %q is invalid; want %s", check.name, check.value, strings.Join(check.allowed, "|"))
		}
	}
	return nil
}

func normalizeComposerPrefix(value string) string {
	v := strings.TrimRight(strings.ReplaceAll(strings.ReplaceAll(value, "\r", ""), "\n", ""), " ")
	if v == "" {
		return ""
	}
	return v + " "
}

func applyBool(dst *bool, override *bool) {
	if override != nil {
		*dst = *override
	}
}

func currentPresentation() UIPresentation {
	return UIPresentation{
		Profile: "current", Density: "compact", TurnSeparator: "space", UserPrompt: "band", AssistantMarker: "name",
		ShowRole: true, ShowActivity: true, ShowImageUnderstanding: true, ShowRecap: true, ShowTurnMetrics: true,
		ComposerFrame: true, StatusLayout: "two", StatusCache: true, StatusPath: true, StatusCost: true,
	}
}

func hybridPresentation() UIPresentation {
	return UIPresentation{
		Profile: "hybrid", Density: "balanced", TurnSeparator: "space", UserPrompt: "band", AssistantMarker: "dot",
		ShowRole: true, ShowActivity: true, ShowImageUnderstanding: true, ShowRecap: true, ShowTurnMetrics: true,
		ComposerPrefix: "› ", ComposerFrame: true, StatusLayout: "two", StatusCache: true, StatusPath: true, StatusCost: true,
	}
}

func claudePresentation() UIPresentation {
	p := hybridPresentation()
	p.Profile, p.Density, p.ShowActivity, p.ShowTurnMetrics = "claude", "comfortable", false, false
	return p
}

func codexPresentation() UIPresentation {
	p := hybridPresentation()
	p.Profile, p.Density, p.TurnSeparator, p.UserPrompt = "codex", "compact", "rule", "boxed"
	p.ShowRole, p.ShowActivity, p.ShowRecap, p.StatusLayout = false, false, false, "one"
	return p
}

func grokPresentation() UIPresentation {
	p := hybridPresentation()
	p.Profile, p.AssistantMarker, p.UserPrompt, p.StatusLayout = "grok", "diamond", "boxed", "one"
	p.ShowRole, p.ShowRecap, p.ShowTurnMetrics = false, false, false
	return p
}
