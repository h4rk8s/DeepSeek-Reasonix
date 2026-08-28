package cli

import "testing"

func TestExpandPastedBlocksImage(t *testing.T) {
	value := "look at [image #1] and [Pasted text #2 · 3 lines]"
	m := newTestChatTUI()
	installTestComposerParts(t, &m, value,
		pastedBlock{label: "[image #1]", payload: "@.reasonix/attachments/clipboard-20260601-010203.000001.png", kind: composerPartImage},
		pastedBlock{label: "[Pasted text #2 · 3 lines]", payload: "a\nb\nc", kind: composerPartFoldedText},
	)
	got := m.expandPastedBlocks(value)
	want := "look at @.reasonix/attachments/clipboard-20260601-010203.000001.png and " +
		renderFoldedPasteBlock(m.pastedBlocks[1])
	if got != want {
		t.Fatalf("expandPastedBlocks = %q, want %q", got, want)
	}
	if displayLineForImageRefs(got) != "look at [image1] and "+renderFoldedPasteBlock(m.pastedBlocks[1]) {
		t.Fatalf("image ref should collapse to a label in the bubble: %q", displayLineForImageRefs(got))
	}
}
