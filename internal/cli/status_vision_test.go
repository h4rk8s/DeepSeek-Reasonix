package cli

import (
	"strings"
	"testing"

	"reasonix/internal/event"
)

func TestModelComboShowsVisionOnlyDuringPrepass(t *testing.T) {
	m := newTestChatTUI()
	m.modelRef = "deepseek-flash/deepseek-v4-flash"
	m.plannerModelRef = "deepseek-pro/deepseek-v4-pro"

	m.ingestEvent(event.Event{
		Kind: event.TurnPhase, PhaseName: event.TurnPhaseVision,
		ModelRef: "deepseek-flash/deepseek-v4-flash-vision-exp",
	})
	if got := m.modelComboTag(); !strings.Contains(got, "DS v4 flash · vision flash · plan pro") {
		t.Fatalf("vision model combo = %q", got)
	}

	m.ingestEvent(event.Event{Kind: event.TurnPhase, PhaseName: event.TurnPhaseWorking})
	if got := m.modelComboTag(); !strings.Contains(got, "DS v4 flash · plan pro") || strings.Contains(got, "vision") {
		t.Fatalf("post-vision model combo = %q", got)
	}
}
