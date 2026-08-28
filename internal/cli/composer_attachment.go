package cli

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

const composerAttachmentUndoLimit = 32

type composerPartID uint64

type composerPartKind uint8

const (
	composerPartFoldedText composerPartKind = iota
	composerPartImage
)

type composerPartState uint8

const (
	composerPartActive composerPartState = iota
	composerPartPending
)

// composerAttachmentRange is a view projection of one real typed part. The
// visible label is deliberately absent: identity is the stable part ID and its
// tracked rune span, never a search for label-shaped user text.
type composerAttachmentRange struct {
	partID     composerPartID
	start, end int
}

// pastedBlock is retained as the local name used by the paste-history helpers,
// but it is now a typed composer part. span is expressed in runes in value (for
// active parts) or pendingValue (for an in-flight submitted part).
type pastedBlock struct {
	id      composerPartID
	kind    composerPartKind
	payload string
	label   string
	span    composerAttachmentRange
	state   composerPartState
}

// composerModel is the single owner of semantic composer parts. textarea owns
// painted text and cursor mechanics; this model owns which rune ranges are real
// attachments/folded pastes and carries that identity through edits and undo.
// The legacy field names are nested here so turn-lifecycle code can keep using
// the existing narrow helpers without gaining a second attachment store.
type composerModel struct {
	pastedBlocks []pastedBlock
	value        string
	pendingValue string
	nextPartID   composerPartID

	nextPasteID    int
	usedPasteIDs   map[int]struct{}
	pendingPartIDs []composerPartID

	attachmentHistory composerAttachmentHistory

	// History and queue navigation temporarily replace textarea contents with
	// plain persisted strings. These snapshots restore only the original draft's
	// typed parts; recalled/persisted label text remains ordinary literal text.
	submittedInputDraftParts []pastedBlock
	queueEditDraftParts      []pastedBlock
}

type composerModelSnapshot struct {
	parts        []pastedBlock
	value        string
	pendingValue string
	nextPartID   composerPartID
	nextPasteID  int
	usedPasteIDs map[int]struct{}
	pendingIDs   []composerPartID
}

type composerAttachmentEdit struct {
	beforeValue  string
	beforeCursor int
	beforeModel  composerModelSnapshot
	afterValue   string
	trackUndo    bool
}

type composerAttachmentHistory struct {
	undo []composerAttachmentEdit
}

func cloneComposerParts(parts []pastedBlock) []pastedBlock {
	return append([]pastedBlock(nil), parts...)
}

func clonePasteIDs(ids map[int]struct{}) map[int]struct{} {
	if ids == nil {
		return nil
	}
	out := make(map[int]struct{}, len(ids))
	for id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func (c *composerModel) snapshot() composerModelSnapshot {
	return composerModelSnapshot{
		parts:        cloneComposerParts(c.pastedBlocks),
		value:        c.value,
		pendingValue: c.pendingValue,
		nextPartID:   c.nextPartID,
		nextPasteID:  c.nextPasteID,
		usedPasteIDs: clonePasteIDs(c.usedPasteIDs),
		pendingIDs:   append([]composerPartID(nil), c.pendingPartIDs...),
	}
}

func (c *composerModel) restore(snapshot composerModelSnapshot) {
	c.pastedBlocks = cloneComposerParts(snapshot.parts)
	c.value = snapshot.value
	c.pendingValue = snapshot.pendingValue
	c.nextPartID = snapshot.nextPartID
	c.nextPasteID = snapshot.nextPasteID
	c.usedPasteIDs = clonePasteIDs(snapshot.usedPasteIDs)
	c.pendingPartIDs = append([]composerPartID(nil), snapshot.pendingIDs...)
}

func (c *composerModel) hasActiveParts() bool {
	for _, part := range c.pastedBlocks {
		if part.state == composerPartActive {
			return true
		}
	}
	return false
}

func (c *composerModel) takePartID() composerPartID {
	c.nextPartID++
	if c.nextPartID == 0 {
		c.nextPartID++
	}
	return c.nextPartID
}

func (m *chatTUI) insertComposerPart(kind composerPartKind, payload, label string) {
	edit := m.newComposerAttachmentEdit()
	edit.trackUndo = true
	m.insertComposerPartUntracked(kind, payload, label)
	m.recordComposerAttachmentEdit(edit)
}

func (m *chatTUI) insertComposerPartUntracked(kind composerPartKind, payload, label string) {
	before := m.input.Value()
	if m.deleteComposerSelectionUntracked() {
		m.reconcileEdit(before, m.input.Value())
	}
	before = m.input.Value()
	start := m.composerCursorOffset()
	m.input.InsertString(label + " ")
	m.reconcileEdit(before, m.input.Value())
	id := m.takePartID()
	m.pastedBlocks = append(m.pastedBlocks, pastedBlock{
		id: id, kind: kind, payload: payload, label: label,
		span: composerAttachmentRange{partID: id, start: start, end: start + len([]rune(label))},
	})
}

func (m *chatTUI) insertComposerText(text string) {
	edit := m.newComposerAttachmentEdit()
	if m.deleteComposerSelectionUntracked() {
		m.reconcileEdit(edit.beforeValue, m.input.Value())
	}
	before := m.input.Value()
	m.input.InsertString(text)
	m.reconcileEdit(before, m.input.Value())
	m.recordComposerAttachmentEdit(edit)
}

func (m *chatTUI) setComposerValueTracked(before, after string) {
	edit := m.newComposerAttachmentEdit()
	m.input.SetValue(after)
	m.reconcileEdit(before, after)
	m.recordComposerAttachmentEdit(edit)
}

func (m *chatTUI) composerPartIDsIn(value string) []composerPartID {
	var parts []pastedBlock
	if active, ok := projectComposerParts(m.pastedBlocks, m.value, value, composerPartActive); ok {
		parts = append(parts, active...)
	}
	if pending, ok := projectComposerParts(m.pastedBlocks, m.pendingValue, value, composerPartPending); ok {
		parts = append(parts, pending...)
	}
	slices.SortFunc(parts, func(a, b pastedBlock) int { return a.span.start - b.span.start })
	seen := make(map[composerPartID]struct{}, len(parts))
	ids := make([]composerPartID, 0, len(parts))
	for _, part := range parts {
		if _, ok := seen[part.id]; ok {
			continue
		}
		seen[part.id] = struct{}{}
		ids = append(ids, part.id)
	}
	return ids
}

func (m *chatTUI) clearSubmittedPastes() {
	if len(m.pendingPartIDs) == 0 {
		return
	}
	submitted := make(map[composerPartID]struct{}, len(m.pendingPartIDs))
	for _, id := range m.pendingPartIDs {
		submitted[id] = struct{}{}
	}
	kept := make([]pastedBlock, 0, len(m.pastedBlocks))
	for _, part := range m.pastedBlocks {
		if _, ok := submitted[part.id]; part.state != composerPartPending || !ok {
			kept = append(kept, part)
		}
	}
	m.pastedBlocks = kept
	m.pendingPartIDs = nil
	hasPending := false
	for _, part := range kept {
		hasPending = hasPending || part.state == composerPartPending
	}
	if !hasPending {
		m.pendingValue = ""
	}
}

func (c *composerModel) ensureValue(value string) {
	if c.value == value {
		return
	}
	// A direct textarea replacement that bypassed the composer edit path has no
	// trustworthy offset mapping. Fail closed: pending turn parts remain intact,
	// while only active parts tied to the replaced draft are discarded.
	c.pastedBlocks = slices.DeleteFunc(c.pastedBlocks, func(part pastedBlock) bool {
		return part.state == composerPartActive
	})
	c.value = value
}

func composerPartTextMatches(part pastedBlock, value []rune) bool {
	return part.span.start >= 0 && part.span.start < part.span.end &&
		part.span.end <= len(value) && string(value[part.span.start:part.span.end]) == part.label
}

func composerEditBounds(before, after []rune) (oldStart, oldEnd, newEnd int) {
	prefix := 0
	for prefix < len(before) && prefix < len(after) && before[prefix] == after[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(before)-prefix && suffix < len(after)-prefix &&
		before[len(before)-1-suffix] == after[len(after)-1-suffix] {
		suffix++
	}
	return prefix, len(before) - suffix, len(after) - suffix
}

// reconcileEdit applies one contiguous textarea edit to every active part.
// Edits before a part shift it, edits outside it preserve it, and any overlap
// removes only that part. No label search is involved.
func (c *composerModel) reconcileEdit(before, after string) {
	c.ensureValue(before)
	if before == after {
		c.value = after
		return
	}
	oldRunes, newRunes := []rune(before), []rune(after)
	oldStart, oldEnd, newEnd := composerEditBounds(oldRunes, newRunes)
	delta := newEnd - oldEnd

	kept := make([]pastedBlock, 0, len(c.pastedBlocks))
	for _, part := range c.pastedBlocks {
		if part.state != composerPartActive {
			kept = append(kept, part)
			continue
		}
		if !composerPartTextMatches(part, oldRunes) {
			continue
		}
		switch {
		case part.span.end <= oldStart:
			// Edit is after the part (including insertion at its end).
		case part.span.start >= oldEnd:
			// Edit is before the part (including insertion at its start).
			part.span.start += delta
			part.span.end += delta
		default:
			// Any replacement/deletion/insertion inside the semantic token turns
			// the remaining visible characters into ordinary user text.
			continue
		}
		kept = append(kept, part)
	}
	c.pastedBlocks = kept
	c.value = after
}

func trimRuneBounds(value []rune) (int, int) {
	start, end := 0, len(value)
	for start < end && unicode.IsSpace(value[start]) {
		start++
	}
	for end > start && unicode.IsSpace(value[end-1]) {
		end--
	}
	return start, end
}

// composerProjectionBounds maps the only supported submission projections:
// the exact textarea value, strings.TrimSpace(value), and the trimmed suffix
// after a slash command such as `/steer`. It intentionally fails closed for an
// arbitrary caller transformation instead of searching for visible labels.
func composerProjectionBounds(source, projected string) (int, int, bool) {
	sourceRunes := []rune(source)
	if source == projected {
		return 0, len(sourceRunes), true
	}
	start, end := trimRuneBounds(sourceRunes)
	if string(sourceRunes[start:end]) == projected {
		return start, end, true
	}
	commandEnd := start
	for commandEnd < end && !unicode.IsSpace(sourceRunes[commandEnd]) {
		commandEnd++
	}
	bodyStart := commandEnd
	for bodyStart < end && unicode.IsSpace(sourceRunes[bodyStart]) {
		bodyStart++
	}
	if bodyStart > commandEnd && string(sourceRunes[bodyStart:end]) == projected {
		return bodyStart, end, true
	}
	return 0, 0, false
}

func projectComposerParts(parts []pastedBlock, source, projected string, state composerPartState) ([]pastedBlock, bool) {
	start, end, ok := composerProjectionBounds(source, projected)
	if !ok {
		return nil, false
	}
	sourceRunes := []rune(source)
	out := make([]pastedBlock, 0, len(parts))
	for _, part := range parts {
		if part.state != state || part.span.start < start || part.span.end > end ||
			!composerPartTextMatches(part, sourceRunes) {
			continue
		}
		part.span.start -= start
		part.span.end -= start
		out = append(out, part)
	}
	slices.SortFunc(out, func(a, b pastedBlock) int {
		if a.span.start != b.span.start {
			return a.span.start - b.span.start
		}
		if a.id < b.id {
			return -1
		}
		if a.id > b.id {
			return 1
		}
		return 0
	})
	return out, true
}

func (m chatTUI) composerAttachmentRanges() []composerAttachmentRange {
	if m.value != m.input.Value() {
		return nil
	}
	value := []rune(m.value)
	ranges := make([]composerAttachmentRange, 0, len(m.pastedBlocks))
	for _, part := range m.pastedBlocks {
		if part.state != composerPartActive || part.kind != composerPartImage ||
			!composerPartTextMatches(part, value) {
			continue
		}
		ranges = append(ranges, part.span)
	}
	slices.SortFunc(ranges, func(a, b composerAttachmentRange) int { return a.start - b.start })
	return ranges
}

func (m chatTUI) composerAttachmentAt(offset int) (composerAttachmentRange, bool) {
	for _, token := range m.composerAttachmentRanges() {
		if offset >= token.start && offset <= token.end {
			return token, true
		}
	}
	return composerAttachmentRange{}, false
}

func (m chatTUI) composerCursorOffset() int {
	lines := strings.Split(m.input.Value(), "\n")
	line := min(max(m.input.Line(), 0), len(lines)-1)
	offset := 0
	for i := range line {
		offset += utf8.RuneCountInString(lines[i]) + 1
	}
	return offset + min(max(m.input.Column(), 0), utf8.RuneCountInString(lines[line]))
}

func (m *chatTUI) selectComposerAttachment(token composerAttachmentRange, origin int) {
	m.composerSel = composerSelection{
		active: true, anchor: token.start, head: token.end, value: m.input.Value(),
		atomic: true, origin: origin, partID: token.partID,
	}
	m.setComposerCursor(token.end)
}

func (m *chatTUI) deleteComposerAttachment(token composerAttachmentRange) bool {
	runes := []rune(m.input.Value())
	if token.partID == 0 || token.start < 0 || token.start >= token.end || token.end > len(runes) {
		return false
	}
	found := false
	for _, part := range m.pastedBlocks {
		if part.state == composerPartActive && part.id == token.partID && part.span == token &&
			composerPartTextMatches(part, runes) {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	start, end := token.start, token.end
	if end < len(runes) && unicode.IsSpace(runes[end]) && runes[end] != '\n' {
		end++
	} else if start > 0 && unicode.IsSpace(runes[start-1]) && runes[start-1] != '\n' {
		start--
	}
	before := m.newComposerAttachmentEdit()
	m.input.SetValue(string(runes[:start]) + string(runes[end:]))
	m.reconcileEdit(before.beforeValue, m.input.Value())
	m.composerSel = composerSelection{}
	m.setComposerCursor(start)
	m.recordComposerAttachmentEdit(before)
	return true
}

func (m *chatTUI) deleteSelectedComposerAttachment() bool {
	if !m.validComposerSelection() || !m.composerSel.atomic || m.composerSel.partID == 0 {
		return false
	}
	start, end := m.composerSel.ordered()
	return m.deleteComposerAttachment(composerAttachmentRange{
		partID: m.composerSel.partID,
		start:  start,
		end:    end,
	})
}

func (m *chatTUI) deleteComposerAttachmentAtCursor(msg tea.KeyPressMsg) bool {
	cursor := m.composerCursorOffset()
	runes := []rune(m.input.Value())
	backward := key.Matches(msg, m.input.KeyMap.DeleteCharacterBackward) ||
		key.Matches(msg, m.input.KeyMap.DeleteWordBackward)
	forward := key.Matches(msg, m.input.KeyMap.DeleteCharacterForward) ||
		key.Matches(msg, m.input.KeyMap.DeleteWordForward)
	if !backward && !forward {
		return false
	}
	for _, token := range m.composerAttachmentRanges() {
		backspaceOverSeparator := cursor == token.end+1 && token.end < len(runes) &&
			unicode.IsSpace(runes[token.end]) && runes[token.end] != '\n'
		if backward && (cursor > token.start && cursor <= token.end || backspaceOverSeparator) {
			return m.deleteComposerAttachment(token)
		}
		if forward && cursor >= token.start && cursor < token.end {
			return m.deleteComposerAttachment(token)
		}
	}
	return false
}

func (m *chatTUI) newComposerAttachmentEdit() composerAttachmentEdit {
	m.ensureValue(m.input.Value())
	return composerAttachmentEdit{
		beforeValue:  m.input.Value(),
		beforeCursor: m.composerCursorOffset(),
		beforeModel:  m.snapshot(),
		trackUndo:    m.hasActiveParts(),
	}
}

func (m *chatTUI) updateComposerInputTracked(msg tea.Msg, edit *composerAttachmentEdit) tea.Cmd {
	beforeValue := m.input.Value()
	if edit == nil {
		before := m.newComposerAttachmentEdit()
		edit = &before
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if edit.beforeValue != m.input.Value() {
		m.reconcileEdit(edit.beforeValue, m.input.Value())
		m.recordComposerAttachmentEdit(*edit)
	} else if beforeValue != m.input.Value() {
		m.reconcileEdit(beforeValue, m.input.Value())
	}
	return cmd
}

func (m *chatTUI) recordComposerAttachmentEdit(edit composerAttachmentEdit) {
	edit.afterValue = m.input.Value()
	if !edit.trackUndo || edit.beforeValue == edit.afterValue {
		return
	}
	undo := append(m.attachmentHistory.undo, edit)
	if len(undo) > composerAttachmentUndoLimit {
		undo = undo[len(undo)-composerAttachmentUndoLimit:]
	}
	m.attachmentHistory.undo = undo
}

func (m *chatTUI) undoComposerAttachmentEdit() bool {
	undo := m.attachmentHistory.undo
	if len(undo) == 0 {
		return false
	}
	last := undo[len(undo)-1]
	if m.input.Value() != last.afterValue {
		m.attachmentHistory.undo = nil
		return false
	}
	m.attachmentHistory.undo = undo[:len(undo)-1]
	m.input.SetValue(last.beforeValue)
	m.restore(last.beforeModel)
	m.composerSel = composerSelection{}
	m.setComposerCursor(last.beforeCursor)
	m.growInputToFit()
	m.updateCompletion()
	return true
}

func (m *chatTUI) snapshotActiveComposerParts() []pastedBlock {
	m.ensureValue(m.input.Value())
	parts := make([]pastedBlock, 0, len(m.pastedBlocks))
	for _, part := range m.pastedBlocks {
		if part.state == composerPartActive {
			parts = append(parts, part)
		}
	}
	return parts
}

func (m *chatTUI) replaceComposerDraft(value string, parts []pastedBlock) {
	m.pastedBlocks = slices.DeleteFunc(m.pastedBlocks, func(part pastedBlock) bool {
		return part.state == composerPartActive
	})
	for _, part := range cloneComposerParts(parts) {
		part.state = composerPartActive
		m.pastedBlocks = append(m.pastedBlocks, part)
	}
	m.input.SetValue(value)
	m.value = value
	m.composerSel = composerSelection{}
	m.attachmentHistory.undo = nil
}

func (m *chatTUI) clearActiveComposerParts() {
	m.pastedBlocks = slices.DeleteFunc(m.pastedBlocks, func(part pastedBlock) bool {
		return part.state == composerPartActive
	})
	m.value = m.input.Value()
	m.composerSel = composerSelection{}
	m.attachmentHistory.undo = nil
}

func (m *chatTUI) stageComposerSubmission(displayed string) {
	m.ensureValue(m.input.Value())
	projected, ok := projectComposerParts(m.pastedBlocks, m.value, displayed, composerPartActive)
	byID := make(map[composerPartID]pastedBlock, len(projected))
	if ok {
		for _, part := range projected {
			part.state = composerPartPending
			byID[part.id] = part
		}
	}
	kept := make([]pastedBlock, 0, len(m.pastedBlocks))
	for _, part := range m.pastedBlocks {
		if part.state == composerPartPending {
			kept = append(kept, part)
			continue
		}
		if pending, found := byID[part.id]; found {
			kept = append(kept, pending)
		}
	}
	m.pastedBlocks = kept
	m.pendingPartIDs = m.pendingPartIDs[:0]
	for _, part := range projected {
		m.pendingPartIDs = append(m.pendingPartIDs, part.id)
	}
	m.pendingValue = ""
	if len(projected) > 0 {
		m.pendingValue = displayed
	}
	m.value = ""
	m.composerSel = composerSelection{}
	m.attachmentHistory.undo = nil
}

func (m *chatTUI) restorePendingComposer(value string) {
	wanted := make(map[composerPartID]struct{}, len(m.pendingPartIDs))
	for _, id := range m.pendingPartIDs {
		wanted[id] = struct{}{}
	}
	kept := make([]pastedBlock, 0, len(m.pastedBlocks))
	for _, part := range m.pastedBlocks {
		switch part.state {
		case composerPartActive:
			// Existing behavior restores the un-sent prompt over any newer draft.
			continue
		case composerPartPending:
			if _, ok := wanted[part.id]; ok {
				part.state = composerPartActive
			}
		}
		kept = append(kept, part)
	}
	m.pastedBlocks = kept
	m.input.SetValue(value)
	m.value = value
	m.pendingValue = ""
	m.pendingPartIDs = nil
	m.composerSel = composerSelection{}
	m.attachmentHistory.undo = nil
}

func composerAttachmentUndoKey(keyName string) bool {
	return keyName == "super+z" || keyName == "meta+z" || keyName == "ctrl+_" || keyName == "ctrl+/"
}
