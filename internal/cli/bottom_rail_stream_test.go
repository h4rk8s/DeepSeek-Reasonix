package cli

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/event"
)

func TestStreamingTranscriptKeepsPinnedRailGeometry(t *testing.T) {
	for _, width := range []int{48, 72, 100} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			m := newInboxTestChatTUI(t)
			next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			m = next.(chatTUI)
			m.state = tuiRunning
			m.input.SetValue("draft remains")
			m.seedInbox("@aoright 的 PR #17 (09-24) 是什么？")

			for i := range 80 {
				m.ingestEvent(event.Event{Kind: event.Reasoning, Text: fmt.Sprintf("\n**分析 %d** | 用户 | 依据 |\n|---|---|---|\n", i)})
				next, _ = m.Update(struct{}{})
				m = next.(chatTUI)
				view := m.View().Content
				lines := strings.Split(view, "\n")
				if len(lines) != m.height {
					t.Fatalf("frame %d: height = %d, want %d", i, len(lines), m.height)
				}
				for row, line := range lines {
					if got := ansi.StringWidth(line); got > width {
						t.Fatalf("frame %d row %d: width = %d, want <= %d: %q", i, row, got, width, ansi.Strip(line))
					}
				}
				if got := strings.Count(view, "[1]"); got != 1 {
					t.Fatalf("frame %d: queue preview count = %d, want 1", i, got)
				}
				if !strings.Contains(view, "draft remains") {
					t.Fatalf("frame %d: composer draft disappeared", i)
				}
			}
		})
	}
}

func TestGhosttyScrollRedrawsPinnedRail(t *testing.T) {
	m := newInboxTestChatTUI(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 72, Height: 12})
	m = next.(chatTUI)
	m.state = tuiRunning
	m.legacyScrollClear = true
	m.seedInbox("queued guidance")
	for i := range 30 {
		m.ingestEvent(event.Event{Kind: event.Reasoning, Text: fmt.Sprintf("\nline %d", i)})
		next, _ = m.Update(struct{}{})
		m = next.(chatTUI)
	}
	if !m.viewport.AtBottom() {
		t.Fatal("setup did not follow the transcript tail")
	}
	beforeStream := m.viewport.YOffset()
	m.commitLine("new trailing line")
	next, cmd := m.Update(struct{}{})
	m = next.(chatTUI)
	if m.viewport.YOffset() <= beforeStream {
		t.Fatalf("transcript growth did not advance the viewport: %d -> %d", beforeStream, m.viewport.YOffset())
	}
	if cmd == nil || reflect.TypeOf(cmd()) != reflect.TypeOf(tea.ClearScreen()) {
		t.Fatal("transcript scroll did not request a full redraw")
	}
	_, cmd = m.Update(struct{}{})
	if cmd != nil {
		t.Fatal("a stable viewport must not request repeated full redraws")
	}
	before := m.viewport.YOffset()
	next, cmd = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	m = next.(chatTUI)
	if m.viewport.YOffset() >= before {
		t.Fatal("wheel did not move the viewport")
	}
	if cmd == nil || reflect.TypeOf(cmd()) != reflect.TypeOf(tea.ClearScreen()) {
		t.Fatal("scrolling an affected terminal did not request a full redraw")
	}
	if got := m.inboxQueuedCount(); got != 1 {
		t.Fatalf("queued instruction count = %d, want 1", got)
	}
}

func TestRunningCtrlLRedrawPreservesWork(t *testing.T) {
	m := newInboxTestChatTUI(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 72, Height: 16})
	m = next.(chatTUI)
	m.state = tuiRunning
	m.input.SetValue("unfinished draft")
	m.seedInbox("queued guidance")
	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "live thinking"})
	before := len(m.transcript)

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})
	m = next.(chatTUI)
	if cmd == nil || reflect.TypeOf(cmd()) != reflect.TypeOf(tea.ClearScreen()) {
		t.Fatal("Ctrl+L during a turn must request a full redraw")
	}
	if m.state != tuiRunning || m.input.Value() != "unfinished draft" || len(m.transcript) != before || m.inboxQueuedCount() != 1 {
		t.Fatal("redraw changed the active turn, draft, transcript, or inbox")
	}
}

func TestIdleCtrlLKeepsClearTranscriptBehavior(t *testing.T) {
	m := newInboxTestChatTUI(t)
	m.state = tuiIdle
	m.commitLine("old transcript row")
	if cmd := m.handleCtrlL(); cmd != nil {
		t.Fatal("idle Ctrl+L should not request a renderer-only redraw")
	}
	for _, block := range m.transcript {
		if strings.Contains(block.rendered, "old transcript row") {
			t.Fatal("idle Ctrl+L failed to clear the transcript")
		}
	}
	if !m.forceGotoBottom {
		t.Fatal("idle Ctrl+L should return to the bottom")
	}
}
