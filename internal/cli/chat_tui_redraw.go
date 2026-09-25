package cli

import (
	tea "charm.land/bubbletea/v2"

	"reasonix/internal/i18n"
)

func (m *chatTUI) handleCtrlL() tea.Cmd {
	if m.state == tuiRunning {
		return tea.ClearScreen
	}
	m.finalizeStreamed()
	m.clearTranscriptDisplay()
	m.commitTranscriptSource(transcriptSource{kind: transcriptSourceBanner})
	m.transcriptDirty = true
	m.forceGotoBottom = true
	m.notice(i18n.M.SlashClsDone)
	return nil
}
