package cli

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func hasThoughtFor(s string) bool {
	return strings.Contains(strings.ToLower(ansi.Strip(s)), "thought for")
}

func TestAssistantBlockAddsStableGutter(t *testing.T) {
	got := ansi.Strip(renderAssistantBlock("hello\n\nworld"))
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3: %q", len(lines), got)
	}
	if lines[0] != "● hello" {
		t.Fatalf("first line = %q, want assistant marker plus text", lines[0])
	}
	if lines[1] != "" {
		t.Fatalf("blank separator should stay blank, got %q", lines[1])
	}
	if lines[2] != "  world" {
		t.Fatalf("continuation line = %q, want two-cell gutter", lines[2])
	}
}

// newTestChatTUI builds a chatTUI with just the pieces the streaming/commit and
// completion paths need, for unit tests that don't run the bubbletea loop.
func newTestChatTUI() chatTUI {
	commit := []string{}
	ti := textarea.New()
	configureChatTextarea(&ti)
	ti.SetWidth(80)
	shellIdx := map[string]int{}
	shellOut := map[string]string{}
	shellExp := map[string]bool{}
	return chatTUI{
		ctrl:                 control.New(control.Options{}),
		input:                ti,
		width:                80,
		height:               40,
		statusLineCount:      2,
		submittedInputCursor: -1,
		queueEditCursor:      -1,
		nextPasteID:          1,
		reasoningLineIdx:     -1,
		reasoningTextIdx:     -1,
		answerIdx:            -1,
		toolStreamIdx:        -1,
		reasoning:            &strings.Builder{},
		pending:              &strings.Builder{},
		pendingCommit:        &commit,
		shellOutputs:         shellOut,
		shellExpanded:        shellExp,
		shellTranscriptIdx:   shellIdx,
		toolLineCountByID:    map[string]int{},
		subagentProgressIdx:  map[string]int{},
		subagentProgress:     map[string]*cliSubagentProgress{},
		showTurnUsage:        true,
	}
}

// subagentStatus / subagentPreview build reserved ToolProgress events the same
// way the agent tracker emits them.
func subagentStatus(id, phase string) event.Event {
	return event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: id, Name: event.SubagentProgressStatusName, Output: phase}}
}

func subagentPreview(id, channel, text string, truncated bool) event.Event {
	return event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: id, Name: channel, Output: text, Truncated: truncated}}
}

func TestNativeReasoningCommitRegistersLazyDisclosure(t *testing.T) {
	m := newTestChatTUI()
	m.lazyReasoning = config.Default().UI.LazyReasoning
	if !m.lazyReasoning {
		t.Fatal("completed reasoning disclosures must be interactive by default")
	}
	m.reasoningNative = true
	m.thinkStart = time.Now().Add(-time.Second)
	m.reasoning.WriteString("native provider reasoning body")
	m.commitLine("before")

	m.commitReasoning()

	idx := firstTranscriptIndexContaining(m.transcript, "Thought for")
	if idx < 0 {
		t.Fatalf("native reasoning summary missing: %q", m.transcript)
	}
	if !m.toggleReasoningAtTranscriptIdx(idx) {
		t.Fatal("native reasoning summary is not registered as a disclosure")
	}
	expanded := ansi.Strip(m.transcript[idx])
	if !strings.Contains(expanded, "native provider reasoning body") {
		t.Fatalf("expanded native reasoning missing raw body: %q", expanded)
	}
	if strings.Contains(expanded, "Thought for") {
		t.Fatalf("expanded native reasoning should replace its summary: %q", expanded)
	}
	if !m.toggleReasoningAtTranscriptIdx(idx) {
		t.Fatal("native reasoning disclosure did not collapse")
	}
	if !hasThoughtFor(m.transcript[idx]) {
		t.Fatalf("collapsed native reasoning summary was not restored: %q", m.transcript[idx])
	}
}

func TestNativeReasoningCommitKeepsExpandedDisclosureInteractive(t *testing.T) {
	m := newTestChatTUI()
	m.lazyReasoning = true
	m.showReasoning = true
	m.reasoningNative = true
	m.thinkStart = time.Now().Add(-time.Second)
	m.reasoning.WriteString("expanded native provider reasoning")

	m.commitReasoning()

	idx := firstTranscriptIndexContaining(m.transcript, "expanded native provider reasoning")
	if idx < 0 {
		t.Fatalf("expanded native reasoning missing: %q", m.transcript)
	}
	if !m.toggleReasoningAtTranscriptIdx(idx) {
		t.Fatal("expanded native reasoning is not registered as a disclosure")
	}
	collapsed := ansi.Strip(m.transcript[idx])
	if !strings.Contains(collapsed, "Thought for") {
		t.Fatalf("expanded native reasoning did not collapse to its summary: %q", collapsed)
	}
	if strings.Contains(collapsed, "expanded native provider reasoning") {
		t.Fatalf("collapsed native reasoning still contains its body: %q", collapsed)
	}
}

func TestCacheRateLabelKeepsTwoDecimals(t *testing.T) {
	if got := cacheRateLabel("hit %s", 998, 1000); got != "hit 99.80%" {
		t.Fatalf("cacheRateLabel = %q, want hit 99.80%%", got)
	}
	if got := cacheRateLabel("avg %s", 1, 3); got != "avg 33.33%" {
		t.Fatalf("cacheRateLabel = %q, want avg 33.33%%", got)
	}
	if got := cacheRateLabel("avg %s", 1, 0); got != "" {
		t.Fatalf("cacheRateLabel with zero denominator = %q, want empty", got)
	}
}

// TestIngestSeparatesReasoningFromAnswer proves the thinking marker plus its live
// text appear as reasoning streams, collapse to a "thought for Ns" summary (the
// streamed text removed) when the answer begins, and the answer commits as its
// own distinct entry.
func TestIngestSeparatesReasoningFromAnswer(t *testing.T) {
	m := newTestChatTUI()

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "…reasoning…"}) // thinking → marker + live text
	if len(m.transcript) != 2 || !strings.Contains(m.transcript[0], "thinking") {
		t.Fatalf("thinking marker should appear at once, transcript=%v", m.transcript)
	}
	if !strings.Contains(m.transcript[1], "…reasoning…") {
		t.Fatalf("reasoning text should stream live below the marker, transcript=%v", m.transcript)
	}

	m.ingestEvent(event.Event{Kind: event.Text, Text: "Hello answer"}) // answer begins → block collapses
	if len(m.transcript) != 2 || !hasThoughtFor(m.transcript[0]) {
		t.Fatalf("block should collapse to a duration summary plus answer separator, transcript=%v", m.transcript)
	}
	if strings.TrimSpace(m.transcript[1]) != "" {
		t.Fatalf("reasoning/answer separator = %q, want one blank block", m.transcript[1])
	}
	if strings.Contains(strings.Join(m.transcript, "\n"), "…reasoning…") {
		t.Fatalf("collapsed reasoning text should be removed, transcript=%v", m.transcript)
	}
	if m.pending.String() != "Hello answer" {
		t.Errorf("answer should be live in pending, got %q", m.pending.String())
	}
	if m.reasoning.Len() != 0 {
		t.Errorf("reasoning buffer should be cleared after commit")
	}

	m.commitPending() // turn end
	if len(m.transcript) != 3 || !strings.Contains(m.transcript[2], "Hello") {
		t.Fatalf("answer should commit as a separate entry, transcript=%v", m.transcript)
	}
	if plain := ansi.Strip(m.transcript[2]); !strings.HasPrefix(plain, "  ◆ Reasonix\n\n  Hello answer") {
		t.Fatalf("answer should have an explicit assistant identity and indented body, got %q", plain)
	}
}

func TestAssistantAnswerWithoutReasoningHasNoLeadingSpacer(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.Text, Text: "Direct answer"})
	m.ingestEvent(event.Event{Kind: event.Message})

	if len(m.transcript) != 1 {
		t.Fatalf("direct answer should remain one compact block, got %d: %v", len(m.transcript), m.transcript)
	}
	if plain := ansi.Strip(m.transcript[0]); !strings.HasPrefix(plain, "  ◆ Reasonix\n\n  Direct answer") {
		t.Fatalf("direct answer block = %q", plain)
	}
}

func TestTurnReceiptLeavesOneBlankRowAfterAssistantAnswer(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.Text, Text: "Answer"})
	m.ingestEvent(event.Event{Kind: event.Message})
	m.ingestEvent(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12,
	}})

	if len(m.transcript) != 3 {
		t.Fatalf("answer + spacer + receipt should be three blocks, got %d: %v", len(m.transcript), m.transcript)
	}
	if strings.TrimSpace(m.transcript[1]) != "" {
		t.Fatalf("answer/receipt separator = %q, want one blank block", m.transcript[1])
	}
	if !strings.Contains(ansi.Strip(m.transcript[2]), "TURN") {
		t.Fatalf("last block should be the turn receipt, got %q", m.transcript[2])
	}
}

func TestTurnReceiptCanBeHiddenWithoutDisablingUsageAccounting(t *testing.T) {
	m := newTestChatTUI()
	m.showTurnUsage = false
	m.ingestEvent(event.Event{Kind: event.Text, Text: "Answer"})
	m.ingestEvent(event.Event{Kind: event.Message})
	m.ingestEvent(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12,
	}})

	if len(m.transcript) != 1 {
		t.Fatalf("hidden turn receipt should not add transcript blocks, got %d: %v", len(m.transcript), m.transcript)
	}
	if m.turnTokens != 2 {
		t.Fatalf("hidden turn receipt should still account for completion tokens, got %d", m.turnTokens)
	}
}

// TestVerboseReasoningInsertsTextUnderSummary proves /verbose mode keeps the full
// thinking text, placed beneath the collapsed duration summary.
func TestVerboseReasoningInsertsTextUnderSummary(t *testing.T) {
	m := newTestChatTUI()
	m.showReasoning = true

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "step one "})
	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "step two"})
	m.ingestEvent(event.Event{Kind: event.Text, Text: "Answer"}) // closes the block

	if len(m.transcript) != 3 {
		t.Fatalf("verbose block should be summary + text + answer separator, transcript=%v", m.transcript)
	}
	if !hasThoughtFor(m.transcript[0]) {
		t.Errorf("first line should be the duration summary, got %q", m.transcript[0])
	}
	if !strings.Contains(m.transcript[1], "step one") || !strings.Contains(m.transcript[1], "step two") {
		t.Errorf("verbose text should appear under the summary, got %q", m.transcript[1])
	}
	if strings.TrimSpace(m.transcript[2]) != "" {
		t.Errorf("verbose reasoning/answer separator = %q, want blank block", m.transcript[2])
	}
}

func TestLazyReasoningTogglesCompletedThinking(t *testing.T) {
	m := newTestChatTUI()
	m.lazyReasoning = true

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "step one\nstep two"})
	m.ingestEvent(event.Event{Kind: event.Text, Text: "Answer"}) // closes the block

	if len(m.transcript) != 1 || strings.Contains(strings.Join(m.transcript, "\n"), "step one") {
		t.Fatalf("lazy reasoning should start collapsed, transcript=%v", m.transcript)
	}
	if !m.toggleReasoningAtTranscriptIdx(0) {
		t.Fatalf("expected summary click to expand reasoning")
	}
	if len(m.transcript) != 1 {
		t.Fatalf("expanded reasoning should replace the summary entry in place, transcript=%v", m.transcript)
	}
	if hasThoughtFor(m.transcript[0]) {
		t.Fatalf("expanded reasoning should not keep the collapsed summary visible: %v", m.transcript)
	}
	if !strings.Contains(m.transcript[0], "step one") || !strings.Contains(m.transcript[0], "step two") {
		t.Fatalf("expanded reasoning body missing text: %v", m.transcript)
	}
	expandedLines := strings.Split(ansi.Strip(m.transcript[0]), "\n")
	if len(expandedLines) < 4 {
		t.Fatalf("expanded reasoning should include breathing room around the body: %q", ansi.Strip(m.transcript[0]))
	}
	if strings.TrimSpace(expandedLines[0]) != "" {
		t.Fatalf("expanded reasoning should start with a blank line, got first line %q", expandedLines[0])
	}
	if expandedLines[1] != "* step one" {
		t.Fatalf("expanded reasoning should start with an icon-aligned body line, got %q", expandedLines[1])
	}
	if strings.TrimSpace(expandedLines[len(expandedLines)-1]) != "" {
		t.Fatalf("expanded reasoning should end with a blank line, got last line %q", expandedLines[len(expandedLines)-1])
	}
	if strings.Contains(ansi.Strip(m.transcript[0]), "⎿") {
		t.Fatalf("expanded lazy reasoning should render as its own block, not a tool-output gutter: %q", m.transcript[0])
	}
	if !m.toggleReasoningAtTranscriptIdx(0) {
		t.Fatalf("expected body click to collapse reasoning")
	}
	if len(m.transcript) != 1 || strings.Contains(strings.Join(m.transcript, "\n"), "step one") {
		t.Fatalf("collapsed reasoning should remove body again, transcript=%v", m.transcript)
	}
}

func TestImageUnderstandingNoticeUsesDisclosure(t *testing.T) {
	m := newTestChatTUI()

	m.ingestEvent(event.Event{
		Kind:   event.Notice,
		Source: event.UsageSourceVision,
		Text:   "image understood: 2 images · OCR + UI state · 0.8s",
		Detail: `<image-understanding source="@.reasonix/attachments/one.png">
visible_text: first screenshot
confidence: high
</image-understanding>

<image-understanding source="@.reasonix/attachments/two.png">
visible_text: second screenshot
confidence: medium
</image-understanding>`,
	})

	joined := ansi.Strip(strings.Join(m.transcript, "\n"))
	if !strings.Contains(joined, "Image understood · 2 images · OCR + UI state · 0.8s") {
		t.Fatalf("summary missing:\n%s", joined)
	}
	if strings.Contains(joined, "first screenshot") || strings.Contains(joined, "<image-understanding") {
		t.Fatalf("image detail should start collapsed:\n%s", joined)
	}
	if len(m.reasoningIndex) != 1 {
		t.Fatalf("image disclosure should be clickable, index=%v", m.reasoningIndex)
	}
	if !m.toggleReasoningAtTranscriptIdx(0) {
		t.Fatalf("expected image disclosure click to expand")
	}
	expanded := ansi.Strip(strings.Join(m.transcript, "\n"))
	if !strings.Contains(expanded, "Image #1") || !strings.Contains(expanded, "Image #2") {
		t.Fatalf("expanded detail should split multiple images:\n%s", expanded)
	}
	if !strings.Contains(expanded, "first screenshot") || !strings.Contains(expanded, "second screenshot") {
		t.Fatalf("expanded image detail missing body:\n%s", expanded)
	}
	if !m.toggleReasoningAtTranscriptIdx(0) {
		t.Fatalf("expected image disclosure click to collapse")
	}
	collapsed := ansi.Strip(strings.Join(m.transcript, "\n"))
	if strings.Contains(collapsed, "first screenshot") {
		t.Fatalf("collapsed image detail leaked:\n%s", collapsed)
	}
}

func TestImageUnderstandingDisclosureKeepsSingleTaggedBlockWithBlankLines(t *testing.T) {
	m := newTestChatTUI()

	m.ingestEvent(event.Event{
		Kind:   event.Notice,
		Source: event.UsageSourceVision,
		Text:   "image understood: 1 image · OCR + UI state · 333ms",
		Detail: `<image-understanding source="@.reasonix/attachments/clipboard.png" sha256="abc">
visible_text:
• reasonix · deepseek-v4-flash + planner deepseek-v4-pro

ui_state: Apple Vision OCR sidecar
errors:

layout: 1218x262; text_regions=2
confidence: medium
</image-understanding>`,
	})

	if !m.toggleReasoningAtTranscriptIdx(0) {
		t.Fatalf("expected image disclosure click to expand")
	}
	expanded := ansi.Strip(strings.Join(m.transcript, "\n"))
	if !strings.Contains(expanded, "Image understanding") {
		t.Fatalf("single image should use single-image heading:\n%s", expanded)
	}
	if strings.Contains(expanded, "Image #2") {
		t.Fatalf("single tagged block with blank lines should not create a fake second image:\n%s", expanded)
	}
	if !strings.Contains(expanded, "</image-understanding>") {
		t.Fatalf("closing tag should stay with the single image block:\n%s", expanded)
	}
}

func TestReplayHistoryCollapsesReasoningContent(t *testing.T) {
	m := newTestChatTUI()
	m.lazyReasoning = true

	m.replayHistory([]provider.Message{
		{Role: provider.RoleUser, Content: "hello"},
		{
			Role:             provider.RoleAssistant,
			ReasoningContent: "private step one\nprivate step two",
			Content:          "visible answer",
		},
	}, m.width)

	joined := strings.Join(m.transcript, "\n")
	if !hasThoughtFor(joined) {
		t.Fatalf("replayed reasoning should render a collapsed summary:\n%s", joined)
	}
	if strings.Contains(joined, "private step") {
		t.Fatalf("replayed lazy reasoning leaked by default:\n%s", joined)
	}
	if !strings.Contains(joined, "visible answer") {
		t.Fatalf("replayed assistant answer missing:\n%s", joined)
	}
	if len(m.reasoningIndex) != 1 {
		t.Fatalf("replayed reasoning should be clickable, index=%v", m.reasoningIndex)
	}

	idx := -1
	for k := range m.reasoningIndex {
		idx = k
	}
	if !m.toggleReasoningAtTranscriptIdx(idx) {
		t.Fatalf("expected replayed reasoning summary to expand")
	}
	if got := strings.Join(m.transcript, "\n"); !strings.Contains(got, "private step one") {
		t.Fatalf("expanded replayed reasoning missing body:\n%s", got)
	}
	if !m.toggleReasoningAtTranscriptIdx(idx) {
		t.Fatalf("expected replayed reasoning body to collapse")
	}
	if got := strings.Join(m.transcript, "\n"); strings.Contains(got, "private step one") {
		t.Fatalf("collapsed replayed reasoning leaked body:\n%s", got)
	}
}

func TestReplayHistorySuppressesReasoningForHiddenAssistantMessages(t *testing.T) {
	m := newTestChatTUI()
	m.lazyReasoning = true

	m.replayHistory([]provider.Message{
		{
			Role:             provider.RoleAssistant,
			ReasoningContent: "private handoff reasoning",
			Content:          "# Reasonix executor handoff\n\nExecutor instructions:\ninternal",
		},
		{Role: provider.RoleAssistant, Content: "visible answer"},
	}, m.width)

	joined := strings.Join(m.transcript, "\n")
	if hasThoughtFor(joined) || strings.Contains(joined, "private handoff reasoning") {
		t.Fatalf("hidden assistant history should not leave reasoning summaries:\n%s", joined)
	}
	if strings.Contains(joined, "Reasonix executor handoff") || strings.Contains(joined, "Executor instructions") {
		t.Fatalf("hidden assistant history leaked:\n%s", joined)
	}
	if !strings.Contains(joined, "visible answer") {
		t.Fatalf("visible assistant answer missing:\n%s", joined)
	}
}

func TestReplayHistoryStripsImageContextFromUserTurn(t *testing.T) {
	m := newTestChatTUI()
	content := `Image understanding context:

<image-understanding source="@.reasonix/attachments/shot.png" sha256="abc">
visible_text: internal ocr
</image-understanding>

Referenced context:

<image path=".reasonix/attachments/shot.png">
[image attachment available at @.reasonix/attachments/shot.png]
</image>

[image1] what is wrong?`

	m.replayHistory([]provider.Message{
		{Role: provider.RoleUser, Content: content},
	}, m.width)

	joined := strings.Join(m.transcript, "\n")
	for _, unwanted := range []string{
		"Image understanding context",
		"<image-understanding",
		"Referenced context",
		"<image path=",
		"image attachment available",
		"internal ocr",
	} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("replayed user turn leaked %q:\n%s", unwanted, joined)
		}
	}
	if !strings.Contains(joined, "[image1] what is wrong?") {
		t.Fatalf("replayed user turn lost visible input:\n%s", joined)
	}
}

func TestReplayHistoryStripsReferencedImageContextAfterComposePrefix(t *testing.T) {
	m := newTestChatTUI()
	content := `<reasoning-language>
可见推理/思考文本偏好：请使用简体中文。
</reasoning-language>

Referenced context:

<image path=".reasonix/attachments/clipboard-20260710-181440.565354-000013.png">
[image attachment available at [image1]; sent as direct model image input only when the selected model supports vision. Text-only models can still use an available OCR/image/vision tool with this local path; image bytes are not inlined into prompt text.]
</image>

[image2] 这个页面太粗糙，不 native`

	m.replayHistory([]provider.Message{
		{Role: provider.RoleUser, Content: content},
	}, m.width)

	joined := strings.Join(m.transcript, "\n")
	for _, unwanted := range []string{
		"reasoning-language",
		"可见推理",
		"Referenced context",
		"<image path=",
		"image attachment available",
		"direct model image input",
		"clipboard-20260710",
	} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("replayed user turn leaked %q:\n%s", unwanted, joined)
		}
	}
	if !strings.Contains(joined, "[image2] 这个页面太粗糙，不 native") {
		t.Fatalf("replayed user turn lost visible input:\n%s", joined)
	}
}

func TestReplayHistoryRebuildsCollapsedImageUnderstandingDisclosure(t *testing.T) {
	m := newTestChatTUI()
	content := `<reasoning-language>
可见推理/思考文本偏好：请使用简体中文。
</reasoning-language>

Referenced context:

<image path=".reasonix/attachments/clipboard-20260710-181440.565354-000013.png">
[image attachment available at [image1]]
</image>

Image understanding context:

<image-understanding source="@.reasonix/attachments/clipboard-20260710-181440.565354-000013.png" sha256="abc">
visible_text: internal OCR text
ui_state: internal UI state
</image-understanding>

[image2] 帮我看看`

	m.replayHistory([]provider.Message{
		{Role: provider.RoleUser, Content: content},
	}, m.width)

	joined := strings.Join(m.transcript, "\n")
	for _, unwanted := range []string{
		"Image understanding context",
		"<image-understanding",
		"visible_text:",
		"internal OCR text",
		"Referenced context",
		"<image path=",
		"reasoning-language",
	} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("replayed user turn leaked %q before expansion:\n%s", unwanted, joined)
		}
	}
	if !strings.Contains(joined, "[image2] 帮我看看") {
		t.Fatalf("replayed user turn lost visible input:\n%s", joined)
	}
	if !strings.Contains(joined, "Image understood") {
		t.Fatalf("image understanding summary missing:\n%s", joined)
	}

	var summaryIdx = -1
	for idx, line := range m.transcript {
		if strings.Contains(line, "Image understood") {
			summaryIdx = idx
			break
		}
	}
	if summaryIdx < 0 {
		t.Fatalf("image understanding summary index missing:\n%s", joined)
	}
	id, ok := m.reasoningIndex[summaryIdx]
	if !ok {
		t.Fatalf("image understanding summary is not clickable")
	}
	block := m.completedReasoning[id]
	if block == nil || block.expanded {
		t.Fatalf("image understanding should be remembered collapsed by default: %+v", block)
	}
	if block.kind != transcriptDisclosureImageUnderstanding {
		t.Fatalf("wrong disclosure kind = %v", block.kind)
	}
	if !strings.Contains(block.raw, "<image-understanding") || !strings.Contains(block.raw, "internal UI state") {
		t.Fatalf("image understanding raw detail not preserved: %q", block.raw)
	}
}

func TestReplayHistoryImageUnderstandingMatchesLiveDisclosureRendering(t *testing.T) {
	detail := `<image-understanding source="@.reasonix/attachments/clipboard.png" sha256="abc">
visible_text: same OCR
ui_state: same UI
</image-understanding>`

	live := newTestChatTUI()
	live.ingestEvent(event.Event{
		Kind:   event.Notice,
		Source: event.UsageSourceVision,
		Text:   "image understood: 1 image · OCR + UI state",
		Detail: detail,
	})

	replay := newTestChatTUI()
	replay.replayHistory([]provider.Message{
		{Role: provider.RoleUser, Content: "Image understanding context:\n\n" + detail + "\n\n[image1] same prompt"},
	}, replay.width)

	liveCollapsed := ansi.Strip(strings.Join(live.transcript, "\n"))
	replayCollapsed := ansi.Strip(strings.Join(replay.transcript, "\n"))
	if !strings.Contains(liveCollapsed, "Image understood · 1 image · OCR + UI state") {
		t.Fatalf("live summary missing:\n%s", liveCollapsed)
	}
	if !strings.Contains(replayCollapsed, "Image understood · 1 image · OCR + UI state") {
		t.Fatalf("replay summary missing:\n%s", replayCollapsed)
	}
	if strings.Contains(replayCollapsed, "<image-understanding") || strings.Contains(replayCollapsed, "same OCR") {
		t.Fatalf("replay disclosure should start collapsed:\n%s", replayCollapsed)
	}

	liveIdx := firstTranscriptIndexContaining(live.transcript, "Image understood")
	replayIdx := firstTranscriptIndexContaining(replay.transcript, "Image understood")
	if liveIdx < 0 || replayIdx < 0 {
		t.Fatalf("missing disclosure indexes: live=%d replay=%d", liveIdx, replayIdx)
	}
	if !live.toggleReasoningAtTranscriptIdx(liveIdx) {
		t.Fatalf("live disclosure did not expand")
	}
	if !replay.toggleReasoningAtTranscriptIdx(replayIdx) {
		t.Fatalf("replay disclosure did not expand")
	}
	liveExpanded := ansi.Strip(strings.Join(live.transcript, "\n"))
	replayExpanded := ansi.Strip(strings.Join(replay.transcript, "\n"))
	for _, want := range []string{"Image understanding", "<image-understanding", "same OCR", "same UI"} {
		if !strings.Contains(liveExpanded, want) {
			t.Fatalf("live expanded missing %q:\n%s", want, liveExpanded)
		}
		if !strings.Contains(replayExpanded, want) {
			t.Fatalf("replay expanded missing %q:\n%s", want, replayExpanded)
		}
	}
}

func firstTranscriptIndexContaining(lines []string, needle string) int {
	for idx, line := range lines {
		if strings.Contains(line, needle) {
			return idx
		}
	}
	return -1
}

func TestReplayHistoryLazyReasoningIgnoresShowReasoning(t *testing.T) {
	m := newTestChatTUI()
	m.lazyReasoning = true
	m.showReasoning = true

	m.replayHistory([]provider.Message{
		{
			Role:             provider.RoleAssistant,
			ReasoningContent: "private replay reasoning",
			Content:          "visible answer",
		},
	}, m.width)

	joined := strings.Join(m.transcript, "\n")
	if !hasThoughtFor(joined) {
		t.Fatalf("replayed reasoning should render a collapsed summary:\n%s", joined)
	}
	if strings.Contains(joined, "private replay reasoning") {
		t.Fatalf("lazy replay should stay collapsed even when showReasoning is enabled:\n%s", joined)
	}
	if !strings.Contains(joined, "visible answer") {
		t.Fatalf("assistant answer missing:\n%s", joined)
	}
	if len(m.reasoningIndex) != 1 {
		t.Fatalf("replayed reasoning should remain clickable, index=%v", m.reasoningIndex)
	}
}

func TestReplayHistoryKeepsTurnStructure(t *testing.T) {
	m := newTestChatTUI()

	m.replayHistory([]provider.Message{
		{Role: provider.RoleUser, Content: "hello"},
		{Role: provider.RoleAssistant, Content: "first answer"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "second answer"},
	}, m.width)

	joined := ansi.Strip(strings.Join(m.transcript, "\n"))
	if strings.Count(joined, "› ") != 2 {
		t.Fatalf("replayed user turns should remain visible as separate bubbles:\n%s", joined)
	}
	lines := strings.Split(joined, "\n")
	nextLine := -1
	for i, line := range lines {
		if strings.Contains(line, "› next") {
			nextLine = i
			break
		}
	}
	if nextLine <= 0 || strings.TrimSpace(lines[nextLine-1]) != "" {
		t.Fatalf("replayed user turn should be separated by a blank turn spacer:\n%s", joined)
	}
	if strings.Count(joined, "◆ Reasonix") != 2 ||
		!strings.Contains(joined, "first answer") ||
		!strings.Contains(joined, "second answer") {
		t.Fatalf("replayed assistant answers should keep assistant identity blocks:\n%s", joined)
	}
}

func TestReplayHistorySkipsSyntheticExecutorHandoff(t *testing.T) {
	m := newTestChatTUI()

	m.replayHistory([]provider.Message{
		{Role: provider.RoleUser, Content: "hello"},
		{Role: provider.RoleUser, Content: "You are already in the executor phase. The planner's read-only limitations do not apply to you.\n\nUse your available tools now to carry out the task."},
		{Role: provider.RoleAssistant, Content: "visible answer"},
	}, m.width)

	joined := strings.Join(m.transcript, "\n")
	if strings.Contains(joined, "executor phase") || strings.Contains(joined, "planner's read-only limitations") {
		t.Fatalf("replayed history leaked synthetic executor handoff:\n%s", joined)
	}
	if !strings.Contains(joined, "hello") {
		t.Fatalf("real user message missing:\n%s", joined)
	}
	if !strings.Contains(joined, "visible answer") {
		t.Fatalf("assistant answer missing:\n%s", joined)
	}
}

func TestReplayHistorySkipsAssistantExecutorHandoff(t *testing.T) {
	m := newTestChatTUI()
	handoff := "# Reasonix executor handoff\n\n" +
		"You are the executor now. Use your available tools to execute the task.\n\n" +
		"Original task:\nhello\n\n" +
		"Planner output:\nplanner boilerplate\n\n" +
		"Executor instructions:\ninternal instructions"

	m.replayHistory([]provider.Message{
		{Role: provider.RoleUser, Content: "hello"},
		{Role: provider.RoleAssistant, Content: handoff},
		{Role: provider.RoleAssistant, Content: "visible answer"},
	}, m.width)

	joined := strings.Join(m.transcript, "\n")
	for _, unwanted := range []string{
		"Reasonix executor handoff",
		"Planner output",
		"Executor instructions",
		"You are the executor now",
	} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("replayed assistant history leaked %q:\n%s", unwanted, joined)
		}
	}
	if !strings.Contains(joined, "hello") {
		t.Fatalf("real user message missing:\n%s", joined)
	}
	if !strings.Contains(joined, "visible answer") {
		t.Fatalf("assistant answer missing:\n%s", joined)
	}
}

func TestReplayHistoryDisplaysExecutorHandoffOriginalTask(t *testing.T) {
	m := newTestChatTUI()
	handoff := "<reasoning-language>\nuse Chinese\n</reasoning-language>\n\n" +
		"# Reasonix executor handoff\n\n" +
		"You are the executor now. Use your available tools to execute the task.\n\n" +
		"Original task:\n" +
		"<hook-context event=\"SessionStart\">\nlocal context\n</hook-context>\n\n" +
		"<reasoning-language>\nuse Chinese\n</reasoning-language>\n\n" +
		"hello\n\n" +
		"Planner output:\nplanner boilerplate\n\n" +
		"Executor instructions:\ninternal instructions"

	m.replayHistory([]provider.Message{
		{Role: provider.RoleUser, Content: handoff},
		{Role: provider.RoleAssistant, Content: "visible answer"},
	}, m.width)

	joined := strings.Join(m.transcript, "\n")
	for _, unwanted := range []string{
		"Reasonix executor handoff",
		"Planner output",
		"Executor instructions",
		"local context",
		"reasoning-language",
	} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("replayed handoff leaked %q:\n%s", unwanted, joined)
		}
	}
	if !strings.Contains(joined, "hello") {
		t.Fatalf("original task missing from replayed handoff:\n%s", joined)
	}
	if !strings.Contains(joined, "visible answer") {
		t.Fatalf("assistant answer missing:\n%s", joined)
	}
}

func TestReplayHistoryRestoresToolCallCards(t *testing.T) {
	m := newTestChatTUI()

	m.replayHistory([]provider.Message{
		{Role: provider.RoleUser, Content: "inspect"},
		{Role: provider.RoleAssistant, ReasoningContent: "Need to inspect files first.", ToolCalls: []provider.ToolCall{{
			ID:        "call_1",
			Name:      "read_file",
			Arguments: `{"path":"README.md"}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "call_1", Name: "read_file", Content: "one\ntwo\n"},
		{Role: provider.RoleAssistant, Content: "done"},
	}, m.width)

	joined := ansi.Strip(strings.Join(m.transcript, "\n"))
	if !strings.Contains(joined, "› inspect") {
		t.Fatalf("user turn missing:\n%s", joined)
	}
	if !hasThoughtFor(joined) {
		t.Fatalf("tool-only assistant reasoning should stay visible as collapsed thinking:\n%s", joined)
	}
	if !strings.Contains(joined, "Read(README.md)") {
		t.Fatalf("replayed tool dispatch missing:\n%s", joined)
	}
	if !strings.Contains(joined, "2 lines") {
		t.Fatalf("replayed tool result summary missing:\n%s", joined)
	}
	if !strings.Contains(joined, "done") {
		t.Fatalf("final assistant reply missing:\n%s", joined)
	}
}

func TestReplayHistoryRestoresToolErrorCards(t *testing.T) {
	m := newTestChatTUI()

	m.replayHistory([]provider.Message{
		{Role: provider.RoleUser, Content: "run it"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID:        "call_1",
			Name:      "bash",
			Arguments: `{"command":"make test"}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "call_1", Name: "bash", Content: "error: command exited: status 2\n"},
	}, m.width)

	joined := ansi.Strip(strings.Join(m.transcript, "\n"))
	if !strings.Contains(joined, "Bash(make test)") {
		t.Fatalf("replayed bash dispatch missing:\n%s", joined)
	}
	if !strings.Contains(joined, "error: command exited: status 2") {
		t.Fatalf("replayed tool error missing:\n%s", joined)
	}
}

func TestBuildConversationRecapSkipsInternalHandoff(t *testing.T) {
	recap := buildConversationRecap([]provider.Message{
		{Role: provider.RoleUser, Content: "hello"},
		{Role: provider.RoleAssistant, Content: "# Reasonix executor handoff\n\nYou are the executor now.\n\nOriginal task:\nhello\n\nPlanner output:\nplan\n\nExecutor instructions:\ninternal"},
		{Role: provider.RoleUser, Content: "You are already in the executor phase. The planner's read-only limitations do not apply to you.\n\nUse your available tools now to carry out the task."},
		{Role: provider.RoleAssistant, Content: "visible answer"},
	}, "focus on state", 80)

	if !strings.Contains(recap, "焦点: focus on state") {
		t.Fatalf("recap missing focus:\n%s", recap)
	}
	if strings.Contains(recap, "executor handoff") || strings.Contains(recap, "Executor instructions") || strings.Contains(recap, "executor phase") {
		t.Fatalf("recap leaked internal handoff text:\n%s", recap)
	}
	if !strings.Contains(recap, "用户: hello") || !strings.Contains(recap, "助手: visible answer") {
		t.Fatalf("recap missing visible turns:\n%s", recap)
	}
}

func TestLazyReasoningToggleShiftsStreamingAnswer(t *testing.T) {
	m := newTestChatTUI()
	m.lazyReasoning = true

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "inspect context"})
	m.ingestEvent(event.Event{Kind: event.Text, Text: "first paragraph\n\n"})
	if m.answerIdx != 1 {
		t.Fatalf("answerIdx = %d, want 1 before expanding reasoning; transcript=%v", m.answerIdx, m.transcript)
	}
	if !m.toggleReasoningAtTranscriptIdx(0) {
		t.Fatalf("expected summary click to expand reasoning")
	}
	if m.answerIdx != 1 {
		t.Fatalf("answerIdx = %d, want 1 after in-place reasoning expansion; transcript=%v", m.answerIdx, m.transcript)
	}

	m.ingestEvent(event.Event{Kind: event.Text, Text: "second paragraph\n\n"})
	if m.answerIdx < 0 || m.answerIdx >= len(m.transcript) {
		t.Fatalf("answerIdx out of range after streaming more text: idx=%d len=%d", m.answerIdx, len(m.transcript))
	}
	if strings.Contains(m.transcript[0], "second paragraph") {
		t.Fatalf("streaming answer overwrote expanded reasoning body: transcript=%v", m.transcript)
	}
	if !strings.Contains(m.transcript[m.answerIdx], "second paragraph") {
		t.Fatalf("streaming answer did not update answer block: idx=%d transcript=%v", m.answerIdx, m.transcript)
	}

	if !m.toggleReasoningAtTranscriptIdx(0) {
		t.Fatalf("expected body click to collapse reasoning")
	}
	if m.answerIdx != 1 {
		t.Fatalf("answerIdx = %d, want 1 after in-place reasoning collapse; transcript=%v", m.answerIdx, m.transcript)
	}
	m.ingestEvent(event.Event{Kind: event.Text, Text: "third paragraph\n\n"})
	if m.answerIdx < 0 || m.answerIdx >= len(m.transcript) {
		t.Fatalf("answerIdx out of range after collapsing reasoning: idx=%d len=%d", m.answerIdx, len(m.transcript))
	}
	if !strings.Contains(m.transcript[m.answerIdx], "third paragraph") {
		t.Fatalf("streaming answer missing third paragraph: idx=%d transcript=%v", m.answerIdx, m.transcript)
	}
}

func TestLazyReasoningToggleRecordsViewportAnchorDelta(t *testing.T) {
	m := newTestChatTUI()
	m.lazyReasoning = true

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: strings.Repeat("long reasoning line ", 20)})
	m.ingestEvent(event.Event{Kind: event.Text, Text: "Answer"})
	if len(m.transcript) != 1 {
		t.Fatalf("collapsed lazy reasoning should be one entry, transcript=%v", m.transcript)
	}
	if !m.toggleReasoningAtTranscriptIdx(0) {
		t.Fatal("expected summary click to expand reasoning")
	}
	if len(m.transcript) != 1 {
		t.Fatalf("expanded lazy reasoning should still be one entry, transcript=%v", m.transcript)
	}
	if m.viewportAnchorDelta <= 0 {
		t.Fatalf("expansion should record a positive viewport anchor delta, got %d", m.viewportAnchorDelta)
	}
	m.viewportAnchorDelta = 0
	if !m.toggleReasoningAtTranscriptIdx(0) {
		t.Fatal("expected body click to collapse reasoning")
	}
	if m.viewportAnchorDelta >= 0 {
		t.Fatalf("collapse should record a negative viewport anchor delta, got %d", m.viewportAnchorDelta)
	}
}

func TestLazyReasoningHoverRestylesSummaryButNotExpandedBody(t *testing.T) {
	prevColor := activeColorProfile
	activeColorProfile = colorprofile.ANSI256
	defer func() { activeColorProfile = prevColor }()

	m := newTestChatTUI()
	m.lazyReasoning = true

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "step one\nstep two"})
	m.ingestEvent(event.Event{Kind: event.Text, Text: "Answer"})
	collapsed := m.transcript[0]
	if !m.setTranscriptHover(0, transcriptHoverReasoning) {
		t.Fatal("expected hover to restyle the collapsed reasoning summary")
	}
	if m.transcript[0] == collapsed {
		t.Fatalf("hover should change the rendered summary style")
	}
	if !hasThoughtFor(m.transcript[0]) {
		t.Fatalf("hover should preserve summary text: %q", m.transcript[0])
	}
	if strings.Contains(ansi.Strip(m.transcript[0]), "\n") {
		t.Fatalf("hovered summary should stay a single-line clickable target: %q", m.transcript[0])
	}
	if !m.toggleReasoningAtTranscriptIdx(0) {
		t.Fatal("expected reasoning to expand")
	}
	body := m.transcript[0]
	if m.setTranscriptHover(0, transcriptHoverReasoning) {
		t.Fatal("hover over expanded reasoning body should not re-render")
	}
	if m.transcript[0] != body {
		t.Fatalf("expanded reasoning body should not visually change on hover")
	}
	if !strings.Contains(ansi.Strip(m.transcript[0]), "step two") {
		t.Fatalf("hover should preserve body text: %q", m.transcript[0])
	}
}

func TestLazyReasoningExpandedPlainClickCollapsesButDragSelectionDoesNot(t *testing.T) {
	makeExpanded := func(t *testing.T) chatTUI {
		t.Helper()
		m := newTestChatTUI()
		m.lazyReasoning = true
		m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "step one\nstep two\nstep three"})
		m.ingestEvent(event.Event{Kind: event.Text, Text: "Answer"})
		if !m.toggleReasoningAtTranscriptIdx(0) {
			t.Fatal("expected reasoning to expand")
		}
		wrapped, lineMap := wrapTranscriptEntries(m.transcript, 80)
		m.viewport = viewport.New(viewport.WithWidth(80))
		m.viewport.SetHeight(20)
		m.viewport.SetContent(wrapped)
		m.wrappedLines = strings.Split(wrapped, "\n")
		m.wrappedLineTranscriptIdx = lineMap
		return m
	}
	rowOf := func(t *testing.T, m chatTUI, needle string) int {
		t.Helper()
		for i, line := range m.wrappedLines {
			if strings.Contains(ansi.Strip(line), needle) {
				return i
			}
		}
		t.Fatalf("could not find %q in wrapped lines:\n%s", needle, strings.Join(m.wrappedLines, "\n"))
		return 0
	}

	click := makeExpanded(t)
	row := rowOf(t, click, "step one")
	next, _ := click.update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 6, Y: row})
	click = next.(chatTUI)
	next, _ = click.update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 6, Y: row})
	click = next.(chatTUI)
	if click.reasoningExpandedAtTranscriptIdx(0) {
		t.Fatal("plain click release should collapse expanded reasoning")
	}

	drag := makeExpanded(t)
	row = rowOf(t, drag, "step one")
	next, _ = drag.update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 6, Y: row})
	drag = next.(chatTUI)
	next, _ = drag.update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: 15, Y: row})
	drag = next.(chatTUI)
	next, _ = drag.update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 15, Y: row})
	drag = next.(chatTUI)
	if !drag.reasoningExpandedAtTranscriptIdx(0) {
		t.Fatal("drag selection over expanded reasoning should not collapse it")
	}
	if !strings.Contains(ansi.Strip(drag.transcript[0]), "step two") {
		t.Fatalf("drag selection should preserve expanded body, got %q", drag.transcript[0])
	}
}

func TestShellClickTogglesTargetOutput(t *testing.T) {
	m := newTestChatTUI()
	m.shellOutputs["shell-1"] = strings.Join([]string{
		"one-01", "one-02", "one-03", "one-04", "one-05", "one-06",
		"one-07", "one-08", "one-09", "one-10", "one-11",
	}, "\n")
	m.shellOutputs["shell-2"] = strings.Join([]string{
		"two-01", "two-02", "two-03", "two-04", "two-05", "two-06",
		"two-07", "two-08", "two-09", "two-10", "two-11",
	}, "\n")
	m.shellTranscriptIdx["shell-1"] = 0
	m.shellTranscriptIdx["shell-2"] = 1
	m.transcript = []string{
		m.renderShellOutputBlock("shell-1", false),
		m.renderShellOutputBlock("shell-2", false),
	}
	wrapped, lineMap := wrapTranscriptEntries(m.transcript, 80)
	m.wrappedLines = strings.Split(wrapped, "\n")
	m.wrappedLineTranscriptIdx = lineMap

	idx, kind, ok := m.clickableAtWrappedLine(0)
	if !ok || idx != 0 || kind != transcriptHoverShell {
		t.Fatalf("line 0 should hit first shell output, got idx=%d kind=%v ok=%v", idx, kind, ok)
	}
	if !m.toggleShellOutputAtTranscriptIdx(idx) {
		t.Fatal("expected target shell output to toggle")
	}
	if !m.shellExpanded["shell-1"] {
		t.Fatal("first shell output should be expanded")
	}
	if m.shellExpanded["shell-2"] {
		t.Fatal("clicking first shell output must not expand the second")
	}
}

// TestIngestEventFlushesAnswer confirms an event line (e.g. a tool dispatch)
// finalizes the answer streamed before it, preserving order in scrollback.
func TestIngestEventFlushesAnswer(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.Text, Text: "partial answer "})
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "read_file", Args: `{"path":"x"}`}})
	// answer, then a blank spacer, then the tool line.
	if n := len(*m.pendingCommit); n != 3 {
		t.Fatalf("answer + spacer + event line should be three commits, got %d: %v", n, *m.pendingCommit)
	}
	if !strings.Contains((*m.pendingCommit)[0], "partial answer") {
		t.Errorf("first commit should be the buffered answer, got %q", (*m.pendingCommit)[0])
	}
	if strings.TrimSpace((*m.pendingCommit)[1]) != "" {
		t.Errorf("second commit should be a blank spacer, got %q", (*m.pendingCommit)[1])
	}
	if !strings.Contains((*m.pendingCommit)[2], "Read(x)") {
		t.Errorf("third commit should be the tool card, got %q", (*m.pendingCommit)[2])
	}
	if m.pending.Len() != 0 {
		t.Errorf("answer buffer should be drained after the event line")
	}
}

// TestStreamAnswerFlushesCompletedParagraphs proves a multi-paragraph answer
// appears chunk by chunk: a closed paragraph renders to scrollback while the
// still-streaming one stays buffered, and turn end flushes the remainder.
func TestStreamAnswerFlushesCompletedParagraphs(t *testing.T) {
	m := newTestChatTUI()

	m.ingestEvent(event.Event{Kind: event.Text, Text: "First paragraph.\n\nSecond para "})
	if m.answerIdx < 0 {
		t.Fatalf("a completed paragraph should open a streamed answer block")
	}
	joined := strings.Join(m.transcript, "\n")
	if !strings.Contains(joined, "First paragraph.") {
		t.Errorf("completed paragraph should be on screen, transcript=%v", m.transcript)
	}
	if strings.Contains(joined, "Second para") {
		t.Errorf("the still-streaming paragraph must stay buffered, transcript=%v", m.transcript)
	}

	m.ingestEvent(event.Event{Kind: event.Text, Text: "is done now."})
	m.ingestEvent(event.Event{Kind: event.Message})
	final := strings.Join(m.transcript, "\n")
	if !strings.Contains(final, "First paragraph.") || !strings.Contains(final, "Second para is done now.") {
		t.Errorf("turn end should flush the whole answer, transcript=%v", m.transcript)
	}
	if m.pending.Len() != 0 || m.answerIdx != -1 {
		t.Errorf("answer state should reset after commit, pending=%d idx=%d", m.pending.Len(), m.answerIdx)
	}
}

// TestFlushableMarkdownPrefixKeepsOpenFence proves a blank line inside an unclosed
// fenced code block is not a flush boundary — the half-written block stays buffered
// so it never renders mangled, while prose before the fence does flush.
func TestFlushableMarkdownPrefixKeepsOpenFence(t *testing.T) {
	open := "intro line\n\n```go\nfunc f() {\n\n\t// still typing"
	if got := flushableMarkdownPrefix(open); got != "intro line" {
		t.Errorf("open fence: flushable prefix = %q, want %q", got, "intro line")
	}

	closed := "```go\ncode\n\nmore\n```\n\ntrailing"
	if got := flushableMarkdownPrefix(closed); got != "```go\ncode\n\nmore\n```" {
		t.Errorf("closed fence: flushable prefix = %q", got)
	}

	if got := flushableMarkdownPrefix("no boundary yet"); got != "" {
		t.Errorf("no blank line should flush nothing, got %q", got)
	}
}

// TestToolProgressStreamsThenCollapses proves a running tool's output streams
// live under its card via the ⎿ connector, then collapses to a line-count
// summary when the result lands.
func TestToolProgressStreamsThenCollapses(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "b1", Name: "bash", Args: `{"command":"go test ./..."}`}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "ok pkg/a\n"}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "ok pkg/b\n"}})

	joined := strings.Join(m.transcript, "\n")
	if !strings.Contains(joined, "ok pkg/a") || !strings.Contains(joined, "ok pkg/b") {
		t.Fatalf("live output should be visible while running:\n%s", joined)
	}
	if !strings.Contains(joined, "⎿") {
		t.Fatalf("live output should use the ⎿ connector:\n%s", joined)
	}

	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "b1", Name: "bash", Output: "ok pkg/a\nok pkg/b\n"}})
	joined = strings.Join(m.transcript, "\n")
	if strings.Contains(joined, "ok pkg/a") {
		t.Fatalf("output should collapse after completion:\n%s", joined)
	}
	if !strings.Contains(joined, "2 lines") {
		t.Fatalf("collapsed block should summarize the line count:\n%s", joined)
	}
}

// TestToolWorkingLineThenClears proves a dispatched tool that streams no output
// (e.g. symbol_context) shows a live "working · Ns" line so it doesn't look
// frozen, and that the line clears on the result instead of collapsing to
// "0 lines".
func TestToolWorkingLineThenClears(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "c1", Name: "symbol_context", Args: `{"q":"x"}`}})

	m.tickToolRunning() // one elapsed tick fills the placeholder
	joined := strings.Join(m.transcript, "\n")
	if !strings.Contains(joined, "⎿") || !strings.Contains(joined, "working") {
		t.Fatalf("a running tool should show a 'working' progress line:\n%s", joined)
	}

	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "c1", Name: "symbol_context"}})
	joined = strings.Join(m.transcript, "\n")
	if strings.Contains(joined, "working") {
		t.Fatalf("working line should clear after the result:\n%s", joined)
	}
	if strings.Contains(joined, "0 lines") {
		t.Fatalf("a no-output tool must not collapse to '0 lines':\n%s", joined)
	}
	if m.toolStreamIdx != -1 {
		t.Fatalf("tool block should be closed after the result, idx=%d", m.toolStreamIdx)
	}
}

// TestConsecutiveToolCallsKeepMarkersUnderOwnCard is a regression test for
// back-to-back Bash tool calls. Before the fix, the late ToolProgress for
// the first tool (already superseded in the controller by a second
// ToolDispatch) appended a fresh live block at the end of the transcript
// under the *second* tool's card. Both "⎿" markers then stacked at the
// end, hiding which run produced which output. The fix threads the
// transcript slot through shellTranscriptIdx so each tool's live block
// stays directly under its own card regardless of the dispatch/progress
// arrival order.
func TestConsecutiveToolCallsKeepMarkersUnderOwnCard(t *testing.T) {
	m := newTestChatTUI()
	// First bash: dispatched and gets one progress chunk before the second
	// bash is dispatched, mirroring the model's parallel-tool-call pattern.
	// The "shell-" prefix ensures streamToolOutput accumulates into
	// shellOutputs, which collapseShellSlot uses to recover the line count
	// after the live state has been reset by the second beginToolRunning.
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "shell-1", Name: "bash", Args: `{"command":"git status"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "shell-1", Output: "On branch main-v2\n"}})
	// Second bash dispatched before the first finishes; this switches
	// m.toolStreamID to "shell-2" and resets the live streaming state.
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "shell-2", Name: "bash", Args: `{"command":"git branch -a"}`}})
	// The second bash also streams one chunk of output so its collapse
	// produces a real ⎿ marker (not the zero-output blank fallback).
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "shell-2", Output: "* main-v2\n"}})
	// Late progress for the FIRST bash — the path that previously stacked
	// its marker under the second card.
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "shell-1", Output: "nothing to commit\n"}})
	// Now finish both; each should collapse in place under its own card.
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "shell-1", Name: "bash", Output: "On branch main-v2\nnothing to commit\n"}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "shell-2", Name: "bash", Output: "* main-v2\n"}})

	// Locate each tool's card. With the fix the transcript is exactly
	// [card1, marker1, "", card2, marker2] — 5 lines, one marker per
	// card. Without the fix the late progress overwrites the last slot
	// in place (or appends), so the first card's slot is left holding
	// only the first live chunk, and both markers end up at the tail.
	transcript := m.transcript
	idx1, idx2 := -1, -1
	for i, ln := range transcript {
		if idx1 == -1 && strings.Contains(ln, "git status") {
			idx1 = i
		}
		if idx2 == -1 && strings.Contains(ln, "git branch -a") {
			idx2 = i
		}
	}
	if idx1 < 0 || idx2 < 0 || idx2 <= idx1 {
		t.Fatalf("expected two bash cards in dispatch order, got idx1=%d idx2=%d\n%s", idx1, idx2, strings.Join(transcript, "\n"))
	}

	// Each card must be followed by its own ⎿-prefixed marker slot —
	// not just "some marker somewhere after the second card".
	for _, pair := range []struct {
		card string
		idx  int
	}{
		{card: "git status", idx: idx1},
		{card: "git branch -a", idx: idx2},
	} {
		next := transcript[pair.idx+1]
		if !strings.Contains(next, "⎿") {
			t.Fatalf("%q's marker should be at transcript[%d] with the ⎿ connector, got %q\nfull transcript:\n%s",
				pair.card, pair.idx+1, next, strings.Join(transcript, "\n"))
		}
	}

	// The first card's marker must reflect the full output of the first
	// run ("On branch main-v2" AND "nothing to commit"), not just the
	// first chunk. The bug left only the pre-late-progress chunk in
	// transcript[idx1+1], so the second line would be missing.
	marker1 := transcript[idx1+1]
	if !strings.Contains(marker1, "On branch main-v2") || !strings.Contains(marker1, "nothing to commit") {
		t.Fatalf("first card's marker should preview the full output of shell-1, got %q", marker1)
	}
}

// TestRepeatedShellCommandDoesNotAccumulateOutput is the regression test for a
// re-run of the same "!" command (e.g. !pwd three times). RunShell derives a
// stable id from the command text ("shell-pwd"), so streamToolOutput kept
// appending each run's output onto the previous run's in m.shellOutputs[id];
// beginToolRunning now clears the entry so each run starts from a clean slate.
func TestRepeatedShellCommandDoesNotAccumulateOutput(t *testing.T) {
	m := newTestChatTUI()
	const id = "shell-pwd"
	const out = "/home/user/project\n"

	for range 3 {
		m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: id, Name: "bash", Args: `{"command":"pwd"}`}})
		m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: id, Output: out}})
		m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: id, Name: "bash", Output: out}})
	}

	if got := m.shellOutputs[id]; got != out {
		t.Fatalf("a re-run must not accumulate prior output: shellOutputs[%q] = %q, want %q", id, got, out)
	}
}

func TestShellResultReplacesCappedLiveProgress(t *testing.T) {
	m := newTestChatTUI()
	const id = "shell-build"
	const final = "build started\n...[truncated]...\nfinal compiler error\n"
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: id, Name: "bash", Args: `{"command":"build"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: id, Output: "build started\n...[live capped]...\n"}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: id, Name: "bash", Output: final}})
	if got := m.shellOutputs[id]; got != final {
		t.Fatalf("shellOutputs[%q] = %q, want bounded final result %q", id, got, final)
	}
}

func TestCollapsedShellHintUsesKeyboardShortcutOnly(t *testing.T) {
	m := newTestChatTUI()
	const id = "shell-long"
	lines := make([]string, shellPreviewLines+2)
	for i := range lines {
		lines[i] = "line"
	}
	output := strings.Join(lines, "\n") + "\n"
	m.shellOutputs[id] = output
	m.transcript = []string{""}

	m.collapseShellSlot(id, 0, output)

	got := m.transcript[0]
	if !strings.Contains(got, "more lines (Ctrl+B)") {
		t.Fatalf("collapsed shell hint should mention Ctrl+B, got %q", got)
	}
	if strings.Contains(got, "click/") {
		t.Fatalf("collapsed shell hint must not advertise mouse click in default TUI mode, got %q", got)
	}
}

// TestConsecutiveNonShellToolsDoNotRenderNegativeLineCount is the regression
// test for the review-blocking case. The original fix to back-to-back shell
// tools records every dispatched id in shellTranscriptIdx so a late
// ToolProgress/Result can land in the correct slot. But for non-shell-
// prefixed tools (e.g. read_file) the streaming state belongs to whichever
// id is current and the accumulator (shellOutputs) is never populated, so
// the late path's "n" stayed at -1 and the final else branch rendered
// "⎿ -1 lines". The fix in collapseShellSlot guards n < 0 by clearing the
// slot — a deliberate blank-line fallback rather than a misleading
// negative count.
func TestConsecutiveNonShellToolsDoNotRenderNegativeLineCount(t *testing.T) {
	m := newTestChatTUI()
	// Two back-to-back read_file tools; the first result lands AFTER
	// the second dispatch (the model dispatched them in parallel and
	// the first one finished last). This is the path the PR reviewer
	// identified as the blocker.
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "read_file-1", Name: "read_file", Args: `{"path":"a.txt"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "read_file-2", Name: "read_file", Args: `{"path":"b.txt"}`}})
	// Late ToolResult for the FIRST tool — this used to render "-1 lines"
	// under the first card.
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "read_file-1", Name: "read_file", Output: "a.txt contents"}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "read_file-2", Name: "read_file", Output: "b.txt contents"}})

	transcript := m.transcript
	// The "-1 lines" bug surfaced literally as that text, so assert its
	// absence first as a clear regression marker.
	if joined := strings.Join(transcript, "\n"); strings.Contains(joined, "-1 lines") {
		t.Fatalf("transcript must not contain a negative line count:\n%s", joined)
	}
	// And the more general contract: no slot under a card should claim
	// a non-positive line count either.
	for _, line := range transcript {
		if strings.Contains(line, "0 lines") || strings.Contains(line, "-1 lines") {
			t.Fatalf("non-shell tool marker should be blank, got %q\nfull transcript:\n%s",
				line, strings.Join(transcript, "\n"))
		}
	}
}

func TestTodoPanelKeepsLastSuccessfulTodoWrite(t *testing.T) {
	m := newTestChatTUI()
	initial := `{"todos":[{"content":"Sync main-v2","status":"in_progress"},{"content":"Push origin","status":"pending"}]}`
	failed := `{"todos":[{"content":"Sync main-v2","status":"completed"},{"content":"Push origin","status":"in_progress"}]}`

	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "todo-1", Name: "todo_write", Args: initial}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "todo-1", Name: "todo_write", Args: initial, Output: "Todos updated"}})
	if m.todoArgs != initial {
		t.Fatalf("todoArgs after successful result = %q, want initial args", m.todoArgs)
	}

	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "todo-2", Name: "todo_write", Args: failed}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "todo-2", Name: "todo_write", Args: failed, Err: "missing complete_step"}})
	if m.todoArgs != initial {
		t.Fatalf("failed todo_write must not replace the panel: got %q, want %q", m.todoArgs, initial)
	}
}

// TestToolProgressTailCap proves the live block only keeps the last
// toolStreamTailLines lines so a chatty build doesn't flood scrollback.
func TestToolProgressTailCap(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "b1", Name: "bash", Args: `{"command":"x"}`}})
	for i := range toolStreamTailLines + 5 {
		m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "line" + string(rune('A'+i)) + "\n"}})
	}
	block := m.transcript[m.toolStreamIdx]
	if got := strings.Count(block, "\n") + 1; got > toolStreamTailLines {
		t.Fatalf("live block kept %d lines, want <= %d:\n%s", got, toolStreamTailLines, block)
	}
	if strings.Contains(block, "lineA") {
		t.Fatalf("oldest line should have scrolled out of the tail:\n%s", block)
	}
}

// TestReasoningViewBounded proves the live thinking view stays bounded under a
// long stream — the fix for the O(n²)/multi-GB re-render of the full thought.
func TestReasoningViewBounded(t *testing.T) {
	m := newTestChatTUI()
	for range 5000 {
		m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "some thinking text token "})
	}
	if len(m.reasoningView) > reasoningViewMax {
		t.Fatalf("reasoningView unbounded: %d > %d", len(m.reasoningView), reasoningViewMax)
	}
	if c := strings.Count(m.transcript[m.reasoningTextIdx], "\n") + 1; c > reasoningTailLines {
		t.Fatalf("live reasoning block kept %d lines, want <= %d", c, reasoningTailLines)
	}
}

// TestSubagentProgressBlockShowsPhaseElapsedActivity proves the default block
// shows phase, elapsed, and recent activity — never the reasoning body.
func TestSubagentProgressBlockShowsPhaseElapsedActivity(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "task-1", Name: "task", Args: `{"prompt":"work"}`}})
	m.ingestEvent(subagentStatus("task-1", "running"))
	m.ingestEvent(subagentPreview("task-1", event.SubagentProgressReasoningName, "secret thinking", false))
	m.ingestEvent(subagentStatus("task-1", "reasoning"))

	joined := strings.Join(m.transcript, "\n")
	if !strings.Contains(joined, "running") && !strings.Contains(joined, "reasoning") {
		t.Fatalf("progress block should show the phase:\n%s", joined)
	}
	if strings.Contains(joined, "secret thinking") {
		t.Fatalf("default block must not print the reasoning body:\n%s", joined)
	}
	if !strings.Contains(joined, "ago") {
		t.Fatalf("progress block should show recent activity:\n%s", joined)
	}
}

// TestSubagentProgressVerboseShowsBoundedTails proves verbose mode renders the
// reasoning/text tails and marks truncation.
func TestSubagentProgressVerboseShowsBoundedTails(t *testing.T) {
	m := newTestChatTUI()
	m.showReasoning = true
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "task-1", Name: "task", Args: `{"prompt":"work"}`}})
	m.ingestEvent(subagentStatus("task-1", "running"))
	m.ingestEvent(subagentPreview("task-1", event.SubagentProgressReasoningName, "chain of thought", false))
	m.ingestEvent(subagentPreview("task-1", event.SubagentProgressTextName, "draft answer", false))
	m.ingestEvent(subagentPreview("task-1", event.SubagentProgressNoticeName, "heads up", true))

	joined := strings.Join(m.transcript, "\n")
	for _, want := range []string{"chain of thought", "draft answer", "heads up", "truncated"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("verbose block should show %q:\n%s", want, joined)
		}
	}

	// Tails are bounded: a huge reasoning body keeps only the recent tail.
	m.ingestEvent(subagentPreview("task-1", event.SubagentProgressReasoningName, strings.Repeat("x", subagentPreviewMax*2)+"END", false))
	joined = strings.Join(m.transcript, "\n")
	if !strings.Contains(joined, "END") || strings.Contains(joined, strings.Repeat("x", subagentPreviewMax)) {
		t.Fatalf("verbose reasoning should keep a bounded tail:\n%s", joined)
	}
}

// TestSubagentProgressTerminalCollapsesToOneLine proves terminal children fold
// to a one-line summary (no recent-activity suffix), while the preview stays
// available in verbose mode.
func TestSubagentProgressTerminalCollapsesToOneLine(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "task-1", Name: "task", Args: `{"prompt":"work"}`}})
	m.ingestEvent(subagentStatus("task-1", "running"))
	m.ingestEvent(subagentPreview("task-1", event.SubagentProgressTextName, "answer body", false))
	m.ingestEvent(subagentStatus("task-1", "completed"))

	joined := strings.Join(m.transcript, "\n")
	if strings.Contains(joined, "answer body") {
		t.Fatalf("terminal block must collapse the preview away:\n%s", joined)
	}
	if !strings.Contains(joined, "completed") || strings.Contains(joined, "ago") {
		t.Fatalf("terminal block should be a one-line summary:\n%s", joined)
	}

	// Verbose keeps the preview after terminal.
	m2 := newTestChatTUI()
	m2.showReasoning = true
	m2.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "task-1", Name: "task", Args: `{"prompt":"work"}`}})
	m2.ingestEvent(subagentPreview("task-1", event.SubagentProgressTextName, "answer body", false))
	m2.ingestEvent(subagentStatus("task-1", "failed"))
	joined = strings.Join(m2.transcript, "\n")
	if !strings.Contains(joined, "answer body") || !strings.Contains(joined, "failed") {
		t.Fatalf("verbose terminal block should keep the preview:\n%s", joined)
	}
}

// TestSubagentProgressChildrenDoNotCrossStream proves concurrent children keep
// their own fixed slots: each child's content stays under its own ID, and a
// late event for one child never appends to another child's block.
func TestSubagentProgressChildrenDoNotCrossStream(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "p-1", Name: "parallel_tasks", Args: `{}`}})
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "p-1/sub-1", Name: "task", Args: `{}`, ParentID: "p-1"}})
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "p-1/sub-2", Name: "task", Args: `{}`, ParentID: "p-1"}})

	m.ingestEvent(subagentStatus("p-1/sub-1", "running"))
	m.ingestEvent(subagentStatus("p-1/sub-2", "running"))
	m.ingestEvent(subagentPreview("p-1/sub-1", event.SubagentProgressReasoningName, "AAAA", false))
	m.ingestEvent(subagentPreview("p-1/sub-2", event.SubagentProgressReasoningName, "BBBB", false))
	m.ingestEvent(subagentStatus("p-1/sub-1", "completed"))
	// A late event for child 2 must land in child 2's own slot.
	m.ingestEvent(subagentPreview("p-1/sub-2", event.SubagentProgressTextName, "child two text", false))
	m.ingestEvent(subagentStatus("p-1/sub-2", "completed"))

	idx1, ok1 := m.subagentProgressIdx["p-1/sub-1"]
	idx2, ok2 := m.subagentProgressIdx["p-1/sub-2"]
	if !ok1 || !ok2 || idx1 == idx2 {
		t.Fatalf("children should own distinct fixed slots: %d %d", idx1, idx2)
	}
	if strings.Contains(m.transcript[idx1], "BBBB") || strings.Contains(m.transcript[idx2], "AAAA") {
		t.Fatalf("children cross-streamed:\nidx1=%s\nidx2=%s", m.transcript[idx1], m.transcript[idx2])
	}
	if strings.Contains(m.transcript[idx1], "child two text") {
		t.Fatalf("late child-2 content must never land in child-1's block:\n%s", m.transcript[idx1])
	}
	// The late preview is attributed to the right child in memory (the default
	// collapsed view hides bodies after terminal, verbose shows them again).
	if got := m.subagentProgress["p-1/sub-2"]; got == nil || got.text != "child two text" {
		t.Fatalf("late child-2 text = %+v, want it stored on child 2", got)
	}
	if strings.Contains(m.transcript[idx2], "BBBB") || !strings.Contains(m.transcript[idx2], "completed") {
		t.Fatalf("child-2 terminal block = %q, want its own completed summary", m.transcript[idx2])
	}
}

// TestSubagentProgressOrdinaryToolProgressUnaffected proves non-reserved
// ToolProgress still streams through the single live tool stream.
func TestSubagentProgressOrdinaryToolProgressUnaffected(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "b1", Name: "bash", Args: `{"command":"ls"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "file.txt\n"}})
	if joined := strings.Join(m.transcript, "\n"); !strings.Contains(joined, "file.txt") {
		t.Fatalf("ordinary tool progress must still stream:\n%s", joined)
	}
	if len(m.subagentProgress) != 0 {
		t.Fatalf("ordinary progress must not create sub-agent state")
	}
}

// TestSubagentProgressUnknownReservedChannelIgnored locks forward compatibility:
// an older CLI must suppress a future reasonix.subagent.* channel instead of
// treating its body as ordinary tool output.
func TestSubagentProgressUnknownReservedChannelIgnored(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(subagentPreview("task-1", event.SubagentProgressPrefix+"future", "must stay hidden", false))

	if got := strings.Join(m.transcript, "\n"); got != "" {
		t.Fatalf("unknown reserved progress entered the transcript: %q", got)
	}
	if m.toolStreamID != "" || m.toolLineCount != 0 || m.toolPartial != "" {
		t.Fatalf("unknown reserved progress opened ordinary tool output: id=%q lines=%d partial=%q", m.toolStreamID, m.toolLineCount, m.toolPartial)
	}
	if len(m.subagentProgress) != 0 {
		t.Fatalf("unknown reserved progress allocated known-channel state: %+v", m.subagentProgress)
	}
}

// TestSubagentProgressNativeScrollbackPrintsOnPhaseChange proves Termux-style
// native scrollback (which cannot rewrite printed output) queues a status line
// on phase changes and terminal only — same-phase repeats stay quiet.
func TestSubagentProgressNativeScrollbackPrintsOnPhaseChange(t *testing.T) {
	m := newTestChatTUI()
	m.nativeScrollback = true
	m.ingestEvent(subagentStatus("task-1", "running"))
	m.ingestEvent(subagentStatus("task-1", "running")) // repeat phase: no print
	m.ingestEvent(subagentStatus("task-1", "reasoning"))
	m.ingestEvent(subagentStatus("task-1", "completed"))
	got := strings.Join(*m.pendingCommit, "\n")
	for _, want := range []string{"running", "reasoning", "completed"} {
		if strings.Count(got, want) != 1 {
			t.Fatalf("scrollback output should print each phase exactly once, got %q (count %q = %d)", got, want, strings.Count(got, want))
		}
	}
	if len(m.subagentProgressIdx) != 0 {
		t.Fatalf("scrollback mode must not allocate fixed transcript slots")
	}
}
