package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestImageUnderstandingSettingsRenderOnceAndRoundTrip(t *testing.T) {
	cfg := Default()
	cfg.Agent.ImageUnderstandingModel = "vision/provider"
	cfg.Agent.ImageUnderstandingCommand = "reasonix-vision-ocr"

	renders := map[string]string{
		"full":          RenderTOMLForScope(cfg, RenderScopeFull),
		"user":          RenderTOMLForScope(cfg, RenderScopeUser),
		"project":       RenderTOMLForScope(cfg, RenderScopeProject),
		"project-delta": RenderTOMLProjectDelta(cfg),
	}
	for name, rendered := range renders {
		t.Run(name, func(t *testing.T) {
			for _, key := range []string{"image_understanding_model =", "image_understanding_command ="} {
				if got := strings.Count(rendered, key); got != 1 {
					t.Fatalf("rendered %q %d times, want once:\n%s", key, got, rendered)
				}
			}

			var got Config
			if _, err := toml.Decode(rendered, &got); err != nil {
				t.Fatalf("rendered TOML does not parse: %v\n%s", err, rendered)
			}
			if got.Agent.ImageUnderstandingModel != cfg.Agent.ImageUnderstandingModel || got.Agent.ImageUnderstandingCommand != cfg.Agent.ImageUnderstandingCommand {
				t.Fatalf("image understanding settings did not round trip: %+v", got.Agent)
			}
		})
	}
}
