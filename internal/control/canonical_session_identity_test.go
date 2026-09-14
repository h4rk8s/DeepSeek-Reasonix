package control

import (
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/session"
	"reasonix/internal/sessioninbox"
)

func TestCanonicalSessionIdentityDoesNotDependOnLegacyPath(t *testing.T) {
	service, err := session.NewService("local", session.NewFilesystemPersistence(filepath.Join(t.TempDir(), "sessions-v4")))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "canonical-identity"})
	if err != nil {
		t.Fatal(err)
	}
	ctrl := New(Options{
		Executor:         agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard),
		SessionService:   service,
		SessionRuntime:   runtime,
		ExclusiveSession: true,
		Sink:             event.Discard,
	})
	defer ctrl.Close()

	if ctrl.SessionPath() != "" {
		t.Fatalf("canonical controller has legacy path %q", ctrl.SessionPath())
	}
	if got := ctrl.SessionID(); got != "canonical-identity" {
		t.Fatalf("SessionID() = %q", got)
	}
	if got := ctrl.PermissionSnapshot().SessionID; got != "canonical-identity" {
		t.Fatalf("permission session = %q", got)
	}
	if got := ctrl.CurrentBranchID(); got != "canonical-identity" {
		t.Fatalf("current branch = %q", got)
	}
	if _, err := ctrl.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentFollowup, Submit: "queued", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	snapshot := ctrl.InboxSnapshot()
	if len(snapshot.Items) != 1 || snapshot.Items[0].SessionID != "canonical-identity" {
		t.Fatalf("inbox identity = %+v", snapshot.Items)
	}
}
