package cli

import (
	"context"
	"path/filepath"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/session"
)

func newOwnedTestController(t testing.TB, options control.Options) *control.Controller {
	t.Helper()
	controller := control.New(options)
	t.Cleanup(func() {
		controller.Close()
		// Close requests teardown; Closed joins the final persistence work and
		// releases stores before t.TempDir removes their files (notably Windows).
		<-controller.Closed()
	})
	return controller
}

func newCanonicalTitleTestController(t testing.TB, workspaceRoot, title string) *control.Controller {
	t.Helper()
	root := t.TempDir()
	service, err := session.NewService("local", session.NewFilesystemPersistence(filepath.Join(root, "sessions-v4")))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := service.Create(context.Background(), session.CreateOptions{SessionID: "title-test"})
	if err != nil {
		t.Fatal(err)
	}
	if title != "" {
		if err := service.SetTitle(context.Background(), runtime.Ref(), title); err != nil {
			t.Fatal(err)
		}
	}
	controller := control.New(control.Options{
		SessionDir:       filepath.Join(root, "sessions"),
		SessionService:   service,
		SessionRuntime:   runtime,
		ExclusiveSession: true,
		WorkspaceRoot:    workspaceRoot,
	})
	t.Cleanup(func() {
		controller.Close()
		_ = service.Close(context.Background(), runtime.Ref())
	})
	return controller
}
