package cli

import (
	"strings"
	"testing"
)

func TestLazyReasoningCollapsedHoverOnlyHitsSummaryText(t *testing.T) {
	m := newTestChatTUI()
	m.lazyReasoning = true
	summary := formatReasoningSummary(0)
	m.transcript = fixedTranscriptBlocks(
		renderReasoningSummary(summary, m.width, false),
		"answer below",
	)
	m.entries = map[int]*disclosureEntry{
		0: {
			raw:        "line one\nline two",
			summary:    summary,
			summaryIdx: 0,
		},
	}
	m.nextID = 1
	m.rebuildDisclosureIndex()
	wrapped, lineMap := wrapTranscriptEntries(m.transcript, 120)
	m.wrappedLines = strings.Split(wrapped, "\n")
	m.wrappedLineTranscriptIdx = lineMap

	width := m.collapsedDisclosureWidth(0)
	if width <= 0 {
		t.Fatalf("collapsed reasoning summary should have a positive hit width")
	}
	idx, kind, ok := m.clickableAtPosition(0, width-1)
	if !ok || idx != 0 || kind != transcriptHoverDisclosure {
		t.Fatalf("summary text should be clickable, got idx=%d kind=%v ok=%v", idx, kind, ok)
	}
	if idx, kind, ok := m.clickableAtPosition(0, width); ok {
		t.Fatalf("first cell after summary text should not be clickable, got idx=%d kind=%v", idx, kind)
	}
	if idx, kind, ok := m.clickableAtPosition(0, width+20); ok {
		t.Fatalf("empty tail of the wrapped row should not be clickable, got idx=%d kind=%v", idx, kind)
	}
}
