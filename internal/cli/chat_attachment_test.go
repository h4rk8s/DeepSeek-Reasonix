package cli

import (
	"encoding/base64"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestRecoverOrphanedPasteLabelFromHistory(t *testing.T) {
	block := pastedBlock{
		label:   "[Pasted text #4 · 2 lines]",
		payload: "old\nbody",
	}
	history := []provider.Message{{
		Role:    provider.RoleUser,
		Content: renderFoldedPasteBlock(block),
	}}

	got := recoverOrphanedPasteLabelsFromHistory("repeat "+block.label, nil, history)
	want := "repeat " + renderFoldedPasteBlock(block)
	if got != want {
		t.Fatalf("recovered paste = %q, want %q", got, want)
	}
}

func TestRecoverOrphanedPasteLabelPreservesOriginalWhitespace(t *testing.T) {
	block := pastedBlock{
		label:   "[Pasted text #5 · 5 lines]",
		payload: "\n  first line\nsecond line  \n\n",
	}
	history := []provider.Message{{
		Role:    provider.RoleUser,
		Content: renderFoldedPasteBlock(block),
	}}

	got := recoverOrphanedPasteLabelsFromHistory(block.label, nil, history)
	want := renderFoldedPasteBlock(block)
	if got != want {
		t.Fatalf("recovered paste = %q, want exact original %q", got, want)
	}
}

func TestRecoverOrphanedPasteLabelPreservesEmbeddedEndMarker(t *testing.T) {
	label := "[Pasted text #4 · 3 lines]"
	block := pastedBlock{
		label:   label,
		payload: "first\n--- End " + label + " ---\nlast",
	}
	history := []provider.Message{{
		Role:    provider.RoleUser,
		Content: renderFoldedPasteBlock(block),
	}}

	got := recoverOrphanedPasteLabelsFromHistory(label, nil, history)
	want := renderFoldedPasteBlock(block)
	if got != want {
		t.Fatalf("recovered paste = %q, want exact original %q", got, want)
	}
}

func TestRecoverOrphanedPasteLabelLeavesConflictingExpansionsUnchanged(t *testing.T) {
	label := "[Pasted text #4 · 2 lines]"
	history := []provider.Message{
		{Role: provider.RoleUser, Content: renderFoldedPasteBlock(pastedBlock{label: label, payload: "old\nbody"})},
		{Role: provider.RoleAssistant, Content: renderFoldedPasteBlock(pastedBlock{label: label, payload: "untrusted\nbody"})},
		{Role: provider.RoleUser, Content: renderFoldedPasteBlock(pastedBlock{label: label, payload: "new\nbody"})},
	}

	if got := recoverOrphanedPasteLabelsFromHistory(label, nil, history); got != label {
		t.Fatalf("ambiguous paste = %q, want unchanged label %q", got, label)
	}
}

func TestRecoverOrphanedPasteLabelAcceptsRepeatedIdenticalExpansion(t *testing.T) {
	label := "[Pasted text #4 · 2 lines]"
	block := pastedBlock{label: label, payload: "same\nbody"}
	history := []provider.Message{
		{Role: provider.RoleUser, Content: renderFoldedPasteBlock(block)},
		{Role: provider.RoleAssistant, Content: renderFoldedPasteBlock(pastedBlock{label: label, payload: "untrusted\nbody"})},
		{Role: provider.RoleUser, Content: renderFoldedPasteBlock(block)},
	}

	got := recoverOrphanedPasteLabelsFromHistory(label, nil, history)
	want := renderFoldedPasteBlock(block)
	if got != want {
		t.Fatalf("recovered paste = %q, want identical user expansion %q", got, want)
	}
}

func TestRecoverOrphanedPasteLabelLeavesUnverifiedTextUnchanged(t *testing.T) {
	sent := "explain [Pasted text #9 · 10 lines] syntax"
	history := []provider.Message{{
		Role:    provider.RoleAssistant,
		Content: renderFoldedPasteBlock(pastedBlock{label: "[Pasted text #9 · 10 lines]", payload: "assistant\ncontent"}),
	}}

	if got := recoverOrphanedPasteLabelsFromHistory(sent, nil, history); got != sent {
		t.Fatalf("unverified label = %q, want unchanged %q", got, sent)
	}
}

func TestRecoverOrphanedPasteLabelIgnoresPinnedRevision(t *testing.T) {
	block := pastedBlock{label: "[Pasted text #9 · 2 lines]", payload: "private\nbody"}
	history := []provider.Message{{
		Role: provider.RoleUser, Origin: provider.MessageOriginHost,
		Content: "<pinned_context_revision>" + renderFoldedPasteBlock(block) + "</pinned_context_revision>",
	}}
	if got := recoverOrphanedPasteLabelsFromHistory(block.label, nil, history); got != block.label {
		t.Fatalf("pinned revision recovered paste body: %q", got)
	}
}

func TestRecoverOrphanedPasteLabelDoesNotReexpandRenderedBlock(t *testing.T) {
	block := pastedBlock{label: "[Pasted text #4 · 2 lines]", payload: "old\nbody"}
	rendered := renderFoldedPasteBlock(block)
	history := []provider.Message{{Role: provider.RoleUser, Content: rendered}}

	if got := recoverOrphanedPasteLabelsFromHistory(rendered, nil, history); got != rendered {
		t.Fatalf("rendered block = %q, want unchanged %q", got, rendered)
	}
}

func TestNextPasteIDForHistoryContinuesAcrossReload(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Content: "[Pasted text #2 · 4 lines]"},
		{Role: provider.RoleAssistant, Content: "--- Begin [Pasted text #7 · 3 lines] ---"},
	}
	if got := nextPasteIDForHistory(history); got != 8 {
		t.Fatalf("nextPasteIDForHistory = %d, want 8", got)
	}
	if got := nextPasteIDForHistory(nil); got != 1 {
		t.Fatalf("nextPasteIDForHistory(nil) = %d, want 1", got)
	}
}

func TestNextPasteIDForHistoryDoesNotOverflow(t *testing.T) {
	history := []provider.Message{{
		Role:    provider.RoleAssistant,
		Content: foldedPasteLabel(math.MaxInt, 1),
	}}
	if got := nextPasteIDForHistory(history); got != 1 {
		t.Fatalf("nextPasteIDForHistory = %d, want 1", got)
	}
}

func TestTakeNextPasteIDSkipsUsedIDsAcrossWrap(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Content: foldedPasteLabel(1, 1)},
		{Role: provider.RoleAssistant, Content: foldedPasteLabel(math.MaxInt-1, 1)},
		{Role: provider.RoleAssistant, Content: foldedPasteLabel(math.MaxInt, 1)},
	}
	next, used := pasteIDStateForHistory(history)
	m := &chatTUI{composerModel: composerModel{nextPasteID: next, usedPasteIDs: used}}

	if got := m.takeNextPasteID(); got != 2 {
		t.Fatalf("takeNextPasteID = %d, want first unused ID 2", got)
	}
	if m.nextPasteID != 3 {
		t.Fatalf("nextPasteID = %d, want 3", m.nextPasteID)
	}
}

func TestTakeNextPasteIDWrapsAfterMaxInt(t *testing.T) {
	m := &chatTUI{composerModel: composerModel{nextPasteID: math.MaxInt}}
	if got := m.takeNextPasteID(); got != math.MaxInt {
		t.Fatalf("first takeNextPasteID = %d, want %d", got, math.MaxInt)
	}
	if got := m.takeNextPasteID(); got != 1 {
		t.Fatalf("wrapped takeNextPasteID = %d, want 1", got)
	}
}

func TestTakeNextPasteIDSynchronizesAdoptedControllerHistory(t *testing.T) {
	first := pastedBlock{label: "[Pasted text #1 · 1 lines]", payload: "first"}
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: renderFoldedPasteBlock(first)})
	executor := agent.New(nil, nil, session, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: executor, Label: "review"})
	t.Cleanup(ctrl.Close)

	m := newChatTUI(ctrl, "", make(chan event.Event), 80)

	second := pastedBlock{label: "[Pasted text #2 · 1 lines]", payload: "second"}
	adopted := agent.NewSession("system")
	adopted.Add(provider.Message{Role: provider.RoleUser, Content: renderFoldedPasteBlock(first)})
	adopted.Add(provider.Message{Role: provider.RoleUser, Content: renderFoldedPasteBlock(second)})
	executor.SetSession(adopted)

	if got := m.takeNextPasteID(); got != 3 {
		t.Fatalf("takeNextPasteID after adopted history = %d, want 3", got)
	}
}

func TestDisplayLineForImageRefs(t *testing.T) {
	got := displayLineForImageRefs("describe @.reasonix/attachments/clipboard-20260601-010203.000001.png @.reasonix/attachments/clipboard-20260601-010204.000002-000002.jpg")
	want := "describe [image1] [image2]"
	if got != want {
		t.Fatalf("displayLineForImageRefs = %q, want %q", got, want)
	}
}

func TestDisplayLineForImageRefsAfterChinesePunctuation(t *testing.T) {
	got := displayLineForImageRefs("我从 Magisk 过来后，@.reasonix/attachments/clipboard-20260710-133417.967634-000004.png 下面只有两部文件")
	want := "我从 Magisk 过来后，[image1] 下面只有两部文件"
	if got != want {
		t.Fatalf("displayLineForImageRefs = %q, want %q", got, want)
	}
}

func TestPastedFileRef(t *testing.T) {
	dir := t.TempDir()
	pdf := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, ok := pastedFileRef(pdf); !ok || got != "@"+filepath.Clean(pdf) {
		t.Fatalf("pastedFileRef(existing pdf) = %q, %v", got, ok)
	}
	if got, ok := pastedFileRef(`"` + pdf + `"`); !ok || got != "@"+filepath.Clean(pdf) {
		t.Fatalf("pastedFileRef(quoted pdf) = %q, %v", got, ok)
	}
	if _, ok := pastedFileRef("just-a-word"); ok {
		t.Fatal("a bare word with no separator must not be a file ref")
	}
	if _, ok := pastedFileRef(filepath.Join(dir, "missing.pdf")); ok {
		t.Fatal("a non-existent path must not be a file ref")
	}
	if _, ok := pastedFileRef(dir); ok {
		t.Fatal("a directory must not be a file ref")
	}
}

func TestPastedFileRefShellEscapedSpaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell-escaped paths are not decoded on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Application Support", "report 2026.pdf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	escaped := strings.ReplaceAll(path, " ", `\ `)

	want := "@" + control.EscapeRefPath(filepath.Clean(path))
	if got, ok := pastedFileRef(escaped); !ok || got != want {
		t.Fatalf("pastedFileRef(shell escaped pdf) = %q, %v; want %s", got, ok, want)
	}
}

func TestPastedImageSources(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		want      []string
		ok        bool
		posixOnly bool
	}{
		{
			name: "data URL",
			text: "data:image/png;base64,aaa",
			want: []string{"data:image/png;base64,aaa"},
			ok:   true,
		},
		{
			name: "markdown images",
			text: "![a](/tmp/a.png)\n![b](file:///tmp/b.jpg)",
			want: []string{"/tmp/a.png", "file:///tmp/b.jpg"},
			ok:   true,
		},
		{
			name: "markdown link image",
			text: "[Image #1](file:///tmp/CleanShot%202026.png)",
			want: []string{"file:///tmp/CleanShot%202026.png"},
			ok:   true,
		},
		{
			name: "html image source",
			text: `<img alt="CleanShot" src="file:///tmp/CleanShot%202026.png">`,
			want: []string{"file:///tmp/CleanShot%202026.png"},
			ok:   true,
		},
		{
			name: "angle wrapped file URL",
			text: "<file:///tmp/CleanShot%202026.png>",
			want: []string{"file:///tmp/CleanShot%202026.png"},
			ok:   true,
		},
		{
			name:      "shell escaped path with spaces",
			text:      `/Users/jawa/Library/Application\ Support/CleanShot/media/CleanShot\ 2026-07-06\ at\ 11.33.14@2x.png`,
			want:      []string{`/Users/jawa/Library/Application\ Support/CleanShot/media/CleanShot\ 2026-07-06\ at\ 11.33.14@2x.png`},
			ok:        true,
			posixOnly: true,
		},
		{
			name: "shell escaped path without whitespace",
			text: `/tmp/capture\(1\).png`,
			want: []string{`/tmp/capture\(1\).png`},
			ok:   true,
		},
		{
			name:      "multiple shell escaped paths on one line",
			text:      `/tmp/first\ image.png /tmp/second\ image.jpg`,
			want:      []string{`/tmp/first\ image.png`, `/tmp/second\ image.jpg`},
			ok:        true,
			posixOnly: true,
		},
		{
			name: "multiple quoted paths on one line",
			text: `'/tmp/first image.png' "/tmp/second image.jpg"`,
			want: []string{`'/tmp/first image.png'`, `"/tmp/second image.jpg"`},
			ok:   true,
		},
		{
			name:      "at-prefixed shell escaped path with spaces",
			text:      `@/Users/jawa/Library/Application\ Support/CleanShot/media/media_UtsBKjOyVs/CleanShot\ 2026-07-06\ at\ 17.29.25@2x.png`,
			want:      []string{`@/Users/jawa/Library/Application\ Support/CleanShot/media/media_UtsBKjOyVs/CleanShot\ 2026-07-06\ at\ 17.29.25@2x.png`},
			ok:        true,
			posixOnly: true,
		},
		{
			name: "sentence with image path remains text",
			text: `see /tmp/CleanShot\ 2026.png`,
			ok:   false,
		},
		{
			name: "plain text",
			text: "hello /tmp/a.png",
			ok:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.posixOnly && runtime.GOOS == "windows" {
				t.Skip("POSIX shell-escaped paths are not decoded on Windows")
			}
			sources, ok := pastedImageSources(c.text)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			var got []string
			if sources != nil {
				got = make([]string, 0, len(sources))
				for _, source := range sources {
					got = append(got, source.value)
				}
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("sources = %v, want %v", got, c.want)
			}
		})
	}
}

func writeTinyPNG(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertSingleImageToken(t *testing.T, updated chatTUI) {
	t.Helper()
	if got := updated.input.Value(); got != "[Image #1] " {
		t.Fatalf("input after paste = %q, want image token", got)
	}
	if len(updated.pastedBlocks) != 1 || updated.pastedBlocks[0].kind != composerPartImage {
		t.Fatalf("pastedBlocks = %+v, want one image block", updated.pastedBlocks)
	}
	if text := updated.pastedBlocks[0].payload; !strings.HasPrefix(text, "@.reasonix/attachments/clipboard-") || !strings.HasSuffix(text, ".png") {
		t.Fatalf("image block text = %q, want saved attachment ref", text)
	}
}

func TestPasteShellEscapedImagePathInsertsImageToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell-escaped paths are not decoded on Windows")
	}
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "Library", "Application Support", "CleanShot", "CleanShot 2026-07-06 at 11.33.14@2x.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: strings.ReplaceAll(path, " ", `\ `)})
	updated := next.(chatTUI)

	if got := updated.input.Value(); got != "[Image #1] " {
		t.Fatalf("input after paste = %q, want image token", got)
	}
	if len(updated.pastedBlocks) != 1 || updated.pastedBlocks[0].kind != composerPartImage {
		t.Fatalf("pastedBlocks = %+v, want one image block", updated.pastedBlocks)
	}
	if text := updated.pastedBlocks[0].payload; !strings.HasPrefix(text, "@.reasonix/attachments/clipboard-") || !strings.HasSuffix(text, ".png") {
		t.Fatalf("image block text = %q, want saved attachment ref", text)
	}
}

func TestPasteShellEscapedImagePathWithoutWhitespaceInsertsImageToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell-escaped paths are not decoded on Windows")
	}
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "capture^(1),x.png")
	raw, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	escaped := strings.NewReplacer("(", `\(`, ")", `\)`, "^", `\^`, ",", `\,`).Replace(path)

	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: escaped})
	updated := next.(chatTUI)

	if got := updated.input.Value(); got != "[Image #1] " {
		t.Fatalf("input after paste = %q, want image token", got)
	}
	if len(updated.pastedBlocks) != 1 || updated.pastedBlocks[0].kind != composerPartImage {
		t.Fatalf("pastedBlocks = %+v, want one image block", updated.pastedBlocks)
	}
	if text := updated.pastedBlocks[0].payload; !strings.HasPrefix(text, "@.reasonix/attachments/clipboard-") || !strings.HasSuffix(text, ".png") {
		t.Fatalf("image block text = %q, want saved attachment ref", text)
	}
}

func TestPasteUnescapedAtImagePathWithSpacesInsertsImageToken(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "Library", "Application Support", "CleanShot", "media", "media_uqlxszAEbJ", "CleanShot 2026-07-09 at 17.13.43@2x.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: "@" + path})
	updated := next.(chatTUI)

	if got := updated.input.Value(); got != "[Image #1] " {
		t.Fatalf("input after paste = %q, want image token", got)
	}
	if len(updated.pastedBlocks) != 1 || updated.pastedBlocks[0].kind != composerPartImage {
		t.Fatalf("pastedBlocks = %+v, want one image block", updated.pastedBlocks)
	}
	if text := updated.pastedBlocks[0].payload; !strings.HasPrefix(text, "@.reasonix/attachments/clipboard-") || !strings.HasSuffix(text, ".png") {
		t.Fatalf("image block text = %q, want saved attachment ref", text)
	}
}

func TestPasteImagePathWhileRunningInsertsImageToken(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "Library", "Application Support", "CleanShot", "media", "media_running", "CleanShot 2026-07-09 at 20.11.45@2x.png")
	writeTinyPNG(t, path)

	m := newTestChatTUI()
	m.state = tuiRunning
	next, _ := m.Update(tea.PasteMsg{Content: "@" + path})
	updated := next.(chatTUI)

	assertSingleImageToken(t, updated)
}

func TestPasteWrappedUnescapedAtImagePathWithSpacesInsertsImageToken(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "Library", "Application Support", "CleanShot", "media", "media_KPL5MdRm81", "CleanShot 2026-07-09 at 20.11.45@2x.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	pasted := "@" + strings.Replace(path, " at 20.", " at\n20.", 1)
	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: pasted})
	updated := next.(chatTUI)

	if got := updated.input.Value(); got != "[Image #1] " {
		t.Fatalf("input after paste = %q, want image token", got)
	}
	if len(updated.pastedBlocks) != 1 || updated.pastedBlocks[0].kind != composerPartImage {
		t.Fatalf("pastedBlocks = %+v, want one image block", updated.pastedBlocks)
	}
	if text := updated.pastedBlocks[0].payload; !strings.HasPrefix(text, "@.reasonix/attachments/clipboard-") || !strings.HasSuffix(text, ".png") {
		t.Fatalf("image block text = %q, want saved attachment ref", text)
	}
}

func TestPastePromptPrefixedWrappedImagePathInsertsImageToken(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "Library", "Application Support", "CleanShot", "media", "media_prompt", "CleanShot 2026-07-09 at 20.11.45@2x.png")
	writeTinyPNG(t, path)

	pasted := "› @" + strings.Replace(path, " at 20.", " at\n› 20.", 1)
	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: pasted})
	updated := next.(chatTUI)

	assertSingleImageToken(t, updated)
}

func TestPasteAsciiPromptPrefixedWrappedImagePathInsertsImageToken(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "Library", "Application Support", "CleanShot", "media", "media_ascii_prompt", "CleanShot 2026-07-09 at 20.11.45@2x.png")
	writeTinyPNG(t, path)

	pasted := "> @" + strings.Replace(path, "CleanShot", "Clean\n> Shot", 1)
	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: pasted})
	updated := next.(chatTUI)

	assertSingleImageToken(t, updated)
}

func TestPasteHardWrappedImagePathWithoutSpaceInsertsImageToken(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "Library", "Application Support", "CleanShot", "media", "media_split", "CleanShot 2026-07-09 at 20.11.45@2x.png")
	writeTinyPNG(t, path)

	pasted := "@" + strings.Replace(path, "CleanShot", "Clean\nShot", 1)
	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: pasted})
	updated := next.(chatTUI)

	assertSingleImageToken(t, updated)
}

func TestPasteFileURLImagePathInsertsImageToken(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "Library", "Application Support", "CleanShot", "media", "media_file_url", "CleanShot 2026-07-09 at 20.11.45@2x.png")
	writeTinyPNG(t, path)
	fileURL := (&url.URL{Scheme: "file", Path: path}).String()

	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: fileURL})
	updated := next.(chatTUI)

	assertSingleImageToken(t, updated)
}

func TestPasteHTMLImagePathInsertsImageToken(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "Library", "Application Support", "CleanShot", "media", "media_html", "CleanShot 2026-07-09 at 20.11.45@2x.png")
	writeTinyPNG(t, path)
	fileURL := (&url.URL{Scheme: "file", Path: path}).String()

	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: `<img alt="CleanShot" src="` + fileURL + `">`})
	updated := next.(chatTUI)

	assertSingleImageToken(t, updated)
}

func TestPasteMarkdownLinkImagePathInsertsImageToken(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "Library", "Application Support", "CleanShot", "media", "media_markdown", "CleanShot 2026-07-09 at 20.11.45@2x.png")
	writeTinyPNG(t, path)
	fileURL := (&url.URL{Scheme: "file", Path: path}).String()

	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: `[Image #1](` + fileURL + `)`})
	updated := next.(chatTUI)

	assertSingleImageToken(t, updated)
}

func TestPasteMultipleFileURLImagePathsInsertsMultipleImageTokens(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	path1 := filepath.Join(root, "Library", "Application Support", "CleanShot", "media", "media_multi", "CleanShot 2026-07-09 at 20.11.45@2x.png")
	path2 := filepath.Join(root, "Library", "Application Support", "CleanShot", "media", "media_multi", "CleanShot 2026-07-09 at 20.11.46@2x.png")
	writeTinyPNG(t, path1)
	writeTinyPNG(t, path2)
	fileURL1 := (&url.URL{Scheme: "file", Path: path1}).String()
	fileURL2 := (&url.URL{Scheme: "file", Path: path2}).String()

	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: fileURL1 + "\n" + fileURL2})
	updated := next.(chatTUI)

	if got := updated.input.Value(); got != "[Image #1] [Image #2] " {
		t.Fatalf("input after paste = %q, want two image tokens", got)
	}
	if len(updated.pastedBlocks) != 2 || updated.pastedBlocks[0].kind != composerPartImage || updated.pastedBlocks[1].kind != composerPartImage {
		t.Fatalf("pastedBlocks = %+v, want two image blocks", updated.pastedBlocks)
	}
}

func TestTypedAtShellEscapedImagePathInsertsImageToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell-escaped paths are not decoded on Windows")
	}
	root := t.TempDir()
	t.Chdir(root)
	path := filepath.Join(root, "Library", "Application Support", "CleanShot", "media", "media_UtsBKjOyVs", "CleanShot 2026-07-06 at 17.29.25@2x.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestChatTUI()
	typed := "@" + strings.ReplaceAll(path, " ", `\ `)
	var model tea.Model = m
	for _, r := range typed {
		next, _ := model.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		model = next
	}
	updated := model.(chatTUI)

	if got := updated.input.Value(); got != "[Image #1] " {
		t.Fatalf("input after typed image path = %q, want image token", got)
	}
	if len(updated.pastedBlocks) != 1 || updated.pastedBlocks[0].kind != composerPartImage {
		t.Fatalf("pastedBlocks = %+v, want one image block", updated.pastedBlocks)
	}
	if text := updated.pastedBlocks[0].payload; !strings.HasPrefix(text, "@.reasonix/attachments/clipboard-") || !strings.HasSuffix(text, ".png") {
		t.Fatalf("image block text = %q, want saved attachment ref", text)
	}
}

func TestPasteMultipleShellEscapedImagePathsInsertsImageTokens(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell-escaped paths are not decoded on Windows")
	}
	root := t.TempDir()
	t.Chdir(root)
	raw, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "first image.png")
	second := filepath.Join(root, "second image.png")
	for _, p := range []string{first, second} {
		if err := os.WriteFile(p, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	content := strings.ReplaceAll(first, " ", `\ `) + " " + strings.ReplaceAll(second, " ", `\ `)

	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: content})
	updated := next.(chatTUI)

	if got := updated.input.Value(); got != "[Image #1] [Image #2] " {
		t.Fatalf("input after paste = %q, want two image tokens", got)
	}
	if len(updated.pastedBlocks) != 2 || updated.pastedBlocks[0].kind != composerPartImage || updated.pastedBlocks[1].kind != composerPartImage {
		t.Fatalf("pastedBlocks = %+v, want two image blocks", updated.pastedBlocks)
	}
}

func TestMissingPastedImagePathRemainsText(t *testing.T) {
	content := `/definitely-missing/reasonix-image.png`
	if runtime.GOOS == "windows" {
		content = `C:/definitely-missing/reasonix-image.png`
	}

	m := newTestChatTUI()
	next, _ := m.Update(tea.PasteMsg{Content: content})
	updated := next.(chatTUI)
	if got := updated.input.Value(); got != content {
		t.Fatalf("input after missing image paste = %q, want original %q", got, content)
	}
	if len(updated.pastedBlocks) != 0 {
		t.Fatalf("pastedBlocks = %+v, want no image attachment", updated.pastedBlocks)
	}
}

func TestPastedImagePathShellUnescape(t *testing.T) {
	cases := []struct {
		name string
		src  string
		goos string
		want string
		ok   bool
	}{
		{
			name: "posix escaped parens without whitespace",
			src:  `/tmp/capture\(1\).png`,
			goos: "linux",
			want: "/tmp/capture(1).png",
			ok:   true,
		},
		{
			name: "posix escaped spaces",
			src:  `/tmp/first\ image.png`,
			goos: "linux",
			want: "/tmp/first image.png",
			ok:   true,
		},
		{
			name: "posix escaped caret and comma",
			src:  `/tmp/capture\^1\,a.png`,
			goos: "linux",
			want: "/tmp/capture^1,a.png",
			ok:   true,
		},
		{
			name: "posix escaped literal backslash",
			src:  `/tmp/a\\b.png`,
			goos: "linux",
			want: `/tmp/a\b.png`,
			ok:   true,
		},
		{
			name: "posix unescaped space rejected",
			src:  "/tmp/first image.png",
			goos: "linux",
			ok:   false,
		},
		{
			name: "windows backslash separators preserved",
			src:  `C:\Users\me\shot(1).png`,
			goos: "windows",
			want: `C:\Users\me\shot(1).png`,
			ok:   true,
		},
		{
			name: "windows dollar directory preserved",
			src:  `C:\$Recycle.Bin\shot.png`,
			goos: "windows",
			want: `C:\$Recycle.Bin\shot.png`,
			ok:   true,
		},
		{
			name: "windows unquoted space rejected",
			src:  `C:\Program Files\shot.png`,
			goos: "windows",
			ok:   false,
		},
		{
			name: "windows quoted path with space preserved",
			src:  `"C:\my dir\shot.png"`,
			goos: "windows",
			want: `C:\my dir\shot.png`,
			ok:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := pastedImagePathForOS(c.src, c.goos)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if c.ok && got != c.want {
				t.Fatalf("path = %q, want %q", got, c.want)
			}
		})
	}
}
