package control

import (
	"path/filepath"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/session"
	"reasonix/internal/sessioninbox"
)

func TestCanonicalInboxSurvivesControllerAndServiceRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := session.NewService("local", session.NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "durable-inbox"})
	if err != nil {
		t.Fatal(err)
	}
	ref := runtime.Ref()
	first := New(Options{
		SessionService:   service,
		SessionRuntime:   runtime,
		ExclusiveSession: true,
		Sink:             event.Discard,
	})
	receipt, err := first.EnqueueInbox(InboxRequest{
		Intent: sessioninbox.IntentFollowup,
		Submit: "continue after restart",
		Source: "test",
	})
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	first.Close()

	reopenedService, err := session.NewService("local", session.NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := reopenedService.Open(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	second := New(Options{
		SessionService:   reopenedService,
		SessionRuntime:   binding.Runtime(),
		ExclusiveSession: true,
		Sink:             event.Discard,
	})
	if err := binding.Release(t.Context()); err != nil {
		second.Close()
		t.Fatal(err)
	}
	defer second.Close()

	snapshot := second.InboxSnapshot()
	if len(snapshot.Items) != 1 || snapshot.Items[0].ID != receipt.ItemID {
		t.Fatalf("reopened inbox = %+v", snapshot)
	}
	_, envelope, err := second.ReadInboxItem(receipt.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.SubmitText != "continue after restart" {
		t.Fatalf("reopened prompt = %q", envelope.SubmitText)
	}
}
