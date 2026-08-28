package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

type transcriptDisclosureKind int

const (
	transcriptDisclosureReasoning transcriptDisclosureKind = iota
	transcriptDisclosureImageUnderstanding
)

// disclosureEntry is the semantic source for one collapsible transcript row.
// The transcript keeps only its current projection; raw detail lives here.
type disclosureEntry struct {
	raw        string
	summary    string
	summaryIdx int
	expanded   bool
	kind       transcriptDisclosureKind
}

// disclosureModel is the sole owner of collapsible transcript state. Its
// stable IDs survive transcript index shifts while index is rebuilt as a
// derived lookup after each mutation.
type disclosureModel struct {
	entries map[int]*disclosureEntry
	index   map[int]int
	nextID  int
}

func newDisclosureModel() disclosureModel {
	return disclosureModel{
		entries: make(map[int]*disclosureEntry),
		index:   make(map[int]int),
	}
}

func (d *disclosureModel) reset() {
	*d = newDisclosureModel()
}

func (d *disclosureModel) remember(summaryIdx int, summary, raw string, expanded bool, kind transcriptDisclosureKind, transcriptLen int) {
	if d.entries == nil {
		d.entries = make(map[int]*disclosureEntry)
	}
	id := d.nextID
	d.nextID++
	d.entries[id] = &disclosureEntry{
		raw:        raw,
		summary:    summary,
		summaryIdx: summaryIdx,
		expanded:   expanded,
		kind:       kind,
	}
	d.rebuildIndex(transcriptLen)
}

func (d *disclosureModel) rebuildIndex(transcriptLen int) {
	if d.index == nil {
		d.index = make(map[int]int)
	} else {
		clear(d.index)
	}
	for id, entry := range d.entries {
		if entry.summaryIdx >= 0 && entry.summaryIdx < transcriptLen {
			d.index[entry.summaryIdx] = id
		}
	}
}

func (d *disclosureModel) shift(start, delta, transcriptLen int) {
	for _, entry := range d.entries {
		if entry.summaryIdx >= start {
			entry.summaryIdx += delta
		}
	}
	d.rebuildIndex(transcriptLen)
}

func (d *disclosureModel) truncate(transcriptLen int) {
	for id, entry := range d.entries {
		if entry.summaryIdx >= transcriptLen {
			delete(d.entries, id)
		}
	}
	d.rebuildIndex(transcriptLen)
}

func (d *disclosureModel) entryAt(transcriptIdx int) (*disclosureEntry, bool) {
	if transcriptIdx < 0 || d.index == nil {
		return nil, false
	}
	id, ok := d.index[transcriptIdx]
	if !ok {
		return nil, false
	}
	entry := d.entries[id]
	return entry, entry != nil
}

// reasoningBlock renders raw thinking text as dim, width-wrapped lines under a
// connector. A positive maxLines keeps only the live trailing visual lines.
func reasoningBlock(raw string, width, maxLines int) string {
	return reasoningBlockStyled(raw, width, maxLines, false, false)
}

func reasoningBlockStyled(raw string, width, maxLines int, background, hover bool) string {
	contentW := transcriptEntryWidth(width)
	gutter := connector
	if background {
		gutter = assistantContentGutter
	}
	w := max(contentW-len([]rune(gutter)), 8)
	var lines []string
	for ln := range strings.SplitSeq(strings.TrimRight(raw, "\n"), "\n") {
		for wl := range strings.SplitSeq(ansi.Wrap(expandTabs(ln), w, ""), "\n") {
			lines = append(lines, wl)
		}
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	if background {
		rows := make([]string, 0, len(lines)+2)
		rows = append(rows, "")
		for i, line := range lines {
			if i == 0 {
				rows = append(rows, "* "+line)
				continue
			}
			rows = append(rows, gutter+line)
		}
		rows = append(rows, "")
		return renderTranscriptRows(rows, contentW, activeCLITheme.faint, false)
	}
	for i := range lines {
		lines[i] = dim(lines[i])
	}
	return connectorBlock(lines)
}

func formatReasoningSummary(secs int) string {
	s := fmt.Sprintf(i18n.M.ChatThoughtForFmt, secs)
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] = r[0] - 'a' + 'A'
	}
	return "* " + string(r)
}

func renderReasoningSummary(summary string, width int, hover bool) string {
	if !colorOn() || !hover {
		return dim(summary)
	}
	return themeStyle(activeCLITheme.muted).Bold(true).Render(summary)
}

func renderImageUnderstandingSummary(summary string, width int, hover bool) string {
	if !colorOn() || !hover {
		return dim(summary)
	}
	return themeStyle(activeCLITheme.muted).Bold(true).Render(summary)
}

func isImageUnderstandingNotice(e event.Event) bool {
	if strings.TrimSpace(e.Detail) == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(e.Text)), "image understood:")
}

func imageUnderstandingSummaryFromNotice(text string) string {
	text = strings.TrimSpace(text)
	const prefix = "image understood:"
	if strings.HasPrefix(strings.ToLower(text), prefix) {
		suffix := strings.TrimSpace(text[len(prefix):])
		if suffix != "" {
			return "◩ Image understood · " + suffix
		}
	}
	return "◩ Image understood"
}

func imageUnderstandingBlockStyled(raw string, width int, hover bool) string {
	contentW := transcriptEntryWidth(width)
	innerW := max(contentW-len([]rune(assistantContentGutter)), 8)
	blocks := splitImageUnderstandingBlocks(raw)
	if len(blocks) == 0 {
		return ""
	}
	rows := make([]string, 0, 8)
	rows = append(rows, "")
	for i, block := range blocks {
		if len(blocks) > 1 {
			rows = append(rows, fmt.Sprintf("◩ Image #%d", i+1))
		} else {
			rows = append(rows, "◩ Image understanding")
		}
		for _, line := range wrapDisclosureBody(block, innerW) {
			rows = append(rows, assistantContentGutter+line)
		}
		if i != len(blocks)-1 {
			rows = append(rows, "")
		}
	}
	rows = append(rows, "")
	return renderTranscriptRows(rows, contentW, activeCLITheme.faint, hover)
}

func splitImageUnderstandingBlocks(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	const openTag = "<image-understanding"
	const closeTag = "</image-understanding>"
	var blocks []string
	searchFrom := 0
	for {
		startRel := strings.Index(raw[searchFrom:], openTag)
		if startRel < 0 {
			break
		}
		start := searchFrom + startRel
		endRel := strings.Index(raw[start:], closeTag)
		if endRel < 0 {
			break
		}
		end := start + endRel + len(closeTag)
		if block := strings.TrimSpace(raw[start:end]); block != "" {
			blocks = append(blocks, block)
		}
		searchFrom = end
	}
	if len(blocks) > 0 {
		return blocks
	}
	parts := strings.Split(raw, "\n\n")
	blocks = make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			blocks = append(blocks, part)
		}
	}
	return blocks
}

func wrapDisclosureBody(raw string, width int) []string {
	if width < 8 {
		width = 8
	}
	var lines []string
	for ln := range strings.SplitSeq(strings.TrimRight(raw, "\n"), "\n") {
		wrapped := ansi.Wrap(expandTabs(ln), width, "")
		if wrapped == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, strings.Split(wrapped, "\n")...)
	}
	return lines
}

func renderDisclosureConnectorBlock(lines []string, width int, fg cliColor, hover bool) string {
	if len(lines) == 0 {
		return ""
	}
	if !colorOn() {
		styled := make([]string, len(lines))
		for i, ln := range lines {
			styled[i] = dim(ln)
		}
		return connectorBlock(styled)
	}
	indent := strings.Repeat(" ", len([]rune(connector)))
	rows := make([]string, 0, len(lines))
	rows = append(rows, connector+lines[0])
	for _, ln := range lines[1:] {
		rows = append(rows, indent+ln)
	}
	return renderTranscriptRows(rows, width, fg, hover)
}

func (m *chatTUI) resetTranscriptDisclosures() {
	m.reset()
	m.hoverTranscriptIdx = -1
	m.hoverKind = transcriptHoverNone
}

func (m *chatTUI) rememberCompletedReasoning(summaryIdx int, summary, raw string, expanded bool) {
	if !m.lazyReasoning || strings.TrimSpace(raw) == "" || summaryIdx < 0 {
		return
	}
	m.rememberTranscriptDisclosure(summaryIdx, summary, raw, expanded, transcriptDisclosureReasoning)
}

func (m *chatTUI) rememberImageUnderstanding(summaryIdx int, summary, raw string) {
	if strings.TrimSpace(raw) == "" || summaryIdx < 0 {
		return
	}
	m.rememberTranscriptDisclosure(summaryIdx, summary, raw, false, transcriptDisclosureImageUnderstanding)
}

func (m *chatTUI) rememberTranscriptDisclosure(summaryIdx int, summary, raw string, expanded bool, kind transcriptDisclosureKind) {
	m.remember(summaryIdx, summary, raw, expanded, kind, len(m.transcript))
}

func (m *chatTUI) rebuildDisclosureIndex() {
	m.rebuildIndex(len(m.transcript))
}

func (m *chatTUI) shiftTranscriptDisclosures(start, delta int) {
	m.shift(start, delta, len(m.transcript))
}

func (m *chatTUI) truncateTranscriptDisclosures(n int) {
	m.truncate(n)
	if m.hoverTranscriptIdx >= n {
		m.hoverTranscriptIdx = -1
		m.hoverKind = transcriptHoverNone
	}
}

func (m *chatTUI) clickableAtWrappedLine(lineIdx int) (int, transcriptHoverKind, bool) {
	if lineIdx < 0 || lineIdx >= len(m.wrappedLineTranscriptIdx) {
		return -1, transcriptHoverNone, false
	}
	idx := m.wrappedLineTranscriptIdx[lineIdx]
	if idx < 0 {
		return -1, transcriptHoverNone, false
	}
	if _, ok := m.entryAt(idx); ok {
		return idx, transcriptHoverDisclosure, true
	}
	if _, ok := m.shellOutputIDAtTranscriptIdx(idx); ok {
		return idx, transcriptHoverShell, true
	}
	return -1, transcriptHoverNone, false
}

func (m *chatTUI) clickableAtPosition(lineIdx, x int) (int, transcriptHoverKind, bool) {
	idx, kind, ok := m.clickableAtWrappedLine(lineIdx)
	if !ok {
		return -1, transcriptHoverNone, false
	}
	if kind == transcriptHoverDisclosure && !m.disclosureExpandedAtTranscriptIdx(idx) {
		if x < 0 || x >= m.collapsedDisclosureWidth(idx) {
			return -1, transcriptHoverNone, false
		}
	}
	return idx, kind, true
}

func (m *chatTUI) collapsedDisclosureWidth(idx int) int {
	if idx < 0 || idx >= len(m.transcript) {
		return 0
	}
	entry, ok := m.entryAt(idx)
	if !ok || entry.expanded {
		return ansi.StringWidth(ansi.Strip(m.transcript[idx].rendered))
	}
	summary := strings.TrimSpace(entry.summary)
	if summary == "" {
		summary = strings.TrimSpace(ansi.Strip(m.transcript[idx].rendered))
	}
	return ansi.StringWidth(summary)
}

func (m *chatTUI) clearPendingTranscriptToggle() {
	m.pendingTranscriptToggleIdx = -1
	m.pendingTranscriptToggleKind = transcriptHoverNone
}

func (m *chatTUI) disclosureExpandedAtTranscriptIdx(idx int) bool {
	entry, ok := m.entryAt(idx)
	return ok && entry.expanded
}

func (m *chatTUI) setTranscriptHover(idx int, kind transcriptHoverKind) bool {
	if idx == m.hoverTranscriptIdx && kind == m.hoverKind {
		return false
	}
	changed := false
	if m.hoverTranscriptIdx >= 0 {
		changed = m.renderTranscriptHover(m.hoverTranscriptIdx, m.hoverKind, false) || changed
	}
	m.hoverTranscriptIdx = idx
	m.hoverKind = kind
	if idx >= 0 {
		changed = m.renderTranscriptHover(idx, kind, true) || changed
	}
	if changed {
		m.transcriptDirty = true
	}
	return changed
}

func (m *chatTUI) clearTranscriptHover() bool {
	if m.hoverTranscriptIdx < 0 {
		return false
	}
	idx, kind := m.hoverTranscriptIdx, m.hoverKind
	m.hoverTranscriptIdx = -1
	m.hoverKind = transcriptHoverNone
	if !m.renderTranscriptHover(idx, kind, false) {
		return false
	}
	m.transcriptDirty = true
	return true
}

func (m *chatTUI) renderTranscriptHover(idx int, kind transcriptHoverKind, hover bool) bool {
	if idx < 0 || idx >= len(m.transcript) {
		return false
	}
	before := m.transcript[idx].rendered
	switch kind {
	case transcriptHoverDisclosure:
		entry, ok := m.entryAt(idx)
		if !ok || entry.expanded {
			return false
		}
		summary := entry.summary
		if strings.TrimSpace(summary) == "" {
			summary = ansi.Strip(m.transcript[idx].rendered)
		}
		m.transcript[idx].rendered = m.renderTranscriptDisclosureSummary(entry, summary, hover)
		return m.transcript[idx].rendered != before
	case transcriptHoverShell:
		if id, ok := m.shellOutputIDAtTranscriptIdx(idx); ok {
			m.transcript[idx].rendered = m.renderShellOutputBlock(id, hover)
			return m.transcript[idx].rendered != before
		}
	}
	return false
}

func (m *chatTUI) renderTranscriptDisclosureSummary(entry *disclosureEntry, summary string, hover bool) string {
	if entry == nil {
		return ""
	}
	if strings.TrimSpace(summary) == "" {
		summary = entry.summary
	}
	switch entry.kind {
	case transcriptDisclosureImageUnderstanding:
		return renderImageUnderstandingSummary(summary, m.width, hover)
	default:
		return renderReasoningSummary(summary, m.width, hover)
	}
}

func (m *chatTUI) renderTranscriptDisclosureBody(entry *disclosureEntry, hover bool) string {
	if entry == nil {
		return ""
	}
	switch entry.kind {
	case transcriptDisclosureImageUnderstanding:
		return imageUnderstandingBlockStyled(entry.raw, m.width, hover)
	default:
		return reasoningBlockStyled(entry.raw, m.width, 0, true, hover)
	}
}

func (m *chatTUI) toggleTranscriptDisclosureAt(transcriptIdx int) bool {
	entry, ok := m.entryAt(transcriptIdx)
	if !ok || entry.summaryIdx < 0 || entry.summaryIdx >= len(m.transcript) {
		return false
	}
	contentW := transcriptContentWidth(m.width, m.nativeScrollback)
	oldRows := transcriptEntryLineCount(m.transcript[entry.summaryIdx].rendered, contentW)
	if entry.expanded {
		entry.expanded = false
		m.rewriteTranscriptBlock(entry.summaryIdx, m.renderTranscriptDisclosureSummary(entry, entry.summary, false))
	} else {
		entry.expanded = true
		m.rewriteTranscriptBlock(entry.summaryIdx, m.renderTranscriptDisclosureBody(entry, false))
	}
	newRows := transcriptEntryLineCount(m.transcript[entry.summaryIdx].rendered, contentW)
	if delta := newRows - oldRows; delta != 0 {
		m.viewportAnchorDelta += delta
	}
	m.hoverTranscriptIdx = -1
	m.hoverKind = transcriptHoverNone
	m.rebuildDisclosureIndex()
	m.transcriptDirty = true
	return true
}

func transcriptEntryLineCount(entry string, width int) int {
	if width <= 0 {
		width = 80
	}
	return len(strings.Split(wrapTranscript(entry, width), "\n"))
}
