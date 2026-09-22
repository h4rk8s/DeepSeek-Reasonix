package cli

import (
	"bytes"
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
