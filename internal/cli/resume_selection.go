package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/session"
)

// modelForResumePath answers which model a resumed run should use. An explicit
// flag always wins; otherwise the saved selection is validated against the
// connection it was recorded against, so a migrated or edited connection fails
// closed instead of silently resuming under a different account.
func modelForResumePath(modelName, resumePath string, cfg *config.Config) (string, error) {
	if strings.TrimSpace(modelName) != "" || strings.TrimSpace(resumePath) == "" {
		return modelName, nil
	}
	sessionModel, identity, ok := agent.LoadSessionModelSelection(resumePath)
	if !ok {
		return modelName, nil
	}
	if cfg == nil {
		return sessionModel, nil
	}
	resolved, err := cfg.ResolveSavedModel(sessionModel, identity)
	if err != nil {
		return "", err
	}
	if _, ok := cfg.ResolveModel(resolved); !ok {
		return modelName, nil
	}
	return resolved, nil
}

func applyResumeModel(model *string, resumePath string, cfg *config.Config) error {
	resolved, err := modelForResumePath(*model, resumePath, cfg)
	if err != nil {
		return err
	}
	*model = resolved
	return nil
}

func modelForResumeEntry(modelName string, entry resumeEntry, cfg *config.Config) (string, error) {
	if strings.TrimSpace(modelName) != "" || entry.isZero() {
		return modelName, nil
	}
	if entry.kind == resumeEntryLegacy {
		return modelForResumePath(modelName, entry.session.Path, cfg)
	}
	model, identity := entry.modelSelection()
	if strings.TrimSpace(model) == "" {
		return modelName, nil
	}
	if cfg == nil {
		return model, nil
	}
	resolved, err := cfg.ResolveSavedModel(model, identity)
	if err != nil {
		return "", err
	}
	if _, ok := cfg.ResolveModel(resolved); !ok {
		return modelName, nil
	}
	return resolved, nil
}

func applyResumeEntryModel(model *string, entry resumeEntry, cfg *config.Config) error {
	resolved, err := modelForResumeEntry(*model, entry, cfg)
	if err != nil {
		return err
	}
	*model = resolved
	return nil
}

func resumeEntryMatchesQuery(entry resumeEntry, query, lowerQuery string) (bool, bool) {
	path := entry.path()
	id := entry.stored.SessionID
	if entry.kind == resumeEntryLegacy {
		id = agent.BranchID(path)
	}
	base := filepath.Base(path)
	sourcePath := cleanResumeSource(entry.stored.SourcePath)
	sourceID := agent.BranchID(sourcePath)
	exact := query == id || query == base || query == sourceID || cleanResumeSource(query) == cleanResumeSource(path) ||
		(sourcePath != "" && cleanResumeSource(query) == sourcePath) || query == entry.key()
	haystack := strings.ToLower(strings.Join([]string{id, base, sourceID, entry.displayTitle()}, "\n"))
	return exact, strings.Contains(haystack, lowerQuery)
}

// copyResumableSession refuses to duplicate a session whose saved selection no
// longer resolves, so the copy cannot silently inherit an unusable connection.
func copyResumableSession(model, resumePath string, cfg *config.Config) (string, error) {
	if _, err := modelForResumePath(model, resumePath, cfg); err != nil {
		return "", err
	}
	return copySessionForWriting(resumePath)
}

// resumeWithPersistedSelection records the selection the resumed controller
// actually accepted, so the next restart restores it instead of re-resolving.
func resumeWithPersistedSelection(ctrl *control.Controller, session *agent.Session, path string) error {
	if ctrl.UsesExclusiveSession() {
		if _, err := ctrl.ContinueLegacySession(context.Background(), path, ""); err != nil {
			return err
		}
		return nil
	}
	ctrl.Resume(session, path)
	return persistCLIModelSelection(ctrl)
}

// commitResumedSession is a no-op without a resume path, so callers need no
// second guard around the takeover handover.
func commitResumedSession(binding *cliTakeoverBinding, manager *cliTakeoverManager, ctrl *control.Controller, session *agent.Session, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := binding.commitPrevious(manager); err != nil {
		return err
	}
	return resumeWithPersistedSelection(ctrl, session, path)
}

func commitResumedEntry(binding *cliTakeoverBinding, manager *cliTakeoverManager, ctrl *control.Controller, legacy *agent.Session, entry resumeEntry) error {
	if entry.isZero() {
		return nil
	}
	if entry.kind == resumeEntryLegacy {
		return commitResumedSession(binding, manager, ctrl, legacy, entry.session.Path)
	}
	identity, ok := any(ctrl).(control.IdentityLifecycle)
	if !ok || !identity.UsesExclusiveSession() {
		return fmt.Errorf("resume: canonical session lifecycle is unavailable")
	}
	ctx := context.Background()
	switch entry.kind {
	case resumeEntryCanonical:
		ref := entry.stored.Ref
		currentRoot := session.RootForLegacyDir(ctrl.SessionDir())
		sourceRoot := filepath.Dir(entry.stored.Path)
		var err error
		if filepath.Clean(currentRoot) != filepath.Clean(sourceRoot) {
			ref, err = importCanonicalResumeEntry(ctx, identity.SessionService(), sourceRoot, ref)
			if err != nil {
				return err
			}
		}
		_, err = identity.OpenSession(ctx, ref)
		return err
	case resumeEntryRetired:
		_, err := identity.ContinuePrototypeSession(ctx, entry.stored.Path)
		return err
	default:
		return fmt.Errorf("resume: unsupported session source")
	}
}

// prepareServeSessionPath picks serve's auto-save target: reuse the resumed
// file, adopt the caller's session id, or leave the controller to stamp a
// fresh path.
func prepareServeSessionPath(ctrl *control.Controller, session *agent.Session, resumePath, sessionID string) error {
	if strings.TrimSpace(resumePath) != "" {
		return resumeWithPersistedSelection(ctrl, session, resumePath)
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	if ctrl.UsesExclusiveSession() {
		_, err := ctrl.BindFreshSession(context.Background(), strings.TrimSpace(sessionID))
		return err
	}
	freshPath, err := freshWebSessionPath(ctrl.SessionDir(), sessionID)
	if err != nil {
		return err
	}
	ctrl.SetFreshSessionPath(freshPath)
	return nil
}
