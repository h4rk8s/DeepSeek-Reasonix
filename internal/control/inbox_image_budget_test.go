package control

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/sessioninbox"
)

func TestInboxAutoCompressesMultipleFrozenImagesToSingleItemLimit(t *testing.T) {
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
	c := New(Options{SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"), WorkspaceRoot: workspace, Sink: event.Discard})
	rec, err := c.EnqueueInbox(InboxRequest{Submit: "inspect " + strings.Join(refs, " ")})
	if err != nil {
		t.Fatalf("enqueue multi-image follow-up: %v", err)
	}
	meta, env, err := c.ReadInboxItem(rec.ItemID)
	if err != nil || len(env.FrozenImages) != 3 {
		t.Fatalf("stored frozen images = %d, err = %v", len(env.FrozenImages), err)
	}
	if meta.ByteSize > sessioninbox.DefaultMaxItemBytes {
		t.Fatalf("stored item = %d bytes, limit = %d", meta.ByteSize, sessioninbox.DefaultMaxItemBytes)
	}
	for i, value := range env.FrozenImages {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "data:image/jpeg;base64,"))
		cfg, _, decodeErr := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil || decodeErr != nil || !strings.HasPrefix(value, "data:image/jpeg;base64,") || cfg.Width < 768 || cfg.Height < 512 {
			t.Errorf("queue image %d is not a readable JPEG snapshot: %dx%d, base64=%v decode=%v", i, cfg.Width, cfg.Height, err, decodeErr)
		}
		got, readErr := os.ReadFile(filepath.Join(workspace, strings.TrimPrefix(refs[i], "@")))
		if readErr != nil || !bytes.Equal(got, originals[i]) {
			t.Errorf("source image %d changed: read=%v", i, readErr)
		}
	}
}
