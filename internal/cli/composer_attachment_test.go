package cli

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

func installTestComposerParts(t *testing.T, m *chatTUI, value string, parts ...pastedBlock) {
	t.Helper()
	searchFrom := 0
	for i := range parts {
		if parts[i].label == "" {
			t.Fatalf("test composer part %d has no label", i)
		}
		rel := strings.Index(value[searchFrom:], parts[i].label)
		if rel < 0 {
			t.Fatalf("test composer part %q is absent from %q", parts[i].label, value)
		}
		byteStart := searchFrom + rel
		start := utf8.RuneCountInString(value[:byteStart])
		parts[i].id = composerPartID(i + 1)
		parts[i].state = composerPartActive
		parts[i].span = composerAttachmentRange{
			partID: parts[i].id,
			start:  start,
			end:    start + utf8.RuneCountInString(parts[i].label),
		}
		searchFrom = byteStart + len(parts[i].label)
	}
	m.input.SetValue(value)
	m.value = value
	m.pastedBlocks = cloneComposerParts(parts)
	m.nextPartID = composerPartID(len(parts))
}

func composerWithImageToken(t *testing.T, value string) chatTUI {
	t.Helper()
	m := newComposerMouseTestTUI(t, 64, 14)
	installTestComposerParts(t, &m, value, pastedBlock{
		label: "[Image #1]", payload: "@.reasonix/attachments/test.png", kind: composerPartImage,
	})
	m.growInputToFit()
	return m
}

func TestComposerImageTokenClickSelectsWholeAttachment(t *testing.T) {
	m := composerWithImageToken(t, "prefix [Image #1] suffix")
	x, y, ok := m.composerOrigin()
	if !ok {
		t.Fatal("composer should expose a mouse origin")
	}
	clickX := x + len("prefix ") + 4
	m = updateComposerMouseTestTUI(t, m, tea.MouseClickMsg{X: clickX, Y: y, Button: tea.MouseLeft})
	m = updateComposerMouseTestTUI(t, m, tea.MouseReleaseMsg{X: clickX, Y: y, Button: tea.MouseLeft})

	if !m.validComposerSelection() || !m.composerSel.atomic {
		t.Fatalf("image click did not leave an atomic selection: %+v", m.composerSel)
	}
	if got := m.selectedComposerText(); got != "[Image #1]" {
		t.Fatalf("selected image token = %q, want whole token", got)
	}
}

func TestComposerImageTokenDragFallsBackToTextSelection(t *testing.T) {
	m := composerWithImageToken(t, "prefix [Image #1] suffix")
	x, y, _ := m.composerOrigin()
	startX := x + len("prefix ") + 3
	endX := x + len("prefix [Image #1] su")
	m = updateComposerMouseTestTUI(t, m, tea.MouseClickMsg{X: startX, Y: y, Button: tea.MouseLeft})
	m = updateComposerMouseTestTUI(t, m, tea.MouseMotionMsg{X: endX, Y: y, Button: tea.MouseLeft})
	m = updateComposerMouseTestTUI(t, m, tea.MouseReleaseMsg{X: endX, Y: y, Button: tea.MouseLeft})

	if !m.validComposerSelection() || m.composerSel.atomic {
		t.Fatalf("drag from image token should become an ordinary text selection: %+v", m.composerSel)
	}
	if got := m.selectedComposerText(); got == "" || got == "[Image #1]" {
		t.Fatalf("drag selection = %q, want an extended text range", got)
	}
}

func TestComposerImageTokenBackspaceIsAtomicAndUndoable(t *testing.T) {
	m := composerWithImageToken(t, "[Image #1] tail")
	m.setComposerCursor(len([]rune("[Image #1] ")))
	m = updateComposerMouseTestTUI(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.input.Value(); got != "tail" {
		t.Fatalf("atomic backspace = %q, want %q", got, "tail")
	}
	if len(m.pastedBlocks) != 0 {
		t.Fatalf("deleted attachment retained stale mappings: %+v", m.pastedBlocks)
	}

	// The user's Ghostty config maps Cmd+Z to ASCII US (0x1f), decoded as Ctrl+_.
	m = updateComposerMouseTestTUI(t, m, tea.KeyPressMsg{Code: '_', Mod: tea.ModCtrl})
	if got := m.input.Value(); got != "[Image #1] tail" {
		t.Fatalf("Cmd+Z restored %q, want original attachment draft", got)
	}
	if len(m.pastedBlocks) != 1 || m.pastedBlocks[0].kind != composerPartImage {
		t.Fatalf("Cmd+Z did not restore the attachment mapping: %+v", m.pastedBlocks)
	}
}

func TestComposerSelectedImageTokenDeleteIsAtomic(t *testing.T) {
	m := composerWithImageToken(t, "prefix [Image #1] suffix")
	token, ok := m.composerAttachmentAt(len([]rune("prefix [ima")))
	if !ok {
		t.Fatal("image token range was not found")
	}
	m.selectComposerAttachment(token, token.start)
	m = updateComposerMouseTestTUI(t, m, tea.KeyPressMsg{Code: tea.KeyDelete})
	if got := m.input.Value(); got != "prefix suffix" {
		t.Fatalf("selected attachment delete = %q, want token and one separator removed", got)
	}
}

func TestComposerImageTokenInsertionIsUndoable(t *testing.T) {
	m := newComposerMouseTestTUI(t, 64, 14)
	m.input.SetValue("describe ")
	m.input.MoveToEnd()
	m.insertImageRef(".reasonix/attachments/test.png")
	if got := m.input.Value(); got != "describe [Image #1] " {
		t.Fatalf("image insertion = %q", got)
	}
	m = updateComposerMouseTestTUI(t, m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModSuper})
	if got := m.input.Value(); got != "describe " {
		t.Fatalf("Cmd+Z after image insertion = %q, want original draft", got)
	}
}

func TestComposerImageTokenReplacementIsUndoable(t *testing.T) {
	m := composerWithImageToken(t, "prefix [Image #1] suffix")
	token, ok := m.composerAttachmentAt(len([]rune("prefix [Ima")))
	if !ok {
		t.Fatal("image token range was not found")
	}
	m.selectComposerAttachment(token, token.start)
	m = updateComposerMouseTestTUI(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := m.input.Value(); got != "prefix x suffix" {
		t.Fatalf("attachment replacement = %q", got)
	}
	m = updateComposerMouseTestTUI(t, m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModSuper})
	if got := m.input.Value(); got != "prefix [Image #1] suffix" {
		t.Fatalf("undo attachment replacement = %q", got)
	}
}

func TestComposerImageTokenHasIndependentVisualStyle(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256
	configureCLITheme("dark")
	m := composerWithImageToken(t, "[Image #1] tail")
	m.setComposerCursor(len([]rune(m.input.Value())))

	rendered := m.renderComposerInput()
	if !strings.Contains(rendered, themeStyle(activeCLITheme.info).Render("[Image #1]")) {
		t.Fatalf("image token did not receive its semantic style: %q", rendered)
	}
}

func TestComposerLiteralImageLabelNeverBecomesAttachment(t *testing.T) {
	m := newComposerMouseTestTUI(t, 64, 14)
	m.input.SetValue("typed [Image #1] literally")
	m.value = m.input.Value()

	if got := m.composerAttachmentRanges(); len(got) != 0 {
		t.Fatalf("literal label became an attachment range: %+v", got)
	}
	if got := m.expandPastedBlocks(m.input.Value()); got != m.input.Value() {
		t.Fatalf("literal label expanded to hidden payload: %q", got)
	}
}

func TestComposerDuplicateVisibleLabelExpandsOnlyTypedPart(t *testing.T) {
	value := "[Image #1] literal [Image #1]"
	m := newComposerMouseTestTUI(t, 64, 14)
	installTestComposerParts(t, &m, value, pastedBlock{
		label: "[Image #1]", payload: "@attachment.png", kind: composerPartImage,
	})

	if got, want := m.expandPastedBlocks(value), "@attachment.png literal [Image #1]"; got != want {
		t.Fatalf("duplicate label expansion = %q, want %q", got, want)
	}
	if ranges := m.composerAttachmentRanges(); len(ranges) != 1 || ranges[0].partID == 0 {
		t.Fatalf("typed identity was not unique: %+v", ranges)
	}
}

func TestComposerPartSpanMovesWithUnicodeEditBeforeIt(t *testing.T) {
	before := "你 [Image #1] tail"
	m := newComposerMouseTestTUI(t, 64, 14)
	installTestComposerParts(t, &m, before, pastedBlock{
		label: "[Image #1]", payload: "@attachment.png", kind: composerPartImage,
	})
	original := m.pastedBlocks[0].span
	after := "好" + before
	m.input.SetValue(after)
	m.reconcileEdit(before, after)

	got := m.pastedBlocks[0].span
	if got.start != original.start+1 || got.end != original.end+1 {
		t.Fatalf("unicode prefix edit moved span %+v, want %+v shifted by one rune", got, original)
	}
	if expanded := m.expandPastedBlocks(after); expanded != "好你 @attachment.png tail" {
		t.Fatalf("shifted attachment expansion = %q", expanded)
	}
}

func TestComposerEditInsidePartInvalidatesOnlyThatIdentity(t *testing.T) {
	before := "[Image #1] and [Image #2]"
	m := newComposerMouseTestTUI(t, 64, 14)
	installTestComposerParts(t, &m, before,
		pastedBlock{label: "[Image #1]", payload: "@one.png", kind: composerPartImage},
		pastedBlock{label: "[Image #2]", payload: "@two.png", kind: composerPartImage},
	)
	after := "[Image #] and [Image #2]"
	m.input.SetValue(after)
	m.reconcileEdit(before, after)

	if len(m.pastedBlocks) != 1 || m.pastedBlocks[0].payload != "@two.png" {
		t.Fatalf("editing one token invalidated the wrong identities: %+v", m.pastedBlocks)
	}
	if expanded := m.expandPastedBlocks(after); expanded != "[Image #] and @two.png" {
		t.Fatalf("remaining typed part expansion = %q", expanded)
	}
}

func TestComposerPendingSubmissionRestoresSamePartIdentity(t *testing.T) {
	value := "inspect [Image #1]"
	m := newComposerMouseTestTUI(t, 64, 14)
	installTestComposerParts(t, &m, value, pastedBlock{
		label: "[Image #1]", payload: "@attachment.png", kind: composerPartImage,
	})
	id := m.pastedBlocks[0].id

	m.stageComposerSubmission(value)
	m.resetComposerInput()
	if len(m.pendingPartIDs) != 1 || m.pendingPartIDs[0] != id || m.pastedBlocks[0].state != composerPartPending {
		t.Fatalf("submission did not retain stable pending identity: %+v ids=%v", m.pastedBlocks, m.pendingPartIDs)
	}
	m.restorePendingComposer(value)
	if len(m.pastedBlocks) != 1 || m.pastedBlocks[0].id != id || m.pastedBlocks[0].state != composerPartActive {
		t.Fatalf("restore changed attachment identity: %+v", m.pastedBlocks)
	}
	if expanded := m.expandPastedBlocks(value); expanded != "inspect @attachment.png" {
		t.Fatalf("restored attachment expansion = %q", expanded)
	}
}
