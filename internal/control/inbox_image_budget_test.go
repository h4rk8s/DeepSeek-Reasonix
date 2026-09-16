package control

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/sessioninbox"
)

func TestInboxStoresMultipleImagesAsExternalAttachmentsWithinLimit(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	refs, originals := make([]string, 3), make([][]byte, 3)
	for i := range refs {
		name := fmt.Sprintf("shot-%d.png", i+1)
		originals[i] = makeNoisyTestPNG(t, 900, 600, uint32(i+1))
		if err := os.WriteFile(filepath.Join(workspace, name), originals[i], 0o600); err != nil {
			t.Fatal(err)
		}
		refs[i] = "@" + name
	}
	c := newOwnedTestController(t, Options{SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"), WorkspaceRoot: workspace, Sink: event.Discard})
	rec, err := c.EnqueueInbox(InboxRequest{Submit: "inspect " + strings.Join(refs, " ")})
	if err != nil {
		t.Fatalf("enqueue multi-image follow-up: %v", err)
	}
	meta, env, err := c.ReadInboxItem(rec.ItemID)
	if err != nil || len(env.ImageInputs) != 3 {
		t.Fatalf("stored image inputs = %d, err = %v", len(env.ImageInputs), err)
	}
	if meta.ByteSize > sessioninbox.DefaultMaxItemBytes {
		t.Fatalf("stored item = %d bytes, limit = %d", meta.ByteSize, sessioninbox.DefaultMaxItemBytes)
	}
	if len(env.FrozenImages) != 0 {
		t.Fatalf("current attachments must not be stored as inline data URLs: %d", len(env.FrozenImages))
	}
	for i := range env.ImageInputs {
		got, readErr := os.ReadFile(filepath.Join(workspace, strings.TrimPrefix(refs[i], "@")))
		if readErr != nil || !bytes.Equal(got, originals[i]) {
			t.Errorf("source image %d changed: read=%v", i, readErr)
		}
	}
}
