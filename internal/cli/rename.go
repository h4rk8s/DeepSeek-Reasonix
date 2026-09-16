package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/i18n"
)

// runRenameCommand handles "/rename": with no argument it shows usage;
// "/rename <new title>" renames the current session;
// "/rename <n> <new title>" renames session #n from the /resume list.
func (m *chatTUI) runRenameCommand(input string) {
	args := tokenizeArgs(input) // args[0] == "/rename"

	if len(args) < 2 {
		m.notice(i18n.M.RenameUsage)
		return
	}

	sessions := mergedResumeEntries(m.ctrl.SessionDir(), resumeListCap)
	title := ""
	targetPath := ""
	var targetCurrent control.SessionTitleLifecycle
	var targetStored *resumeEntry

	// Check if the first arg after /rename is a session index (a number).
	idx, err := strconv.Atoi(args[1])
	if err == nil && len(args) >= 3 {
		// "/rename <n> <new title>"
		if idx < 1 || idx > len(sessions) {
			m.notice(fmt.Sprintf(i18n.M.ResumeBadIndexFmt, len(sessions)))
			return
		}
		picked := sessions[idx-1]
		if picked.target.canonical() {
			if resumeEntryIsActive(m.ctrl, picked) {
				targetCurrent, _ = m.ctrl.(control.SessionTitleLifecycle)
			} else {
				targetStored = &picked
			}
		} else {
			targetPath = picked.session.Path
		}
		title = strings.TrimSpace(strings.TrimPrefix(input, args[0]+" "+args[1]))
	} else {
		// "/rename <new title>" -- rename the current session.
		identity, identityOK := m.ctrl.(control.IdentityLifecycle)
		if identityOK {
			if _, active := identity.SessionRef(); active {
				targetCurrent, _ = m.ctrl.(control.SessionTitleLifecycle)
			}
		}
		if targetCurrent == nil {
			if m.ctrl.SessionPath() == "" {
				m.notice(i18n.M.RenameNoSession)
				return
			}
			targetPath = m.ctrl.SessionPath()
		}
		title = strings.TrimSpace(strings.TrimPrefix(input, args[0]))
	}

	if title == "" {
		m.notice(i18n.M.RenameUsage)
		return
	}

	var renameErr error
	if targetStored != nil {
		service := cliSessionServiceForRoot(filepath.Dir(targetStored.session.Path))
		if service == nil {
			renameErr = fmt.Errorf("canonical session service is unavailable")
		} else {
			renameErr = service.SetTitle(context.Background(), targetStored.target.ref, title)
		}
	} else if targetCurrent != nil {
		renameErr = targetCurrent.SetSessionTitle(context.Background(), title)
	} else {
		renameErr = agent.RenameSession(targetPath, title)
	}
	if renameErr != nil {
		m.notice("rename: " + renameErr.Error())
		return
	}
	if targetCurrent != nil || (targetStored != nil && resumeEntryIsActive(m.ctrl, *targetStored)) || targetPath == m.ctrl.SessionPath() {
		m.syncWindowTitle()
	}

	m.notice(fmt.Sprintf(i18n.M.RenameDoneFmt, title))
}
