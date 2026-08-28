package cli

import tea "charm.land/bubbletea/v2"

// viewportProjection captures the pre-update facts needed to preserve the
// visible transcript anchor while the semantic model changes.
type viewportProjection struct {
	logFirstFrame bool
	followTail    bool
	lines         int
	width         int
	height        int
	yOffset       int
	resizeAnchor  transcriptResizeAnchor
}

func captureViewportProjection(m chatTUI, msg tea.Msg) viewportProjection {
	if m.diagnostics != nil {
		m.diagnostics.NoteBooted()
	}
	p := viewportProjection{
		followTail: m.shouldFollowTail(),
		lines:      len(m.transcript),
		width:      m.width,
		height:     m.height,
		yOffset:    m.viewport.YOffset(),
	}
	if m.diagnostics != nil && !m.firstFrameLogged {
		_, p.logFirstFrame = msg.(tea.WindowSizeMsg)
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok && size.Width != m.width && !p.followTail {
		p.resizeAnchor = captureTranscriptResizeAnchor(m.transcript, m.viewport.Width(), p.yOffset)
	}
	return p
}

func (m chatTUI) applyViewportProjection(msg tea.Msg, before viewportProjection, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m.markFirstFrame(before.logFirstFrame)
	contentWidth := transcriptContentWidth(m.width, m.nativeScrollback)
	m.viewport.SetWidth(contentWidth)
	m.statusLineCount = m.computeStatusLineCount(m.width)
	m.syncInputHeightLimit()
	m.syncWindowTitle()
	m.viewport.SetHeight(m.transcriptHeight())

	widthChanged := m.width != before.width
	if widthChanged {
		m.reflowTranscript(m.width)
		m.sel = selection{}
	}
	m.syncViewportTranscript(before, contentWidth, widthChanged)
	mouseCmd := m.viewportMouseCommand(msg, before)

	if m.legacyScrollClear && m.viewport.YOffset() != before.yOffset && !m.nativeScrollback && !m.sessionSwitch {
		m.sessionSwitch = false
		return m, batchCmds(tea.ClearScreen, mouseCmd, cmd)
	}
	m.sessionSwitch = false
	return m, batchCmds(mouseCmd, cmd)
}

func (m *chatTUI) markFirstFrame(mark bool) {
	if !mark {
		return
	}
	m.firstFrameLogged = true
	if m.diagnostics != nil {
		m.diagnostics.Milestone("first_frame")
	}
}

func (m *chatTUI) syncViewportTranscript(before viewportProjection, contentWidth int, widthChanged bool) {
	forceFullWrap := widthChanged || len(m.transcript) < before.lines
	wrapBehind := m.wrapWidth != contentWidth || m.wrapBlockCount != len(m.transcript)
	anchorYOffset := -1
	if m.viewportAnchorDelta != 0 {
		anchorYOffset = max(0, before.yOffset+m.viewportAnchorDelta)
	}

	if forceFullWrap || wrapBehind || len(m.transcript) != before.lines {
		m.feedChangedTranscript(contentWidth, forceFullWrap, anchorYOffset)
		m.restoreViewportPosition(before, contentWidth, widthChanged, anchorYOffset)
	} else if before.followTail && (m.forceGotoBottom || m.height != before.height) {
		m.viewport.GotoBottom()
	}
	if m.forceGotoBottom {
		m.viewport.GotoBottom()
		m.markFollowTail()
		m.forceGotoBottom = false
	}
	if m.viewport.AtBottom() {
		m.clearJumpToBottomNotice()
	} else if len(m.transcript) > before.lines && !before.followTail {
		m.noteOffscreenTranscriptGrowth()
	}
	m.transcriptDirty = false
}

func (m *chatTUI) feedChangedTranscript(contentWidth int, forceFullWrap bool, anchorYOffset int) {
	if !m.syncWrappedLines(contentWidth, forceFullWrap) {
		return
	}
	if anchorYOffset >= 0 {
		m.padWrappedCacheForYOffset(anchorYOffset, m.viewport.Height())
	}
	m.feedViewportContent()
}

func (m *chatTUI) restoreViewportPosition(before viewportProjection, contentWidth int, widthChanged bool, anchorYOffset int) {
	switch {
	case anchorYOffset >= 0:
		m.viewport.SetYOffset(anchorYOffset)
		m.viewportAnchorDelta = 0
	case before.followTail || m.shouldFollowTail():
		m.viewport.GotoBottom()
		m.markFollowTail()
	case widthChanged && before.resizeAnchor.valid:
		m.viewport.SetYOffset(before.resizeAnchor.yOffset(m.transcript, contentWidth))
	}
}

func (m *chatTUI) viewportMouseCommand(msg tea.Msg, before viewportProjection) tea.Cmd {
	var cmd tea.Cmd
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		if m.width != before.width || m.height != before.height {
			cmd = m.maybeReenableMouse()
		}
	case tea.FocusMsg:
		cmd = m.maybeReenableMouse()
	case mouseReenableMsg:
		cmd = m.handleMouseReenableMsg(v)
	}
	if m.wantMouseReenable {
		m.wantMouseReenable = false
		cmd = batchCmds(cmd, m.maybeReenableMouse())
	}
	return cmd
}
