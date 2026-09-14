package cli

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
)

func TestCLIHotRebuildPreservesCanonicalSessionOwner(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := session.NewService("local", session.NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "same-session"})
	if err != nil {
		t.Fatal(err)
	}
	messages := []provider.Message{
		{ID: "system", Role: provider.RoleSystem, Content: "old system"},
		{ID: "user", Role: provider.RoleUser, Content: "keep this turn"},
	}
	events := make([]session.Event, 0, len(messages))
	for _, message := range messages {
		payload, marshalErr := json.Marshal(map[string]any{"message": message})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		events = append(events, session.Event{Kind: "message/complete", Payload: payload})
	}
	if _, err := runtime.Session().AppendBatch(t.Context(), "seed", events); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := service.SetTitle(t.Context(), runtime.Ref(), "stable title"); err != nil {
		t.Fatal(err)
	}

	old := control.New(control.Options{
		Executor:         agent.New(nil, nil, agent.NewSession("old system"), agent.Options{}, event.Discard),
		SessionService:   service,
		SessionRuntime:   runtime,
		ExclusiveSession: true,
	})
	defer old.Close()

	overrides := cliBuildOverrides{}
	inheritCLIHotRebuildState(&overrides, old)
	if overrides.SessionService != service || overrides.SessionRuntime != runtime || overrides.SessionHostID != "local" {
		t.Fatalf("hot rebuild binding = service %p runtime %p host %q", overrides.SessionService, overrides.SessionRuntime, overrides.SessionHostID)
	}
	if overrides.SessionTemp != old.SessionTemp() {
		t.Fatal("hot rebuild changed the logical-session temp owner")
	}

	replacement := control.New(control.Options{
		Executor:         agent.New(nil, nil, agent.NewSession("new system"), agent.Options{}, event.Discard),
		SessionService:   overrides.SessionService,
		SessionRuntime:   overrides.SessionRuntime,
		ExclusiveSession: true,
		SessionTemp:      overrides.SessionTemp,
	})
	defer replacement.Close()
	if err := adoptCarriedHistoryPreservingProfileAndGrants(replacement, old.History(), "", old); err != nil {
		t.Fatal(err)
	}
	if got, ok := replacement.SessionRef(); !ok || got != runtime.Ref() {
		t.Fatalf("replacement session = %+v, active=%v; want %+v", got, ok, runtime.Ref())
	}
	if title, canonical := replacement.CurrentSessionTitle(); !canonical || title != "stable title" {
		t.Fatalf("replacement title = %q, canonical=%v", title, canonical)
	}

	old.ReleaseResources()
	if _, ok := service.Runtime(runtime.Ref()); !ok {
		t.Fatal("retiring the old controller closed the shared canonical runtime")
	}
}
