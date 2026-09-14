package control

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/session"
)

func TestCanonicalBackgroundJobsSurviveControllerAndServiceRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions-v4")
	service, err := session.NewService("local", session.NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "durable-jobs"})
	if err != nil {
		t.Fatal(err)
	}
	ref := runtime.Ref()
	firstJobs := jobs.NewManager(event.Discard)
	first := New(Options{
		SessionService:   service,
		SessionRuntime:   runtime,
		ExclusiveSession: true,
		Jobs:             firstJobs,
		Sink:             event.Discard,
	})
	job := firstJobs.StartForSession(first.SessionID(), "bash", "durable", func(_ context.Context, out io.Writer) (string, error) {
		_, err := io.WriteString(out, "survives restart")
		return "", err
	})
	results := firstJobs.WaitForSession(t.Context(), ref.SessionID, []string{job.ID}, 5)
	if len(results) != 1 || results[0].Status != jobs.Done {
		first.Close()
		t.Fatalf("first job results = %+v", results)
	}
	dir, err := service.JobsDirectory(ref)
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "*.json")); err != nil || len(matches) != 1 {
		first.Close()
		t.Fatalf("canonical job metadata = %v, err = %v", matches, err)
	}
	first.Close()
	if err := service.CloseAll(t.Context()); err != nil {
		t.Fatal(err)
	}

	reopenedService, err := session.NewService("local", session.NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedService.Shutdown(context.Background()) })
	binding, err := reopenedService.Open(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	secondJobs := jobs.NewManager(event.Discard)
	second := New(Options{
		SessionService:   reopenedService,
		SessionRuntime:   binding.Runtime(),
		ExclusiveSession: true,
		Jobs:             secondJobs,
		Sink:             event.Discard,
	})
	if err := binding.Release(t.Context()); err != nil {
		second.Close()
		t.Fatal(err)
	}
	defer second.Close()

	text, status, ok := secondJobs.OutputForSession(ref.SessionID, job.ID)
	if !ok || status != jobs.Done || text != "survives restart" {
		t.Fatalf("reopened job = ok:%v status:%q text:%q", ok, status, text)
	}
}
