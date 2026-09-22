package cli

import (
	"bytes"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"strings"
	"testing"
	"time"
)

func TestStartupProgressLineShowsStageAndElapsedTime(t *testing.T) {
	got := startupProgressLine("restoring session", 12*time.Second+900*time.Millisecond)
	if !strings.Contains(got, "restoring session · 12s") {
		t.Fatalf("startup progress = %q", got)
	}
	if !strings.HasPrefix(got, "\r\x1b[2K") {
		t.Fatalf("startup progress does not replace one terminal line: %q", got)
	}
}

func TestStartupProgressCoversInitialDisplayHistoryLoad(t *testing.T) {
	var out bytes.Buffer
	diagnostics := &tuiDiagnostics{}
	diagnostics.StartStartupProgress(&out, true, "loading history")
	ctrl := newOwnedTestController(t, control.Options{})
	_ = newChatTUIWithStartupProgress(ctrl, "", make(chan event.Event, 1), 80, diagnostics)
	if diagnostics.startupProgress != nil {
		t.Fatal("display history returned without releasing startup progress")
	}
	if !strings.HasSuffix(out.String(), "\r\x1b[2K") {
		t.Fatalf("display history did not clear startup progress: %q", out.String())
	}
}

func TestStartupProgressStopsAndClearsTemporaryLine(t *testing.T) {
	var out bytes.Buffer
	p := newStartupProgress(&out, true, "loading history")
	p.Stop()
	got := out.String()
	if !strings.Contains(got, "loading history · 0s") {
		t.Fatalf("startup progress did not render immediately: %q", got)
	}
	if !strings.HasSuffix(got, "\r\x1b[2K") {
		t.Fatalf("startup progress did not clear its terminal line: %q", got)
	}
	// Stop is deliberately idempotent because every error path also defers it.
	p.Stop()
}
