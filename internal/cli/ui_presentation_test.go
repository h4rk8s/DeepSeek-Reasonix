package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	transcriptmodel "reasonix/internal/transcript"
)

func hybridTestPresentation() config.UIPresentation {
	cfg := config.Default()
	cfg.UI.Transcript.Profile = "hybrid"
	return cfg.UIPresentation()
}

func TestHybridPresentationRendersSelectedTranscriptStructure(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256
	p := hybridTestPresentation()

	assistant := ansi.Strip(renderAssistantMarkdownWithPresentation("answer", 80, p))
	if !strings.HasPrefix(assistant, "● Reasonix\n\nanswer") {
		t.Fatalf("hybrid assistant = %q", assistant)
	}
	user := renderUserBubbleWithPresentation("question", 80, false, p)
	if plain := ansi.Strip(user); !strings.Contains(plain, "› question") || ansi.StringWidth(plain) != transcriptContentWidth(80, false) {
		t.Fatalf("hybrid user band = %q", plain)
	}
}

func TestPresentationVisibilitySuppressesActivityAndImageDisclosure(t *testing.T) {
	m := newTestChatTUI()
	m.presentation = hybridTestPresentation()
	m.presentation.ShowActivity = false
	m.presentation.ShowImageUnderstanding = false

	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "tool-1", Name: "read_file", Args: `{"path":"x"}`}})
	m.ingestEvent(event.Event{Kind: event.Notice, Source: event.UsageSourceVision, Text: "image understood: 1 image", Detail: "visible_text: hidden"})
	if got := ansi.Strip(strings.Join(renderedTranscriptBlocks(m.transcript), "\n")); strings.Contains(got, "read_file") || strings.Contains(got, "Image understood") {
		t.Fatalf("hidden presentation leaked activity:\n%s", got)
	}
}

func TestHybridActivityCompletesInPlaceWithDuration(t *testing.T) {
	m := newTestChatTUI()
	m.presentation = hybridTestPresentation()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "tool-1", Name: "read_file", Args: `{"path":"cache.go"}`}})
	cardIdx := m.toolCardIdx["tool-1"]
	before := ansi.Strip(m.transcript[cardIdx].rendered)
	if !strings.Contains(before, "│  ◆ Read cache.go") {
		t.Fatalf("hybrid activity dispatch = %q", before)
	}
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "tool-1", Name: "read_file", DurationMs: 3800}})
	after := ansi.Strip(m.transcript[cardIdx].rendered)
	if !strings.Contains(after, "3.8s") || len(m.transcript) < 1 {
		t.Fatalf("hybrid activity result = %q", after)
	}
	if _, ok := m.toolCardIdx["tool-1"]; ok {
		t.Fatal("completed activity retained mutable index")
	}
}

func TestPresentationTurnSeparatorModes(t *testing.T) {
	m := newTestChatTUI()
	m.presentation = hybridTestPresentation()
	m.commitLine("previous")
	m.presentation.TurnSeparator = "none"
	m.commitTurnSeparator()
	if len(m.transcript) != 1 {
		t.Fatalf("none separator appended blocks: %v", m.transcript)
	}
	m.presentation.TurnSeparator = "space"
	m.commitTurnSeparator()
	if len(m.transcript) != 2 || m.transcript[1].rendered != "" {
		t.Fatalf("space separator = %v", m.transcript)
	}
	m.commitLine("next")
	m.presentation.TurnSeparator = "rule"
	m.commitTurnSeparator()
	if !strings.Contains(ansi.Strip(m.transcript[len(m.transcript)-1].rendered), "─") {
		t.Fatalf("rule separator = %q", m.transcript[len(m.transcript)-1].rendered)
	}
}

func TestPresentationCanHideRecap(t *testing.T) {
	m := newTestChatTUI()
	m.presentation.ShowRecap = false
	m.runRecapCommand("/recap")

	joined := strings.Join(renderedTranscriptBlocks(m.transcript), "\n")
	if !strings.Contains(joined, "recap is hidden by ui.transcript.show.recap") {
		t.Fatalf("expected hidden recap notice, got %q", joined)
	}
}

func TestHybridStatusUsesTopRuleAndRespectsTelemetryToggles(t *testing.T) {
	m := newTestChatTUI()
	m.presentation = hybridTestPresentation()
	m.balance = "¥66.69"
	got := ansi.Strip(m.renderStatusBlock("  YOLO", 80))
	if !strings.HasPrefix(got, "  ─") || !strings.Contains(got, "YOLO") || !strings.Contains(got, "¥66.69") {
		t.Fatalf("hybrid status = %q", got)
	}
	m.presentation.StatusCost = false
	if got := ansi.Strip(m.renderStatusBlock("  YOLO", 80)); strings.Contains(got, "¥66.69") {
		t.Fatalf("disabled cost leaked into status: %q", got)
	}
}

func TestReplayHistoryCarriesCanonicalSemanticKinds(t *testing.T) {
	m := newTestChatTUI()
	m.width = 88
	m.lazyReasoning = true
	m.presentation = hybridTestPresentation()
	m.replayHistory([]provider.Message{
		{Role: provider.RoleUser, Content: "Image understanding context:\n\n<image-understanding source=\"screen.png\">\nvisible_text: settings\n</image-understanding>\n\ninspect settings"},
		{Role: provider.RoleAssistant, Content: "done", ReasoningContent: "inspect the screen", ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read_file", Arguments: `{"path":"config.toml"}`}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read_file", Content: "line one\nline two"},
	}, m.width)

	var got []transcriptmodel.Kind
	for _, block := range m.transcript {
		source := block.source
		if source.entryKind != "" && source.entryKind != transcriptmodel.KindUnknown {
			got = append(got, source.entryKind)
		}
	}
	want := []transcriptmodel.Kind{
		transcriptmodel.KindUserPrompt,
		transcriptmodel.KindImageDisclosure,
		transcriptmodel.KindThinkingDisclosure,
		transcriptmodel.KindAssistant,
		transcriptmodel.KindToolActivity,
		transcriptmodel.KindToolActivity,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("semantic kinds = %v, want %v", got, want)
	}

	m.reflowTranscript(52)
	var after []transcriptmodel.Kind
	for _, block := range m.transcript {
		source := block.source
		if source.entryKind != "" && source.entryKind != transcriptmodel.KindUnknown {
			after = append(after, source.entryKind)
		}
	}
	if !slices.Equal(after, want) {
		t.Fatalf("semantic kinds after reflow = %v, want %v", after, want)
	}
}
