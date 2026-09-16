package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
)

func TestRetiredV3OnlyResumeContinuesAndReopensSameCanonicalSession(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	retiredID := "retired-only"
	saveRetiredMachineTestSession(t, session.RetiredRootForLegacyDir(sessionDir), retiredID, "original v3 prompt")
	identityKey := []byte("01234567890123456789012345678901")

	listed, err := machineSessions(sessionDir, identityKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != machineSessionIDWithKey(retiredID, identityKey) {
		t.Fatalf("v3-only machine list = %+v", listed)
	}
	newest, ok := mostRecentResumeEntry(sessionDir)
	if !ok || newest.kind != resumeEntryRetired || newest.stored.SessionID != retiredID {
		t.Fatalf("v3-only --continue target = %+v, found=%v", newest, ok)
	}

	service := cliSessionService(sessionDir)
	if service == nil {
		t.Fatal("canonical session service is unavailable")
	}
	placeholder, err := service.Create(t.Context(), session.CreateOptions{SessionID: "first-placeholder"})
	if err != nil {
		t.Fatal(err)
	}
	first := control.New(control.Options{
		Executor:         agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard),
		SessionDir:       sessionDir,
		SessionService:   service,
		SessionRuntime:   placeholder,
		ExclusiveSession: true,
		Sink:             event.Discard,
	})
	pickerEntries := resumeEntriesForController(sessionDir, first)
	foundRetired := false
	for _, entry := range pickerEntries {
		if entry.kind == resumeEntryRetired && entry.stored.SessionID == retiredID {
			foundRetired = true
			break
		}
	}
	if !foundRetired {
		first.Close()
		t.Fatalf("v3-only /resume entries = %+v", pickerEntries)
	}
	target, err := resolveResumeEntry(sessionDir, retiredID)
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	if target.kind != resumeEntryRetired {
		first.Close()
		t.Fatalf("resolved source kind = %v, want retired v3", target.kind)
	}
	if err := commitResumedEntry(nil, nil, first, nil, target); err != nil {
		first.Close()
		t.Fatal(err)
	}
	importedRef, ok := first.SessionRef()
	if !ok || importedRef.SessionID == "" || importedRef.SessionID == retiredID {
		first.Close()
		t.Fatalf("imported ref = %+v, bound=%v", importedRef, ok)
	}
	_, importedRuntime, bound := first.SessionBinding()
	if !bound {
		first.Close()
		t.Fatal("imported runtime is not bound")
	}
	appendCanonicalResumeTurn(t, importedRuntime, "continued user prompt", "continued answer")
	first.Close()
	if err := service.Close(t.Context(), placeholder.Ref()); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), importedRef); err != nil {
		t.Fatal(err)
	}

	listed, err = machineSessions(sessionDir, identityKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != machineSessionIDWithKey(importedRef.SessionID, identityKey) || listed[0].Turns != 2 {
		t.Fatalf("post-import machine list = %+v, want one continued canonical session", listed)
	}
	target, err = resolveResumeEntry(sessionDir, retiredID)
	if err != nil {
		t.Fatalf("resolve imported session by retired id: %v", err)
	}
	if target.kind != resumeEntryCanonical || target.stored.Ref != importedRef {
		t.Fatalf("retired id resolved to %+v, want canonical successor %+v", target, importedRef)
	}

	secondPlaceholder, err := service.Create(t.Context(), session.CreateOptions{SessionID: "second-placeholder"})
	if err != nil {
		t.Fatal(err)
	}
	second := control.New(control.Options{
		Executor:         agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard),
		SessionDir:       sessionDir,
		SessionService:   service,
		SessionRuntime:   secondPlaceholder,
		ExclusiveSession: true,
		Sink:             event.Discard,
	})
	target, err = resolveResumeEntry(sessionDir, importedRef.SessionID)
	if err != nil {
		second.Close()
		t.Fatal(err)
	}
	if target.kind != resumeEntryCanonical {
		second.Close()
		t.Fatalf("second source kind = %v, want canonical", target.kind)
	}
	if err := (&chatTUI{ctrl: second}).commitResumeEntry(target); err != nil {
		second.Close()
		t.Fatal(err)
	}
	if got, ok := second.SessionRef(); !ok || got != importedRef {
		second.Close()
		t.Fatalf("second resume ref = %+v, bound=%v; want %+v", got, ok, importedRef)
	}
	history := second.History()
	second.Close()
	if err := service.Close(t.Context(), secondPlaceholder.Ref()); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(t.Context(), importedRef); err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[0].Content != "original v3 prompt" || history[1].Content != "continued user prompt" || history[2].Content != "continued answer" {
		t.Fatalf("resumed history = %+v", history)
	}
}

func appendCanonicalResumeTurn(t *testing.T, runtime *session.Runtime, userText, assistantText string) {
	t.Helper()
	messages := []provider.Message{
		{ID: "continued-user", Role: provider.RoleUser, Content: userText},
		{ID: "continued-assistant", Role: provider.RoleAssistant, Content: assistantText},
	}
	events := []session.Event{{Kind: "turn/start"}}
	for _, message := range messages {
		payload, err := json.Marshal(map[string]any{"message": message})
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, session.Event{Kind: "message/complete", Payload: payload})
	}
	events = append(events, session.Event{Kind: "turn/end", Payload: json.RawMessage(`{"status":"completed"}`)})
	if _, err := runtime.Session().Append(context.Background(), session.Batch{
		OperationID: "continued-turn",
		TurnID:      "continued-turn",
		Events:      events,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalResumePickerFindsAndOpensStoredSession(t *testing.T) {
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	service := cliSessionService(sessionDir)
	if service == nil {
		t.Fatal("canonical session service is unavailable")
	}
	first, err := service.Create(t.Context(), session.CreateOptions{SessionID: "current-session"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(t.Context(), session.CreateOptions{SessionID: "resume-target"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"message": provider.Message{
		ID: "resume-user", Role: provider.RoleUser, Content: "resume this canonical session",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Session().AppendBatch(t.Context(), "seed-resume", []session.Event{{Kind: "message/complete", Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := service.SetTitle(t.Context(), second.Ref(), "canonical resume target"); err != nil {
		t.Fatal(err)
	}

	ctrl := control.New(control.Options{
		Executor:         agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard),
		SessionDir:       sessionDir,
		SessionService:   service,
		SessionRuntime:   first,
		ExclusiveSession: true,
		Sink:             event.Discard,
	})
	defer ctrl.Close()

	var target resumeEntry
	for _, entry := range resumeEntriesForController(sessionDir, ctrl) {
		if entry.kind == resumeEntryCanonical && entry.stored.SessionID == second.Ref().SessionID {
			target = entry
			break
		}
	}
	if target.isZero() {
		t.Fatal("canonical target is missing from the resume picker")
	}
	target, err = hydrateResumeEntry(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if target.displayTitle() != "canonical resume target" {
		t.Fatalf("resume title = %q", target.displayTitle())
	}

	m := &chatTUI{ctrl: ctrl}
	if err := m.commitResumeEntry(target); err != nil {
		t.Fatal(err)
	}
	if got, ok := ctrl.SessionRef(); !ok || got != second.Ref() {
		t.Fatalf("active session = %+v, bound=%v; want %+v", got, ok, second.Ref())
	}
	if ctrl.SessionPath() != "" {
		t.Fatalf("canonical resume manufactured legacy path %q", ctrl.SessionPath())
	}
}
